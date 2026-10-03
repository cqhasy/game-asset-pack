package imageclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxOpenAIErrorBodyBytes = 64 << 10

// openAITransport is the small HTTP layer shared by OpenAI-compatible image
// protocols. It deliberately owns no provider-specific request or response
// types; protocol adapters provide those values to execute.
type openAITransport struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	configErr  error
}

type openAIConfigurationError struct{ message string }

func (e *openAIConfigurationError) Error() string { return e.message }

type openAIResponseDecodeError struct{ cause error }

func (e *openAIResponseDecodeError) Error() string { return "decode OpenAI-compatible response" }
func (e *openAIResponseDecodeError) Unwrap() error { return e.cause }

// openAIHTTPError represents a bounded, sanitized non-success response from
// an OpenAI-compatible endpoint.
type openAIHTTPError struct {
	StatusCode int
	Message    string
	Cause      error
}

func (e *openAIHTTPError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if status := http.StatusText(e.StatusCode); status != "" {
		return status
	}
	return fmt.Sprintf("HTTP status %d", e.StatusCode)
}

func (e *openAIHTTPError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func newOpenAITransport(baseURL, apiKey string, httpClient *http.Client) *openAITransport {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	var configErr error
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		configErr = &openAIConfigurationError{message: "image baseURL must be an absolute HTTP(S) URL without credentials, query, or fragment"}
	}
	return &openAITransport{
		baseURL:    normalizeOpenAIBaseURL(baseURL),
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: httpClient,
		configErr:  configErr,
	}
}

func normalizeOpenAIBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "/v1"
	}
	if !strings.HasSuffix(strings.ToLower(baseURL), "/v1") {
		baseURL += "/v1"
	}
	return baseURL
}

func (t *openAITransport) execute(
	ctx context.Context,
	method string,
	path string,
	requestBody any,
	responseBody any,
) error {
	if t == nil {
		return errors.New("openai transport is nil")
	}
	if t.configErr != nil {
		return t.configErr
	}
	endpoint := joinOpenAIEndpoint(t.baseURL, path)

	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("encode OpenAI-compatible request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("create OpenAI-compatible request: %w", err)
	}
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if t.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+t.apiKey)
	}

	response, err := t.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return t.httpError(response)
	}
	if responseBody == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(responseBody); err != nil {
		return &openAIResponseDecodeError{cause: err}
	}
	return nil
}

func (t *openAITransport) httpError(response *http.Response) error {
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxOpenAIErrorBodyBytes))
	message := openAIResponseErrorMessage(body, response.Status)
	if t.apiKey != "" {
		message = strings.ReplaceAll(message, t.apiKey, "[redacted]")
	}
	return &openAIHTTPError{
		StatusCode: response.StatusCode,
		Message:    message,
		Cause:      readErr,
	}
}

func joinOpenAIEndpoint(baseURL, path string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	path = strings.TrimSpace(path)
	path = strings.TrimLeft(path, "/")
	if strings.HasPrefix(strings.ToLower(path), "v1/") {
		path = path[len("v1/"):]
	} else if strings.EqualFold(path, "v1") {
		path = ""
	}
	if path == "" {
		return baseURL
	}
	return baseURL + "/" + path
}

func openAIResponseErrorMessage(body []byte, status string) string {
	var envelope struct {
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		if message := strings.TrimSpace(envelope.Message); message != "" {
			return message
		}
		if len(envelope.Error) > 0 {
			var nested struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(envelope.Error, &nested); err == nil {
				if message := strings.TrimSpace(nested.Message); message != "" {
					return message
				}
			}
			var message string
			if err := json.Unmarshal(envelope.Error, &message); err == nil {
				if message = strings.TrimSpace(message); message != "" {
					return message
				}
			}
		}
	}
	return strings.TrimSpace(status)
}
