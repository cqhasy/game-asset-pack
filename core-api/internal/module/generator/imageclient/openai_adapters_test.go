package imageclient_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/1024XEngineer/Holonic-Asset/internal/module/generator/imageclient"
)

func TestOpenAIImagesAdapterGeneratesWithConfiguredModelAndParameters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("path = %s, want /v1/images/generations", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer image-key" {
			t.Fatalf("authorization = %q", got)
		}
		var payload struct {
			Model        string `json:"model"`
			Prompt       string `json:"prompt"`
			N            int    `json:"n"`
			Size         string `json:"size"`
			Quality      string `json:"quality"`
			Seed         string `json:"seed"`
			OutputFormat string `json:"output_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		want := struct {
			Model, Prompt                     string
			N                                 int
			Size, Quality, Seed, OutputFormat string
		}{
			"nebula/image-model", "pixel sword", 2, "1024x1024", "high", "17", "webp",
		}
		got := struct {
			Model, Prompt                     string
			N                                 int
			Size, Quality, Seed, OutputFormat string
		}{
			payload.Model, payload.Prompt, payload.N, payload.Size, payload.Quality, payload.Seed, payload.OutputFormat,
		}
		if got != want {
			t.Fatalf("payload = %+v, want %+v", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":123,"output_format":"webp","size":"1024x1024","data":[{"b64_json":"generated"}]}`)
	}))
	defer server.Close()

	provider := imageclient.NewOpenAIImagesAdapter(imageclient.OpenAIImagesAdapterConfig{
		BaseURL: server.URL, APIKey: "image-key", DefaultModel: "nebula/image-model",
	})
	result, err := provider.Generate(context.Background(), &imageclient.ProviderRequest{
		Prompt: "pixel sword", N: 2, Size: "1024x1024",
		Params: imageclient.Params{"quality": "high", "seed": "17", "output_format": "webp"},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	wantResult := &imageclient.ProviderResult{Images: []string{"generated"}, OutputFormat: "webp", Size: "1024x1024", CreatedAt: 123}
	if !reflect.DeepEqual(result, wantResult) {
		t.Fatalf("result = %+v, want %+v", result, wantResult)
	}
}

func TestOpenAIImagesAdapterEditsWithReferencesAndMask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" {
			t.Fatalf("path = %s, want /v1/images/edits", r.URL.Path)
		}
		var payload struct {
			Model, Prompt string
			Image         []string `json:"image"`
			Mask          string   `json:"mask"`
			N             int      `json:"n"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Model != "edit-model" || payload.Prompt != "make blue" ||
			!reflect.DeepEqual(payload.Image, []string{"https://example.test/ref.png"}) ||
			payload.Mask != "data:image/png;base64,mask" || payload.N != 2 {
			t.Fatalf("payload = %+v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"edited"}]}`)
	}))
	defer server.Close()

	provider := imageclient.NewOpenAIImagesAdapter(imageclient.OpenAIImagesAdapterConfig{BaseURL: server.URL, APIKey: "key", DefaultModel: "edit-model", EditFormat: "json"})
	result, err := provider.Edit(context.Background(), &imageclient.ProviderRequest{
		Prompt: "make blue", ReferenceImages: []string{"https://example.test/ref.png"}, MaskImage: "data:image/png;base64,mask", N: 2,
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if !reflect.DeepEqual(result.Images, []string{"edited"}) {
		t.Fatalf("images = %+v", result.Images)
	}
}

func TestOpenAIChatCompletionsAdapterExtractsInlineAndURLImages(t *testing.T) {
	raw := []byte("fake-png")
	inline := base64.StdEncoding.EncodeToString(raw)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":88,"choices":[{"message":{"content":"data:image/png;base64,`+inline+` https://images.example/result.png"}}]}`)
	}))
	defer server.Close()

	provider := imageclient.NewOpenAIChatCompletionsAdapter(imageclient.OpenAIChatCompletionsAdapterConfig{
		BaseURL: server.URL, APIKey: "chat-key", DefaultModel: "chat-model",
		DownloadHTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != "https://images.example/result.png" {
				t.Fatalf("download URL = %s", r.URL)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(raw)), Request: r}, nil
		})},
	})
	result, err := provider.Generate(context.Background(), &imageclient.ProviderRequest{Prompt: "a knight"})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !reflect.DeepEqual(result.Images, []string{inline, inline}) {
		t.Fatalf("images = %+v", result.Images)
	}
}

func TestOpenAIChatCompletionsAdapterPassesReferenceAndMaskImages(t *testing.T) {
	rawReference := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("raw-reference", 4)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model    string `json:"model"`
			Messages []struct {
				Content []struct {
					Type, Text string
					ImageURL   *struct {
						URL string `json:"url"`
					} `json:"image_url"`
				} `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Model != "chat-edit" || len(payload.Messages) != 1 || len(payload.Messages[0].Content) != 4 {
			t.Fatalf("payload = %+v", payload)
		}
		parts := payload.Messages[0].Content
		if parts[0].ImageURL == nil || parts[0].ImageURL.URL != "https://example.test/ref.png" {
			t.Fatalf("reference part = %+v", parts[0])
		}
		if parts[1].ImageURL == nil || parts[1].ImageURL.URL != "data:image/png;base64,"+rawReference {
			t.Fatalf("raw reference part = %+v", parts[1])
		}
		if parts[2].ImageURL == nil || parts[2].ImageURL.URL != "data:image/png;base64,mask" {
			t.Fatalf("mask part = %+v", parts[2])
		}
		if parts[3].Text != "edit instructions" {
			t.Fatalf("prompt part = %+v", parts[3])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"data:image/png;base64,`+base64.StdEncoding.EncodeToString([]byte("out"))+`"}}]}`)
	}))
	defer server.Close()

	provider := imageclient.NewOpenAIChatCompletionsAdapter(imageclient.OpenAIChatCompletionsAdapterConfig{BaseURL: server.URL, APIKey: "key", DefaultModel: "chat-edit"})
	_, err := provider.Edit(context.Background(), &imageclient.ProviderRequest{Prompt: "edit instructions", ReferenceImages: []string{"https://example.test/ref.png", rawReference}, MaskImage: "data:image/png;base64,mask"})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
}

func TestOpenAIAdaptersClassifyAuthenticationRateLimitAndServerErrors(t *testing.T) {
	statuses := []struct {
		status    int
		kind      imageclient.ErrorKind
		transient bool
	}{
		{http.StatusUnauthorized, imageclient.ErrorKindAuthentication, false}, {http.StatusTooManyRequests, imageclient.ErrorKindRateLimited, true}, {http.StatusBadGateway, imageclient.ErrorKindUnavailable, true},
	}
	for _, test := range statuses {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, `{"error":{"message":"provider failure"}}`)
			}))
			defer server.Close()
			provider := imageclient.NewOpenAIImagesAdapter(imageclient.OpenAIImagesAdapterConfig{BaseURL: server.URL, APIKey: "secret", DefaultModel: "model"})
			_, err := provider.Generate(context.Background(), &imageclient.ProviderRequest{Prompt: "test"})
			var providerErr *imageclient.ProviderError
			if !errors.As(err, &providerErr) || providerErr.Kind != test.kind || providerErr.Transient != test.transient {
				t.Fatalf("error = %v, want kind=%s transient=%t", err, test.kind, test.transient)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("error leaked API key: %v", err)
			}
		})
	}
}

func TestOpenAIAdaptersRejectEmptyImageResponses(t *testing.T) {
	for _, body := range []string{`{"data":[]}`, `{"choices":[{"message":{"content":"plain text"}}]}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			images := imageclient.NewOpenAIImagesAdapter(imageclient.OpenAIImagesAdapterConfig{BaseURL: server.URL, DefaultModel: "model"})
			if _, err := images.Generate(context.Background(), &imageclient.ProviderRequest{Prompt: "test"}); err == nil {
				t.Fatal("images adapter accepted empty response")
			}
			chat := imageclient.NewOpenAIChatCompletionsAdapter(imageclient.OpenAIChatCompletionsAdapterConfig{BaseURL: server.URL, DefaultModel: "model"})
			if _, err := chat.Generate(context.Background(), &imageclient.ProviderRequest{Prompt: "test"}); err == nil {
				t.Fatal("chat adapter accepted empty response")
			}
		})
	}
}
