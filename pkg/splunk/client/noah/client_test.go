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
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const testAuthHeader = "X-Test-Noah-Auth"

var testAuthenticator = AuthenticatorFunc(func(request *http.Request, _ []byte) error {
	request.Header.Set(testAuthHeader, "authenticated")
	return nil
})

func TestNormaliseEndpointStripsTrailingSlash(t *testing.T) {
	assert.Equal(t, "https://noah.test:8443", normaliseEndpoint("https://noah.test:8443/"))
	assert.Equal(t, "https://noah.test:8443", normaliseEndpoint("https://noah.test:8443"))
}

func TestNewClientValidatesConfiguration(t *testing.T) {
	tests := []struct {
		name          string
		endpoint      string
		tenant        string
		authenticator Authenticator
		options       []Option
	}{
		{name: "nil authenticator", endpoint: "https://noah.test", tenant: "tenant"},
		{name: "nil option", endpoint: "https://noah.test", tenant: "tenant", authenticator: testAuthenticator, options: []Option{nil}},
		{name: "nil HTTP client", endpoint: "https://noah.test", tenant: "tenant", authenticator: testAuthenticator, options: []Option{WithHTTPClient(nil)}},
		{name: "invalid timeout", endpoint: "https://noah.test", tenant: "tenant", authenticator: testAuthenticator, options: []Option{WithRequestTimeout(0)}},
		{name: "invalid response limit", endpoint: "https://noah.test", tenant: "tenant", authenticator: testAuthenticator, options: []Option{WithMaxResponseBytes(0)}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewClient(test.endpoint, test.tenant, test.authenticator, test.options...)
			assert.Error(t, err)
		})
	}
}

func TestClientClassifiesAndRedactsHTTPFailures(t *testing.T) {
	tests := []struct {
		statusCode int
		kind       ErrorKind
		retryable  bool
	}{
		{statusCode: http.StatusBadRequest, kind: ErrorKindInvalidRequest},
		{statusCode: http.StatusUnauthorized, kind: ErrorKindUnauthorized},
		{statusCode: http.StatusForbidden, kind: ErrorKindForbidden},
		{statusCode: http.StatusNotFound, kind: ErrorKindNotFound},
		{statusCode: http.StatusConflict, kind: ErrorKindConflict},
		{statusCode: http.StatusTooManyRequests, kind: ErrorKindRateLimited, retryable: true},
		{statusCode: http.StatusServiceUnavailable, kind: ErrorKindUnavailable, retryable: true},
	}

	for _, test := range tests {
		t.Run(http.StatusText(test.statusCode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.statusCode)
				_, _ = response.Write([]byte("pass4SymmKey=must-not-leak"))
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "tenant", testAuthenticator, WithHTTPClient(server.Client()))
			assert.NoError(t, err)
			_, err = client.ListPeers(context.Background())
			apiError := assertErrorKind(t, err, test.kind)
			assert.Equal(t, test.retryable, apiError.Retryable())
			assert.NotContains(t, err.Error(), "pass4SymmKey")
		})
	}
}

type httpClientFunc func(*http.Request) (*http.Response, error)

func (fn httpClientFunc) Do(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestClientAppliesRequestTimeout(t *testing.T) {
	httpClient := httpClientFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	client, err := NewClient(
		"https://noah.test",
		"tenant",
		testAuthenticator,
		WithHTTPClient(httpClient),
		WithRequestTimeout(time.Millisecond),
	)
	assert.NoError(t, err)

	_, err = client.ListPeers(context.Background())
	apiError := assertErrorKind(t, err, ErrorKindTimeout)
	assert.True(t, apiError.Retryable())
}

func TestClientRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(strings.Repeat("x", 17)))
	}))
	defer server.Close()

	client, err := NewClient(
		server.URL,
		"tenant",
		testAuthenticator,
		WithHTTPClient(server.Client()),
		WithMaxResponseBytes(16),
	)
	assert.NoError(t, err)
	_, err = client.ListPeers(context.Background())
	assertErrorKind(t, err, ErrorKindInvalidResponse)
}

func TestClientClassifiesCallerCancellation(t *testing.T) {
	httpClient := httpClientFunc(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	})
	client, err := NewClient(
		"https://noah.test",
		"tenant",
		testAuthenticator,
		WithHTTPClient(httpClient),
	)
	assert.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.ListPeers(ctx)
	assertErrorKind(t, err, ErrorKindCanceled)
}

func assertErrorKind(t *testing.T, err error, want ErrorKind) *Error {
	t.Helper()
	assert.Error(t, err)
	var apiError *Error
	assert.ErrorAs(t, err, &apiError)
	assert.Equal(t, want, apiError.Kind)
	return apiError
}
