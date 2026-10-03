package imageclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type coverageImageAdapter struct {
	editErr error
}

func (*coverageImageAdapter) Generate(context.Context, *ProviderRequest) (*ProviderResult, error) {
	return &ProviderResult{Images: []string{"image"}}, nil
}

func (a *coverageImageAdapter) Edit(context.Context, *ProviderRequest) (*ProviderResult, error) {
	return nil, a.editErr
}

type coverageImageProvider struct {
	cancel context.CancelFunc
}

func (p *coverageImageProvider) Generate(context.Context, *ProviderRequest) (*ProviderResult, error) {
	if p.cancel != nil {
		p.cancel()
	}
	return nil, &ProviderError{Kind: ErrorKindUnavailable, Transient: true, Message: "retry"}
}

func (*coverageImageProvider) Edit(context.Context, *ProviderRequest) (*ProviderResult, error) {
	return nil, errors.New("unexpected edit")
}

func TestImageCoverageLegacyFactoryFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/images/generations":
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = writer.Write([]byte(`{"error":{"message":"overloaded"}}`))
		case "/v1/chat/completions":
			_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"data:image/png;base64,aW1hZ2U="}}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	provider := NewImageProvider(FactoryConfig{
		BaseURL:       server.URL,
		DefaultModel:  "openai/gpt-image-2",
		FallbackModel: "google/gemini-image",
	})
	result, err := provider.Generate(context.Background(), &ProviderRequest{Prompt: "test"})
	if err != nil || len(result.Images) != 1 {
		t.Fatalf("legacy fallback result = %+v, error = %v", result, err)
	}
}

func TestImageCoverageProtocolHelpers(t *testing.T) {
	adapter := NewOpenAIChatCompletionsAdapter(OpenAIChatCompletionsAdapterConfig{})
	if _, err := adapter.extractImages(context.Background(), []openAIChatChoice{{
		Message: openAIChatMessage{Content: "data:image/png;base64,not-base64"},
	}}); err == nil {
		t.Fatal("invalid inline image was accepted")
	}

	for _, value := range []string{"not-a-data-url", "data:image/png;base64,"} {
		if _, err := parseOpenAIImageDataURL(value); err == nil {
			t.Fatalf("invalid data URL %q was accepted", value)
		}
	}
	if got := openAIResponseErrorMessage([]byte(`{"error":{"message":"nested"}}`), "502 Bad Gateway"); got != "nested" {
		t.Fatalf("nested error message = %q", got)
	}

	dialer := generatedImageDialer{}
	addresses, err := dialer.resolve(context.Background(), "127.0.0.1")
	if err != nil || len(addresses) != 1 || addresses[0].String() != "127.0.0.1" {
		t.Fatalf("literal address resolution = %v, error = %v", addresses, err)
	}

	providerErr := &ProviderError{Kind: ErrorKindInvalidRequest, StatusCode: http.StatusUnprocessableEntity}
	if shouldRetryQNAEditWithoutMask(&ProviderRequest{MaskImage: "mask"}, providerErr) {
		t.Fatal("non-400 mask error was retried")
	}
	if got := normalizeQNAImageSize("invalid"); got != "invalid" {
		t.Fatalf("invalid size = %q", got)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	classifications := []error{
		classifyOpenAIAdapterError(canceled, errors.New("transport")),
		classifyOpenAIAdapterError(context.Background(), context.DeadlineExceeded),
		classifyOpenAIAdapterError(context.Background(), errors.New("transport")),
	}
	wantKinds := []ErrorKind{ErrorKindCanceled, ErrorKindTimeout, ErrorKindTransport}
	for index, err := range classifications {
		var classified *ProviderError
		if !errors.As(err, &classified) || classified.Kind != wantKinds[index] {
			t.Fatalf("classification %d = %v, want %s", index, err, wantKinds[index])
		}
	}
	if kind, transient := classifyOpenAIStatus(http.StatusTeapot); kind != ErrorKindInvalidRequest || transient {
		t.Fatalf("teapot classification = (%s, %t)", kind, transient)
	}
	cause := errors.New("cause message")
	if got := newOpenAIProviderError(ErrorKindTransport, 0, true, "", cause).Message; got != cause.Error() {
		t.Fatalf("derived message = %q", got)
	}
}

func TestImageCoverageGatewayEditFailure(t *testing.T) {
	wantErr := errors.New("edit failed")
	provider := &QNAProvider{
		defaultModel: "model",
		adapters: map[string]protocolAdapter{
			"model": &coverageImageAdapter{editErr: wantErr},
		},
	}
	if _, err := provider.Edit(context.Background(), &ProviderRequest{}); !errors.Is(err, wantErr) {
		t.Fatalf("edit error = %v, want %v", err, wantErr)
	}
	if _, err := provider.Edit(context.Background(), nil); err == nil {
		t.Fatal("nil edit request was accepted")
	}
}

func TestImageCoverageHTTPErrorClassification(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, err := range []error{
		classifyOpenAIAdapterError(canceled, errors.New("request")),
		classifyOpenAIAdapterError(context.Background(), &openAIHTTPError{StatusCode: http.StatusInternalServerError, Message: "failure"}),
		classifyOpenAIAdapterError(context.Background(), errors.New("transport")),
	} {
		var providerErr *ProviderError
		if !errors.As(err, &providerErr) {
			t.Fatalf("classification = %T, want ProviderError", err)
		}
	}
}
func TestImageCoverageCanceledRetryBackoff(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	provider := &coverageImageProvider{}
	_, err := NewImageGenerationService(provider).Generate(ctx, &GenerateRequest{
		Prompt:      "test",
		MaxAttempts: 2,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("retry error = %v, want deadline exceeded", err)
	}

	canceled, stop := context.WithCancel(context.Background())
	stop()
	if err := backoffSleep(canceled, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("backoff error = %v", err)
	}
}
