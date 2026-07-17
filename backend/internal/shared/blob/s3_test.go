package blob

import (
	"context"
	"strings"
	"testing"
	"time"
)

// newTestStore builds an s3Store with EnsureBucket:false so NewS3 constructs
// the clients without any network call — presigning is purely local signing.
func newTestStore(t *testing.T, endpoint, publicEndpoint string) *s3Store {
	t.Helper()
	store, err := NewS3(context.Background(), S3Config{
		Endpoint:       endpoint,
		PublicEndpoint: publicEndpoint,
		Region:         "us-east-1",
		Bucket:         "orkestra-crm-wallet",
		AccessKey:      "k",
		SecretKey:      "s",
		ForcePathStyle: true,
		EnsureBucket:   false,
	})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	return store.(*s3Store)
}

// When PublicEndpoint is set, presigned PUT/GET URLs must point at it (the
// browser-reachable host) — not at the internal Endpoint. This is the whole
// point of the split: the SPA can reach the presigned host even when Endpoint
// is a docker-internal address.
func TestPresignUsesPublicEndpointWhenSet(t *testing.T) {
	s := newTestStore(t, "http://rustfs:9000", "https://storage.example.com")

	put, err := s.PresignPut(context.Background(), "crm-wallet/t/e/x.png", "image/png", time.Minute)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	if !strings.HasPrefix(put.URL, "https://storage.example.com/") {
		t.Errorf("presigned PUT should target PublicEndpoint, got %q", put.URL)
	}
	if strings.Contains(put.URL, "rustfs:9000") {
		t.Errorf("presigned PUT must not leak the internal Endpoint, got %q", put.URL)
	}

	get, err := s.PresignGet(context.Background(), "crm-wallet/t/e/x.png", time.Minute)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	if !strings.HasPrefix(get, "https://storage.example.com/") {
		t.Errorf("presigned GET should target PublicEndpoint, got %q", get)
	}
}

// With no PublicEndpoint (the default single-endpoint deploy), presigned URLs
// fall back to Endpoint unchanged — the split must be opt-in.
func TestPresignFallsBackToEndpoint(t *testing.T) {
	s := newTestStore(t, "http://rustfs:9000", "")

	put, err := s.PresignPut(context.Background(), "crm-wallet/t/e/x.png", "image/png", time.Minute)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	if !strings.HasPrefix(put.URL, "http://rustfs:9000/") {
		t.Errorf("presigned PUT should fall back to Endpoint, got %q", put.URL)
	}
}
