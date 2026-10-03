package imageclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

type compatibilityExecutorStub struct {
	path    string
	payload map[string]any
	err     error
}

func (s *compatibilityExecutorStub) Execute(_ context.Context, _ string, path string, params, result any) error {
	s.path = path
	if s.err != nil {
		return s.err
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(encoded, &s.payload); err != nil {
		return err
	}
	return json.Unmarshal([]byte(`{"data":[{"b64_json":"generated"}]}`), result)
}

type compatibilityHTTPError struct {
	StatusCode int
	Message    string
	Cause      error
}

func (e *compatibilityHTTPError) Error() string { return e.Message }
func (e *compatibilityHTTPError) Unwrap() error { return e.Cause }

func TestQNACompatibilityClassifiesInjectedExecutorError(t *testing.T) {
	executor := &compatibilityExecutorStub{err: fmt.Errorf("wrapped: %w", &compatibilityHTTPError{StatusCode: http.StatusTooManyRequests, Message: "invalid key secret-key"})}
	adapter := NewQNAImagesAdapter(QNAImagesAdapterConfig{APIKey: "secret-key", SDKClient: executor})
	_, err := adapter.Generate(context.Background(), &ProviderRequest{Prompt: "test"})
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != ErrorKindRateLimited || !providerErr.Transient {
		t.Fatalf("error = %v, want transient rate limit", err)
	}
	if providerErr.Message != "invalid key [redacted]" {
		t.Fatalf("message = %q", providerErr.Message)
	}
}

func TestQNACompatibilityUsesInjectedExecutor(t *testing.T) {
	executor := &compatibilityExecutorStub{}
	adapter := NewQNAImagesAdapter(QNAImagesAdapterConfig{SDKClient: executor})
	result, err := adapter.Generate(context.Background(), &ProviderRequest{Prompt: "test", Size: "256x256"})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if executor.path != "images/generations" || executor.payload["size"] != "1024x1024" {
		t.Fatalf("legacy request = path %q payload %#v", executor.path, executor.payload)
	}
	if !reflect.DeepEqual(result.Images, []string{"generated"}) {
		t.Fatalf("images = %v", result.Images)
	}
}

func TestQNACompatibilityTypedNilExecutorUsesHTTP(t *testing.T) {
	var executor *compatibilityExecutorStub
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"generated"}],"choices":[{"message":{"content":"data:image/png;base64,aW1hZ2U="}}]}`))
	}))
	defer server.Close()
	for _, adapter := range []ImageProvider{
		NewQNAImagesAdapter(QNAImagesAdapterConfig{BaseURL: server.URL, SDKClient: executor}),
		NewQNAChatCompletionsAdapter(QNAChatCompletionsAdapterConfig{BaseURL: server.URL, SDKClient: executor}),
	} {
		if _, err := adapter.Generate(context.Background(), &ProviderRequest{Prompt: "test"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestQNACompatibilityPreservesInjectedErrorCause(t *testing.T) {
	cause := errors.New("read error")
	executor := &compatibilityExecutorStub{err: &compatibilityHTTPError{StatusCode: http.StatusBadGateway, Message: "upstream failed", Cause: cause}}
	adapter := NewQNAImagesAdapter(QNAImagesAdapterConfig{SDKClient: executor})
	_, err := adapter.Generate(context.Background(), &ProviderRequest{})
	if !errors.Is(err, cause) {
		t.Fatalf("error=%v, missing cause", err)
	}
}
