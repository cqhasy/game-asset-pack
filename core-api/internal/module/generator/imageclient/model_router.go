package imageclient

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type protocolAdapter = ImageProvider

type invalidProtocolAdapter struct{ message string }

func newInvalidProtocolAdapter(message string) protocolAdapter {
	return &invalidProtocolAdapter{message: message}
}

func (p *invalidProtocolAdapter) Generate(context.Context, *ProviderRequest) (*ProviderResult, error) {
	return nil, newGenericRoutingError(p.message)
}

func (p *invalidProtocolAdapter) Edit(context.Context, *ProviderRequest) (*ProviderResult, error) {
	return nil, newGenericRoutingError(p.message)
}

// modelRouter routes requests to protocol adapters selected by model name.
type modelRouter struct {
	defaultModel string
	adapters     map[string]protocolAdapter
}

func newModelRouter(cfg FactoryConfig) ImageProvider {
	routes := make(map[string]protocolAdapter, len(cfg.Models))
	for _, modelConfig := range cfg.Models {
		model := strings.TrimSpace(modelConfig.Name)
		if model == "" {
			continue
		}
		key := strings.ToLower(model)
		if _, exists := routes[key]; exists {
			routes[key] = newInvalidProtocolAdapter(fmt.Sprintf("model %q is assigned to multiple image protocols", model))
			continue
		}
		modelDefaults := cfg
		if strings.TrimSpace(modelConfig.EditFormat) != "" {
			modelDefaults.EditFormat = modelConfig.EditFormat
		}
		routes[key] = createGenericProtocolAdapter(modelConfig.Protocol, model, modelDefaults, modelConfig.BaseURL, modelConfig.APIKey)
	}
	return &modelRouter{defaultModel: strings.TrimSpace(cfg.DefaultModel), adapters: routes}
}

func (r *modelRouter) Generate(ctx context.Context, request *ProviderRequest) (*ProviderResult, error) {
	adapter, routed, err := r.route(request)
	if err != nil {
		return nil, err
	}
	return adapter.Generate(ctx, routed)
}

func (r *modelRouter) Edit(ctx context.Context, request *ProviderRequest) (*ProviderResult, error) {
	adapter, routed, err := r.route(request)
	if err != nil {
		return nil, err
	}
	return adapter.Edit(ctx, routed)
}

func (r *modelRouter) route(request *ProviderRequest) (protocolAdapter, *ProviderRequest, error) {
	if request == nil {
		return nil, nil, newGenericRoutingError("image request is required")
	}
	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = r.defaultModel
	}
	if model == "" {
		return nil, nil, newGenericRoutingError("image model is required")
	}
	adapter := r.adapters[strings.ToLower(model)]
	if adapter == nil {
		return nil, nil, newGenericRoutingError(fmt.Sprintf("no image protocol is configured for model %q", model))
	}
	routed := *request
	routed.Model = model
	return adapter, &routed, nil
}

func createGenericProtocolAdapter(protocol, model string, cfg FactoryConfig, modelBaseURL, modelAPIKey string) protocolAdapter {
	baseURL := strings.TrimSpace(modelBaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(cfg.BaseURL)
	}
	apiKey := strings.TrimSpace(modelAPIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(cfg.APIKey)
	}
	selected := ProtocolType(strings.ToLower(strings.TrimSpace(protocol)))
	if selected == protocolTypeLegacyGeminiChat {
		selected = ProtocolTypeChatCompletions
	}
	if selected == "" || selected == ProtocolTypeAuto {
		if IsChatProtocolModel(model) {
			selected = ProtocolTypeChatCompletions
		} else {
			selected = ProtocolTypeOpenAIImages
		}
	}
	switch selected {
	case ProtocolTypeChatCompletions:
		return NewOpenAIChatCompletionsAdapter(OpenAIChatCompletionsAdapterConfig{BaseURL: baseURL, APIKey: apiKey, DefaultModel: model, HTTPClient: cfg.HTTPClient})
	case ProtocolTypeOpenAIImages:
		editFormat := strings.TrimSpace(cfg.EditFormat)
		if editFormat == "" {
			editFormat = "json"
		}
		return NewOpenAIImagesAdapter(OpenAIImagesAdapterConfig{BaseURL: baseURL, APIKey: apiKey, DefaultModel: model, HTTPClient: cfg.HTTPClient, EditFormat: editFormat})
	default:
		return newInvalidProtocolAdapter("unsupported image protocol " + strconv.Quote(protocol))
	}
}

func newGenericRoutingError(message string) *ProviderError {
	return newOpenAIProviderError(ErrorKindInvalidRequest, 0, false, message, nil)
}

var _ ImageProvider = (*modelRouter)(nil)
