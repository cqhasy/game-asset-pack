package imageclient

import (
	"net/http"
	"strings"

	"github.com/1024XEngineer/Holonic-Asset/internal/module/logger"
)

// ProtocolType selects the wire format used by one OpenAI-compatible image model.
type ProtocolType string

const (
	// ProtocolTypeAuto determines the API format from the configured model name.
	ProtocolTypeAuto ProtocolType = "auto"
	// ProtocolTypeOpenAIImages uses the /v1/images/* endpoint format.
	ProtocolTypeOpenAIImages ProtocolType = "openai_images"
	// ProtocolTypeChatCompletions uses the /v1/chat/completions format.
	ProtocolTypeChatCompletions ProtocolType = "chat_completions"

	// protocolTypeLegacyGeminiChat is accepted for existing deployments. The
	// value is intentionally not used for new configuration or diagnostics.
	protocolTypeLegacyGeminiChat ProtocolType = "gemini_chat"
)

// ModelConfig assigns one model to its wire protocol and endpoint settings.
type ModelConfig struct {
	Name       string
	Protocol   string
	BaseURL    string
	APIKey     string
	EditFormat string
}

// FactoryConfig provides parameters to initialize an ImageProvider.
type FactoryConfig struct {
	BaseURL       string
	APIKey        string
	DefaultModel  string
	FallbackModel string
	// EditFormat defaults to JSON for existing gateway configurations.
	EditFormat string
	// Provider is the legacy global protocol override used when Models is empty.
	Provider   string
	Models     []ModelConfig
	HTTPClient *http.Client
	Logger     logger.Logger
}

// NewImageProvider constructs an ImageProvider for one or more model endpoints.
// When FallbackModel is set, transient primary-model failures fall back to a
// second model.
func NewImageProvider(cfg FactoryConfig) ImageProvider {
	if len(cfg.Models) > 0 {
		provider := newModelRouter(cfg)
		fallbackModel := strings.TrimSpace(cfg.FallbackModel)
		if fallbackModel == "" || strings.EqualFold(fallbackModel, cfg.DefaultModel) {
			return provider
		}
		return NewModelFallbackProvider(ModelFallbackConfig{
			Primary:       provider,
			Fallback:      provider,
			PrimaryModel:  cfg.DefaultModel,
			FallbackModel: fallbackModel,
			Logger:        cfg.Logger,
		})
	}

	primary := createProtocolAdapter(cfg.Provider, cfg.DefaultModel, cfg, cfg.BaseURL, cfg.APIKey)

	fallbackModel := strings.TrimSpace(cfg.FallbackModel)
	if fallbackModel == "" || strings.EqualFold(fallbackModel, cfg.DefaultModel) {
		return primary
	}

	fallback := createProtocolAdapter(string(ProtocolTypeAuto), fallbackModel, FactoryConfig{
		BaseURL:      cfg.BaseURL,
		APIKey:       cfg.APIKey,
		DefaultModel: fallbackModel,
		EditFormat:   cfg.EditFormat,
		HTTPClient:   cfg.HTTPClient,
		Logger:       cfg.Logger,
	}, cfg.BaseURL, cfg.APIKey)

	return NewModelFallbackProvider(ModelFallbackConfig{
		Primary:       primary,
		Fallback:      fallback,
		PrimaryModel:  cfg.DefaultModel,
		FallbackModel: fallbackModel,
		Logger:        cfg.Logger,
	})
}

func createProtocolAdapter(
	protocol, model string,
	cfg FactoryConfig,
	modelBaseURL, modelAPIKey string,
) protocolAdapter {
	return createGenericProtocolAdapter(protocol, model, cfg, modelBaseURL, modelAPIKey)
}

// IsChatProtocolModel reports whether modelName targets a chat-based multimodal image model.
func IsChatProtocolModel(modelName string) bool {
	lower := strings.ToLower(strings.TrimSpace(modelName))
	return strings.HasPrefix(lower, "google/") ||
		strings.Contains(lower, "gemini") ||
		strings.Contains(lower, "banana") ||
		strings.Contains(lower, "chat")
}
