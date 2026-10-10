package models

import (
	"errors"
	"math"
	"reflect"
	"strings"
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

// I2: access is not part of the model bodies; only the grants body sets it.
// M3: stored defaults reach the provider adapters as they are, so they are
// held to the same ranges as per-request options.
func TestValidateModelInput_DefaultsRanges(t *testing.T) {
	base := ModelInput{
		Name: "Fast", Provider: ProviderOpenAI, ModelID: "gpt-5.6-terra",
		Capabilities:  LLMModelCapabilities{Chat: true},
		CredentialRef: LLMCredentialRef{Kind: CredentialKindOrg, CredentialUUID: "c1"},
		Purposes:      []LLMModelPurpose{{Purpose: "default"}},
	}
	f := func(v float64) *float64 { return &v }
	n := func(v int) *int { return &v }
	for _, tc := range []struct {
		name string
		temp *float64
		max  *int
		want error
	}{
		{"zero temperature", f(0), nil, nil},
		{"top temperature", f(2), nil, nil},
		{"negative temperature", f(-0.1), nil, ErrInvalidTemperature},
		{"temperature above 2", f(2.01), nil, ErrInvalidTemperature},
		{"NaN temperature", f(math.NaN()), nil, ErrInvalidTemperature},
		{"infinite temperature", f(math.Inf(1)), nil, ErrInvalidTemperature},
		{"one output token", nil, n(1), nil},
		{"max output tokens", nil, n(MaxOutputTokens), nil},
		{"zero output tokens", nil, n(0), ErrInvalidMaxOutputTokens},
		{"negative output tokens", nil, n(-5), ErrInvalidMaxOutputTokens},
		{"too many output tokens", nil, n(MaxOutputTokens + 1), ErrInvalidMaxOutputTokens},
	} {
		in := base
		in.Defaults = LLMModelDefaults{Temperature: tc.temp, MaxOutputTokens: tc.max}
		if err := ValidateModelInput(in); !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestModelBodiesCarryNoAccess(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(LLMModelBody{}), reflect.TypeOf(LLMModelPatchBody{}), reflect.TypeOf(ModelInput{})} {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if f.Name == "Access" || strings.HasPrefix(f.Tag.Get("json"), "access") {
				t.Errorf("%s.%s: access belongs to the grants route", typ.Name(), f.Name)
			}
		}
	}
	f, ok := reflect.TypeOf(LLMGrantsPutBody{}).FieldByName("Access")
	if !ok || f.Tag.Get("json") != "access" || f.Tag.Get("enum") != "granted,everyone" {
		t.Fatalf("LLMGrantsPutBody.Access must be a required enum, got %+v", f)
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
	if v := (&Credential{}).View(); v.HasSecret {
		t.Fatal("empty credential must report no secret")
	}
}

// Huma's schema-link transformer rebuilds every response body with
// reflect.StructOf, prepending a $schema field. That panics (Huma recovers
// and prints a warning) when a non-first embedded struct has value methods,
// which is why Credential and Model keep pointer receivers.
func TestViewsSupportHumaSchemaLinks(t *testing.T) {
	for _, body := range []reflect.Type{reflect.TypeFor[LLMCredentialView](), reflect.TypeFor[LLMModelView]()} {
		fields := []reflect.StructField{{Name: "Schema", Type: reflect.TypeFor[string](), Tag: `json:"$schema,omitempty"`}}
		for i := 0; i < body.NumField(); i++ {
			fields = append(fields, body.Field(i))
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s cannot carry a $schema link: %v", body, r)
				}
			}()
			reflect.StructOf(fields)
		}()
	}
}

func TestValidateCredentialInput_BaseURLOnlyForProvidersThatTakeOne(t *testing.T) {
	cases := []struct {
		name string
		in   CredentialInput
		want error
	}{
		{"mock with baseUrl", CredentialInput{Name: "M", Provider: ProviderMock, BaseURL: "http://x"}, ErrBaseURLNotAllowed},
		{"mock without baseUrl", CredentialInput{Name: "M", Provider: ProviderMock}, nil},
		{"openai custom baseUrl", CredentialInput{Name: "O", Provider: ProviderOpenAI, Secret: "k", BaseURL: "https://evil.example"}, ErrBaseURLFixed},
		{"openai fixed baseUrl echoed back", CredentialInput{Name: "O", Provider: ProviderOpenAI, Secret: "k", BaseURL: "https://api.openai.com/v1"}, nil},
		{"ollama needs baseUrl", CredentialInput{Name: "L", Provider: ProviderOllama}, ErrBaseURLRequired},
		{"ollama with baseUrl", CredentialInput{Name: "L", Provider: ProviderOllama, BaseURL: "http://ollama:11434"}, nil},
		{"openai_compatible with baseUrl", CredentialInput{Name: "C", Provider: ProviderOpenAICompatible, Secret: "k", BaseURL: "https://x.example"}, nil},
	}
	for _, c := range cases {
		if got := ValidateCredentialInput(c.in, true); got != c.want {
			t.Errorf("%s: err = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestBudgetReserveOutputTokens_ZeroMeansUnset: once an override is stored,
// PATCH must be able to remove it (back to the module default). A pointer
// field cannot tell "absent" from "null", so 0 is the clear value; on create
// 0 is the same as absent. Both schemas therefore accept 0.
func TestBudgetReserveOutputTokens_ZeroMeansUnset(t *testing.T) {
	zero, n := 0, 512
	stored := ModelInput{BudgetReserveOutputTokens: &n}
	if got := (LLMModelPatchBody{BudgetReserveOutputTokens: &zero}).ApplyTo(stored); got.BudgetReserveOutputTokens != nil {
		t.Fatalf("PATCH 0 kept the override: %d", *got.BudgetReserveOutputTokens)
	}
	if got := (LLMModelPatchBody{}).ApplyTo(stored); got.BudgetReserveOutputTokens == nil || *got.BudgetReserveOutputTokens != 512 {
		t.Fatalf("absent field changed the override: %v", got.BudgetReserveOutputTokens)
	}
	m := 1024
	if got := (LLMModelPatchBody{BudgetReserveOutputTokens: &m}).ApplyTo(stored); got.BudgetReserveOutputTokens == nil || *got.BudgetReserveOutputTokens != 1024 {
		t.Fatalf("PATCH 1024 = %v", got.BudgetReserveOutputTokens)
	}
	if got := (LLMModelBody{BudgetReserveOutputTokens: &zero}).Input(); got.BudgetReserveOutputTokens != nil {
		t.Fatalf("create 0 stored an override: %d", *got.BudgetReserveOutputTokens)
	}
	for _, typ := range []reflect.Type{reflect.TypeOf(LLMModelBody{}), reflect.TypeOf(LLMModelPatchBody{})} {
		f, _ := typ.FieldByName("BudgetReserveOutputTokens")
		if min := f.Tag.Get("minimum"); min != "0" {
			t.Errorf("%s.budgetReserveOutputTokens minimum = %q, want 0 (the clear value)", typ.Name(), min)
		}
	}
}
