// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/client-go/rest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

type metricsAuthRoundTripper func(*http.Request) (*http.Response, error)

func (f metricsAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Exercise the filter through its real Kubernetes clients without a live API
// server. A new filter per case keeps authentication/authorization caches apart.
func TestMetricsAuthFilterProvider(t *testing.T) {
	const (
		tokenReviewPath  = "/apis/authentication.k8s.io/v1/tokenreviews"
		accessReviewPath = "/apis/authorization.k8s.io/v1/subjectaccessreviews"
	)
	identity := authenticationv1.UserInfo{
		Username: "system:serviceaccount:monitoring:metrics-reader",
		UID:      "reader-uid",
		Groups:   []string{"system:serviceaccounts", "system:serviceaccounts:monitoring"},
		Extra:    map[string]authenticationv1.ExtraValue{"tenant": {"team-a", "team-b"}},
	}
	for _, tt := range []struct {
		name              string
		token             string
		authenticated     bool
		allowed           bool
		failedReview      string
		transportFailure  bool
		reviewError       string
		requestAudiences  []string
		reviewAudiences   []string
		wantStatuses      []int
		wantTokenReviews  int
		wantAccessReviews int
	}{
		{name: "missing token", wantStatuses: []int{http.StatusUnauthorized}},
		{name: "unauthenticated token", token: "invalid-token", wantStatuses: []int{http.StatusUnauthorized, http.StatusUnauthorized}, wantTokenReviews: 1},
		{name: "malformed bearer token", token: "reader-token extra", wantStatuses: []int{http.StatusUnauthorized}},
		{name: "empty bearer token", token: " ", wantStatuses: []int{http.StatusUnauthorized}},
		{name: "invalid token with rejection reason", token: "invalid-token", reviewError: "token signature is invalid",
			wantStatuses: []int{http.StatusUnauthorized}, wantTokenReviews: 1},
		{name: "expired token with rejection reason", token: "expired-token", reviewError: "service account token has expired",
			wantStatuses: []int{http.StatusUnauthorized}, wantTokenReviews: 1},
		{name: "token review status error is not cached", token: "reader-token", authenticated: true, allowed: true,
			reviewError:  "authentication backend unavailable",
			wantStatuses: []int{http.StatusUnauthorized, http.StatusOK, http.StatusOK}, wantTokenReviews: 2, wantAccessReviews: 1},
		{name: "reader denied", token: "reader-token", authenticated: true, wantStatuses: []int{http.StatusForbidden},
			wantTokenReviews: 1, wantAccessReviews: 1},
		{name: "reader allowed", token: "reader-token", authenticated: true, allowed: true, wantStatuses: []int{http.StatusOK, http.StatusOK},
			wantTokenReviews: 1, wantAccessReviews: 1},
		{name: "matching audience", token: "reader-token", authenticated: true, allowed: true,
			requestAudiences: []string{"metrics"}, reviewAudiences: []string{"metrics"},
			wantStatuses: []int{http.StatusOK}, wantTokenReviews: 1, wantAccessReviews: 1},
		{name: "mismatched audience", token: "reader-token", authenticated: true, allowed: true,
			requestAudiences: []string{"metrics"}, reviewAudiences: []string{"other"},
			wantStatuses: []int{http.StatusUnauthorized}, wantTokenReviews: 1},
		{name: "missing reviewed audience", token: "reader-token", authenticated: true, allowed: true,
			requestAudiences: []string{"metrics"}, wantStatuses: []int{http.StatusUnauthorized}, wantTokenReviews: 1},
		{name: "token review failure", token: "reader-token", failedReview: tokenReviewPath,
			wantStatuses: []int{http.StatusInternalServerError, http.StatusOK}, authenticated: true, allowed: true, wantTokenReviews: 2, wantAccessReviews: 1},
		{name: "access review failure", token: "reader-token", authenticated: true, failedReview: accessReviewPath,
			wantStatuses: []int{http.StatusInternalServerError, http.StatusOK}, allowed: true, wantTokenReviews: 1, wantAccessReviews: 2},
		{name: "token review transport failure recovers", token: "reader-token", authenticated: true, allowed: true,
			failedReview: tokenReviewPath, transportFailure: true,
			wantStatuses: []int{http.StatusInternalServerError, http.StatusOK}, wantTokenReviews: 2, wantAccessReviews: 1},
		{name: "access review transport failure recovers", token: "reader-token", authenticated: true, allowed: true,
			failedReview: accessReviewPath, transportFailure: true,
			wantStatuses: []int{http.StatusInternalServerError, http.StatusOK}, wantTokenReviews: 1, wantAccessReviews: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(PodNamespaceEnvVar, "operator-ns")
			t.Setenv("WATCH_NAMESPACE", "watched-ns")
			var failReview bool
			var mu sync.Mutex
			var tokenReviews []authenticationv1.TokenReviewSpec
			var accessReviews []authorizationv1.SubjectAccessReviewSpec
			client := &http.Client{Transport: metricsAuthRoundTripper(func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, http.MethodPost, req.Method)
				var response any
				mu.Lock()
				defer mu.Unlock()
				switch req.URL.Path {
				case tokenReviewPath:
					var review authenticationv1.TokenReview
					err := json.NewDecoder(req.Body).Decode(&review)
					if err != nil {
						return nil, err
					}
					tokenReviews = append(tokenReviews, review.Spec)
					status := authenticationv1.TokenReviewStatus{Authenticated: tt.authenticated, User: identity, Audiences: tt.reviewAudiences}
					if len(tokenReviews) == 1 && tt.reviewError != "" {
						status.Authenticated = false
						status.Error = tt.reviewError
					}
					response = &authenticationv1.TokenReview{
						TypeMeta: metav1.TypeMeta{APIVersion: "authentication.k8s.io/v1", Kind: "TokenReview"},
						Status:   status,
					}
				case accessReviewPath:
					var review authorizationv1.SubjectAccessReview
					err := json.NewDecoder(req.Body).Decode(&review)
					if err != nil {
						return nil, err
					}
					accessReviews = append(accessReviews, review.Spec)
					response = &authorizationv1.SubjectAccessReview{
						TypeMeta: metav1.TypeMeta{APIVersion: "authorization.k8s.io/v1", Kind: "SubjectAccessReview"},
						Status:   authorizationv1.SubjectAccessReviewStatus{Allowed: tt.allowed},
					}
				default:
					return nil, fmt.Errorf("unexpected review request: %s", req.URL.Path)
				}
				status := http.StatusOK
				if failReview && req.URL.Path == tt.failedReview {
					if tt.transportFailure {
						return nil, fmt.Errorf("review transport unavailable")
					}
					status = http.StatusForbidden
					response = &metav1.Status{
						TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
						Status:   metav1.StatusFailure, Reason: metav1.StatusReasonForbidden,
						Code: http.StatusForbidden, Message: "review permission denied",
					}
				}
				body, err := json.Marshal(response)
				if err != nil {
					return nil, err
				}
				return &http.Response{
					StatusCode: status,
					Header:     http.Header{"Content-Type": {"application/json"}},
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			})}

			filter, err := MetricsAuthFilterProvider(&rest.Config{Host: "https://kubernetes.example"}, client)
			require.NoError(t, err)
			handlerCalled := false
			handler, err := filter(logr.Discard(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				handlerCalled = true
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, "test_metric 1\n")
			}))
			require.NoError(t, err)
			for attempt, wantStatus := range tt.wantStatuses {
				mu.Lock()
				failReview = attempt == 0
				mu.Unlock()
				handlerCalled = false
				req := httptest.NewRequest(http.MethodGet, "http://operator.example/metrics", nil)
				if tt.requestAudiences != nil {
					req = req.WithContext(authenticator.WithAudiences(req.Context(), tt.requestAudiences))
				}
				if tt.token != "" {
					req.Header.Set("Authorization", "Bearer "+tt.token)
				}
				result := httptest.NewRecorder()
				handler.ServeHTTP(result, req)
				assert.Equal(t, wantStatus, result.Code, result.Body.String())
				assert.Equal(t, wantStatus == http.StatusOK, handlerCalled)
				if handlerCalled {
					assert.Equal(t, "test_metric 1\n", result.Body.String())
				}
			}

			mu.Lock()
			defer mu.Unlock()
			require.Len(t, tokenReviews, tt.wantTokenReviews)
			for _, review := range tokenReviews {
				assert.Equal(t, tt.token, review.Token)
				assert.Equal(t, tt.requestAudiences, review.Audiences)
			}
			require.Len(t, accessReviews, tt.wantAccessReviews)
			for _, review := range accessReviews {
				assert.Equal(t, authorizationv1.SubjectAccessReviewSpec{
					ResourceAttributes: &authorizationv1.ResourceAttributes{
						Namespace: "operator-ns",
						Verb:      "get",
						Group:     "metrics.splunk.com",
						Resource:  "operatormetrics",
					},
					User: identity.Username, UID: identity.UID,
					Groups: []string{"system:serviceaccounts", "system:serviceaccounts:monitoring", "system:authenticated"},
					Extra:  map[string]authorizationv1.ExtraValue{"tenant": {"team-a", "team-b"}},
				}, review)
			}
		})
	}
}

func TestMetricsAuthRequiresPodNamespace(t *testing.T) {
	t.Setenv(PodNamespaceEnvVar, "")
	_, err := MetricsAuthFilterProvider(&rest.Config{Host: "https://kubernetes.example"}, http.DefaultClient)
	require.ErrorContains(t, err, PodNamespaceEnvVar)
}

func TestMetricsServerRequiresPodNamespace(t *testing.T) {
	t.Setenv(PodNamespaceEnvVar, "")
	t.Setenv("WATCH_NAMESPACE", "watched-ns")
	_, err := metricsserver.NewServer(metricsserver.Options{
		BindAddress:    "127.0.0.1:8080",
		FilterProvider: MetricsAuthFilterProvider,
	}, &rest.Config{Host: "https://kubernetes.example"}, http.DefaultClient)

	require.ErrorContains(t, err, PodNamespaceEnvVar)
}
