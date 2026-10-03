package imageclient

import (
	"net/http"
	"strings"
	"time"

	"github.com/1024XEngineer/Holonic-Asset/internal/module/logger"
)

// DefaultQNAChatCompletionsModel preserves the deprecated constructor default.
// Deprecated: Set an explicit model on OpenAIChatCompletionsAdapterConfig.
const DefaultQNAChatCompletionsModel = "google/nano-banana-2"
const defaultChatHTTPTimeout = 5 * time.Minute

// QNAChatCompletionsAdapterConfig preserves legacy constructor options.
// Deprecated: Use OpenAIChatCompletionsAdapterConfig.
type QNAChatCompletionsAdapterConfig struct {
	BaseURL            string
	APIKey             string
	DefaultModel       string
	HTTPClient         *http.Client
	SDKClient          legacyImageExecutor
	DownloadHTTPClient *http.Client
	Logger             logger.Logger
}

// QNAChatCompletionsAdapter retains the old name for the generic adapter.
// Deprecated: Use OpenAIChatCompletionsAdapter.
type QNAChatCompletionsAdapter = OpenAIChatCompletionsAdapter

// NewQNAChatCompletionsAdapter preserves the legacy constructor defaults.
// Deprecated: Use NewOpenAIChatCompletionsAdapter with an explicit endpoint and model.
func NewQNAChatCompletionsAdapter(config QNAChatCompletionsAdapterConfig) *QNAChatCompletionsAdapter {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = DefaultQNABaseURL
	}
	model := strings.TrimSpace(config.DefaultModel)
	if model == "" {
		model = DefaultQNAChatCompletionsModel
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultChatHTTPTimeout}
	}
	if hasLegacyExecutor(config.SDKClient) {
		client = legacyExecutorHTTPClient(config.SDKClient, config.APIKey, client.Timeout)
	}
	adapter := NewOpenAIChatCompletionsAdapter(OpenAIChatCompletionsAdapterConfig{BaseURL: baseURL, APIKey: config.APIKey, DefaultModel: model, HTTPClient: client, DownloadHTTPClient: config.DownloadHTTPClient})
	adapter.legacyStringSeed = true
	return adapter
}
