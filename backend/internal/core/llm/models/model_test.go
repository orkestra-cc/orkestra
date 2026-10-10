package models

import (
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/bson"
)

func TestValidateModelInput(t *testing.T) {
	ok := ModelInput{
		Name: "Fast", Provider: ProviderOpenAI, ModelID: "gpt-5.6-terra",
		Capabilities:  LLMModelCapabilities{Chat: true, Streaming: true},
		CredentialRef: LLMCredentialRef{Kind: CredentialKindOrg, CredentialUUID: "c1"},
		Purposes:      []LLMModelPurpose{{Purpose: "default", Priority: 10}},
		Access:        AccessGranted,
	}
	if err := ValidateModelInput(ok); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}

	cases := map[string]func(m *ModelInput){
		"empty name":            func(m *ModelInput) { m.Name = "" },
		"unknown provider":      func(m *ModelInput) { m.Provider = "cohere" },
		"org kind without cred": func(m *ModelInput) { m.CredentialRef.CredentialUUID = "" },
		"user_account not openai": func(m *ModelInput) {
			m.CredentialRef = LLMCredentialRef{Kind: CredentialKindUserAccount}
			m.Provider = ProviderAnthropic
		},
		"user_account with embed": func(m *ModelInput) {
			m.CredentialRef = LLMCredentialRef{Kind: CredentialKindUserAccount}
			m.Capabilities.Embeddings = true
		},
		"user_account with temp": func(m *ModelInput) {
			m.CredentialRef = LLMCredentialRef{Kind: CredentialKindUserAccount}
			f := 0.2
			m.Defaults.Temperature = &f
		},
		"purpose bad pattern": func(m *ModelInput) { m.Purposes = []LLMModelPurpose{{Purpose: "has space", Priority: 1}} },
		"duplicate purpose": func(m *ModelInput) {
			m.Purposes = []LLMModelPurpose{{Purpose: "a", Priority: 1}, {Purpose: "a", Priority: 2}}
		},
		"no purposes":             func(m *ModelInput) { m.Purposes = nil },
		"bad access":              func(m *ModelInput) { m.Access = "friends" },
		"embeddings without dims": func(m *ModelInput) { m.Capabilities = LLMModelCapabilities{Embeddings: true} },
		"no capability at all":    func(m *ModelInput) { m.Capabilities = LLMModelCapabilities{} },
	}
	for name, mutate := range cases {
		m := ok
		m.Purposes = append([]LLMModelPurpose(nil), ok.Purposes...)
		mutate(&m)
		if err := ValidateModelInput(m); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestEnvelope_Last4(t *testing.T) {
	if got := Last4("sk-abcdef"); got != "cdef" {
		t.Fatalf("Last4 = %q", got)
	}
	if got := Last4("abc"); got != "" {
		t.Fatalf("Last4 short = %q, want empty", got)
	}
}

func TestCapabilitiesRoundTrip(t *testing.T) {
	in := iface.LLMCapabilities{Chat: true, Streaming: true, Tools: true, StructuredOutput: true, Embeddings: true, Dimensions: 1536}
	if got := CapabilitiesFromIface(in).ToIface(); got != in {
		t.Fatalf("round trip = %+v, want %+v", got, in)
	}
}

func TestPersistedFieldNames(t *testing.T) {
	now := time.Now()
	cred := Credential{
		UUID: "c1", TenantID: "t1", Secret: Envelope{Alg: EnvelopeAlgLocal, Ciphertext: "x"},
		SecretLast4: "cdef", LastTestedAt: &now, LastTestStatus: "ok", LastTestError: "llm.x",
	}
	raw, err := bson.Marshal(cred)
	if err != nil {
		t.Fatal(err)
	}
	var doc bson.M
	if err := bson.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"secretLast4", "lastTestedAt", "lastTestStatus", "lastTestError"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("credential missing top-level %q", k)
		}
	}
	env, _ := doc["secret"].(bson.M)
	if _, has := env["last4"]; has {
		t.Error("envelope must not carry last4")
	}

	raw, _ = bson.Marshal(Model{Capabilities: LLMModelCapabilities{StructuredOutput: true}})
	doc = bson.M{}
	_ = bson.Unmarshal(raw, &doc)
	caps, _ := doc["capabilities"].(bson.M)
	if _, ok := caps["structuredOutput"]; !ok {
		t.Errorf("capabilities missing camelCase structuredOutput: %v", caps)
	}

	raw, _ = bson.Marshal(LLMGrant{UUID: "g1", CreatedAt: now})
	doc = bson.M{}
	_ = bson.Unmarshal(raw, &doc)
	for _, k := range []string{"uuid", "createdAt"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("grant missing %q", k)
		}
	}
	if _, bad := doc["grantedAt"]; bad {
		t.Error("grant must use createdAt, not grantedAt")
	}
}

func TestCredentialView(t *testing.T) {
	c := Credential{SecretLast4: "cdef", Secret: Envelope{Ciphertext: "x"}}
	if v := c.View(); !v.HasSecret || v.SecretLast4 != "cdef" {
		t.Fatalf("view = %+v", v)
	}
	if v := (Credential{}).View(); v.HasSecret {
		t.Fatal("empty credential must report no secret")
	}
}
