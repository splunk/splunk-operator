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

package common

import (
	"errors"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TerminalError is a user-actionable, non-retryable reconciliation failure.
// It carries a short, sanitized Message suitable for a CR status condition,
// while preserving the full underlying error for operator logs. Its Unwrap
// chain contains a controller-runtime terminal error, so the standard
// errors.Is(err, reconcile.TerminalError(nil)) check succeeds and the
// controller does not requeue.
type TerminalError struct {
	Reason  string // PascalCase, machine readable
	Message string // concise, user-facing; MUST NOT contain the full trace
	Err     error  // full detail, for logs
}

// NewTerminalError wraps cause with a PascalCase machine-readable reason and a short condition message.
func NewTerminalError(reason, message string, cause error) error {
	return &TerminalError{Reason: reason, Message: message, Err: cause}
}

func (e *TerminalError) Error() string {
	if e.Err == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Err)
}

// Unwrap yields a controller-runtime terminal error so errors.Is/As traverse
// through to both the terminal marker and the original cause.
func (e *TerminalError) Unwrap() error {
	return reconcile.TerminalError(e.Err)
}

// TerminalMessage returns the condition-friendly message if err (or anything it
// wraps) is a *TerminalError.
func TerminalMessage(err error) (string, bool) {
	var te *TerminalError
	if errors.As(err, &te) {
		return te.Message, true
	}
	return "", false
}

// TerminalReason returns the PascalCase machine-readable reason if err (or
// anything it wraps) is a *TerminalError.
func TerminalReason(err error) (string, bool) {
	var te *TerminalError
	if errors.As(err, &te) {
		return te.Reason, true
	}
	return "", false
}
