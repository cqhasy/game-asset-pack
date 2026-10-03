package imageclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIEndpointConfigurationIsPermanentAndNeverCallsHTTP(t *testing.T) {
	for _, endpoint := range []string{"", "://invalid", "ftp://example.com", "https://user:password@example.com", "https://example.com?token=secret"} {
		t.Run(endpoint, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: internalRoundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("should not execute invalid config")
			})}
			adapter := NewOpenAIChatCompletionsAdapter(OpenAIChatCompletionsAdapterConfig{BaseURL: endpoint, DefaultModel: "model", HTTPClient: client})
			_, err := adapter.Generate(context.Background(), &ProviderRequest{Prompt: "test"})
			var providerErr *ProviderError
			if !errors.As(err, &providerErr) || providerErr.Kind != ErrorKindInvalidRequest || providerErr.Transient || calls != 0 {
				t.Fatalf("error=%v calls=%d, want permanent invalid request before network", err, calls)
			}
		})
	}
}

func TestChatImageModelMustBeConfigured(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: internalRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("should not execute without model")
	})}
	adapter := NewOpenAIChatCompletionsAdapter(OpenAIChatCompletionsAdapterConfig{BaseURL: "https://example.com", HTTPClient: client})
	_, err := adapter.Generate(context.Background(), &ProviderRequest{Prompt: "test"})
	if !IsPermanent(err) || calls != 0 {
		t.Fatalf("error=%v calls=%d, want required model", err, calls)
	}
}

func TestChatMarkdownInlineImageIsExtractedOnce(t *testing.T) {
	const payload = "aW1hZ2U="
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"![result](data:image/png;base64,` + payload + `)"}}]}`))
	}))
	defer server.Close()
	adapter := NewOpenAIChatCompletionsAdapter(OpenAIChatCompletionsAdapterConfig{BaseURL: server.URL, DefaultModel: "model"})
	result, err := adapter.Generate(context.Background(), &ProviderRequest{Prompt: "test"})
	if err != nil || len(result.Images) != 1 || result.Images[0] != payload {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestChatGeneratedJPEGHasCorrectMediaType(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte{0xff, 0xd8, 0xff, 0xe0, 0, 0x10, 'J', 'F', 'I', 'F', 0})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"data:image/jpeg;base64,` + payload + `"}}]}`))
	}))
	defer server.Close()
	service := NewImageGenerationService(NewOpenAIChatCompletionsAdapter(OpenAIChatCompletionsAdapterConfig{BaseURL: server.URL, DefaultModel: "model"}))
	result, err := service.Generate(context.Background(), &GenerateRequest{Prompt: "test"})
	if err != nil || result.Images[0].MediaType != "image/jpeg" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestProviderErrorFormattingIgnoresLegacyProviderLabel(t *testing.T) {
	err := &ProviderError{Provider: "qna", Kind: ErrorKindAuthentication, Message: "invalid credential"}
	if strings.Contains(err.Error(), "qna") || err.Error() != "image provider: invalid credential" {
		t.Fatal(err)
	}
}

func TestOpenAIStatusClassifiesOtherClientErrorsAsInvalidRequest(t *testing.T) {
	kind, transient := classifyOpenAIStatus(http.StatusNotFound)
	if kind != ErrorKindInvalidRequest || transient {
		t.Fatalf("404=(%s,%v)", kind, transient)
	}
}

func TestMultipartEditInvalidEndpointNeverDownloadsOrCallsHTTP(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: internalRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected network call")
	})}
	adapter := NewOpenAIImagesAdapter(OpenAIImagesAdapterConfig{BaseURL: "", DefaultModel: "model", HTTPClient: client, DownloadHTTPClient: client})
	_, err := adapter.Edit(context.Background(), &ProviderRequest{ReferenceImages: []string{"https://example.com/input.png"}})
	if !IsPermanent(err) || calls != 0 {
		t.Fatalf("error=%v calls=%d, want invalid endpoint before any network access", err, calls)
	}
}

func TestOpenAIImagesModelMustBeConfigured(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: internalRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected network call")
	})}
	adapter := NewOpenAIImagesAdapter(OpenAIImagesAdapterConfig{BaseURL: "https://example.com", HTTPClient: client})
	_, err := adapter.Generate(context.Background(), &ProviderRequest{Prompt: "test"})
	if !IsPermanent(err) || calls != 0 {
		t.Fatalf("error=%v calls=%d, want required model", err, calls)
	}
}

func TestChatSeedUsesOpenAIIntegerType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["seed"] != float64(17) {
			t.Errorf("seed=%#v, want JSON integer 17", payload["seed"])
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"data:image/png;base64,aW1hZ2U="}}]}`))
	}))
	defer server.Close()
	adapter := NewOpenAIChatCompletionsAdapter(OpenAIChatCompletionsAdapterConfig{BaseURL: server.URL, DefaultModel: "model"})
	if _, err := adapter.Generate(context.Background(), &ProviderRequest{Params: Params{"seed": "17"}}); err != nil {
		t.Fatal(err)
	}
}

func TestFactoryEditFormatInheritance(t *testing.T) {
	for _, test := range []struct {
		name, global, model, want string
		topLevel                  bool
	}{
		{name: "legacy default", want: "application/json"},
		{name: "global multipart", global: "multipart", want: "multipart/form-data"},
		{name: "model overrides json", global: "json", model: "multipart", want: "multipart/form-data"},
		{name: "model overrides multipart", global: "multipart", model: "json", want: "application/json"},
		{name: "top level legacy", want: "application/json", topLevel: true},
		{name: "top level multipart", global: "multipart", want: "multipart/form-data", topLevel: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/images/edits" || !strings.HasPrefix(r.Header.Get("Content-Type"), test.want) {
					t.Errorf("path=%s content-type=%s, want edits %s", r.URL.Path, r.Header.Get("Content-Type"), test.want)
				}
				_, _ = w.Write([]byte(`{"data":[{"b64_json":"aW1hZ2U="}]}`))
			}))
			defer server.Close()
			cfg := FactoryConfig{BaseURL: server.URL, DefaultModel: "model", EditFormat: test.global}
			if !test.topLevel {
				cfg.Models = []ModelConfig{{Name: "model", Protocol: "openai_images", EditFormat: test.model}}
			}
			provider := NewImageProvider(cfg)
			png := base64.StdEncoding.EncodeToString([]byte{137, 'P', 'N', 'G', 13, 10, 26, 10})
			if _, err := provider.Edit(context.Background(), &ProviderRequest{ReferenceImages: []string{"data:image/png;base64," + png}}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestChatMixedImagesHaveIndividualMediaTypes(t *testing.T) {
	jpeg := base64.StdEncoding.EncodeToString([]byte{255, 216, 255, 224})
	png := base64.StdEncoding.EncodeToString([]byte{137, 'P', 'N', 'G', 13, 10, 26, 10})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"images":[{"image_url":{"url":"data:image/jpeg;base64,` + jpeg + `"}},{"image_url":{"url":"data:image/png;base64,` + png + `"}}]}}]}`))
	}))
	defer server.Close()
	service := NewImageGenerationService(NewOpenAIChatCompletionsAdapter(OpenAIChatCompletionsAdapterConfig{BaseURL: server.URL, DefaultModel: "model"}))
	result, err := service.Generate(context.Background(), &GenerateRequest{Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Images) != 2 || result.Images[0].MediaType != "image/jpeg" || result.Images[1].MediaType != "image/png" {
		t.Fatalf("images=%+v", result.Images)
	}
}

func TestChatContentTextPartsCanContainGeneratedImages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":[{"type":"text","text":"![result](data:image/png;base64,aW1hZ2U=)"}]}}]}`))
	}))
	defer server.Close()
	adapter := NewOpenAIChatCompletionsAdapter(OpenAIChatCompletionsAdapterConfig{BaseURL: server.URL, DefaultModel: "model"})
	result, err := adapter.Generate(context.Background(), &ProviderRequest{})
	if err != nil || len(result.Images) != 1 || result.Images[0] != "aW1hZ2U=" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestChatRawJPEGReferenceUsesCorrectDataURLType(t *testing.T) {
	jpeg := base64.StdEncoding.EncodeToString(append([]byte{255, 216, 255}, []byte(strings.Repeat("image", 10))...))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload openAIChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		parts, ok := payload.Messages[0].Content.([]openAIChatContentPart)
		if !ok {
			t.Errorf("content=%#v", payload.Messages[0].Content)
		} else {
			url := parts[0].ImageURL.URL
			if url != "data:image/jpeg;base64,"+jpeg {
				t.Errorf("reference=%v", url)
			}
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"data:image/png;base64,aW1hZ2U="}}]}`)
	}))
	defer server.Close()
	adapter := NewOpenAIChatCompletionsAdapter(OpenAIChatCompletionsAdapterConfig{BaseURL: server.URL, DefaultModel: "model"})
	if _, err := adapter.Edit(context.Background(), &ProviderRequest{ReferenceImages: []string{jpeg}}); err != nil {
		t.Fatal(err)
	}
}

func TestChatDownloadCancellationIsClassifiedAndPreservesCause(t *testing.T) {
	adapter := NewOpenAIChatCompletionsAdapter(OpenAIChatCompletionsAdapterConfig{BaseURL: "https://example.com", DownloadHTTPClient: &http.Client{Transport: internalRoundTripFunc(func(*http.Request) (*http.Response, error) { return nil, context.Canceled })}})
	_, err := adapter.resolveImageToB64(context.Background(), "https://images.example/output.png")
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != ErrorKindCanceled || providerErr.Transient || !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want permanent canceled", err)
	}
}
