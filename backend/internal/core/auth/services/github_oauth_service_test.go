package services

import (
	"context"
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
	cases := map[string]githubRoundTripper{
		"no verified address":   {profile: githubProfile, emails: `[{"email":"x@example.com","primary":true,"verified":false}]`},
		"emails endpoint 401":   {profile: githubProfile, emails: `{}`, status: map[string]int{"/user/emails": 401}},
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
