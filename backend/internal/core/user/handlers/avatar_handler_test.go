package handlers

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// fakeAvatarStore is a minimal iface.ObjectStore for the avatar handler
// tests. Exists is driven by the `present` set.
type fakeAvatarStore struct {
	present map[string]bool
}

func (f *fakeAvatarStore) PresignPut(_ context.Context, key, _ string, _ time.Duration) (*iface.PresignedPut, error) {
	return &iface.PresignedPut{URL: "http://s3/" + key, Key: key}, nil
}
func (f *fakeAvatarStore) Put(context.Context, string, string, io.Reader) error { return nil }
func (f *fakeAvatarStore) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", nil
}
func (f *fakeAvatarStore) Delete(context.Context, string) error { return nil }
func (f *fakeAvatarStore) Exists(_ context.Context, k string) (bool, error) {
	return f.present[k], nil
}

func avatarCtx(userUUID string) context.Context {
	return context.WithValue(context.Background(), ctxauth.KeyUserUUID, userUUID)
}

func presignReq(ct string, size int64) *presignUploadRequest {
	r := &presignUploadRequest{}
	r.Body.ContentType = ct
	r.Body.SizeBytes = size
	return r
}

// TestAvatarPresign_ViaUploadController locks the refactored presign path:
// the mime allowlist, the 2 MiB cap, the distinct non-positive-size 400,
// and the avatars/<tier>/<userUUID>/<uuidv7>.<ext> key shape — all now
// produced through the shared blob.UploadController.
func TestAvatarPresign_ViaUploadController(t *testing.T) {
	h := NewAvatarHandler(nil, &fakeAvatarStore{}, "operator")
	ctx := avatarCtx("u1")

	_, err := h.PresignAvatarUpload(ctx, presignReq("image/gif", 10))
	assertStatus(t, err, http.StatusBadRequest) // avatar_invalid_content_type

	_, err = h.PresignAvatarUpload(ctx, presignReq("image/png", 3*1024*1024))
	assertStatus(t, err, http.StatusRequestEntityTooLarge) // avatar_too_large

	_, err = h.PresignAvatarUpload(ctx, presignReq("image/png", 0))
	assertStatus(t, err, http.StatusBadRequest) // avatar_invalid_size

	out, err := h.PresignAvatarUpload(ctx, presignReq("image/png", 1000))
	if err != nil {
		t.Fatalf("happy presign: %v", err)
	}
	if m, _ := regexp.MatchString(`^avatars/operator/u1/[0-9a-fA-F-]+\.png$`, out.Body.Key); !m {
		t.Errorf("key shape = %q, want avatars/operator/u1/<uuid>.png", out.Body.Key)
	}
}

// TestAvatarCommit_OwnershipAndExistence locks the refactored commit path's
// two rejection branches (both short-circuit before OnCommit, so no
// UserService is needed): a key outside the caller's prefix → 400, and a
// well-scoped key whose object never landed → 404.
func TestAvatarCommit_OwnershipAndExistence(t *testing.T) {
	h := NewAvatarHandler(nil, &fakeAvatarStore{present: map[string]bool{}}, "operator")
	ctx := avatarCtx("u1")
	mk := func(k string) *commitAvatarRequest { r := &commitAvatarRequest{}; r.Body.Key = k; return r }

	_, err := h.CommitAvatarUpload(ctx, mk("avatars/operator/OTHER/x.png"))
	assertStatus(t, err, http.StatusBadRequest) // avatar_key_mismatch

	_, err = h.CommitAvatarUpload(ctx, mk("avatars/operator/u1/x.png"))
	assertStatus(t, err, http.StatusNotFound) // avatar_blob_missing
}

// TestAvatarStorageUnavailable: a nil store leaves upload nil, so the
// upload endpoints degrade to 503 (source-switch stays unaffected).
func TestAvatarStorageUnavailable(t *testing.T) {
	h := NewAvatarHandler(nil, nil, "operator")
	_, err := h.PresignAvatarUpload(avatarCtx("u1"), presignReq("image/png", 10))
	assertStatus(t, err, http.StatusServiceUnavailable)
}
