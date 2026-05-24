package blob

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orkestra-cc/orkestra-sdk/iface"
)

// fakeStore is the smallest blob.Store useful for testing
// ResolveAvatarURL — only PresignGet matters here.
type fakeStore struct {
	presignURL string
	presignErr error
}

func (f *fakeStore) PresignPut(context.Context, string, string, time.Duration) (*PresignedPut, error) {
	return nil, errors.New("not used")
}
func (f *fakeStore) PresignGet(_ context.Context, _ string, _ time.Duration) (string, error) {
	return f.presignURL, f.presignErr
}
func (f *fakeStore) Delete(context.Context, string) error         { return nil }
func (f *fakeStore) Exists(context.Context, string) (bool, error) { return true, nil }

func TestResolveAvatarURL(t *testing.T) {
	t.Parallel()

	user := func(source string, fields func(*iface.User)) *iface.User {
		u := &iface.User{UUID: "u-1", Avatar: "https://stale.example/img.png", AvatarSource: source}
		if fields != nil {
			fields(u)
		}
		return u
	}

	tests := []struct {
		name  string
		user  *iface.User
		store Store
		want  string
	}{
		{
			name:  "nil user returns empty",
			user:  nil,
			store: &fakeStore{presignURL: "x"},
			want:  "",
		},
		{
			name:  "initials clears avatar",
			user:  user(iface.AvatarSourceInitials, nil),
			store: &fakeStore{presignURL: "x"},
			want:  "",
		},
		{
			name: "uploaded uses presigned GET",
			user: user(iface.AvatarSourceUploaded, func(u *iface.User) {
				u.AvatarObjectKey = "avatars/operator/u-1/abc.png"
			}),
			store: &fakeStore{presignURL: "https://signed.example/abc.png?sig=x"},
			want:  "https://signed.example/abc.png?sig=x",
		},
		{
			name: "uploaded with nil store falls back to stored Avatar",
			user: user(iface.AvatarSourceUploaded, func(u *iface.User) {
				u.AvatarObjectKey = "avatars/operator/u-1/abc.png"
			}),
			store: nil,
			want:  "https://stale.example/img.png",
		},
		{
			name: "uploaded with presign error falls back to stored Avatar",
			user: user(iface.AvatarSourceUploaded, func(u *iface.User) {
				u.AvatarObjectKey = "avatars/operator/u-1/abc.png"
			}),
			store: &fakeStore{presignErr: errors.New("boom")},
			want:  "https://stale.example/img.png",
		},
		{
			name: "oauth google reads embedded picture",
			user: user(iface.AvatarSourceOAuthGoogle, func(u *iface.User) {
				u.OAuthLinks = []iface.OAuthLink{
					{Provider: iface.OAuthProviderGoogle, IsActive: true, OAuthData: map[string]interface{}{"picture": "https://g.example/me.jpg"}},
				}
			}),
			store: nil,
			want:  "https://g.example/me.jpg",
		},
		{
			name: "oauth provider not linked returns empty",
			user: user(iface.AvatarSourceOAuthApple, func(u *iface.User) {
				u.OAuthLinks = []iface.OAuthLink{
					{Provider: iface.OAuthProviderGoogle, IsActive: true, OAuthData: map[string]interface{}{"picture": "https://g.example/me.jpg"}},
				}
			}),
			store: nil,
			want:  "",
		},
		{
			name: "oauth inactive link is skipped",
			user: user(iface.AvatarSourceOAuthGoogle, func(u *iface.User) {
				u.OAuthLinks = []iface.OAuthLink{
					{Provider: iface.OAuthProviderGoogle, IsActive: false, OAuthData: map[string]interface{}{"picture": "https://g.example/me.jpg"}},
				}
			}),
			store: nil,
			want:  "",
		},
		{
			name:  "legacy user with no source preserves stored Avatar",
			user:  user("", nil),
			store: nil,
			want:  "https://stale.example/img.png",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveAvatarURL(context.Background(), tc.user, tc.store); got != tc.want {
				t.Fatalf("ResolveAvatarURL = %q, want %q", got, tc.want)
			}
		})
	}
}
