package providers

import "net/http"

// RegisterAll wires every adapter into r. clientFor returns the shared
// SSRF-safe client for a provider kind (the module builds one per kind so
// per-kind pools do not share connections across base URLs).
func RegisterAll(r *Registry, clientFor func(kind string) *http.Client) {
	r.Register("mock", func(c Credential, _ *http.Client) (Provider, error) { return NewMock(c, nil) })
	r.Register("ollama", func(c Credential, _ *http.Client) (Provider, error) { return NewOllama(c, clientFor("ollama")) })
	r.Register("openai_compatible", func(c Credential, _ *http.Client) (Provider, error) {
		return NewOpenAICompatible(c, clientFor("openai_compatible"))
	})
	r.Register("openai", func(c Credential, _ *http.Client) (Provider, error) { return NewOpenAI(c, clientFor("openai")) })
	// anthropic, gemini: Tasks 4-5 append their lines here.
}
