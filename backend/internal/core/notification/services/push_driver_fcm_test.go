package services

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fakeSAJSON(t *testing.T, tokenURI, projectID string) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})
	b, _ := json.Marshal(map[string]string{
		"type": "service_account", "client_email": "svc@example.iam.gserviceaccount.com",
		"private_key": string(pemKey), "token_uri": tokenURI, "project_id": projectID, "private_key_id": "k1",
	})
	return b
}

func tokenServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-123","token_type":"Bearer","expires_in":3600}`))
	}))
	t.Cleanup(s.Close)
	return s
}

func newFCMForTest(t *testing.T, h http.HandlerFunc) (PushDriver, *httptest.Server) {
	t.Helper()
	ts := tokenServer(t)
	fs := httptest.NewServer(h)
	t.Cleanup(fs.Close)
	d, err := NewFCMPushDriver(fakeSAJSON(t, ts.URL, "proj-json"), "", FCMOptions{BaseURL: fs.URL})
	if err != nil {
		t.Fatal(err)
	}
	return d, fs
}

func TestFCM_SuccessRequestShape(t *testing.T) {
	var mu sync.Mutex
	var auth, path string
	var body map[string]any
	d, _ := newFCMForTest(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"name":"projects/p/messages/1"}`))
	})
	if d.Name() != "fcm" {
		t.Fatal(d.Name())
	}
	out := d.Send(context.Background(), []PushMessage{{Token: "tok-1", Title: "T", Body: "B",
		Data: map[string]string{"a": "b"}, CollapseKey: "ck", Priority: "high", TTL: 90 * time.Second}})
	if len(out) != 1 || !out[0].Delivered {
		t.Fatalf("%+v", out)
	}
	if auth != "Bearer at-123" || path != "/v1/projects/proj-json/messages:send" {
		t.Fatalf("auth=%q path=%q", auth, path)
	}
	m := body["message"].(map[string]any)
	if m["token"] != "tok-1" || m["notification"].(map[string]any)["title"] != "T" {
		t.Fatalf("%v", m)
	}
	and := m["android"].(map[string]any)
	if and["priority"] != "HIGH" || and["collapse_key"] != "ck" || and["ttl"] != "90s" {
		t.Fatalf("android %v", and)
	}
	if and["notification"].(map[string]any)["tag"] != "ck" {
		t.Fatalf("tag %v", and)
	}
	hd := m["apns"].(map[string]any)["headers"].(map[string]any)
	if hd["apns-priority"] != "10" || hd["apns-collapse-id"] != "ck" || hd["apns-expiration"] == "" {
		t.Fatalf("apns %v", hd)
	}
}

func TestFCM_ProjectIDConfigWins(t *testing.T) {
	ts := tokenServer(t)
	var path string
	fs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { path = r.URL.Path; _, _ = w.Write([]byte(`{}`)) }))
	defer fs.Close()
	d, err := NewFCMPushDriver(fakeSAJSON(t, ts.URL, "proj-json"), "proj-cfg", FCMOptions{BaseURL: fs.URL})
	if err != nil {
		t.Fatal(err)
	}
	d.Send(context.Background(), []PushMessage{{Token: "t"}})
	if path != "/v1/projects/proj-cfg/messages:send" {
		t.Fatal(path)
	}
}

func TestFCM_ErrorMapping(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		permanent bool
		reason    string
	}{
		{"unregistered", 404, `{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`, true, "fcm_unregistered"},
		{"invalid token", 400, `{"error":{"status":"INVALID_ARGUMENT","message":"The registration token is not a valid FCM registration token","details":[{"errorCode":"INVALID_ARGUMENT"}]}}`, true, "fcm_invalid"},
		{"bad payload", 400, `{"error":{"status":"INVALID_ARGUMENT","message":"Invalid value at 'message.data'"}}`, false, "fcm_bad_request"},
		{"bare 404", 404, `{"error":{"code":404,"status":"NOT_FOUND","message":"Requested entity was not found."}}`, false, "fcm_not_found"},
		{"404 unregistered", 404, `{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`, true, "fcm_unregistered"},
		{"400 unregistered", 400, `{"error":{"status":"INVALID_ARGUMENT","message":"bad","details":[{"errorCode":"UNREGISTERED"}]}}`, true, "fcm_unregistered"},
		{"bare 400", 400, `bad`, false, "fcm_bad_request"},
		{"429", 429, `{"error":{"status":"RESOURCE_EXHAUSTED"}}`, false, "transient"},
		{"503", 503, `unavailable`, false, "transient"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, _ := newFCMForTest(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			})
			out := d.Send(context.Background(), []PushMessage{{Token: "tok-secret-0123456789"}})
			o := out[0]
			if o.Delivered || o.Permanent != c.permanent || o.Reason != c.reason {
				t.Fatalf("%+v", o)
			}
			if o.Err == nil || len(o.Err.Error()) > 512 || strings.Contains(o.Err.Error(), "tok-secret-0123456789") {
				t.Fatalf("err = %v", o.Err)
			}
		})
	}
}

func TestFCM_TokenEndpointDown(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(strings.Repeat("x", 5000)))
	}))
	defer ts.Close()
	called := atomic.Int32{}
	fs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called.Add(1) }))
	defer fs.Close()
	d, err := NewFCMPushDriver(fakeSAJSON(t, ts.URL, "p"), "", FCMOptions{BaseURL: fs.URL})
	if err != nil {
		t.Fatal(err)
	}
	out := d.Send(context.Background(), []PushMessage{{Token: "a"}, {Token: "b"}})
	for _, o := range out {
		if o.Delivered || o.Permanent || o.Reason != "transient" || o.Err == nil || len(o.Err.Error()) > 512 {
			t.Fatalf("%+v", o)
		}
		if strings.Contains(o.Err.Error(), "PRIVATE KEY") {
			t.Fatal("leaks key")
		}
	}
	if called.Load() != 0 {
		t.Fatal("FCM called without token")
	}
}

func TestFCM_MalformedJSON(t *testing.T) {
	for _, in := range []string{"not json", `{"type":"service_account"}`, ``} {
		if _, err := NewFCMPushDriver([]byte(in), "p", FCMOptions{}); err == nil {
			t.Errorf("want error for %q", in)
		}
	}
}

func TestFCM_ConcurrencyBounded(t *testing.T) {
	var cur, peak atomic.Int32
	d, _ := newFCMForTest(t, func(w http.ResponseWriter, r *http.Request) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		cur.Add(-1)
		_, _ = w.Write([]byte(`{}`))
	})
	msgs := make([]PushMessage, 40)
	for i := range msgs {
		msgs[i] = PushMessage{Token: "t"}
	}
	out := d.Send(context.Background(), msgs)
	for _, o := range out {
		if !o.Delivered {
			t.Fatalf("%+v", o)
		}
	}
	if peak.Load() > 8 || peak.Load() < 2 {
		t.Fatalf("peak = %d", peak.Load())
	}
}
