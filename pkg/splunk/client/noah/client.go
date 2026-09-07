// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package noah

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultRequestTimeout   = 5 * time.Second
	defaultMaxResponseBytes = 1 << 20
)

// HTTPClient is the transport boundary used by Client.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Authenticator applies Noah authentication to an outbound request. The body
// is provided so signing implementations can include it in the signature.
type Authenticator interface {
	Authenticate(*http.Request, []byte) error
}

// AuthenticatorFunc adapts a function to Authenticator.
type AuthenticatorFunc func(*http.Request, []byte) error

// Authenticate implements Authenticator.
func (fn AuthenticatorFunc) Authenticate(request *http.Request, body []byte) error {
	return fn(request, body)
}

// ErrorKind is a stable classification for a failed Noah operation.
type ErrorKind string

const (
	ErrorKindAuthentication   ErrorKind = "authentication"
	ErrorKindCanceled         ErrorKind = "canceled"
	ErrorKindConflict         ErrorKind = "conflict"
	ErrorKindForbidden        ErrorKind = "forbidden"
	ErrorKindInvalidRequest   ErrorKind = "invalid-request"
	ErrorKindInvalidResponse  ErrorKind = "invalid-response"
	ErrorKindNotFound         ErrorKind = "not-found"
	ErrorKindRateLimited      ErrorKind = "rate-limited"
	ErrorKindTimeout          ErrorKind = "timeout"
	ErrorKindTransport        ErrorKind = "transport"
	ErrorKindUnauthorized     ErrorKind = "unauthorized"
	ErrorKindUnavailable      ErrorKind = "unavailable"
	ErrorKindUnexpectedStatus ErrorKind = "unexpected-status"
)

// Error describes a Noah API failure without retaining response bodies or
// credentials.
type Error struct {
	Operation  string
	Kind       ErrorKind
	StatusCode int
	Err        error
	retryable  bool
}

// Error implements error.
func (err *Error) Error() string {
	if err.StatusCode != 0 {
		return fmt.Sprintf("%s: %s (HTTP %d)", err.Operation, err.Kind, err.StatusCode)
	}
	return fmt.Sprintf("%s: %s", err.Operation, err.Kind)
}

// Unwrap returns the underlying transport, context, authentication, or decode
// error when one exists.
func (err *Error) Unwrap() error {
	return err.Err
}

// Retryable reports whether retrying the operation may succeed without an
// input change. Callers must still apply mutation-specific idempotency rules.
func (err *Error) Retryable() bool {
	return err.retryable
}

// Option configures a Client.
type Option func(*Client) error

// WithHTTPClient replaces the default HTTP client.
func WithHTTPClient(httpClient HTTPClient) Option {
	return func(client *Client) error {
		if httpClient == nil {
			return fmt.Errorf("HTTP client is nil")
		}
		client.httpClient = httpClient
		return nil
	}
}

// WithRequestTimeout changes the per-request deadline applied by the client.
func WithRequestTimeout(timeout time.Duration) Option {
	return func(client *Client) error {
		if timeout <= 0 {
			return fmt.Errorf("request timeout must be positive")
		}
		client.requestTimeout = timeout
		return nil
	}
}

// WithMaxResponseBytes changes the maximum accepted response body size.
func WithMaxResponseBytes(maxBytes int64) Option {
	return func(client *Client) error {
		if maxBytes <= 0 {
			return fmt.Errorf("maximum response size must be positive")
		}
		client.maxResponseBytes = maxBytes
		return nil
	}
}

// Client calls the Noah membership API for one tenant.
type Client struct {
	endpoint         string
	tenant           string
	authenticator    Authenticator
	httpClient       HTTPClient
	requestTimeout   time.Duration
	maxResponseBytes int64
}

// NewClient constructs a Noah membership client.
func NewClient(endpoint, tenant string, authenticator Authenticator, options ...Option) (*Client, error) {
	validatedEndpoint, err := validateClientConfig(endpoint, tenant)
	if err != nil {
		return nil, err
	}
	if authenticator == nil {
		return nil, fmt.Errorf("authenticator is nil")
	}

	client := &Client{
		endpoint:         validatedEndpoint,
		tenant:           tenant,
		authenticator:    authenticator,
		httpClient:       &http.Client{},
		requestTimeout:   defaultRequestTimeout,
		maxResponseBytes: defaultMaxResponseBytes,
	}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("client option is nil")
		}
		if err := option(client); err != nil {
			return nil, fmt.Errorf("configure Noah client: %w", err)
		}
	}
	return client, nil
}

// ValidateClientConfig validates Noah connection coordinates without creating
// an authenticated client or deriving credential material.
func ValidateClientConfig(endpoint, tenant string) error {
	_, err := validateClientConfig(endpoint, tenant)
	return err
}

func validateClientConfig(endpoint, tenant string) (string, error) {
	validatedEndpoint, err := validateEndpoint(endpoint)
	if err != nil {
		return "", err
	}
	if tenant == "" || strings.TrimSpace(tenant) != tenant {
		return "", fmt.Errorf("tenant must be non-empty and contain no surrounding whitespace")
	}
	return validatedEndpoint, nil
}

func (client *Client) do(ctx context.Context, operation, method, requestURL string, expectedStatus int, retrySafe bool, output any) error {
	if ctx == nil {
		return &Error{Operation: operation, Kind: ErrorKindInvalidRequest}
	}
	requestCtx, cancel := context.WithTimeout(ctx, client.requestTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(requestCtx, method, requestURL, nil)
	if err != nil {
		return &Error{Operation: operation, Kind: ErrorKindInvalidRequest, Err: err}
	}
	request.Header.Set("Accept", "application/json")
	if err := client.authenticator.Authenticate(request, nil); err != nil {
		return &Error{Operation: operation, Kind: ErrorKindAuthentication, Err: err}
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return classifyRequestError(operation, requestCtx, err, retrySafe)
	}
	defer response.Body.Close()

	if response.StatusCode != expectedStatus {
		kind := classifyStatus(response.StatusCode)
		return &Error{
			Operation:  operation,
			Kind:       kind,
			StatusCode: response.StatusCode,
			retryable:  retrySafe && retryableKind(kind),
		}
	}
	if output == nil {
		return nil
	}
	if err := decodeResponse(response.Body, client.maxResponseBytes, output); err != nil {
		return invalidResponse(operation, err)
	}
	return nil
}

func classifyRequestError(operation string, ctx context.Context, err error, retrySafe bool) *Error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return &Error{Operation: operation, Kind: ErrorKindCanceled, Err: err}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return &Error{Operation: operation, Kind: ErrorKindTimeout, Err: err, retryable: retrySafe}
	}
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return &Error{Operation: operation, Kind: ErrorKindTimeout, Err: err, retryable: retrySafe}
	}
	return &Error{Operation: operation, Kind: ErrorKindTransport, Err: err, retryable: retrySafe}
}

func classifyStatus(statusCode int) ErrorKind {
	switch statusCode {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return ErrorKindInvalidRequest
	case http.StatusUnauthorized:
		return ErrorKindUnauthorized
	case http.StatusForbidden:
		return ErrorKindForbidden
	case http.StatusNotFound:
		return ErrorKindNotFound
	case http.StatusConflict:
		return ErrorKindConflict
	case http.StatusTooManyRequests:
		return ErrorKindRateLimited
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return ErrorKindUnavailable
	default:
		return ErrorKindUnexpectedStatus
	}
}

func decodeResponse(body io.Reader, maxBytes int64, output any) error {
	payload, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if int64(len(payload)) > maxBytes {
		return fmt.Errorf("response exceeds %d bytes", maxBytes)
	}
	if len(payload) == 0 {
		return fmt.Errorf("response body is empty")
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("response contains multiple JSON values")
		}
		return fmt.Errorf("decode trailing response data: %w", err)
	}
	return nil
}

func invalidResponse(operation string, err error) *Error {
	return &Error{Operation: operation, Kind: ErrorKindInvalidResponse, Err: err}
}

func retryableKind(kind ErrorKind) bool {
	switch kind {
	case ErrorKindRateLimited, ErrorKindTimeout, ErrorKindTransport, ErrorKindUnavailable:
		return true
	default:
		return false
	}
}

func validateEndpoint(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("endpoint scheme must be http or https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("endpoint host is empty")
	}
	if parsed.User != nil || parsed.ForceQuery || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("endpoint must not contain credentials, a query, or a fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", fmt.Errorf("endpoint must not contain a path")
	}
	parsed.Path = ""
	parsed.RawPath = ""
	return strings.TrimSuffix(parsed.String(), "/"), nil
}
