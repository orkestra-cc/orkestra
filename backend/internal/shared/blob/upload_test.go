package blob

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

type uploadFakeStore struct {
	present map[string]bool
	deleted []string
}

func (f *uploadFakeStore) PresignPut(_ context.Context, key, _ string, _ time.Duration) (*iface.PresignedPut, error) {
	return &iface.PresignedPut{URL: "http://s3/" + key, Key: key}, nil
}
func (f *uploadFakeStore) Put(context.Context, string, string, io.Reader) error { return nil }
func (f *uploadFakeStore) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", nil
}
func (f *uploadFakeStore) Delete(_ context.Context, k string) error {
	f.deleted = append(f.deleted, k)
	return nil
}
func (f *uploadFakeStore) Exists(_ context.Context, k string) (bool, error) { return f.present[k], nil }

func newCtl(store iface.ObjectStore, onCommit func(context.Context, UploadScope, string) (string, error)) *UploadController {
	return NewUploadController(UploadConfig{
		Store:               store,
		AllowedContentTypes: map[string]string{"image/png": "png"},
		MaxBytes:            100,
		KeyBuilder: func(s UploadScope, ext string) string {
			return "photos/" + s.Tenant + "/" + s.Entity + "/x." + ext
		},
		OnCommit: onCommit,
	})
}

func TestPresignRejectsMimeAndSize(t *testing.T) {
	c := newCtl(&uploadFakeStore{}, nil)
	if _, err := c.Presign(context.Background(), UploadScope{Tenant: "t"}, "image/gif", 10); !errors.Is(err, ErrContentTypeNotAllowed) {
		t.Errorf("bad mime: want ErrContentTypeNotAllowed, got %v", err)
	}
	if _, err := c.Presign(context.Background(), UploadScope{Tenant: "t"}, "image/png", 1000); !errors.Is(err, ErrTooLarge) {
		t.Errorf("oversize: want ErrTooLarge, got %v", err)
	}
	if _, err := c.Presign(context.Background(), UploadScope{Tenant: "t"}, "image/png", 0); !errors.Is(err, ErrTooLarge) {
		t.Errorf("zero size: want ErrTooLarge, got %v", err)
	}
	got, err := c.Presign(context.Background(), UploadScope{Tenant: "t", Entity: "e"}, "image/png", 10)
	if err != nil || got.Key != "photos/t/e/x.png" {
		t.Errorf("happy presign: key=%q err=%v", got.Key, err)
	}
}

func TestCommitScopeAndExistence(t *testing.T) {
	store := &uploadFakeStore{present: map[string]bool{"photos/t/e/x.png": true}}
	var got string
	c := newCtl(store, func(_ context.Context, _ UploadScope, key string) (string, error) {
		got = key
		return "photos/t/e/old.png", nil
	})
	scope := UploadScope{Tenant: "t", Entity: "e"}
	// key outside the caller's scope prefix
	if err := c.Commit(context.Background(), scope, "photos/OTHER/e/x.png"); !errors.Is(err, ErrKeyOutOfScope) {
		t.Errorf("cross-scope: want ErrKeyOutOfScope, got %v", err)
	}
	// empty key
	if err := c.Commit(context.Background(), scope, ""); !errors.Is(err, ErrKeyOutOfScope) {
		t.Errorf("empty key: want ErrKeyOutOfScope, got %v", err)
	}
	// object absent
	if err := c.Commit(context.Background(), scope, "photos/t/e/missing.png"); !errors.Is(err, ErrUploadNotFound) {
		t.Errorf("absent: want ErrUploadNotFound, got %v", err)
	}
	// happy: OnCommit called, prior key deleted
	if err := c.Commit(context.Background(), scope, "photos/t/e/x.png"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if got != "photos/t/e/x.png" {
		t.Errorf("OnCommit key = %q", got)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "photos/t/e/old.png" {
		t.Errorf("prior key not deleted: %v", store.deleted)
	}
}

// downloadCapableStore implements iface.ObjectStore AND blob.ObjectDownloadPresigner
// so PresignGetDownloadURL takes the capability path; PresignGet returns a distinct
// value so the fallback path is observable.
type downloadCapableStore struct{ captured string }

func (*downloadCapableStore) PresignPut(context.Context, string, string, time.Duration) (*iface.PresignedPut, error) {
	return nil, nil
}
func (*downloadCapableStore) Put(context.Context, string, string, io.Reader) error { return nil }
func (*downloadCapableStore) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "plainGET", nil
}
func (s *downloadCapableStore) PresignGetDownload(_ context.Context, _, downloadAs string, _ time.Duration) (string, error) {
	s.captured = downloadAs
	return "downloadGET", nil
}
func (*downloadCapableStore) Delete(context.Context, string) error         { return nil }
func (*downloadCapableStore) Exists(context.Context, string) (bool, error) { return false, nil }

func TestPresignGetDownloadURL(t *testing.T) {
	// Capability present + non-blank name → uses PresignGetDownload with the name.
	dl := &downloadCapableStore{}
	c := newCtl(dl, nil)
	url, err := c.PresignGetDownloadURL(context.Background(), "k", "Atlante.pdf", time.Minute)
	if err != nil || url != "downloadGET" || dl.captured != "Atlante.pdf" {
		t.Fatalf("capability path: url=%q captured=%q err=%v", url, dl.captured, err)
	}
	// Blank name → falls back to plain PresignGet even when the store is capable.
	if url, _ := c.PresignGetDownloadURL(context.Background(), "k", "", time.Minute); url != "plainGET" {
		t.Fatalf("blank name should fall back to PresignGet, got %q", url)
	}
	// Store WITHOUT the capability → falls back to plain PresignGet (uploadFakeStore returns "").
	if url, _ := newCtl(&uploadFakeStore{}, nil).PresignGetDownloadURL(context.Background(), "k", "Atlante.pdf", time.Minute); url != "" {
		t.Fatalf("no-capability store should fall back to PresignGet, got %q", url)
	}
}
