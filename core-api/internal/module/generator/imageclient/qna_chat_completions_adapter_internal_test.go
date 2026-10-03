package imageclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type internalRoundTripFunc func(*http.Request) (*http.Response, error)

func (f internalRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (failingReader) Close() error             { return nil }

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}

func TestNewQNAChatCompletionsAdapterDefaults(t *testing.T) {
	var requestPath, requestModel string
	client := &http.Client{Transport: internalRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestPath = request.URL.String()
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requestModel = payload.Model
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"data:image/png;base64,aW1hZ2U="}}]}`)), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
	})}
	provider := NewQNAChatCompletionsAdapter(QNAChatCompletionsAdapterConfig{HTTPClient: client})
	if _, err := provider.Generate(context.Background(), &ProviderRequest{Prompt: "test"}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if requestPath != DefaultQNABaseURL+"/v1/chat/completions" || requestModel != DefaultQNAChatCompletionsModel {
		t.Fatalf("legacy defaults: URL=%q model=%q", requestPath, requestModel)
	}
}
func TestQNAChatCompletionsAdapterRejectsInvalidEndpoint(t *testing.T) {
	provider := NewQNAChatCompletionsAdapter(QNAChatCompletionsAdapterConfig{BaseURL: "://invalid"})
	_, err := provider.Generate(context.Background(), &ProviderRequest{Prompt: "test"})
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != ErrorKindInvalidRequest {
		t.Fatalf("generate error = %v, want invalid request", err)
	}
}

func TestClassifyChatRequestError(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		err       error
		wantKind  ErrorKind
		transient bool
	}{
		{
			name: "canceled context", ctx: canceledContext(), err: errors.New("request failed"),
			wantKind: ErrorKindCanceled, transient: false,
		},
		{
			name: "expired context", ctx: expiredContext(), err: errors.New("request failed"),
			wantKind: ErrorKindTimeout, transient: true,
		},
		{
			name: "client timeout", ctx: context.Background(),
			err:      &url.Error{Op: "Get", URL: "https://images.example", Err: timeoutError{}},
			wantKind: ErrorKindTimeout, transient: true,
		},
		{
			name: "transport failure", ctx: context.Background(), err: errors.New("connection refused"),
			wantKind: ErrorKindTransport, transient: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var providerErr *ProviderError
			if err := classifyOpenAIAdapterError(test.ctx, test.err); !errors.As(err, &providerErr) {
				t.Fatalf("classification error = %v, want provider error", err)
			}
			if providerErr.Kind != test.wantKind || providerErr.Transient != test.transient {
				t.Fatalf("classification = (%s, %t), want (%s, %t)",
					providerErr.Kind, providerErr.Transient, test.wantKind, test.transient)
			}
		})
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func expiredContext() context.Context {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	cancel()
	return ctx
}

func TestClassifyChatStatusCoversProviderResponses(t *testing.T) {
	tests := []struct {
		statusCode int
		wantKind   ErrorKind
		transient  bool
	}{
		{statusCode: http.StatusUnprocessableEntity, wantKind: ErrorKindInvalidRequest},
		{statusCode: http.StatusForbidden, wantKind: ErrorKindAuthentication},
		{statusCode: http.StatusRequestTimeout, wantKind: ErrorKindTimeout, transient: true},
		{statusCode: http.StatusInternalServerError, wantKind: ErrorKindUnavailable, transient: true},
		{statusCode: http.StatusBadGateway, wantKind: ErrorKindUnavailable, transient: true},
		{statusCode: http.StatusGatewayTimeout, wantKind: ErrorKindUnavailable, transient: true},
		{statusCode: 599, wantKind: ErrorKindUnavailable, transient: true},
		{statusCode: http.StatusTeapot, wantKind: ErrorKindInvalidRequest},
	}

	for _, test := range tests {
		kind, transient := classifyOpenAIStatus(test.statusCode)
		if kind != test.wantKind || transient != test.transient {
			t.Fatalf("status %d = (%s, %t), want (%s, %t)",
				test.statusCode, kind, transient, test.wantKind, test.transient)
		}
	}
}

func TestOpenAIErrorMessageShapes(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"top-level message", `{"message":"top level"}`, "top level"},
		{"nested text", `{"error":"nested text"}`, "nested text"},
		{"plain text fallback", "  upstream unavailable  ", "502 Bad Gateway"},
		{"empty object fallback", `{}`, "502 Bad Gateway"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := openAIResponseErrorMessage([]byte(test.body), "502 Bad Gateway"); got != test.want {
				t.Fatalf("message = %q, want %q", got, test.want)
			}
		})
	}
}
func TestChatMessageUnmarshalShapes(t *testing.T) {
	t.Run("malformed", func(t *testing.T) {
		var message openAIChatMessage
		if err := message.UnmarshalJSON([]byte(`{`)); err == nil {
			t.Fatal("malformed message was accepted")
		}
	})

	tests := []struct {
		name        string
		body        string
		wantContent any
		wantParts   int
	}{
		{name: "missing content", body: `{"role":"assistant"}`, wantContent: ""},
		{name: "string", body: `{"content":"hello"}`, wantContent: "hello"},
		{name: "parts", body: `{"content":[{"type":"text","text":"hello"}]}`, wantParts: 1},
		{name: "unknown object", body: `{"content":{"value":1}}`, wantContent: `{"value":1}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var message openAIChatMessage
			if err := message.UnmarshalJSON([]byte(test.body)); err != nil {
				t.Fatalf("unmarshal message: %v", err)
			}
			if test.wantParts > 0 {
				if len(message.ContentParts) != test.wantParts || message.contentString() != "" {
					t.Fatalf("unexpected content parts: %+v", message)
				}
				return
			}
			if message.Content != test.wantContent {
				t.Fatalf("content = %#v, want %#v", message.Content, test.wantContent)
			}
		})
	}
}

func TestChatCompletionsImageReferenceHelpers(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
	if got := formatOpenAIChatImageRef(""); got != "" {
		t.Fatalf("empty reference = %q", got)
	}
	if got := formatOpenAIChatImageRef(raw); got != "data:image/png;base64,"+raw {
		t.Fatalf("raw base64 reference = %q", got)
	}
	if got := formatOpenAIChatImageRef("asset-id"); got != "asset-id" {
		t.Fatalf("opaque reference = %q", got)
	}
	if got, err := parseOpenAIImageDataURL("data:image/png;base64," + raw); err != nil || got != raw {
		t.Fatalf("data URL payload = %q, error = %v", got, err)
	}
	if !isLikelyBase64("  " + raw + "\n") {
		t.Fatal("whitespace-wrapped base64 was rejected")
	}
}

func TestQNAChatCompletionsAdapterImageResolutionFailures(t *testing.T) {
	baseProvider := func(roundTrip internalRoundTripFunc) *QNAChatCompletionsAdapter {
		return NewQNAChatCompletionsAdapter(QNAChatCompletionsAdapterConfig{
			BaseURL:            "https://api.example",
			DownloadHTTPClient: &http.Client{Transport: roundTrip},
		})
	}

	t.Run("raw base64", func(t *testing.T) {
		raw := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
		provider := baseProvider(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("download must not run")
		})
		got, err := provider.resolveImageToB64(context.Background(), raw)
		if err != nil || got != raw {
			t.Fatalf("raw base64 result = %q, error = %v", got, err)
		}
	})

	t.Run("invalid data URL base64", func(t *testing.T) {
		provider := baseProvider(nil)
		_, err := provider.resolveImageToB64(context.Background(), "data:image/png;base64,not-base64")
		var providerErr *ProviderError
		if !errors.As(err, &providerErr) || providerErr.Kind != ErrorKindInvalidResponse || providerErr.Transient {
			t.Fatalf("data URL error = %v, want permanent invalid response", err)
		}
	})

	t.Run("malformed URL", func(t *testing.T) {
		provider := baseProvider(nil)
		_, err := provider.resolveImageToB64(context.Background(), "http://[::1")
		if err == nil {
			t.Fatal("malformed URL was accepted")
		}
	})

	t.Run("transport failure", func(t *testing.T) {
		provider := baseProvider(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection reset")
		})
		_, err := provider.resolveImageToB64(context.Background(), "https://images.example/output.png")
		var providerErr *ProviderError
		if !errors.As(err, &providerErr) || providerErr.Kind != ErrorKindTransport {
			t.Fatalf("download error = %v, want transport error", err)
		}
	})

	t.Run("body read failure", func(t *testing.T) {
		provider := baseProvider(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"image/png"}},
				Body:       failingReader{},
				Request:    request,
			}, nil
		})
		_, err := provider.resolveImageToB64(context.Background(), "https://images.example/output.png")
		if err == nil || !strings.Contains(err.Error(), "read downloaded image data") {
			t.Fatalf("download error = %v, want body read failure", err)
		}
	})

	t.Run("streamed response exceeds limit", func(t *testing.T) {
		provider := baseProvider(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				Header:        http.Header{"Content-Type": []string{"image/png"}},
				Body:          io.NopCloser(io.LimitReader(zeroReader{}, maxGeneratedImageBytes+1)),
				ContentLength: -1,
				Request:       request,
			}, nil
		})
		_, err := provider.resolveImageToB64(context.Background(), "https://images.example/output.png")
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("download error = %v, want size rejection", err)
		}
	})
}

func TestQNAChatCompletionsAdapterExtractImageBranches(t *testing.T) {
	provider := NewQNAChatCompletionsAdapter(QNAChatCompletionsAdapterConfig{
		BaseURL: "https://api.example",
		DownloadHTTPClient: &http.Client{Transport: internalRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"image/png"}},
				Body:       io.NopCloser(strings.NewReader("image")),
				Request:    request,
			}, nil
		})},
	})

	tests := []struct {
		name    string
		choice  openAIChatChoice
		wantErr bool
	}{
		{
			name: "images field error",
			choice: openAIChatChoice{Message: openAIChatMessage{Images: []openAIChatContentPart{{
				Type: "image_url", ImageURL: &openAIChatImageURL{URL: "http://127.0.0.1/image.png"},
			}}}},
			wantErr: true,
		},
		{
			name: "content parts error",
			choice: openAIChatChoice{Message: openAIChatMessage{ContentParts: []openAIChatContentPart{{
				Type: "image_url", ImageURL: &openAIChatImageURL{URL: "http://127.0.0.1/image.png"},
			}}}},
			wantErr: true,
		},
		{
			name:    "markdown error",
			choice:  openAIChatChoice{Message: openAIChatMessage{Content: "![image](http://127.0.0.1/image.png)"}},
			wantErr: true,
		},
		{
			name:    "plain URL error",
			choice:  openAIChatChoice{Message: openAIChatMessage{Content: "http://127.0.0.1/image.png"}},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := provider.extractImages(context.Background(), []openAIChatChoice{test.choice})
			if (err != nil) != test.wantErr {
				t.Fatalf("extract error = %v, wantErr = %t", err, test.wantErr)
			}
		})
	}

	images, err := provider.extractImages(context.Background(), []openAIChatChoice{{
		Message: openAIChatMessage{Content: "download https://images.example/output.png"},
	}})
	if err != nil || len(images) != 1 {
		t.Fatalf("plain URL images = %v, error = %v", images, err)
	}
}
