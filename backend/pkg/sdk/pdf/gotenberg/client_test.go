package gotenberg

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func pdfOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/pdf")
	_, _ = w.Write([]byte("%PDF-1.7\n...%%EOF"))
}

func TestRenderHTML_MultipartShapeAndAuth(t *testing.T) {
	var got struct {
		path, user, pass string
		files            map[string]string
		fields           map[string]string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.user, got.pass, _ = r.BasicAuth()
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		got.files, got.fields = map[string]string{}, map[string]string{}
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			b, _ := io.ReadAll(p)
			if p.FileName() != "" {
				got.files[p.FileName()] = string(b)
			} else {
				got.fields[p.FormName()] = string(b)
			}
		}
		pdfOK(w)
	}))
	defer srv.Close()
	c := New(Config{URL: srv.URL, Username: "u", Password: "p"})
	out, err := c.RenderHTML(context.Background(), iface.HTMLDocument{
		HTML:   "<html><body><img src=\"asset-1.png\"></body></html>",
		Assets: map[string][]byte{"asset-1.png": []byte("PNG")},
		Paper:  iface.PaperSpec{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "%PDF-") {
		t.Fatalf("out = %q", out)
	}
	if got.path != "/forms/chromium/convert/html" || got.user != "u" || got.pass != "p" {
		t.Fatalf("path/auth = %q %q %q", got.path, got.user, got.pass)
	}
	if got.files["index.html"] == "" || got.files["asset-1.png"] != "PNG" {
		t.Fatalf("files = %v", got.files)
	}
	for k, want := range map[string]string{"paperWidth": "8.27", "paperHeight": "11.69", "marginTop": "0.591",
		"printBackground": "true", "preferCssPageSize": "false"} {
		if got.fields[k] != want {
			t.Fatalf("field %s = %q, want %q", k, got.fields[k], want)
		}
	}
}

func TestRenderHTML_ErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		ctype  string
		want   error
	}{
		{"5xx", 503, "busy", "text/plain", iface.ErrPDFRendererUnavailable},
		{"4xx", 400, "bad", "text/plain", iface.ErrPDFRenderFailed},
		{"not pdf", 200, "<html>", "text/html", iface.ErrPDFRenderFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.ctype)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, err := New(Config{URL: srv.URL}).RenderHTML(context.Background(), iface.HTMLDocument{HTML: "<html></html>"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRenderHTML_SizeCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-" + strings.Repeat("x", 2048)))
	}))
	defer srv.Close()
	_, err := New(Config{URL: srv.URL, MaxBytes: 1024}).RenderHTML(context.Background(), iface.HTMLDocument{HTML: "<html></html>"})
	if !errors.Is(err, iface.ErrPDFTooLarge) {
		t.Fatalf("err = %v, want ErrPDFTooLarge", err)
	}
}

func TestRenderHTML_CallerDeadlineCoversSemaphoreWait(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-release
		pdfOK(w)
	}))
	defer srv.Close()
	defer close(release)
	c := New(Config{URL: srv.URL, MaxConcurrency: 1})
	go func() { _, _ = c.RenderHTML(context.Background(), iface.HTMLDocument{HTML: "<html></html>"}) }()
	for calls.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.RenderHTML(ctx, iface.HTMLDocument{HTML: "<html></html>"})
	if !errors.Is(err, iface.ErrPDFRendererUnavailable) {
		t.Fatalf("err = %v, want ErrPDFRendererUnavailable", err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("waited %v: semaphore ignored the caller deadline", time.Since(start))
	}
	if calls.Load() != 1 {
		t.Fatalf("second call reached the server while the slot was busy")
	}
}

func TestRenderHTML_ConnectionRefused(t *testing.T) {
	_, err := New(Config{URL: "http://127.0.0.1:1"}).RenderHTML(context.Background(), iface.HTMLDocument{HTML: "<html></html>"})
	if !errors.Is(err, iface.ErrPDFRendererUnavailable) {
		t.Fatalf("err = %v", err)
	}
}
