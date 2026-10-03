package imageclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAITransportNormalizesBaseURLAndAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/images/generations" {
			t.Errorf("path = %q, want /v1/images/generations", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("authorization = %q, want Bearer test-key", request.Header.Get("Authorization"))
		}
		if request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type = %q, want application/json", request.Header.Get("Content-Type"))
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if payload["prompt"] != "a pixel sword" {
			t.Errorf("prompt = %v, want a pixel sword", payload["prompt"])
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	transport := newOpenAITransport(server.URL, "test-key", server.Client())
	var response struct {
		OK bool `json:"ok"`
	}
	if err := transport.execute(
		context.Background(),
		http.MethodPost,
		"v1/images/generations",
		map[string]any{"prompt": "a pixel sword"},
		&response,
	); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !response.OK {
		t.Fatal("response ok = false, want true")
	}
}

func TestOpenAITransportAcceptsBaseURLWithV1Suffix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/images/generations" {
			t.Errorf("path = %q, want /v1/images/generations", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{}`))
	}))
	defer server.Close()

	transport := newOpenAITransport(server.URL+"/v1/", "", server.Client())
	if err := transport.execute(
		context.Background(),
		http.MethodPost,
		"/v1/images/generations",
		struct{}{},
		nil,
	); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestOpenAITransportReportsStructuredHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"error":{"message":"rate limit exceeded"}}`))
	}))
	defer server.Close()

	transport := newOpenAITransport(server.URL, "test-key", server.Client())
	err := transport.execute(context.Background(), http.MethodPost, "/images/generations", struct{}{}, nil)
	if err == nil {
		t.Fatal("execute error = nil, want HTTP error")
	}
	var httpErr *openAIHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error type = %T, want *openAIHTTPError", err)
	}
	if httpErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status code = %d, want %d", httpErr.StatusCode, http.StatusTooManyRequests)
	}
	if httpErr.Message != "rate limit exceeded" {
		t.Fatalf("message = %q, want rate limit exceeded", httpErr.Message)
	}
}

func TestOpenAITransportDoesNotExposeAPIKeyInErrors(t *testing.T) {
	const apiKey = "super-secret-key"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error":{"message":"invalid credential super-secret-key"}}`))
	}))
	defer server.Close()

	transport := newOpenAITransport(server.URL, apiKey, server.Client())
	err := transport.execute(context.Background(), http.MethodPost, "/images/generations", struct{}{}, nil)
	if err == nil {
		t.Fatal("execute error = nil, want HTTP error")
	}
	if strings.Contains(err.Error(), apiKey) {
		t.Fatalf("error %q exposes API key", err)
	}
	var httpErr *openAIHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error type = %T, want *openAIHTTPError", err)
	}
	if strings.Contains(httpErr.Message, apiKey) {
		t.Fatalf("HTTP error message %q exposes API key", httpErr.Message)
	}
}
