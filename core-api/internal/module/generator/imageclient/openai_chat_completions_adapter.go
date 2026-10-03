package imageclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// OpenAIChatCompletionsAdapterConfig configures an OpenAI-compatible Chat Completions endpoint.
type OpenAIChatCompletionsAdapterConfig struct {
	BaseURL            string
	APIKey             string
	DefaultModel       string
	HTTPClient         *http.Client
	DownloadHTTPClient *http.Client
}

// OpenAIChatCompletionsAdapter sends multimodal prompts and extracts generated images.
type OpenAIChatCompletionsAdapter struct {
	transport          *openAITransport
	defaultModel       string
	downloadHTTPClient *http.Client
	legacyStringSeed   bool
}

func NewOpenAIChatCompletionsAdapter(config OpenAIChatCompletionsAdapterConfig) *OpenAIChatCompletionsAdapter {
	model := strings.TrimSpace(config.DefaultModel)
	download := config.DownloadHTTPClient
	if download == nil {
		download = newGeneratedImageHTTPClient()
	}
	return &OpenAIChatCompletionsAdapter{transport: newOpenAITransport(config.BaseURL, config.APIKey, config.HTTPClient), defaultModel: model, downloadHTTPClient: download}
}

func (a *OpenAIChatCompletionsAdapter) Generate(ctx context.Context, request *ProviderRequest) (*ProviderResult, error) {
	return a.call(ctx, request, nil)
}
func (a *OpenAIChatCompletionsAdapter) Edit(ctx context.Context, request *ProviderRequest) (*ProviderResult, error) {
	if request == nil {
		return a.call(ctx, nil, nil)
	}
	return a.call(ctx, request, request.ReferenceImages)
}

func (a *OpenAIChatCompletionsAdapter) call(ctx context.Context, request *ProviderRequest, references []string) (*ProviderResult, error) {
	if request == nil {
		return nil, newOpenAIProviderError(ErrorKindInvalidRequest, 0, false, "image request is required", nil)
	}
	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = a.defaultModel
	}
	if model == "" {
		return nil, newGenericRoutingError("image model is required")
	}
	var seed any
	if value := strings.TrimSpace(request.Params["seed"]); value != "" {
		if a.legacyStringSeed {
			seed = value
		} else {
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return nil, newGenericRoutingError("chat seed must be an integer")
			}
			seed = parsed
		}
	}
	contents := make([]openAIChatContentPart, 0, len(references)+2)
	for _, ref := range references {
		if formatted := formatOpenAIChatImageRef(ref); formatted != "" {
			contents = append(contents, openAIChatContentPart{Type: "image_url", ImageURL: &openAIChatImageURL{URL: formatted}})
		}
	}
	if formatted := formatOpenAIChatImageRef(request.MaskImage); formatted != "" {
		contents = append(contents, openAIChatContentPart{Type: "image_url", ImageURL: &openAIChatImageURL{URL: formatted}})
	}
	contents = append(contents, openAIChatContentPart{Type: "text", Text: request.Prompt})
	payload := openAIChatCompletionRequest{Model: model, N: request.N, Seed: seed, Messages: []openAIChatMessage{{Role: "user", Content: contents}}, Stream: false}
	var decoded openAIChatCompletionResponse
	if err := a.transport.execute(ctx, http.MethodPost, "chat/completions", payload, &decoded); err != nil {
		return nil, classifyOpenAIAdapterError(ctx, err)
	}
	if len(decoded.Choices) == 0 {
		return nil, newOpenAIProviderError(ErrorKindInvalidResponse, http.StatusOK, true, "chat completion response contains no choices", nil)
	}
	images, err := a.extractImages(ctx, decoded.Choices)
	if err != nil {
		return nil, err
	}
	if len(images) == 0 {
		return nil, newOpenAIProviderError(ErrorKindInvalidResponse, http.StatusOK, true, "chat completion response contains no image data in choices", nil)
	}
	format := "png"
	if data, err := base64.StdEncoding.DecodeString(images[0]); err == nil {
		if detected := openAIImageFormat(data, ""); detected != "" {
			format = detected
		}
	}
	return &ProviderResult{Images: images, OutputFormat: format, Size: request.Size, CreatedAt: decoded.Created, Usage: Usage{TotalTokens: decoded.Usage.TotalTokens, InputTokens: decoded.Usage.PromptTokens, OutputTokens: decoded.Usage.CompletionTokens, RequestCount: 1}}, nil
}

func (a *OpenAIChatCompletionsAdapter) extractImages(ctx context.Context, choices []openAIChatChoice) ([]string, error) {
	images := make([]string, 0, len(choices))
	for _, choice := range choices {
		for _, part := range append(choice.Message.Images, choice.Message.ContentParts...) {
			if part.ImageURL != nil && part.ImageURL.URL != "" {
				value, err := a.resolveImageToB64(ctx, part.ImageURL.URL)
				if err != nil {
					return nil, err
				}
				images = append(images, value)
			}
		}
		var contentBuilder strings.Builder
		contentBuilder.WriteString(choice.Message.contentString())
		for _, part := range choice.Message.ContentParts {
			if part.Type == "text" && part.Text != "" {
				contentBuilder.WriteByte('\n')
				contentBuilder.WriteString(part.Text)
			}
		}
		content := contentBuilder.String()
		if strings.TrimSpace(content) == "" {
			continue
		}
		dataURLs := openAIDataImageURLRegex.FindAllString(content, -1)
		seen := make(map[string]bool)
		for _, dataURL := range dataURLs {
			value, err := a.resolveImageToB64(ctx, dataURL)
			if err != nil {
				return nil, err
			}
			images = append(images, value)
			seen[dataURL] = true
		}
		matches := openAIMarkdownImageRegex.FindAllStringSubmatch(content, -1)
		if len(matches) > 0 {
			for _, match := range matches {
				if len(match) > 1 && !seen[match[1]] {
					value, err := a.resolveImageToB64(ctx, match[1])
					if err != nil {
						return nil, err
					}
					images = append(images, value)
				}
			}
			continue
		}
		urls := openAIHTTPURLRegex.FindAllString(content, -1)
		if len(urls) > 0 {
			for _, value := range urls {
				image, err := a.resolveImageToB64(ctx, value)
				if err != nil {
					return nil, err
				}
				images = append(images, image)
			}
			continue
		}
		trimmed := strings.TrimSpace(openAIDataImageURLRegex.ReplaceAllString(content, ""))
		if strings.HasPrefix(trimmed, "data:image/") || isLikelyBase64(trimmed) {
			value, err := a.resolveImageToB64(ctx, trimmed)
			if err != nil {
				return nil, err
			}
			images = append(images, value)
		}
	}
	return images, nil
}

func (a *OpenAIChatCompletionsAdapter) resolveImageToB64(ctx context.Context, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "data:image/") {
		value, err := parseOpenAIImageDataURL(raw)
		if err != nil {
			return "", newOpenAIProviderError(ErrorKindInvalidResponse, 0, false, "invalid generated image data URL", err)
		}
		return value, nil
	}
	if isLikelyBase64(raw) {
		return raw, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", newOpenAIProviderError(ErrorKindTransport, 0, true, "create image download request: "+err.Error(), err)
	}
	if err := validateGeneratedImageURL(req.URL); err != nil {
		return "", newOpenAIProviderError(ErrorKindInvalidResponse, 0, false, "reject generated image URL", err)
	}
	resp, err := a.downloadHTTPClient.Do(req)
	if err != nil {
		return "", classifyOpenAIAdapterError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", newOpenAIProviderError(ErrorKindTransport, resp.StatusCode, true, fmt.Sprintf("download generated image failed with status %d", resp.StatusCode), nil)
	}
	if resp.ContentLength > maxGeneratedImageBytes {
		return "", newOpenAIProviderError(ErrorKindInvalidResponse, resp.StatusCode, false, fmt.Sprintf("generated image exceeds %d bytes", maxGeneratedImageBytes), nil)
	}
	if contentType := resp.Header.Get("Content-Type"); contentType != "" && !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		return "", newOpenAIProviderError(ErrorKindInvalidResponse, resp.StatusCode, false, "generated image response has non-image content type", nil)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxGeneratedImageBytes+1))
	if err != nil {
		return "", newOpenAIProviderError(ErrorKindTransport, 0, true, "read downloaded image data: "+err.Error(), err)
	}
	if len(data) > maxGeneratedImageBytes {
		return "", newOpenAIProviderError(ErrorKindInvalidResponse, resp.StatusCode, false, fmt.Sprintf("generated image exceeds %d bytes", maxGeneratedImageBytes), nil)
	}
	if len(data) == 0 {
		return "", newOpenAIProviderError(ErrorKindInvalidResponse, 0, true, "downloaded image is empty", nil)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func formatOpenAIChatImageRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "data:") || strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	if isLikelyBase64(ref) {
		format := "png"
		if data, err := base64.StdEncoding.DecodeString(ref); err == nil {
			if detected := openAIImageFormat(data, ""); detected != "" {
				format = detected
			}
		}
		return "data:" + mediaTypeForFormat(format) + ";base64," + ref
	}
	return ref
}
func parseOpenAIImageDataURL(value string) (string, error) {
	comma := strings.IndexByte(value, ',')
	if comma < 0 || !strings.HasPrefix(value, "data:image/") || !strings.HasSuffix(strings.ToLower(value[:comma]), ";base64") {
		return "", errors.New("image data URL must contain a base64 payload")
	}
	payload := value[comma+1:]
	if payload == "" {
		return "", errors.New("image data URL payload is empty")
	}
	if _, err := base64.StdEncoding.DecodeString(payload); err != nil {
		return "", fmt.Errorf("decode image data URL payload: %w", err)
	}
	return payload, nil
}

var openAIMarkdownImageRegex = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)
var openAIHTTPURLRegex = regexp.MustCompile(`https?://[^\s"'>)]+`)
var openAIDataImageURLRegex = regexp.MustCompile(`data:image/[A-Za-z0-9.+-]+;base64,[A-Za-z0-9+/=]+`)

type openAIChatCompletionRequest struct {
	Model    string              `json:"model"`
	N        int                 `json:"n,omitempty"`
	Seed     any                 `json:"seed,omitempty"`
	Messages []openAIChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
}
type openAIChatMessage struct {
	Role         string                  `json:"role"`
	Content      any                     `json:"content"`
	Images       []openAIChatContentPart `json:"images,omitempty"`
	ContentParts []openAIChatContentPart `json:"-"`
}

func (m *openAIChatMessage) contentString() string {
	if value, ok := m.Content.(string); ok {
		return value
	}
	return ""
}
func (m *openAIChatMessage) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role    string                  `json:"role"`
		Content json.RawMessage         `json:"content"`
		Images  []openAIChatContentPart `json:"images,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Role, m.Images = raw.Role, raw.Images
	if len(raw.Content) == 0 {
		m.Content = ""
		return nil
	}
	var str string
	if json.Unmarshal(raw.Content, &str) == nil {
		m.Content = str
		return nil
	}
	var parts []openAIChatContentPart
	if json.Unmarshal(raw.Content, &parts) == nil {
		m.Content = parts
		m.ContentParts = parts
		return nil
	}
	m.Content = string(raw.Content)
	return nil
}

type openAIChatContentPart struct {
	Type     string              `json:"type"`
	Text     string              `json:"text,omitempty"`
	ImageURL *openAIChatImageURL `json:"image_url,omitempty"`
}
type openAIChatImageURL struct {
	URL string `json:"url"`
}
type openAIChatCompletionResponse struct {
	Created int64              `json:"created"`
	Choices []openAIChatChoice `json:"choices"`
	Usage   openAIChatUsage    `json:"usage"`
}
type openAIChatChoice struct {
	Message openAIChatMessage `json:"message"`
}
type openAIChatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

var _ ImageProvider = (*OpenAIChatCompletionsAdapter)(nil)
