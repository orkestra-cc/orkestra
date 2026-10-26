package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/llm/installation"
	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/internal/core/llm/providers/teitest"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// prodLikePlatform is a production-like staging platform.
type prodLikePlatform struct{}

func (prodLikePlatform) IsProduction() bool     { return false }
func (prodLikePlatform) IsStaging() bool        { return true }
func (prodLikePlatform) IsDevelopment() bool    { return false }
func (prodLikePlatform) IsProductionLike() bool { return true }
func (prodLikePlatform) GetEnvironment() string { return "staging" }
func (prodLikePlatform) FrontendURL() string    { return "" }

// initWithPlatform is initModule with a platform; the caller sets the
// inference environment first.
func initWithPlatform(t *testing.T, platform module.PlatformInfo) (*Module, *module.ServiceRegistry, string) {
	t.Helper()
	t.Setenv("LLM_SECRET_ENCRYPTION_KEY", strings.Repeat("ab", 32))
	logs := &bytes.Buffer{}
	reg := module.NewServiceRegistry()
	reg.Register(module.ServiceTenantDirectoryReader, iface.TenantDirectoryReader(noMembers{}))
	reg.Register(module.ServicePIIProducerRegistry, iface.NewPIIProducerRegistry())
	m := NewModule()
	deps := &module.Dependencies{DB: offlineDB(t), Logger: slog.New(slog.NewTextHandler(logs, nil)), Services: reg, Platform: platform}
	if err := m.Init(deps); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return m, reg, logs.String()
}

func TestModule_InitWithoutInferenceEndpointsIsSilent(t *testing.T) {
	m, _, _, logs := initModule(t, strings.Repeat("ab", 32))
	if strings.Contains(logs, "RETRIEVAL_INFERENCE") || strings.Contains(logs, "level=WARN") {
		t.Fatalf("an unset RETRIEVAL_INFERENCE_ENDPOINTS must log nothing:\n%s", logs)
	}
	if _, err := m.gateway.InferenceProfile(context.Background(), "embed"); !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("InferenceProfile = %v, want ErrLLMNotConfigured", err)
	}
}

func TestModule_GatewayExposesInstallationInterfaces(t *testing.T) {
	_, reg, _, _ := initModule(t, strings.Repeat("ab", 32))
	if _, ok := module.GetTyped[iface.LLMInferenceProfiles](reg, module.ServiceLLMGateway); !ok {
		t.Error("gateway must implement LLMInferenceProfiles")
	}
	if _, ok := module.GetTyped[iface.LLMPinnedEmbedder](reg, module.ServiceLLMGateway); !ok {
		t.Error("gateway must implement LLMPinnedEmbedder")
	}
	if _, ok := module.GetTyped[iface.LLMReranker](reg, module.ServiceLLMGateway); !ok {
		t.Error("gateway must implement LLMReranker")
	}
}

func TestModule_MalformedInferenceEndpointsWarnAndBoot(t *testing.T) {
	t.Setenv(installation.EndpointsEnv, `[{"name":"embed",`)
	t.Setenv(installation.APIKeyEnv, "sekrit-xyz")
	m, _, logs := initWithPlatform(t, nil)
	if !strings.Contains(logs, "level=WARN") || !strings.Contains(logs, "RETRIEVAL_INFERENCE_ENDPOINTS") {
		t.Fatalf("want a WARN naming RETRIEVAL_INFERENCE_ENDPOINTS:\n%s", logs)
	}
	if strings.Contains(logs, "sekrit-xyz") {
		t.Fatalf("the API key leaked into the logs:\n%s", logs)
	}
	if _, err := m.gateway.InferenceProfile(context.Background(), "embed"); !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("InferenceProfile = %v, want ErrLLMNotConfigured", err)
	}
}

func TestModule_InstallationEndpointsReachPrivateHostsButOrgModelsCannot(t *testing.T) {
	const embedID, key = "BAAI/bge-m3", "inst-key-7f3a"
	srv := teitest.New(t, teitest.Config{ModelID: embedID, Revision: "rev-1", Pooling: "cls", Dimensions: 8, APIKey: key})
	raw, err := json.Marshal([]map[string]any{{
		"name": "embed", "url": srv.URL, "kind": "embed", "boundary": "local",
		"model": embedID, "revision": "rev-1", "pooling": "cls", "normalize": true, "truncate": true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(installation.EndpointsEnv, string(raw))
	t.Setenv(installation.APIKeyEnv, key)
	m, _, logs := initWithPlatform(t, prodLikePlatform{})

	if !strings.Contains(logs, "llm: installation inference endpoint") || !strings.Contains(logs, "embed") || !strings.Contains(logs, embedID) {
		t.Fatalf("want the INFO line for the endpoint:\n%s", logs)
	}
	if strings.Contains(logs, key) {
		t.Fatalf("the API key leaked into the logs:\n%s", logs)
	}
	p, err := m.gateway.InferenceProfile(context.Background(), "embed")
	if err != nil {
		t.Fatalf("InferenceProfile: %v", err)
	}
	if p.Dimensions != 8 {
		t.Fatalf("Dimensions = %d, want 8", p.Dimensions)
	}

	for _, kind := range []string{models.ProviderOllama, models.ProviderOpenAICompatible} {
		prov, err := m.registry.Build(kind, providers.Credential{BaseURL: srv.URL}, nil)
		if err != nil {
			t.Fatalf("Build(%s): %v", kind, err)
		}
		_, err = prov.Embed(context.Background(), providers.EmbedCall{ModelID: "x", Inputs: []string{"hi"}})
		if !errors.Is(err, providers.ErrDestinationNotPublic) {
			t.Errorf("%s organization endpoint: err = %v, want ErrDestinationNotPublic", kind, err)
		}
	}
	if _, err := m.registry.Build(models.ProviderTEI, providers.Credential{BaseURL: srv.URL}, nil); !errors.Is(err, providers.ErrUnknownProvider) {
		t.Errorf("Build(tei) = %v, want ErrUnknownProvider", err)
	}
}
