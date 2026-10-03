package imageclient

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

func newOpenAIProviderError(kind ErrorKind, status int, transient bool, message string, cause error) *ProviderError {
	if message == "" && cause != nil {
		message = cause.Error()
	}
	return &ProviderError{Kind: kind, StatusCode: status, Transient: transient, Message: message, Cause: cause}
}

func classifyOpenAIAdapterError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.Canceled) {
			return newOpenAIProviderError(ErrorKindCanceled, 0, false, "request canceled", ctxErr)
		}
		return newOpenAIProviderError(ErrorKindTimeout, 0, true, "request timed out", ctxErr)
	}
	var configErr *openAIConfigurationError
	if errors.As(err, &configErr) {
		return newOpenAIProviderError(ErrorKindInvalidRequest, 0, false, configErr.Error(), err)
	}
	var decodeErr *openAIResponseDecodeError
	if errors.As(err, &decodeErr) {
		return newOpenAIProviderError(ErrorKindInvalidResponse, http.StatusOK, true, "decode provider response", err)
	}
	var httpErr *openAIHTTPError
	if errors.As(err, &httpErr) {
		kind, transient := classifyOpenAIStatus(httpErr.StatusCode)
		return newOpenAIProviderError(kind, httpErr.StatusCode, transient, httpErr.Message, err)
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		return newOpenAIProviderError(ErrorKindTimeout, 0, true, "request timed out", err)
	}
	if errors.Is(err, context.Canceled) {
		return newOpenAIProviderError(ErrorKindCanceled, 0, false, "request canceled", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return newOpenAIProviderError(ErrorKindTimeout, 0, true, "request timed out", err)
	}
	return newOpenAIProviderError(ErrorKindTransport, 0, true, "request failed", err)
}

func classifyOpenAIStatus(status int) (ErrorKind, bool) {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrorKindAuthentication, false
	case http.StatusTooManyRequests:
		return ErrorKindRateLimited, true
	case http.StatusRequestTimeout:
		return ErrorKindTimeout, true
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return ErrorKindInvalidRequest, false
	default:
		if status >= http.StatusInternalServerError {
			return ErrorKindUnavailable, true
		}
		return ErrorKindInvalidRequest, false
	}
}
