package services

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// githubRoundTripper answers api.github.com by path so GetUserInfo can be
// exercised without the network. The service hard-codes its URLs; the
// http.Client transport is the seam.
type githubRoundTripper struct {
	profile string
	emails  string
	status  map[string]int
}

func (rt githubRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	body, code := "", http.StatusOK
	switch r.URL.Path {
	case "/user":
		body = rt.profile
	case "/user/emails":
		body = rt.emails
	default:
		code = http.StatusNotFound
	}
	if c, ok := rt.status[r.URL.Path]; ok {
		code = c
	}
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
}

func githubService(rt http.RoundTripper) *githubOAuthService {
	return &githubOAuthService{config: &OAuthProviderConfig{ClientID: "cid"}, httpClient: &http.Client{Transport: rt}}
}

const githubProfile = `{"id": 42, "login": "octo", "name": "Octo", "email": "public-profile@example.com", "avatar_url": "https://a"}`

func TestGitHubGetUserInfo_PrimaryVerifiedFromEmailsEndpoint(t *testing.T) {
	svc := githubService(githubRoundTripper{profile: githubProfile,
		emails: `[{"email":"other@example.com","primary":false,"verified":true},{"email":"primary@example.com","primary":true,"verified":true}]`})
	info, err := svc.GetUserInfo(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if info.Email != "primary@example.com" || !info.EmailVerified {
		t.Fatalf("got %q verified=%v; the primary verified address wins over the public profile", info.Email, info.EmailVerified)
	}
}

func TestGitHubGetUserInfo_FallsBackToAnyVerified(t *testing.T) {
	svc := githubService(githubRoundTripper{profile: githubProfile,
		emails: `[{"email":"unverified-primary@example.com","primary":true,"verified":false},{"email":"second@example.com","primary":false,"verified":true}]`})
	info, err := svc.GetUserInfo(context.Background(), "tok")
	if err != nil || info.Email != "second@example.com" || !info.EmailVerified {
		t.Fatalf("got %+v err=%v; a non-primary verified address is still one GitHub verified", info, err)
	}
}

func TestGitHubGetUserInfo_PublicProfileEmailIsNeverVerifiedByAssumption(t *testing.T) {
	// A 200 that carries no verified address is an ANSWER, not a failure: the
	// public-profile email survives as an UNVERIFIED fallback and the callback
	// refuses to auto-link or sign up with it. A failing endpoint is a
	// different thing entirely — see the test below.
	cases := map[string]githubRoundTripper{
		"no verified address":   {profile: githubProfile, emails: `[{"email":"x@example.com","primary":true,"verified":false}]`},
		"emails endpoint empty": {profile: githubProfile, emails: `[]`},
	}
	for name, rt := range cases {
		t.Run(name, func(t *testing.T) {
			info, err := githubService(rt).GetUserInfo(context.Background(), "tok")
			if err != nil {
				t.Fatal(err)
			}
			if info.Email != "public-profile@example.com" || info.EmailVerified {
				t.Fatalf("got %q verified=%v; the profile email survives only as an UNVERIFIED fallback", info.Email, info.EmailVerified)
			}
		})
	}
}

func TestGitHubGetUserInfo_EmailsEndpointFailureIsAProviderError(t *testing.T) {
	// A transient /user/emails failure must not masquerade as "you have no
	// verified address": collapsing it into the unverified fallback makes the
	// callback answer a permanent-sounding ErrOAuthEmailUnverified for what is
	// an upstream outage. GitHub answers 401 on a revoked token, 403/429 on
	// rate limits, and a truncated or non-JSON body is the same class of
	// problem. Each is reported as a provider error for operation
	// "user_emails", carrying the HTTP status where there is one, and its text
	// leaks neither the access token nor any address.
	for name, tc := range map[string]struct {
		rt         githubRoundTripper
		wantStatus int
	}{
		"401 unauthorized": {githubRoundTripper{profile: githubProfile, emails: `{}`, status: map[string]int{"/user/emails": 401}}, 401},
		"429 rate limited": {githubRoundTripper{profile: githubProfile, emails: `{}`, status: map[string]int{"/user/emails": 429}}, 429},
		"500 server error": {githubRoundTripper{profile: githubProfile, emails: `{}`, status: map[string]int{"/user/emails": 500}}, 500},
		"malformed body":   {githubRoundTripper{profile: githubProfile, emails: `not json`}, 0},
	} {
		t.Run(name, func(t *testing.T) {
			info, err := githubService(tc.rt).GetUserInfo(context.Background(), "tok")
			if err == nil {
				t.Fatalf("got %+v, want an error: an unreachable /user/emails is an outage, not an unverified address", info)
			}
			var provErr *ProviderError
			if !errors.As(err, &provErr) || provErr.Operation != "user_emails" {
				t.Fatalf("err = %v, want a *ProviderError for operation user_emails", err)
			}
			if provErr.StatusCode != tc.wantStatus {
				t.Fatalf("StatusCode = %d, want %d", provErr.StatusCode, tc.wantStatus)
			}
			if strings.Contains(err.Error(), "tok") || strings.Contains(err.Error(), "@example.com") {
				t.Fatalf("error text leaks the access token or an address: %v", err)
			}
		})
	}
}
