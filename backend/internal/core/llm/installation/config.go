// Package installation parses the operator-declared inference endpoints
// (RETRIEVAL_INFERENCE_ENDPOINTS, ADR-0023 D4). It is a leaf package: it
// never contacts an endpoint and never imports the llm services.
package installation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	// EndpointsEnv holds the JSON array of installation endpoints.
	EndpointsEnv = "RETRIEVAL_INFERENCE_ENDPOINTS"
	// APIKeyEnv is the default bearer-key variable of an endpoint.
	APIKeyEnv = "RETRIEVAL_INFERENCE_API_KEY"

	maxPrefixBytes = 256
)

var (
	nameRe      = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	modelRe     = regexp.MustCompile(`^[A-Za-z0-9/][A-Za-z0-9._/:-]{0,199}$`)
	revisionRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)
	apiKeyEnvRe = regexp.MustCompile(`^RETRIEVAL_INFERENCE_[A-Z0-9_]{1,64}$`)
)

// Endpoint is one validated installation endpoint.
type Endpoint struct {
	Name           string
	URL            string // scheme://host[:port][/path], no trailing slash
	Kind           iface.InferenceKind
	Boundary       iface.InferenceBoundary
	Model          string
	Revision       string
	Pooling        string
	Normalize      bool
	Truncate       bool
	QueryPrefix    string
	DocumentPrefix string
	APIKey         string // resolved from the environment; never logged, never in a Profile
}

// Profile returns the declared profile with its ID. Dimensions and
// MaxInputTokens stay zero: they are measured at verification.
func (e Endpoint) Profile() iface.InferenceProfile {
	p := iface.InferenceProfile{
		Endpoint:       e.Name,
		Kind:           e.Kind,
		Boundary:       e.Boundary,
		ModelID:        e.Model,
		Revision:       e.Revision,
		Pooling:        e.Pooling,
		Normalize:      e.Normalize,
		Truncate:       e.Truncate,
		QueryPrefix:    e.QueryPrefix,
		DocumentPrefix: e.DocumentPrefix,
	}
	p.ID = ProfileID(p)
	return p
}

// LedgerModelUUID is the modelUuid of the usage rows of this endpoint.
func (e Endpoint) LedgerModelUUID() string { return "installation:" + e.Name }

type rawEndpoint struct {
	Name           string  `json:"name"`
	URL            string  `json:"url"`
	Kind           string  `json:"kind"`
	Boundary       string  `json:"boundary"`
	Model          string  `json:"model"`
	Revision       string  `json:"revision"`
	Pooling        string  `json:"pooling"`
	Normalize      bool    `json:"normalize"`
	Truncate       bool    `json:"truncate"`
	QueryPrefix    string  `json:"queryPrefix"`
	DocumentPrefix string  `json:"documentPrefix"`
	APIKeyEnv      *string `json:"apiKeyEnv"`
}

// fieldError names the offending field and why; it never carries a key value.
type fieldError struct{ field, reason string }

func (e *fieldError) Error() string { return e.field + ": " + e.reason }

func bad(field, reason string) *fieldError { return &fieldError{field, reason} }

// Parse reads the endpoint declaration. It never fails: an invalid endpoint is
// dropped and described in the returned warnings (one line each, never a key
// value). An empty or blank raw yields nil, nil.
func Parse(raw string, getenv func(string) string, productionLike bool) ([]Endpoint, []string) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	malformed := []string{EndpointsEnv + " is malformed JSON (want an array of objects); no installation endpoint loaded"}

	var elems []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &elems); err != nil {
		return nil, malformed
	}
	for _, el := range elems {
		if !bytes.HasPrefix(bytes.TrimSpace(el), []byte("{")) {
			return nil, malformed
		}
	}

	var warns []string
	var valid []Endpoint
	for i, el := range elems {
		ep, ferr := parseElement(el, getenv, productionLike)
		if ferr != nil {
			warns = append(warns, fmt.Sprintf("%s[%d]: %s: %s; endpoint ignored", EndpointsEnv, i, ferr.field, ferr.reason))
			continue
		}
		valid = append(valid, ep)
	}

	valid, warns = dropDuplicateNames(valid, warns)
	valid, warns = dropDuplicateProfiles(valid, warns)
	if len(valid) == 0 {
		valid = nil
	}
	return valid, warns
}

func dropDuplicateNames(eps []Endpoint, warns []string) ([]Endpoint, []string) {
	count := map[string]int{}
	for _, e := range eps {
		count[e.Name]++
	}
	warned := map[string]bool{}
	var out []Endpoint
	for _, e := range eps {
		if count[e.Name] > 1 {
			if !warned[e.Name] {
				warned[e.Name] = true
				warns = append(warns, fmt.Sprintf("%s: duplicate name %q; every endpoint with this name ignored", EndpointsEnv, e.Name))
			}
			continue
		}
		out = append(out, e)
	}
	return out, warns
}

func dropDuplicateProfiles(eps []Endpoint, warns []string) ([]Endpoint, []string) {
	byID := map[string][]string{}
	for _, e := range eps {
		id := e.Profile().ID
		byID[id] = append(byID[id], e.Name)
	}
	warned := map[string]bool{}
	var out []Endpoint
	for _, e := range eps {
		id := e.Profile().ID
		if names := byID[id]; len(names) > 1 {
			if !warned[id] {
				warned[id] = true
				warns = append(warns, fmt.Sprintf("%s: duplicate profile %s (endpoints %q, %q); all ignored", EndpointsEnv, id, names[0], names[1]))
			}
			continue
		}
		out = append(out, e)
	}
	return out, warns
}

func parseElement(el json.RawMessage, getenv func(string) string, productionLike bool) (Endpoint, *fieldError) {
	var r rawEndpoint
	dec := json.NewDecoder(bytes.NewReader(el))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		var ute *json.UnmarshalTypeError
		switch {
		case errors.As(err, &ute) && ute.Field != "":
			return Endpoint{}, bad(ute.Field, "wrong JSON type")
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			return Endpoint{}, bad(strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`), "unknown field")
		default:
			return Endpoint{}, bad("element", "not a valid endpoint object")
		}
	}

	if !nameRe.MatchString(r.Name) {
		return Endpoint{}, bad("name", "want ^[a-z][a-z0-9_-]{0,31}$")
	}
	cleanURL, scheme, uerr := cleanEndpointURL(r.URL)
	if uerr != nil {
		return Endpoint{}, uerr
	}
	if r.Boundary == string(iface.InferenceBoundaryHosted) && scheme == "http" && productionLike {
		return Endpoint{}, bad("url", "a hosted endpoint must use https in a production-like environment")
	}

	kind := iface.InferenceKind(r.Kind)
	switch kind {
	case iface.InferenceKindEmbed, iface.InferenceKindRerank, iface.InferenceKindChat:
	default:
		return Endpoint{}, bad("kind", "want embed, rerank or chat")
	}
	boundary := iface.InferenceBoundary(r.Boundary)
	if boundary != iface.InferenceBoundaryLocal && boundary != iface.InferenceBoundaryHosted {
		return Endpoint{}, bad("boundary", "required: local or hosted")
	}
	if !modelRe.MatchString(r.Model) {
		return Endpoint{}, bad("model", "want ^[A-Za-z0-9/][A-Za-z0-9._/:-]{0,199}$")
	}
	if !revisionRe.MatchString(r.Revision) {
		return Endpoint{}, bad("revision", "required: want ^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$")
	}
	if kind == iface.InferenceKindEmbed {
		if r.Pooling != "cls" && r.Pooling != "mean" && r.Pooling != "last_token" {
			return Endpoint{}, bad("pooling", "required for embed: cls, mean or last_token")
		}
	} else if r.Pooling != "" {
		return Endpoint{}, bad("pooling", "only valid for embed")
	}
	if kind != iface.InferenceKindEmbed && r.Normalize {
		return Endpoint{}, bad("normalize", "only valid for embed")
	}
	if kind == iface.InferenceKindChat && r.Truncate {
		return Endpoint{}, bad("truncate", "not valid for chat")
	}
	for _, p := range []struct{ field, v string }{{"queryPrefix", r.QueryPrefix}, {"documentPrefix", r.DocumentPrefix}} {
		if kind != iface.InferenceKindEmbed {
			if p.v != "" {
				return Endpoint{}, bad(p.field, "only valid for embed")
			}
			continue
		}
		if !utf8.ValidString(p.v) || len(p.v) > maxPrefixBytes {
			return Endpoint{}, bad(p.field, "want valid UTF-8 of at most 256 bytes")
		}
	}

	keyVar := APIKeyEnv
	if r.APIKeyEnv != nil {
		keyVar = *r.APIKeyEnv
		if !apiKeyEnvRe.MatchString(keyVar) {
			return Endpoint{}, bad("apiKeyEnv", "want a variable named RETRIEVAL_INFERENCE_*")
		}
		if getenv(keyVar) == "" {
			return Endpoint{}, bad("apiKeyEnv", "the named variable is unset or empty")
		}
	}

	return Endpoint{
		Name:           r.Name,
		URL:            cleanURL,
		Kind:           kind,
		Boundary:       boundary,
		Model:          r.Model,
		Revision:       r.Revision,
		Pooling:        r.Pooling,
		Normalize:      r.Normalize,
		Truncate:       r.Truncate,
		QueryPrefix:    r.QueryPrefix,
		DocumentPrefix: r.DocumentPrefix,
		APIKey:         getenv(keyVar),
	}, nil
}

func cleanEndpointURL(raw string) (clean, scheme string, ferr *fieldError) {
	if strings.ContainsAny(raw, "?#") {
		return "", "", bad("url", "query and fragment are not allowed")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", bad("url", "not a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", bad("url", "scheme must be http or https")
	}
	if u.Host == "" || u.Hostname() == "" {
		return "", "", bad("url", "host is required")
	}
	if u.User != nil {
		return "", "", bad("url", "userinfo is not allowed")
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.EscapedPath(), "/"), u.Scheme, nil
}
