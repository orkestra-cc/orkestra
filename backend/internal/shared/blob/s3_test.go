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

	"github.com/redis/go-redis/v9"

	"github.com/orkestra/backend/pkg/sdk/iface"
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

// newTestCachedStore constructs a CachedStore wrapping newTestStore.
// It passes a non-nil redis.Client (never dialed) so NewCached actually
// builds a *CachedStore instead of short-circuiting to the inner store.
func newTestCachedStore(t *testing.T) Store {
	t.Helper()
	innerStore := newTestStore(t, "http://rustfs:9000", "")
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	return NewCached(innerStore, rdb, CachedConfig{})
}

// When PublicEndpoint is set, presigned PUT/GET URLs must point at it (the
// browser-reachable host) — not at the internal Endpoint. This is the whole
// point of the split: the SPA can reach the presigned host even when Endpoint
// is a docker-internal address.
func TestPresignUsesPublicEndpointWhenSet(t *testing.T) {
	s := newTestStore(t, "http://rustfs:9000", "https://storage.example.com")

	put, err := s.PresignPut(context.Background(), "crm-wallet/t/e/x.png", "image/png", 1024, time.Minute)
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

	put, err := s.PresignPut(context.Background(), "crm-wallet/t/e/x.png", "image/png", 1024, time.Minute)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	if !strings.HasPrefix(put.URL, "http://rustfs:9000/") {
		t.Errorf("presigned PUT should fall back to Endpoint, got %q", put.URL)
	}
}

// PresignPut must sign the exact content-length, not just host — otherwise a
// URL minted for a small file accepts a PUT of arbitrary size. Verified live
// against RustFS: a 64 KiB PUT on a URL coined for 1 KiB succeeded with 200
// before this fix (X-Amz-SignedHeaders=host only).
func TestPresignPutSignsContentLength(t *testing.T) {
	st := newTestStore(t, "http://rustfs:9000", "")
	out, err := st.PresignPut(context.Background(), "k/obj.pdf", "application/pdf", 1024, time.Minute)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	u, err := url.Parse(out.URL)
	if err != nil {
		t.Fatalf("URL non valido: %v", err)
	}
	signed := u.Query().Get("X-Amz-SignedHeaders")
	if !strings.Contains(signed, "content-length") {
		t.Fatalf("X-Amz-SignedHeaders = %q, deve contenere content-length: senza, l'URL consente una scrittura di dimensione arbitraria", signed)
	}
	if out.SizeBytes != 1024 {
		t.Fatalf("SizeBytes = %d, atteso 1024", out.SizeBytes)
	}
	if _, ok := out.Headers["Content-Length"]; ok {
		t.Fatal("Content-Length NON va in Headers: e' un header che uno script non puo' impostare e che il browser compila da se'")
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

func TestS3StoreImplementsObjectInspector(t *testing.T) {
	var st iface.ObjectStore = newTestStore(t, "http://rustfs:9000", "")
	if _, ok := st.(iface.ObjectInspector); !ok {
		t.Fatal("s3Store deve implementare iface.ObjectInspector: senza, il commit degrada a 503 e la verifica del tipo non e' possibile")
	}
}

func TestCachedStoreForwardsObjectInspector(t *testing.T) {
	var st iface.ObjectStore = newTestCachedStore(t) // costruisce CachedStore su newTestStore
	if _, ok := st.(iface.ObjectInspector); !ok {
		t.Fatal("CachedStore deve inoltrare ObjectInspector, altrimenti il commit smette di funzionare quando la cache e' attiva")
	}
}

// statRecorder captures HEAD and GET requests for behavioral testing.
type statRecorder struct {
	mu           sync.Mutex
	headRequests []headRequest
	getRanges    []rangeRequest
}

type headRequest struct {
	key    string
	length int64
	mime   string
}

type rangeRequest struct {
	key        string
	rangeValue string
}

func (r *statRecorder) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()

		key := req.URL.Path
		if strings.HasPrefix(key, "/") {
			key = key[1:]
		}

		switch req.Method {
		case http.MethodHead:
			// HEAD for Stat: return ContentLength and ContentType
			r.headRequests = append(r.headRequests, headRequest{
				key:    key,
				length: 1024,
				mime:   "application/octet-stream",
			})
			w.Header().Set("Content-Length", "1024")
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)

		case http.MethodGet:
			// GET for GetRange: capture the Range header and return partial content
			rng := req.Header.Get("Range")
			r.getRanges = append(r.getRanges, rangeRequest{
				key:        key,
				rangeValue: rng,
			})

			// If Range header is present, return 206 Partial Content
			if rng != "" {
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Range", "bytes 0-511/1024")
				w.WriteHeader(http.StatusPartialContent)
				// Return 512 bytes (range requested 0-511)
				w.Write(make([]byte, 512))
			} else {
				w.WriteHeader(http.StatusOK)
				w.Write(make([]byte, 1024))
			}
		}
	}
}

// TestStatReadsContentLengthAndType verifies Stat correctly maps
// ContentLength and ContentType from HeadObject.
func TestStatReadsContentLengthAndType(t *testing.T) {
	rec := &statRecorder{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	store, err := NewS3(context.Background(), S3Config{
		Endpoint:     srv.URL,
		Region:       "us-east-1",
		Bucket:       "test-bucket",
		AccessKey:    "k",
		SecretKey:    "s",
		ForcePathStyle: true,
		EnsureBucket: false,
	})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}

	insp := store.(iface.ObjectInspector)
	stat, err := insp.Stat(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if stat.SizeBytes != 1024 {
		t.Errorf("SizeBytes = %d, want 1024", stat.SizeBytes)
	}
	if stat.ContentType != "application/octet-stream" {
		t.Errorf("ContentType = %q, want %q", stat.ContentType, "application/octet-stream")
	}

	rec.mu.Lock()
	if len(rec.headRequests) != 1 {
		t.Errorf("HeadObject called %d times, want 1", len(rec.headRequests))
	}
	rec.mu.Unlock()
}

// TestGetRangeSendsCorrectHeader verifies GetRange sends the correct
// HTTP Range header in the format "bytes=offset-(offset+length-1)".
func TestGetRangeSendsCorrectHeader(t *testing.T) {
	rec := &statRecorder{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	store, err := NewS3(context.Background(), S3Config{
		Endpoint:     srv.URL,
		Region:       "us-east-1",
		Bucket:       "test-bucket",
		AccessKey:    "k",
		SecretKey:    "s",
		ForcePathStyle: true,
		EnsureBucket: false,
	})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}

	insp := store.(iface.ObjectInspector)
	body, err := insp.GetRange(context.Background(), "test-key", 0, 512)
	if err != nil {
		t.Fatalf("GetRange: %v", err)
	}
	defer body.Close()

	rec.mu.Lock()
	if len(rec.getRanges) != 1 {
		t.Errorf("GetObject called %d times, want 1", len(rec.getRanges))
	} else if rec.getRanges[0].rangeValue != "bytes=0-511" {
		t.Errorf("Range header = %q, want %q", rec.getRanges[0].rangeValue, "bytes=0-511")
	}
	rec.mu.Unlock()

	// Verify we got back the partial content (512 bytes)
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(data) != 512 {
		t.Errorf("GetRange returned %d bytes, want 512", len(data))
	}
}

// TestGetRangePastEndReturnsWhatExists verifies that when a range request
// runs past the end of an object, the server returns only what exists
// without an error.
func TestGetRangePastEndReturnsWhatExists(t *testing.T) {
	rec := &statRecorder{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	store, err := NewS3(context.Background(), S3Config{
		Endpoint:     srv.URL,
		Region:       "us-east-1",
		Bucket:       "test-bucket",
		AccessKey:    "k",
		SecretKey:    "s",
		ForcePathStyle: true,
		EnsureBucket: false,
	})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}

	insp := store.(iface.ObjectInspector)
	// Request bytes 600-1199 (600 bytes) from a 1024-byte object
	// S3 will return only bytes 600-1023 (424 bytes) without error
	body, err := insp.GetRange(context.Background(), "test-key", 600, 600)
	if err != nil {
		t.Fatalf("GetRange past end: %v", err)
	}
	defer body.Close()

	rec.mu.Lock()
	if len(rec.getRanges) != 1 {
		t.Errorf("GetObject called %d times, want 1", len(rec.getRanges))
	} else if rec.getRanges[0].rangeValue != "bytes=600-1199" {
		t.Errorf("Range header = %q, want %q", rec.getRanges[0].rangeValue, "bytes=600-1199")
	}
	rec.mu.Unlock()

	// The mock returns 512 bytes; a real S3 would return fewer
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(data) == 0 {
		t.Error("GetRange past end returned 0 bytes, expected what exists")
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
// SignatureDoesNotMatch. That is not hypothetical: it was live on this
// deployment, breaking both attachment downloads and the gated-document
// downloads that had been working before an SDK bump.
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
