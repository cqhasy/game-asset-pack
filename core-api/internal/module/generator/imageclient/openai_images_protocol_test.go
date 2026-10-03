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

func TestOpenAIImagesAdapterPreservesSizeAndOmitsOptionalParameters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode generation: %v", err)
			return
		}
		want := map[string]any{"model": "dall-e-2", "prompt": "small sprite", "size": "256x256"}
		if !reflect.DeepEqual(payload, want) {
			t.Errorf("generation = %#v, want %#v", payload, want)
		}
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"image"}]}`)
	}))
	defer server.Close()
	adapter := imageclient.NewOpenAIImagesAdapter(imageclient.OpenAIImagesAdapterConfig{BaseURL: server.URL, DefaultModel: "dall-e-2"})
	if _, err := adapter.Generate(context.Background(), &imageclient.ProviderRequest{Prompt: "small sprite", Size: "256x256"}); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAIImagesAdapterPassesExplicitResponseFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		if payload["response_format"] != "b64_json" {
			t.Errorf("response_format = %#v", payload["response_format"])
		}
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"image"}]}`)
	}))
	defer server.Close()
	adapter := imageclient.NewOpenAIImagesAdapter(imageclient.OpenAIImagesAdapterConfig{BaseURL: server.URL, DefaultModel: "test-image-model"})
	if _, err := adapter.Generate(context.Background(), &imageclient.ProviderRequest{Prompt: "sprite", Params: imageclient.Params{"response_format": "b64_json"}}); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAIImagesAdapterDownloadsURLResponse(t *testing.T) {
	for _, test := range []struct {
		format, contentType string
		data                []byte
	}{
		{"png", "image/png", []byte("\x89PNG\r\n\x1a\nimage")},
		{"jpeg", "image/jpeg", []byte("\xff\xd8\xffimage")},
		{"webp", "", []byte("RIFF\x08\x00\x00\x00WEBPVP8 ")},
	} {
		t.Run(test.format, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"data":[{"url":"https://cdn.example/image"}]}`)
			}))
			defer server.Close()
			adapter := imageclient.NewOpenAIImagesAdapter(imageclient.OpenAIImagesAdapterConfig{
				BaseURL: server.URL, DefaultModel: "test-image-model", APIKey: "provider-key",
				DownloadHTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					if r.URL.String() != "https://cdn.example/image" || r.Header.Get("Authorization") != "" {
						t.Errorf("download request = %s, authorization=%q", r.URL, r.Header.Get("Authorization"))
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{test.contentType}}, Body: io.NopCloser(bytes.NewReader(test.data)), Request: r}, nil
				})},
			})
			result, err := adapter.Generate(context.Background(), &imageclient.ProviderRequest{Prompt: "sprite"})
			if err != nil {
				t.Fatal(err)
			}
			if result.OutputFormat != test.format || !reflect.DeepEqual(result.Images, []string{base64.StdEncoding.EncodeToString(test.data)}) {
				t.Fatalf("result = %+v, want %s image", result, test.format)
			}
		})
	}
}

func TestOpenAIImagesAdapterRejectsPrivateOutputURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"url":"http://127.0.0.1/image.png"}]}`)
	}))
	defer server.Close()
	adapter := imageclient.NewOpenAIImagesAdapter(imageclient.OpenAIImagesAdapterConfig{BaseURL: server.URL, DefaultModel: "test-image-model",
		DownloadHTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("private image URL reached download transport")
			return nil, errors.New("unexpected download")
		})},
	})
	_, err := adapter.Generate(context.Background(), &imageclient.ProviderRequest{Prompt: "sprite"})
	var providerErr *imageclient.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != imageclient.ErrorKindInvalidResponse || providerErr.Transient {
		t.Fatalf("error = %v, want permanent invalid response", err)
	}
}

func TestOpenAIImagesAdapterEditsWithMultipartFiles(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nreference")
	jpeg := []byte("\xff\xd8\xffreference")
	mask := []byte("\x89PNG\r\n\x1a\nmask")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" || !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Errorf("edit request: path=%s content-type=%s", r.URL.Path, r.Header.Get("Content-Type"))
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "Bearer edit-key" {
			t.Error("missing bearer key")
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseMultipartForm(1 << 20); err != nil { // #nosec G120 -- request body is limited by MaxBytesReader above.
			t.Error(err)
			return
		}
		defer func() { _ = r.MultipartForm.RemoveAll() }()
		wantValues := map[string][]string{"model": {"edit-model"}, "prompt": {"make blue"}, "n": {"2"}, "size": {"256x256"}, "quality": {"high"}}
		if !reflect.DeepEqual(r.MultipartForm.Value, wantValues) {
			t.Errorf("fields = %#v", r.MultipartForm.Value)
		}
		files := r.MultipartForm.File["image[]"]
		if len(files) != 3 {
			t.Errorf("image files = %d, want 3", len(files))
			return
		}
		for index, want := range [][]byte{png, jpeg, png} {
			file, err := files[index].Open()
			if err != nil {
				t.Error(err)
				return
			}
			data, err := io.ReadAll(file)
			_ = file.Close()
			if err != nil {
				t.Error(err)
				return
			}
			if !bytes.Equal(data, want) {
				t.Errorf("image %d = %v, want %v", index, data, want)
			}
		}
		maskFiles := r.MultipartForm.File["mask"]
		if len(maskFiles) != 1 {
			t.Errorf("mask files = %d", len(maskFiles))
			return
		}
		file, err := maskFiles[0].Open()
		if err != nil {
			t.Error(err)
			return
		}
		data, err := io.ReadAll(file)
		_ = file.Close()
		if err != nil {
			t.Error(err)
			return
		}
		if !bytes.Equal(data, mask) {
			t.Errorf("mask = %v", data)
		}
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"edited"}]}`)
	}))
	defer server.Close()
	adapter := imageclient.NewOpenAIImagesAdapter(imageclient.OpenAIImagesAdapterConfig{BaseURL: server.URL, APIKey: "edit-key", DefaultModel: "edit-model",
		DownloadHTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(png)), Request: r}, nil
		})},
	})
	_, err := adapter.Edit(context.Background(), &imageclient.ProviderRequest{Prompt: "make blue", N: 2, Size: "256x256", Params: imageclient.Params{"quality": "high"},
		ReferenceImages: []string{"data:image/png;base64," + base64.StdEncoding.EncodeToString(png), base64.StdEncoding.EncodeToString(jpeg), "https://cdn.example/ref.png"},
		MaskImage:       "data:image/png;base64," + base64.StdEncoding.EncodeToString(mask),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenAIImagesAdapterJSONEditKeepsMaskOnRejection(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["mask"] != "data:image/png;base64,mask" {
			t.Errorf("mask = %#v", payload["mask"])
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"mask must be an object"}}`)
	}))
	defer server.Close()
	adapter := imageclient.NewOpenAIImagesAdapter(imageclient.OpenAIImagesAdapterConfig{BaseURL: server.URL, DefaultModel: "test-image-model", EditFormat: "json"})
	_, err := adapter.Edit(context.Background(), &imageclient.ProviderRequest{Prompt: "edit", ReferenceImages: []string{"ref"}, MaskImage: "data:image/png;base64,mask"})
	if err == nil || calls != 1 {
		t.Fatalf("calls=%d error=%v, want one failure without mask removal", calls, err)
	}
}

func TestOpenAIImagesAdapterMultipartEditKeepsMaskOnRejection(t *testing.T) {
	image := []byte("\x89PNG\r\n\x1a\nimage")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseMultipartForm(1 << 20); err != nil { // #nosec G120 -- request body is limited by MaxBytesReader above.
			t.Error(err)
			return
		}
		defer func() { _ = r.MultipartForm.RemoveAll() }()
		if len(r.MultipartForm.File["mask"]) != 1 {
			t.Error("edit omitted mask")
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"unable to download content from the provided URL"}}`)
	}))
	defer server.Close()
	adapter := imageclient.NewOpenAIImagesAdapter(imageclient.OpenAIImagesAdapterConfig{BaseURL: server.URL, DefaultModel: "test-image-model"})
	_, err := adapter.Edit(context.Background(), &imageclient.ProviderRequest{Prompt: "edit", ReferenceImages: []string{base64.StdEncoding.EncodeToString(image)}, MaskImage: base64.StdEncoding.EncodeToString(image)})
	if err == nil || calls != 1 {
		t.Fatalf("calls=%d error=%v, want one rejection", calls, err)
	}
}

func TestOpenAIImagesAdapterSingleMultipartImageUsesImageField(t *testing.T) {
	image := []byte("\x89PNG\r\n\x1a\nimage")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseMultipartForm(1 << 20); err != nil { // #nosec G120 -- request body is limited by MaxBytesReader above.
			t.Error(err)
			return
		}
		defer func() { _ = r.MultipartForm.RemoveAll() }()
		if len(r.MultipartForm.File["image"]) != 1 || len(r.MultipartForm.File["image[]"]) != 0 {
			t.Errorf("files = %#v, want one image file", r.MultipartForm.File)
		}
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"edited"}]}`)
	}))
	defer server.Close()
	adapter := imageclient.NewOpenAIImagesAdapter(imageclient.OpenAIImagesAdapterConfig{BaseURL: server.URL, DefaultModel: "test-image-model"})
	if _, err := adapter.Edit(context.Background(), &imageclient.ProviderRequest{Prompt: "edit", ReferenceImages: []string{base64.StdEncoding.EncodeToString(image)}}); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAIImagesAdapterBoundsURLDownloads(t *testing.T) {
	for _, test := range []struct {
		name, contentType, body string
		contentLength           int64
		want                    string
	}{
		{name: "declared size", contentType: "image/png", contentLength: 1 << 40, want: "exceeds"},
		{name: "content type", contentType: "text/html", body: "not image", want: "non-image"},
		{name: "empty", contentType: "image/png", want: "empty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"data":[{"url":"https://cdn.example/image.png"}]}`)
			}))
			defer server.Close()
			adapter := imageclient.NewOpenAIImagesAdapter(imageclient.OpenAIImagesAdapterConfig{BaseURL: server.URL, DefaultModel: "test-image-model", DownloadHTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{test.contentType}}, ContentLength: test.contentLength, Body: io.NopCloser(strings.NewReader(test.body)), Request: r}, nil
			})}})
			_, err := adapter.Generate(context.Background(), &imageclient.ProviderRequest{Prompt: "sprite"})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %s", err, test.want)
			}
		})
	}
}

func TestOpenAIImagesAdapterDetectsBase64ResponseFormat(t *testing.T) {
	image := []byte("\xff\xd8\xffimage")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"`+base64.StdEncoding.EncodeToString(image)+`"}]}`)
	}))
	defer server.Close()
	adapter := imageclient.NewOpenAIImagesAdapter(imageclient.OpenAIImagesAdapterConfig{BaseURL: server.URL, DefaultModel: "test-image-model"})
	result, err := adapter.Generate(context.Background(), &imageclient.ProviderRequest{Prompt: "image"})
	if err != nil {
		t.Fatal(err)
	}
	if result.OutputFormat != "jpeg" {
		t.Fatalf("format = %q, want jpeg", result.OutputFormat)
	}
}
