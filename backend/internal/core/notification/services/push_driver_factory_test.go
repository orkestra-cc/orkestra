package services

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/module"
)

func TestPushDriverFactory_DefaultsToNoop(t *testing.T) {
	f := NewPushDriverFactory(func(ctx context.Context) (*module.ModuleConfig, error) { return nil, nil }, discardLogger(), FCMOptions{})
	d, err := f.PushDriver(context.Background())
	if err != nil || d.Name() != "noop" {
		t.Fatalf("%v %v", d, err)
	}
	f = NewPushDriverFactory(nil, nil, FCMOptions{})
	if d, err = f.PushDriver(context.Background()); err != nil || d.Name() != "noop" {
		t.Fatal(err)
	}
}

func TestPushDriverFactory_ReadErrorFailsClosed(t *testing.T) {
	f := NewPushDriverFactory(func(ctx context.Context) (*module.ModuleConfig, error) { return nil, errors.New("db down") }, discardLogger(), FCMOptions{})
	if _, err := f.PushDriver(context.Background()); err == nil {
		t.Fatal("want error")
	}
}

func TestPushDriverFactory_CachesFCMByHash(t *testing.T) {
	ts := tokenServer(t)
	sa := string(fakeSAJSON(t, ts.URL, "p1"))
	f := NewPushDriverFactory(nil, discardLogger(), FCMOptions{})
	a, err := f.fcm(sa, "")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := f.fcm(sa, "")
	if a != b {
		t.Fatal("not cached")
	}
	c, _ := f.fcm(sa, "other")
	if c == a {
		t.Fatal("project change must rebuild")
	}
	if _, err := f.fcm("garbage", ""); err == nil {
		t.Fatal("want error")
	}
}

func TestPushDriverFactory_FCMFromSnapshot(t *testing.T) {
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", testKeyHex)
	ts := tokenServer(t)
	doc := &module.ModuleConfig{
		ConfigSchema: []module.ConfigField{
			{Key: "push.provider", Type: module.FieldEnum, Options: []string{"noop", "fcm"}, Default: "noop"},
			{Key: "push.fcm.service_account_json", Type: module.FieldSecret},
			{Key: "push.fcm.project_id", Type: module.FieldString},
		},
		ActiveEnvironment: "production",
		Environments: map[string]module.EnvironmentConfig{"production": {
			ConfigValues:    map[string]string{"push.provider": "fcm"},
			EncryptedValues: map[string]string{"push.fcm.service_account_json": encryptForTest(t, string(fakeSAJSON(t, ts.URL, "p1")))},
		}},
	}
	calls := 0
	f := NewPushDriverFactory(func(context.Context) (*module.ModuleConfig, error) { calls++; return doc, nil }, discardLogger(), FCMOptions{})
	a, err := f.PushDriver(context.Background())
	if err != nil || a.Name() != "fcm" {
		t.Fatalf("%v %v", a, err)
	}
	b, _ := f.PushDriver(context.Background())
	if a != b || calls != 2 {
		t.Fatalf("cached=%v calls=%d", a == b, calls)
	}
}
