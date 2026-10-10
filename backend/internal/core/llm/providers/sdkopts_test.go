package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go"
	"google.golang.org/genai"
)

func TestOpenAIClientOptions_IgnoreProcessEnvironment(t *testing.T) {
	t.Setenv("OPENAI_ORG_ID", "org-from-env")
	t.Setenv("OPENAI_PROJECT_ID", "proj-from-env")
	t.Setenv("OPENAI_API_KEY", "sk-from-env")
	t.Setenv("OPENAI_BASE_URL", "http://env.invalid/")
	var hdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer srv.Close()
	hc := NewHTTPClient(TransportConfig{ProductionLike: false, Timeout: 2 * time.Second})
	c := openai.NewClient(OpenAIClientOptions(srv.URL+"/v1", "sk-explicit", hc)...)
	if _, err := c.Models.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hdr == nil {
		t.Fatal("request did not reach the configured base URL")
	}
	if got := hdr.Get("Authorization"); got != "Bearer sk-explicit" {
		t.Errorf("Authorization = %q", got)
	}
	if v := hdr.Get("OpenAI-Organization"); v != "" {
		t.Errorf("OpenAI-Organization leaked from env: %q", v)
	}
	if v := hdr.Get("OpenAI-Project"); v != "" {
		t.Errorf("OpenAI-Project leaked from env: %q", v)
	}
}

func TestAnthropicClientOptions_IgnoreProcessEnvironment(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-from-env")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "tok-from-env")
	t.Setenv("ANTHROPIC_BASE_URL", "http://env.invalid/")
	t.Setenv("ANTHROPIC_CUSTOM_HEADERS", "X-Leak: yes")
	t.Setenv("ANTHROPIC_PROFILE", "does-not-exist")
	var hdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"has_more":false}`))
	}))
	defer srv.Close()
	hc := NewHTTPClient(TransportConfig{ProductionLike: false, Timeout: 2 * time.Second})
	c := anthropic.NewClient(AnthropicClientOptions(srv.URL, "sk-ant-explicit", hc)...)
	if _, err := c.Models.List(context.Background(), anthropic.ModelListParams{}); err != nil {
		t.Fatal(err)
	}
	if hdr == nil {
		t.Fatal("request did not reach the configured base URL")
	}
	if got := hdr.Get("X-Api-Key"); got != "sk-ant-explicit" {
		t.Errorf("X-Api-Key = %q", got)
	}
	if hdr.Get("Authorization") != "" || hdr.Get("X-Leak") != "" {
		t.Errorf("env credentials/headers leaked: %v", hdr)
	}
}

func TestGeminiClientConfig_IgnoresProcessEnvironment(t *testing.T) {
	t.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "true")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "env-project")
	t.Setenv("GOOGLE_API_KEY", "key-from-env")
	t.Setenv("GOOGLE_GEMINI_BASE_URL", "http://env.invalid/")
	hc := NewHTTPClient(TransportConfig{ProductionLike: false, Timeout: 2 * time.Second})
	cfg := GeminiClientConfig("https://generativelanguage.googleapis.com", "key-explicit", hc)
	c, err := genai.NewClient(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := c.ClientConfig()
	if got.APIKey != "key-explicit" || got.Backend != genai.BackendGeminiAPI {
		t.Errorf("config = %+v", got)
	}
	if got.HTTPOptions.BaseURL != "https://generativelanguage.googleapis.com" {
		t.Errorf("base url = %q", got.HTTPOptions.BaseURL)
	}
}
