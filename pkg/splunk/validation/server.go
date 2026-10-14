/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package validation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/logging"
)

var serverLog = slog.Default().With("component", "webhook-server")

// WebhookServerOptions contains configuration for the webhook server
type WebhookServerOptions struct {
	// Validators is the map of validators by GVR
	Validators map[schema.GroupVersionResource]Validator

	// Client is the Kubernetes client for resource lookups (optional)
	Client client.Client

	// CrossVersionGuard checks for fields which can't be converted safely to another version.
	// Required when the CrossVersionFieldProtection feature gate is enabled.
	CrossVersionGuard *CrossVersionFieldGuard
}

// WebhookServer validates admission requests. Register HandleValidate on a
// webhook server, such as the manager's, to serve it.
type WebhookServer struct {
	options WebhookServerOptions
}

// NewWebhookServer creates a new webhook server
func NewWebhookServer(options WebhookServerOptions) *WebhookServer {
	return &WebhookServer{
		options: options,
	}
}

// HandleValidate handles validation requests
func (s *WebhookServer) HandleValidate(w http.ResponseWriter, r *http.Request) {
	logger := logging.FromContext(r.Context()).With("func", "HandleValidate")
	logger.DebugContext(r.Context(), "Received validation request", "method", r.Method, "path", r.URL.Path)

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.ErrorContext(r.Context(), "Failed to read request body", "error", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var admissionReview admissionv1.AdmissionReview
	if err := json.Unmarshal(body, &admissionReview); err != nil {
		logger.ErrorContext(r.Context(), "Failed to decode admission review", "error", err)
		http.Error(w, "Failed to decode admission review", http.StatusBadRequest)
		return
	}

	if admissionReview.Request != nil {
		logger.InfoContext(r.Context(), "Processing admission request",
			"kind", admissionReview.Request.Kind.Kind,
			"name", admissionReview.Request.Name,
			"namespace", admissionReview.Request.Namespace,
			"operation", admissionReview.Request.Operation,
			"user", admissionReview.Request.UserInfo.Username)
	}

	warnings, validationErr := s.validate(r.Context(), &admissionReview)

	response := &admissionv1.AdmissionResponse{
		UID: admissionReview.Request.UID,
	}

	if validationErr != nil {
		logger.InfoContext(r.Context(), "Validation failed",
			"kind", admissionReview.Request.Kind.Kind,
			"name", admissionReview.Request.Name,
			"error", validationErr.Error())
		response.Allowed = false
		if se, ok := errors.AsType[*apierrors.StatusError](validationErr); ok {
			response.Result = new(se.Status())
		} else {
			response.Result = &metav1.Status{
				Status:  metav1.StatusFailure,
				Message: validationErr.Error(),
				Reason:  metav1.StatusReasonInvalid,
				Code:    http.StatusUnprocessableEntity,
			}
		}
	} else {
		response.Allowed = true
		response.Result = &metav1.Status{
			Status: metav1.StatusSuccess,
			Code:   http.StatusOK,
		}
	}

	if len(warnings) > 0 {
		response.Warnings = warnings
	}

	responseReview := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: response,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(responseReview); err != nil {
		serverLog.ErrorContext(r.Context(), "Failed to encode response", "error", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// validate runs the cross-version guard ahead of typed validation, so that a
// write relabelled from an older version is denied with the guard's message
// rather than whatever the stripped object fails in the typed validator.
func (s *WebhookServer) validate(ctx context.Context, ar *admissionv1.AdmissionReview) ([]string, error) {
	if req := ar.Request; req != nil {
		if config.DefaultMutableFeatureGate.Enabled(config.CrossVersionFieldProtection) {
			if err := s.options.CrossVersionGuard.Validate(ctx, req); err != nil {
				return nil, err
			}
		}
		// Version guarded requests get sent for versions where there are no validators in ValidateWithClient
		// so return early to avoid 'no validator registered' errors
		if isCrossVersionGuarded(schema.GroupVersionResource{Group: req.Resource.Group, Version: req.Resource.Version, Resource: req.Resource.Resource}) {
			return nil, nil
		}
	}
	if config.DefaultMutableFeatureGate.Enabled(config.ValidationWebhook) {
		return ValidateWithClient(ar, s.options.Validators, s.options.Client)
	}
	return nil, nil
}
