// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	authenticationapi "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authentication/authenticatorfactory"
	"k8s.io/apiserver/pkg/authentication/group"
	"k8s.io/apiserver/pkg/authentication/token/cache"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/authorization/authorizerfactory"
	"k8s.io/apiserver/pkg/util/webhook"
	authenticationv1 "k8s.io/client-go/kubernetes/typed/authentication/v1"
	authorizationv1 "k8s.io/client-go/kubernetes/typed/authorization/v1"
	"k8s.io/client-go/rest"

	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// PodNamespaceEnvVar is the env var the Helm chart populates, via the
// Downward API, with the namespace the operator Pod itself runs in.
const PodNamespaceEnvVar = "POD_NAMESPACE"

// metricsAPIGroup and metricsResource identify a synthetic resource — it has
// no backing Kubernetes API type — used only as the subject of the
// SubjectAccessReview that authorizes GET /metrics.
const (
	metricsAPIGroup = "metrics.splunk.com"
	metricsResource = "operatormetrics"
)

var errMetricsTokenRejected = errors.New("token rejected")

// MetricsAuthFilterProvider authenticates metrics requests and authorizes access
// to metrics.splunk.com/operatormetrics in the operator Pod's namespace.
// This supports namespace-local reader grants. See docs/operate/OperatorMetrics.md
// for the RBAC rationale and deployment requirements.
func MetricsAuthFilterProvider(cfg *rest.Config, httpClient *http.Client) (metricsserver.Filter, error) {
	podNamespace := os.Getenv(PodNamespaceEnvVar)
	if podNamespace == "" {
		return nil, fmt.Errorf("%s must identify the operator Pod namespace", PodNamespaceEnvVar)
	}
	authenticationV1Client, err := authenticationv1.NewForConfigAndClient(cfg, httpClient)
	if err != nil {
		return nil, err
	}
	authorizationV1Client, err := authorizationv1.NewForConfigAndClient(cfg, httpClient)
	if err != nil {
		return nil, err
	}

	authenticatorfactory.RegisterMetrics()
	tokenAuthenticator := authenticator.TokenFunc(func(ctx context.Context, token string) (*authenticator.Response, bool, error) {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		wantAudiences, checkAudiences := authenticator.AudiencesFrom(ctx)
		review := &authenticationapi.TokenReview{
			Spec: authenticationapi.TokenReviewSpec{Token: token, Audiences: wantAudiences},
		}
		err := webhook.WithExponentialBackoff(ctx, wait.Backoff{
			Duration: 500 * time.Millisecond,
			Factor:   1.5,
			Jitter:   0.2,
			Steps:    5,
		}, func() error {
			var statusCode int
			start := time.Now()
			err := authenticationV1Client.RESTClient().Post().Resource("tokenreviews").Body(review).
				Do(ctx).StatusCode(&statusCode).Into(review)
			status := "<error>"
			if statusCode != 0 {
				status = strconv.Itoa(statusCode)
			}
			authenticatorfactory.RecordRequestTotal(ctx, status)
			authenticatorfactory.RecordRequestLatency(ctx, status, time.Since(start).Seconds())
			return err
		}, webhook.DefaultShouldRetry)
		if err != nil {
			return nil, false, err
		}
		if !review.Status.Authenticated {
			// Keep status.error out of the cache; a completed rejection still returns 401.
			if review.Status.Error != "" {
				return nil, false, fmt.Errorf("%w: %s", errMetricsTokenRejected, review.Status.Error)
			}
			return nil, false, nil
		}
		var audiences authenticator.Audiences
		if checkAudiences {
			audiences = wantAudiences.Intersect(review.Status.Audiences)
			if len(audiences) == 0 {
				return nil, false, nil
			}
		}
		var extra map[string][]string
		if review.Status.User.Extra != nil {
			extra = make(map[string][]string, len(review.Status.User.Extra))
			for key, values := range review.Status.User.Extra {
				extra[key] = values
			}
		}
		return &authenticator.Response{
			User: &user.DefaultInfo{
				Name: review.Status.User.Username, UID: review.Status.User.UID,
				Groups: review.Status.User.Groups, Extra: extra,
			},
			Audiences: audiences,
		}, true, nil
	})
	cachingTokenAuthenticator := cache.New(tokenAuthenticator, false, time.Minute, time.Minute)
	delegatingAuthenticator := group.NewAuthenticatedGroupAdder(authenticator.RequestFunc(func(req *http.Request) (*authenticator.Response, bool, error) {
		// Preserve 401 for rejected tokens without hiding review API failures.
		parts := strings.SplitN(strings.TrimSpace(req.Header.Get("Authorization")), " ", 3)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") || parts[1] == "" {
			return nil, false, nil
		}
		response, ok, err := cachingTokenAuthenticator.AuthenticateToken(req.Context(), parts[1])
		if ok {
			req.Header.Del("Authorization")
		}
		return response, ok, err
	}))

	authorizerConfig := authorizerfactory.DelegatingAuthorizerConfig{
		SubjectAccessReviewClient: authorizationV1Client,
		AllowCacheTTL:             5 * time.Minute,
		DenyCacheTTL:              30 * time.Second,
		WebhookRetryBackoff: &wait.Backoff{
			Duration: 500 * time.Millisecond,
			Factor:   1.5,
			Jitter:   0.2,
			Steps:    5,
		},
	}
	delegatingAuthorizer, err := authorizerConfig.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create authorizer: %w", err)
	}

	return func(log logr.Logger, handler http.Handler) (http.Handler, error) {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := req.Context()

			res, ok, err := delegatingAuthenticator.AuthenticateRequest(req)
			if errors.Is(err, errMetricsTokenRejected) {
				log.V(4).Info("Token rejected", "reason", err)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			if err != nil {
				log.Error(err, "Authentication failed")
				http.Error(w, "Authentication failed", http.StatusInternalServerError)
				return
			}
			if !ok {
				log.V(4).Info("Authentication failed")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			attributes := authorizer.AttributesRecord{
				User:            res.User,
				Verb:            strings.ToLower(req.Method),
				Namespace:       podNamespace,
				APIGroup:        metricsAPIGroup,
				Resource:        metricsResource,
				ResourceRequest: true,
			}

			authorized, reason, err := delegatingAuthorizer.Authorize(ctx, attributes)
			if err != nil {
				msg := fmt.Sprintf("Authorization for user %s failed", attributes.User.GetName())
				log.Error(err, msg)
				http.Error(w, msg, http.StatusInternalServerError)
				return
			}
			if authorized != authorizer.DecisionAllow {
				msg := fmt.Sprintf("Authorization denied for user %s", attributes.User.GetName())
				log.V(4).Info(fmt.Sprintf("%s: %s", msg, reason))
				http.Error(w, msg, http.StatusForbidden)
				return
			}

			handler.ServeHTTP(w, req)
		}), nil
	}, nil
}
