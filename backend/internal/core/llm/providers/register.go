package providers

import "net/http"

// RegisterAll wires every adapter into r. clientFor returns the shared
// SSRF-safe client for a provider kind. The module builds one client per
// kind, which keeps each kind's connection pool (and idle-connection
// budget) separate from the others; within a kind, connections are
// pooled per host as usual, so different base URLs never share one.
func RegisterAll(r *Registry, clientFor func(kind string) *http.Client) {
	r.Register("mock", func(c Credential, _ *http.Client) (Provider, error) { return NewMock(c, nil) })
	r.Register("ollama", func(c Credential, _ *http.Client) (Provider, error) { return NewOllama(c, clientFor("ollama")) })
	r.Register("openai_compatible", func(c Credential, _ *http.Client) (Provider, error) {
		return NewOpenAICompatible(c, clientFor("openai_compatible"))
	})
	r.Register("openai", func(c Credential, _ *http.Client) (Provider, error) { return NewOpenAI(c, clientFor("openai")) })
	r.Register("anthropic", func(c Credential, _ *http.Client) (Provider, error) { return NewAnthropic(c, clientFor("anthropic")) })
	r.Register("gemini", func(c Credential, _ *http.Client) (Provider, error) { return NewGemini(c, clientFor("gemini")) })
}
