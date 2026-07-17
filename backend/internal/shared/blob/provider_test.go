package blob

import "testing"

// newTestProvider builds a provider with EnsureBucket:false so NewS3
// constructs the client without any network call.
func newTestProvider() *Provider {
	return NewProvider(ProviderConfig{
		S3: S3Config{
			Endpoint: "http://localhost:9", Region: "us-east-1",
			AccessKey: "k", SecretKey: "s", ForcePathStyle: true, EnsureBucket: false,
		},
		BucketPrefix: "orkestra",
	})
}

func TestProviderBucketNameAndCaching(t *testing.T) {
	p := newTestProvider()
	a1, err := p.Bucket("avatars")
	if err != nil {
		t.Fatalf("Bucket(avatars): %v", err)
	}
	a2, _ := p.Bucket("avatars")
	if a1 != a2 {
		t.Error("same domain must return the cached instance")
	}
	if got := p.bucketName("avatars"); got != "orkestra-avatars" {
		t.Errorf("bucketName = %q, want orkestra-avatars", got)
	}
	if got := p.bucketName("crm-photos"); got != "orkestra-crm-photos" {
		t.Errorf("bucketName = %q, want orkestra-crm-photos", got)
	}
}

func TestProviderRejectsBadDomain(t *testing.T) {
	p := newTestProvider()
	for _, bad := range []string{"", "Avatars", "a/b", "under_score", "sp ace"} {
		if _, err := p.Bucket(bad); err == nil {
			t.Errorf("Bucket(%q) should be rejected", bad)
		}
	}
}

func TestProviderDefaultsBucketPrefix(t *testing.T) {
	p := NewProvider(ProviderConfig{S3: S3Config{AccessKey: "k", SecretKey: "s"}})
	if got := p.bucketName("avatars"); got != "orkestra-avatars" {
		t.Errorf("empty prefix should default to orkestra: got %q", got)
	}
}
