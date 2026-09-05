package blob

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
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

// --- Bucket CORS -----------------------------------------------------------
// A browser upload is a cross-origin PUT straight to the object store, so the
// bucket itself must permit the SPA's origin. Nothing else in the stack can
// supply that header, and without it the presign→PUT→commit flow dies at the
// preflight with no server-side trace (observed on RustFS: OPTIONS 200, no
// Access-Control-* headers, PUT blocked by the browser).

type corsRecorder struct {
	mu       sync.Mutex
	corsBody string
	corsPuts int
	status   int
}

func (r *corsRecorder) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPut && req.URL.Query().Has("cors") {
			body, _ := io.ReadAll(req.Body)
			r.mu.Lock()
			r.corsPuts++
			r.corsBody = string(body)
			status := r.status
			r.mu.Unlock()
			if status != 0 {
				w.WriteHeader(status)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		// HeadBucket and anything else: the bucket exists.
		w.WriteHeader(http.StatusOK)
	}
}

func newCORSTestStore(t *testing.T, rec *corsRecorder, origins []string) error {
	t.Helper()
	srv := httptest.NewServer(rec.handler())
	t.Cleanup(srv.Close)
	_, err := NewS3(context.Background(), S3Config{
		Endpoint:           srv.URL,
		Region:             "us-east-1",
		Bucket:             "orkestra-avatars",
		AccessKey:          "k",
		SecretKey:          "s",
		ForcePathStyle:     true,
		EnsureBucket:       true,
		CORSAllowedOrigins: origins,
	})
	return err
}

func TestEnsureBucketAppliesCORSForConfiguredOrigins(t *testing.T) {
	rec := &corsRecorder{}
	if err := newCORSTestStore(t, rec, []string{"https://console.example.com"}); err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.corsPuts != 1 {
		t.Fatalf("PutBucketCors calls = %d, want 1", rec.corsPuts)
	}
	for _, want := range []string{
		"<AllowedOrigin>https://console.example.com</AllowedOrigin>",
		"<AllowedMethod>PUT</AllowedMethod>",
		"<AllowedMethod>GET</AllowedMethod>",
	} {
		if !strings.Contains(rec.corsBody, want) {
			t.Fatalf("CORS body missing %q:\n%s", want, rec.corsBody)
		}
	}
	// Deletes are server-side only; the browser must never be granted one.
	if strings.Contains(rec.corsBody, "<AllowedMethod>DELETE</AllowedMethod>") {
		t.Fatalf("CORS policy grants DELETE to browsers:\n%s", rec.corsBody)
	}
}

func TestEnsureBucketSkipsCORSWhenNoOriginsConfigured(t *testing.T) {
	// The default for every existing deployment: no origins, no call. A
	// managed S3 whose IAM lacks s3:PutBucketCORS must not start failing.
	rec := &corsRecorder{}
	if err := newCORSTestStore(t, rec, nil); err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.corsPuts != 0 {
		t.Fatalf("PutBucketCors called %d times with no origins configured", rec.corsPuts)
	}
}

func TestEnsureBucketToleratesCORSRejection(t *testing.T) {
	// Storage that refuses the policy (no permission, or an implementation
	// without bucket CORS) must not take the whole store down with it:
	// uploads would break anyway, but reads and server-side ops still work.
	rec := &corsRecorder{status: http.StatusNotImplemented}
	if err := newCORSTestStore(t, rec, []string{"https://console.example.com"}); err != nil {
		t.Fatalf("a rejected CORS policy failed the store: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.corsPuts == 0 {
		t.Fatal("expected the policy to be attempted")
	}
}

// TestPresignGetDoesNotSignChecksumMode pins the reason the presigner is a
// separate client with response checksum validation turned off.
//
// aws-sdk-go-v2 defaults ResponseChecksumValidation to WhenSupported, which
// puts `x-amz-checksum-mode` into a presigned GET's X-Amz-SignedHeaders — and
// a signed header is one the recipient MUST send. The recipients here are a
// browser opening the URL and a plain fetch of it; neither sends it, so the
// signature never matches and every presigned download answers 403
// SignatureDoesNotMatch. That is not hypothetical: it was observed live on a
// real deployment, where every operator-facing download that had been working
// began answering 403 after an SDK bump, with no code change of our own.
//
// Asserting on the exact header list is deliberate. A test that only checked
// the URL parses, or that some signature exists, would have passed throughout
// the outage.
func TestPresignGetDoesNotSignChecksumMode(t *testing.T) {
	st := newTestStore(t, "http://rustfs:9000", "")

	for _, tc := range []struct {
		name string
		get  func() (string, error)
	}{
		{"PresignGet", func() (string, error) {
			return st.PresignGet(context.Background(), "k/obj.pdf", time.Minute)
		}},
		{"PresignGetDownload", func() (string, error) {
			return st.PresignGetDownload(context.Background(), "k/obj.pdf", "curriculum.pdf", time.Minute)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := tc.get()
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatalf("URL non valido: %v", err)
			}
			signed := u.Query().Get("X-Amz-SignedHeaders")
			if strings.Contains(signed, "checksum") {
				t.Fatalf("X-Amz-SignedHeaders = %q: un header che il destinatario non puo' inviare "+
					"rende ogni download firmato un 403", signed)
			}
			if signed != "host" {
				t.Fatalf("X-Amz-SignedHeaders = %q, atteso solo \"host\": ogni header in piu' "+
					"e' un header che il browser deve inviare e non invia", signed)
			}
		})
	}
}
