package imageclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// OpenAIImagesAdapterConfig configures an OpenAI-compatible Images endpoint.
type OpenAIImagesAdapterConfig struct {
	BaseURL      string
	APIKey       string
	DefaultModel string
	HTTPClient   *http.Client
	// EditFormat is "multipart" (the default) or "json" for compatible gateways.
	EditFormat string
	// DownloadHTTPClient overrides the secure client for image URL downloads.
	DownloadHTTPClient *http.Client
}

// OpenAIImagesAdapter calls /v1/images/generations and /v1/images/edits.
type OpenAIImagesAdapter struct {
	transport          *openAITransport
	defaultModel       string
	editFormat         string
	downloadHTTPClient *http.Client
}

// NewOpenAIImagesAdapter creates an adapter for any OpenAI-compatible Images endpoint.
func NewOpenAIImagesAdapter(config OpenAIImagesAdapterConfig) *OpenAIImagesAdapter {
	model := strings.TrimSpace(config.DefaultModel)
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}
	download := config.DownloadHTTPClient
	if download == nil {
		download = newGeneratedImageHTTPClient()
	}
	editFormat := strings.ToLower(strings.TrimSpace(config.EditFormat))
	if editFormat == "" {
		editFormat = "multipart"
	}
	return &OpenAIImagesAdapter{transport: newOpenAITransport(config.BaseURL, config.APIKey, httpClient), defaultModel: model, editFormat: editFormat, downloadHTTPClient: download}
}

func (a *OpenAIImagesAdapter) Generate(ctx context.Context, request *ProviderRequest) (*ProviderResult, error) {
	if request == nil {
		return nil, newOpenAIProviderError(ErrorKindInvalidRequest, 0, false, "image request is required", nil)
	}
	if err := a.validateRequest(ctx, request); err != nil {
		return nil, err
	}
	var decoded openAIImagesResponse
	if err := a.transport.execute(ctx, http.MethodPost, "images/generations", a.payload(request), &decoded); err != nil {
		return nil, classifyOpenAIAdapterError(ctx, err)
	}
	return a.result(ctx, request, decoded)
}

func (a *OpenAIImagesAdapter) Edit(ctx context.Context, request *ProviderRequest) (*ProviderResult, error) {
	if request == nil {
		return nil, newOpenAIProviderError(ErrorKindInvalidRequest, 0, false, "image request is required", nil)
	}
	if err := a.validateRequest(ctx, request); err != nil {
		return nil, err
	}
	if len(request.ReferenceImages) == 0 {
		return nil, newOpenAIProviderError(ErrorKindInvalidRequest, 0, false, "image editing requires a reference image", nil)
	}
	var decoded openAIImagesResponse
	switch a.editFormat {
	case "json":
		payload := a.payload(request)
		payload.Image, payload.Mask = request.ReferenceImages, request.MaskImage
		if err := a.transport.execute(ctx, http.MethodPost, "images/edits", payload, &decoded); err != nil {
			return nil, classifyOpenAIAdapterError(ctx, err)
		}
	case "multipart":
		if err := a.multipartEdit(ctx, request, &decoded); err != nil {
			return nil, err
		}
	default:
		return nil, newOpenAIProviderError(ErrorKindInvalidRequest, 0, false, "unsupported image edit format "+strconv.Quote(a.editFormat), nil)
	}
	return a.result(ctx, request, decoded)
}

func (a *OpenAIImagesAdapter) validateRequest(ctx context.Context, request *ProviderRequest) error {
	if a.transport.configErr != nil {
		return classifyOpenAIAdapterError(ctx, a.transport.configErr)
	}
	if strings.TrimSpace(request.Model) == "" && a.defaultModel == "" {
		return newGenericRoutingError("image model is required")
	}
	return nil
}

func (a *OpenAIImagesAdapter) payload(request *ProviderRequest) openAIImagesRequest {
	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = a.defaultModel
	}
	return openAIImagesRequest{Model: model, Prompt: request.Prompt, N: request.N, Size: request.Size, Quality: request.Params["quality"], Seed: request.Params["seed"], OutputFormat: request.Params["output_format"], ResponseFormat: request.Params["response_format"]}
}

func (a *OpenAIImagesAdapter) multipartEdit(ctx context.Context, request *ProviderRequest, decoded *openAIImagesResponse) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	payload := a.payload(request)
	fields := map[string]string{"model": payload.Model, "prompt": payload.Prompt, "size": payload.Size, "quality": payload.Quality, "seed": payload.Seed, "output_format": payload.OutputFormat, "response_format": payload.ResponseFormat}
	if payload.N != 0 {
		fields["n"] = strconv.Itoa(payload.N)
	}
	for name, value := range fields {
		if value != "" || name == "prompt" {
			if err := writer.WriteField(name, value); err != nil {
				return newOpenAIProviderError(ErrorKindInvalidRequest, 0, false, "encode edit form field", err)
			}
		}
	}
	imageField := "image"
	if len(request.ReferenceImages) > 1 {
		imageField = "image[]"
	}
	for index, reference := range request.ReferenceImages {
		image, err := a.inputImage(ctx, reference)
		if err != nil {
			return err
		}
		if err := writeOpenAIImagePart(writer, imageField, "image-"+strconv.Itoa(index), image); err != nil {
			return newOpenAIProviderError(ErrorKindInvalidRequest, 0, false, "encode edit image", err)
		}
	}
	if strings.TrimSpace(request.MaskImage) != "" {
		image, err := a.inputImage(ctx, request.MaskImage)
		if err != nil {
			return err
		}
		if err := writeOpenAIImagePart(writer, "mask", "mask", image); err != nil {
			return newOpenAIProviderError(ErrorKindInvalidRequest, 0, false, "encode edit mask", err)
		}
	}
	if err := writer.Close(); err != nil {
		return newOpenAIProviderError(ErrorKindInvalidRequest, 0, false, "finish edit form", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, joinOpenAIEndpoint(a.transport.baseURL, "images/edits"), &body)
	if err != nil {
		return newOpenAIProviderError(ErrorKindInvalidRequest, 0, false, "create edit request", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if a.transport.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.transport.apiKey)
	}
	response, err := a.transport.httpClient.Do(req)
	if err != nil {
		return classifyOpenAIAdapterError(ctx, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return classifyOpenAIAdapterError(ctx, a.transport.httpError(response))
	}
	if err := json.NewDecoder(response.Body).Decode(decoded); err != nil {
		return newOpenAIProviderError(ErrorKindInvalidResponse, response.StatusCode, true, "decode image edit response", err)
	}
	return nil
}

func writeOpenAIImagePart(writer *multipart.Writer, field, name string, image openAIImageData) error {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s.%s"`, field, name, image.format))
	header.Set("Content-Type", "image/"+image.format)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = part.Write(image.data)
	return err
}

func (a *OpenAIImagesAdapter) inputImage(ctx context.Context, reference string) (openAIImageData, error) {
	reference = strings.TrimSpace(reference)
	if strings.HasPrefix(reference, "http://") || strings.HasPrefix(reference, "https://") {
		return a.downloadImage(ctx, reference)
	}
	contentType := ""
	if strings.HasPrefix(reference, "data:") {
		comma := strings.IndexByte(reference, ',')
		if comma < 0 || !strings.HasPrefix(reference, "data:image/") || !strings.HasSuffix(reference[:comma], ";base64") {
			return openAIImageData{}, newOpenAIProviderError(ErrorKindInvalidRequest, 0, false, "edit image data URL must contain a base64 image", nil)
		}
		contentType = strings.TrimSuffix(strings.TrimPrefix(reference[:comma], "data:"), ";base64")
		reference = reference[comma+1:]
	}
	if base64.StdEncoding.DecodedLen(len(reference)) > maxGeneratedImageBytes {
		return openAIImageData{}, newOpenAIProviderError(ErrorKindInvalidRequest, 0, false, "edit image exceeds size limit", nil)
	}
	data, err := base64.StdEncoding.DecodeString(reference)
	if err != nil || len(data) == 0 {
		return openAIImageData{}, newOpenAIProviderError(ErrorKindInvalidRequest, 0, false, "edit image must be a data URL, base64 image, or HTTP URL", err)
	}
	format := openAIImageFormat(data, contentType)
	if format == "" {
		return openAIImageData{}, newOpenAIProviderError(ErrorKindInvalidRequest, 0, false, "edit image must use PNG, JPEG, or WebP", nil)
	}
	return openAIImageData{data: data, format: format}, nil
}

func (a *OpenAIImagesAdapter) result(ctx context.Context, request *ProviderRequest, decoded openAIImagesResponse) (*ProviderResult, error) {
	if len(decoded.Data) == 0 {
		return nil, newOpenAIProviderError(ErrorKindInvalidResponse, http.StatusOK, true, "image response contains no data", nil)
	}
	images := make([]string, 0, len(decoded.Data))
	outputFormat := decoded.OutputFormat
	if outputFormat == "" {
		outputFormat = request.Params["output_format"]
	}
	for _, item := range decoded.Data {
		if strings.TrimSpace(item.Base64) != "" {
			if outputFormat == "" {
				if data, err := base64.StdEncoding.DecodeString(item.Base64); err == nil {
					outputFormat = openAIImageFormat(data, "")
				}
			}
			images = append(images, item.Base64)
			continue
		}
		if strings.TrimSpace(item.URL) == "" {
			return nil, newOpenAIProviderError(ErrorKindInvalidResponse, http.StatusOK, true, "image response contains neither b64_json nor url", nil)
		}
		image, err := a.downloadImage(ctx, item.URL)
		if err != nil {
			return nil, err
		}
		if outputFormat != "" && outputFormat != image.format && (outputFormat != "jpg" || image.format != "jpeg") {
			return nil, newOpenAIProviderError(ErrorKindInvalidResponse, http.StatusOK, false, "image response has inconsistent output formats", nil)
		}
		outputFormat = image.format
		images = append(images, base64.StdEncoding.EncodeToString(image.data))
	}
	if outputFormat == "" {
		outputFormat = "png"
	}
	return &ProviderResult{Images: images, OutputFormat: outputFormat, Size: decoded.Size, CreatedAt: decoded.Created, Usage: Usage{TotalTokens: decoded.Usage.TotalTokens, InputTokens: decoded.Usage.InputTokens, OutputTokens: decoded.Usage.OutputTokens, TextToImageCount: decoded.Usage.TextToImageCount, ImageToImageCount: decoded.Usage.ImageToImageCount, RequestCount: decoded.Usage.RequestCount}}, nil
}

func (a *OpenAIImagesAdapter) downloadImage(ctx context.Context, value string) (openAIImageData, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(value), nil)
	if err != nil {
		return openAIImageData{}, newOpenAIProviderError(ErrorKindInvalidResponse, 0, false, "invalid image URL", err)
	}
	if err := validateGeneratedImageURL(req.URL); err != nil {
		return openAIImageData{}, newOpenAIProviderError(ErrorKindInvalidResponse, 0, false, "reject image URL", err)
	}
	response, err := a.downloadHTTPClient.Do(req)
	if err != nil {
		return openAIImageData{}, classifyOpenAIAdapterError(ctx, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return openAIImageData{}, newOpenAIProviderError(ErrorKindTransport, response.StatusCode, true, fmt.Sprintf("download image failed with status %d", response.StatusCode), nil)
	}
	if response.ContentLength > maxGeneratedImageBytes {
		return openAIImageData{}, newOpenAIProviderError(ErrorKindInvalidResponse, response.StatusCode, false, "downloaded image exceeds size limit", nil)
	}
	contentType := response.Header.Get("Content-Type")
	if contentType != "" && !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		return openAIImageData{}, newOpenAIProviderError(ErrorKindInvalidResponse, response.StatusCode, false, "downloaded image has non-image content type", nil)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxGeneratedImageBytes+1))
	if err != nil {
		return openAIImageData{}, newOpenAIProviderError(ErrorKindTransport, response.StatusCode, true, "read downloaded image", err)
	}
	if len(data) > maxGeneratedImageBytes {
		return openAIImageData{}, newOpenAIProviderError(ErrorKindInvalidResponse, response.StatusCode, false, "downloaded image exceeds size limit", nil)
	}
	if len(data) == 0 {
		return openAIImageData{}, newOpenAIProviderError(ErrorKindInvalidResponse, response.StatusCode, true, "downloaded image is empty", nil)
	}
	format := openAIImageFormat(data, contentType)
	if format == "" {
		return openAIImageData{}, newOpenAIProviderError(ErrorKindInvalidResponse, response.StatusCode, false, "downloaded image must use PNG, JPEG, or WebP", nil)
	}
	return openAIImageData{data: data, format: format}, nil
}

func openAIImageFormat(data []byte, contentType string) string {
	if bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		return "png"
	}
	if bytes.HasPrefix(data, []byte("\xff\xd8\xff")) {
		return "jpeg"
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "webp"
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	switch strings.ToLower(mediaType) {
	case "image/png":
		return "png"
	case "image/jpeg", "image/jpg":
		return "jpeg"
	case "image/webp":
		return "webp"
	}
	return ""
}

type openAIImageData struct {
	data   []byte
	format string
}

type openAIImagesRequest struct {
	Model          string   `json:"model"`
	Prompt         string   `json:"prompt"`
	Image          []string `json:"image,omitempty"`
	Mask           string   `json:"mask,omitempty"`
	N              int      `json:"n,omitempty"`
	Size           string   `json:"size,omitempty"`
	Quality        string   `json:"quality,omitempty"`
	Seed           string   `json:"seed,omitempty"`
	OutputFormat   string   `json:"output_format,omitempty"`
	ResponseFormat string   `json:"response_format,omitempty"`
}

type openAIImagesResponse struct {
	Created      int64  `json:"created"`
	OutputFormat string `json:"output_format"`
	Size         string `json:"size"`
	Data         []struct {
		Base64 string `json:"b64_json"`
		URL    string `json:"url"`
	} `json:"data"`
	Usage struct {
		TotalTokens       int `json:"total_tokens"`
		InputTokens       int `json:"input_tokens"`
		OutputTokens      int `json:"output_tokens"`
		TextToImageCount  int `json:"ti_quantity"`
		ImageToImageCount int `json:"ii_quantity"`
		RequestCount      int `json:"req_count"`
	} `json:"usage"`
}

var _ ImageProvider = (*OpenAIImagesAdapter)(nil)
