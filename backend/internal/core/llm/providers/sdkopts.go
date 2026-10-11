package providers

import (
	"errors"
	"net/http"

	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	openaiopt "github.com/openai/openai-go/option"
	"google.golang.org/genai"
)

// The vendor SDKs' NewClient/NewClient-equivalents silently read process
// environment variables (OPENAI_API_KEY/ORG_ID/PROJECT_ID/BASE_URL/
// WEBHOOK_SECRET, ANTHROPIC_API_KEY/AUTH_TOKEN/BASE_URL/PROFILE/
// CUSTOM_HEADERS, GOOGLE_API_KEY/GEMINI_API_KEY/GOOGLE_CLOUD_*/
// GOOGLE_GENAI_USE_VERTEXAI, ...). In a multi-tenant gateway a deployment
// variable must never become a tenant request's credential, organization or
// destination. Adapters therefore build their clients ONLY through these
// helpers, which pass every identity-bearing setting explicitly.

// OpenAIClientOptions: options for openai.NewClient. The organization and
// project headers are deleted (WithOrganization("") would still send an empty
// header) so OPENAI_ORG_ID / OPENAI_PROJECT_ID are never sent.
// MaxRetries is 0: retries are the router's decision, not the SDK's.
func OpenAIClientOptions(baseURL, apiKey string, hc *http.Client) []openaiopt.RequestOption {
	return []openaiopt.RequestOption{
		openaiopt.WithBaseURL(baseURL),
		openaiopt.WithAPIKey(apiKey),
		openaiopt.WithHeaderDel("OpenAI-Organization"),
		openaiopt.WithHeaderDel("OpenAI-Project"),
		openaiopt.WithHTTPClient(hc),
		openaiopt.WithMaxRetries(0),
	}
}

// AnthropicClientOptions: options for anthropic.NewClient.
// WithoutEnvironmentDefaults skips every ANTHROPIC_* variable, the profile
// loader and env federation (the SDK documents this marker for exactly that);
// the base URL and the credential are then explicit.
func AnthropicClientOptions(baseURL, apiKey string, hc *http.Client) []anthropicopt.RequestOption {
	return []anthropicopt.RequestOption{
		anthropicopt.WithoutEnvironmentDefaults(),
		anthropicopt.WithBaseURL(baseURL),
		anthropicopt.WithAPIKey(apiKey),
		anthropicopt.WithHTTPClient(hc),
		anthropicopt.WithMaxRetries(0),
	}
}

// GeminiClientConfig: config for genai.NewClient. The backend is pinned to
// the Gemini Developer API and the key, base URL and HTTP client are
// explicit, which the SDK documents as taking precedence over
// GOOGLE_GENAI_USE_VERTEXAI / GOOGLE_API_KEY / GOOGLE_CLOUD_* / base-URL
// variables (GOOGLE_CLOUD_PROJECT is still copied into the config but is
// unused on the Gemini API backend). Retries are disabled (the router owns them).
func GeminiClientConfig(baseURL, apiKey string, hc *http.Client) (*genai.ClientConfig, error) {
	// genai falls back to GOOGLE_API_KEY / GEMINI_API_KEY / GOOGLE_GEMINI_BASE_URL
	// for an empty value, so an empty one is refused rather than passed on.
	if baseURL == "" || apiKey == "" || hc == nil {
		return nil, errors.New("llm providers: gemini client needs an explicit base URL, API key and HTTP client")
	}
	return &genai.ClientConfig{
		Backend:    genai.BackendGeminiAPI,
		APIKey:     apiKey,
		HTTPClient: hc,
		HTTPOptions: genai.HTTPOptions{
			BaseURL:      baseURL,
			RetryOptions: &genai.HTTPRetryOptions{Attempts: genai.Ptr(int32(1))},
		},
	}, nil
}
