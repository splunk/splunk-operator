// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//  http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package api

// Event reasons for Platform SDK operations.
// These constants are used with EventRecorder to emit Kubernetes events
// that are visible via kubectl describe.
//
// Usage:
//
//	rctx.EventRecorder().Event(obj, corev1.EventTypeNormal, EventReasonCertificateProvisioned,
//	    "Certificate my-tls has been provisioned by cert-manager")
const (
	// Certificate events
	EventReasonCertificateProvisioned = "CertificateProvisioned"
	EventReasonCertificateReady       = "CertificateReady"
	EventReasonCertificateFailed      = "CertificateFailed"
	EventReasonCertificateRenewing    = "CertificateRenewing"
	EventReasonSelfSignedCertCreated  = "SelfSignedCertCreated"

	// Secret events
	EventReasonSecretValidated      = "SecretValidated"
	EventReasonSecretRotated        = "SecretRotated"
	EventReasonSecretVersionCreated = "SecretVersionCreated"
	EventReasonSecretMissing        = "SecretMissing"
	EventReasonSecretInvalid        = "SecretInvalid"

	// Discovery events
	EventReasonServiceDiscovered  = "ServiceDiscovered"
	EventReasonServiceUnavailable = "ServiceUnavailable"

	// Observability events
	EventReasonObservabilityEnabled = "ObservabilityEnabled"
	EventReasonObservabilityFailed  = "ObservabilityFailed"

	// Configuration events
	EventReasonConfigLoaded  = "ConfigLoaded"
	EventReasonConfigInvalid = "ConfigInvalid"
	EventReasonConfigChanged = "ConfigChanged"
)

// Log levels for Platform SDK operations.
// The SDK uses logr which supports V-levels for log verbosity:
//
// V(0) - Info level: Important state changes, errors
// V(1) - Debug level: Detailed operation logs, cache hits/misses
// V(2) - Trace level: Very detailed logs including all API calls
//
// Usage in services:
//
//	logger.Info("Certificate provisioned", "name", cert.Name)                    // Always logged
//	logger.V(1).Info("Using cached certificate", "name", cert.Name)             // Only in debug mode
//	logger.V(2).Info("Calling cert-manager API", "endpoint", url)               // Only in trace mode
//	logger.Error(err, "Failed to provision certificate", "name", cert.Name)     // Always logged
const (
	// LogLevelInfo (V=0) logs important state changes and errors.
	// These logs are always visible and should be used sparingly.
	LogLevelInfo = 0

	// LogLevelDebug (V=1) logs detailed operation information.
	// Enable with --zap-log-level=debug or --v=1
	LogLevelDebug = 1

	// LogLevelTrace (V=2) logs very detailed information including all API calls.
	// Enable with --zap-log-level=trace or --v=2
	LogLevelTrace = 2
)
