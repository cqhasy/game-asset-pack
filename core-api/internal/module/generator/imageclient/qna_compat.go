package imageclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"
)

// legacyImageExecutor preserves injected clients accepted by deprecated
// constructors. The compatibility bridge has no SDK dependency.
type legacyImageExecutor interface {
	Execute(context.Context, string, string, any, any) error
}

func hasLegacyExecutor(executor legacyImageExecutor) bool {
	if executor == nil {
		return false
	}
	value := reflect.ValueOf(executor)
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return !value.IsNil()
	default:
		return true
	}
}

type legacyExecutorTransport struct {
	executor legacyImageExecutor
	apiKey   string
}

func legacyExecutorHTTPClient(executor legacyImageExecutor, apiKey string, timeout time.Duration) *http.Client {
	return &http.Client{Transport: legacyExecutorTransport{executor: executor, apiKey: apiKey}, Timeout: timeout}
}

func (t legacyExecutorTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var payload any
	if request.Body != nil {
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			return nil, err
		}
	}
	var result json.RawMessage
	path := strings.TrimPrefix(request.URL.Path, "/")
	if position := strings.Index(path, "v1/"); position >= 0 {
		path = path[position+3:]
	}
	if err := t.executor.Execute(request.Context(), request.Method, path, payload, &result); err != nil {
		if status, message, ok := legacyHTTPError(err); ok {
			if t.apiKey != "" {
				message = strings.ReplaceAll(message, t.apiKey, "[redacted]")
			}
			return nil, &openAIHTTPError{StatusCode: status, Message: message, Cause: err}
		}
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(result)), Request: request}, nil
}

func legacyHTTPError(err error) (int, string, bool) {
	for current := err; current != nil; current = errors.Unwrap(current) {
		value := reflect.ValueOf(current)
		if value.Kind() == reflect.Pointer {
			value = value.Elem()
		}
		if !value.IsValid() || value.Kind() != reflect.Struct {
			continue
		}
		status := value.FieldByName("StatusCode")
		message := value.FieldByName("Message")
		if status.IsValid() && status.Kind() == reflect.Int && message.IsValid() && message.Kind() == reflect.String {
			return int(status.Int()), message.String(), true
		}
	}
	return 0, "", false
}
