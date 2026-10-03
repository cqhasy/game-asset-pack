package imageclient

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/1024XEngineer/Holonic-Asset/internal/module/logger"
)

const (
	// DefaultQNABaseURL is retained only for deprecated QNA constructors.
	// Deprecated: Set an explicit BaseURL on OpenAIImagesAdapterConfig.
	DefaultQNABaseURL = "https://api.qnaigc.com"
	// DefaultQNAImagesModel is retained only for the deprecated constructor.
	// Deprecated: Set an explicit model on OpenAIImagesAdapterConfig.
	DefaultQNAImagesModel = "openai/gpt-image-2"
	defaultQNAHTTPTimeout = 5 * time.Minute
)

// QNAImagesAdapterConfig preserves legacy constructor options.
// Deprecated: Use OpenAIImagesAdapterConfig.
type QNAImagesAdapterConfig struct {
	BaseURL      string
	APIKey       string
	DefaultModel string
	HTTPClient   *http.Client
	// SDKClient preserves injected legacy executors without a dependency on an SDK.
	SDKClient legacyImageExecutor
	Logger    logger.Logger
}

// QNAImagesAdapter delegates requests to the generic Images adapter while
// preserving legacy JSON edits, size normalization, and mask fallback.
// Deprecated: Use OpenAIImagesAdapter.
type QNAImagesAdapter struct {
	*OpenAIImagesAdapter
	logger logger.Logger
}

// NewQNAImagesAdapter preserves the legacy constructor defaults.
// Deprecated: Use NewOpenAIImagesAdapter with an explicit endpoint and model.
func NewQNAImagesAdapter(config QNAImagesAdapterConfig) *QNAImagesAdapter {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = DefaultQNABaseURL
	}
	model := strings.TrimSpace(config.DefaultModel)
	if model == "" {
		model = DefaultQNAImagesModel
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultQNAHTTPTimeout}
	}
	if hasLegacyExecutor(config.SDKClient) {
		client = legacyExecutorHTTPClient(config.SDKClient, config.APIKey, client.Timeout)
	}
	adapter := NewOpenAIImagesAdapter(OpenAIImagesAdapterConfig{BaseURL: baseURL, APIKey: config.APIKey, DefaultModel: model, HTTPClient: client, EditFormat: "json"})
	return &QNAImagesAdapter{OpenAIImagesAdapter: adapter, logger: config.Logger}
}

func (a *QNAImagesAdapter) Generate(ctx context.Context, request *ProviderRequest) (*ProviderResult, error) {
	return a.OpenAIImagesAdapter.Generate(ctx, legacyImageRequest(request))
}

func (a *QNAImagesAdapter) Edit(ctx context.Context, request *ProviderRequest) (*ProviderResult, error) {
	legacy := legacyImageRequest(request)
	result, err := a.OpenAIImagesAdapter.Edit(ctx, legacy)
	if !shouldRetryQNAEditWithoutMask(legacy, err) {
		return result, err
	}
	if a.logger != nil {
		a.logger.Warn("image endpoint rejected the legacy edit mask format; retrying without native mask", logger.Error(err))
	}
	fallback := *legacy
	fallback.MaskImage = ""
	return a.OpenAIImagesAdapter.Edit(ctx, &fallback)
}

func legacyImageRequest(request *ProviderRequest) *ProviderRequest {
	if request == nil {
		return nil
	}
	copy := *request
	copy.Size = normalizeQNAImageSize(request.Size)
	copy.Params = make(Params, len(request.Params)+1)
	maps.Copy(copy.Params, request.Params)
	if strings.TrimSpace(copy.Params["output_format"]) == "" {
		copy.Params["output_format"] = "png"
	}
	return &copy
}

func shouldRetryQNAEditWithoutMask(request *ProviderRequest, err error) bool {
	if request == nil || strings.TrimSpace(request.MaskImage) == "" || err == nil {
		return false
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != ErrorKindInvalidRequest || providerErr.StatusCode != http.StatusBadRequest {
		return false
	}
	message := strings.ToLower(providerErr.Message)
	return strings.Contains(message, "mask must be an object") || strings.Contains(message, "unable to download content from the provided url")
}

func normalizeQNAImageSize(size string) string {
	size = strings.TrimSpace(size)
	var width, height int
	if _, err := fmt.Sscanf(size, "%dx%d", &width, &height); err != nil || width <= 0 || height <= 0 {
		return size
	}
	if int64(width)*int64(height) < 655_360 {
		return "1024x1024"
	}
	return size
}

var _ ImageProvider = (*QNAImagesAdapter)(nil)
