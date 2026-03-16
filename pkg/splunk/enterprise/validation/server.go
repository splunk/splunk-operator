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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var serverLog = ctrl.Log.WithName("webhook-server")

// WebhookServerOptions contains configuration for the webhook server
type WebhookServerOptions struct {
	// TLSCertFile is the path to the TLS certificate file
	TLSCertFile string

	// TLSKeyFile is the path to the TLS key file
	TLSKeyFile string

	// Port is the port to listen on
	Port int

	// Validators is the map of validators by GVR
	Validators map[schema.GroupVersionResource]Validator

	// CertDir is the directory containing tls.crt and tls.key
	CertDir string

	// ReadTimeout is the maximum duration for reading the entire request (default: 10s)
	ReadTimeout time.Duration

	// WriteTimeout is the maximum duration before timing out writes of the response (default: 10s)
	WriteTimeout time.Duration

	// Client is the Kubernetes client for resource lookups (optional)
	Client client.Client
}

// WebhookServer is the HTTP server for validation webhooks
type WebhookServer struct {
	options    WebhookServerOptions
	httpServer *http.Server
}

// NewWebhookServer creates a new webhook server
func NewWebhookServer(options WebhookServerOptions) *WebhookServer {
	return &WebhookServer{
		options: options,
	}
}

// Start starts the webhook server
func (s *WebhookServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Register validation endpoint
	mux.HandleFunc("/validate", s.handleValidate)

	// Register health check endpoint
	mux.HandleFunc("/readyz", s.handleReadyz)

	// Determine cert and key paths
	certFile := s.options.TLSCertFile
	keyFile := s.options.TLSKeyFile
	if certFile == "" && s.options.CertDir != "" {
		certFile = s.options.CertDir + "/tls.crt"
		keyFile = s.options.CertDir + "/tls.key"
	}

	// Configure TLS
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// Use configured timeouts or defaults
	readTimeout := s.options.ReadTimeout
	if readTimeout == 0 {
		readTimeout = 10 * time.Second
	}
	writeTimeout := s.options.WriteTimeout
	if writeTimeout == 0 {
		writeTimeout = 10 * time.Second
	}

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.options.Port),
		Handler:      mux,
		TLSConfig:    tlsConfig,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	serverLog.Info("Starting webhook server", "port", s.options.Port)

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		if certFile != "" && keyFile != "" {
			errChan <- s.httpServer.ListenAndServeTLS(certFile, keyFile)
		} else {
			errChan <- s.httpServer.ListenAndServe()
		}
	}()

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		serverLog.Info("Shutting down webhook server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(shutdownCtx)
	case err := <-errChan:
		return err
	}
}

// handleValidate handles validation requests
func (s *WebhookServer) handleValidate(w http.ResponseWriter, r *http.Request) {
	reqLog := log.FromContext(r.Context()).WithName("webhook-server")
	reqLog.V(1).Info("Received validation request", "method", r.Method, "path", r.URL.Path)

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		reqLog.Error(err, "Failed to read request body")
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var admissionReview admissionv1.AdmissionReview
	if err := json.Unmarshal(body, &admissionReview); err != nil {
		reqLog.Error(err, "Failed to decode admission review")
		http.Error(w, "Failed to decode admission review", http.StatusBadRequest)
		return
	}

	if admissionReview.Request != nil {
		reqLog.Info("Processing admission request",
			"kind", admissionReview.Request.Kind.Kind,
			"name", admissionReview.Request.Name,
			"namespace", admissionReview.Request.Namespace,
			"operation", admissionReview.Request.Operation,
			"user", admissionReview.Request.UserInfo.Username)
	}

	warnings, validationErr := ValidateWithClient(&admissionReview, s.options.Validators, s.options.Client)

	response := &admissionv1.AdmissionResponse{
		UID: admissionReview.Request.UID,
	}

	if validationErr != nil {
		reqLog.Info("Validation failed",
			"kind", admissionReview.Request.Kind.Kind,
			"name", admissionReview.Request.Name,
			"error", validationErr.Error())
		response.Allowed = false
		response.Result = &metav1.Status{
			Status:  metav1.StatusFailure,
			Message: validationErr.Error(),
			Reason:  metav1.StatusReasonInvalid,
			Code:    http.StatusUnprocessableEntity,
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
		serverLog.Error(err, "Failed to encode response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// handleReadyz handles readiness probe requests
func (s *WebhookServer) handleReadyz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
