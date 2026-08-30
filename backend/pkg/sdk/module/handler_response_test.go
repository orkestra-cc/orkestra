package module

import (
	"context"
	"testing"
)

// A plaintext secret a legacy document still carries under a schema-declared
// secret key must not be echoed by any admin read: the console renders
// configValues into ordinary inputs, and the response itself is a
// distribution channel the operator never asked for.
func TestAdminResponses_StripSchemaDeclaredSecretsFromConfigValues(t *testing.T) {
	h, repo, _ := newAuditHandler(t, &auditDemoModule{})
	doc := repo.docs["demo"]
	doc.ConfigValues["apiKey"] = "leak"
	prod := doc.Environments["production"]
	prod.ConfigValues["apiKey"] = "leak"
	prod.ConfigValues["email.profiles.__items"] = "acme"
	prod.ConfigValues["email.profiles.acme.__label"] = "Acme"
	prod.ConfigValues["email.profiles.acme.host"] = "smtp.acme"
	prod.ConfigValues["email.profiles.acme.password"] = "leak2"
	doc.Environments["production"] = prod
	ctx := context.Background()

	got, err := h.GetModule(ctx, &GetModuleInput{Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	assertNoPlaintextSecret(t, "GetModule", got.Body.ConfigValues)

	env, err := h.GetEnvironment(ctx, &GetEnvironmentInput{Name: "demo", Env: "production"})
	if err != nil {
		t.Fatal(err)
	}
	assertNoPlaintextSecret(t, "GetEnvironment", env.Body.ConfigValues)

	// The stored document is not mutated by a read; the repair belongs to the
	// next write.
	if repo.docs["demo"].Environments["production"].ConfigValues["apiKey"] != "leak" {
		t.Error("a read rewrote the stored document")
	}
}

func assertNoPlaintextSecret(t *testing.T, surface string, values map[string]string) {
	t.Helper()
	for _, k := range []string{"apiKey", "email.profiles.acme.password"} {
		if v, ok := values[k]; ok {
			t.Errorf("%s echoed a plaintext secret at %q = %q", surface, k, v)
		}
	}
	if values["flag"] != "false" || values["email.profiles.acme.host"] != "smtp.acme" ||
		values["email.profiles.acme.__label"] != "Acme" || values["email.profiles.__items"] != "acme" {
		t.Errorf("%s disturbed a non-secret key: %v", surface, values)
	}
}
