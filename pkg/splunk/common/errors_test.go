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
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestNewTerminalError_Error(t *testing.T) {
	cause := errors.New("image pull failed")
	err := NewTerminalError("PodTerminalFailure", "Pod stuck in terminal state", cause)

	want := "Pod stuck in terminal state: image pull failed"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestNewTerminalError_NilCause(t *testing.T) {
	err := NewTerminalError("SpecValidationFailed", "spec is invalid", nil)
	if got := err.Error(); got != "spec is invalid" {
		t.Errorf("Error() with nil cause = %q, want %q", got, "spec is invalid")
	}
}

func TestNewTerminalError_SatisfiesReconcileTerminalError(t *testing.T) {
	err := NewTerminalError("PodTerminalFailure", "Pod stuck in terminal state", errors.New("ImagePullBackOff"))
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("errors.Is(err, reconcile.TerminalError(nil)) = false; want true so controller does not requeue")
	}
}

func TestTerminalMessage_Found(t *testing.T) {
	err := NewTerminalError("SpecValidationFailed", "spec validation failed", errors.New("bad field"))
	msg, ok := TerminalMessage(err)
	if !ok {
		t.Fatal("TerminalMessage returned ok=false, want true")
	}
	if msg != "spec validation failed" {
		t.Errorf("TerminalMessage = %q, want %q", msg, "spec validation failed")
	}
}

func TestTerminalMessage_NotFound(t *testing.T) {
	err := errors.New("plain error")
	_, ok := TerminalMessage(err)
	if ok {
		t.Error("TerminalMessage returned ok=true for a plain error, want false")
	}
}

func TestTerminalMessage_WrappedTerminalError(t *testing.T) {
	inner := NewTerminalError("InnerReason", "inner message", errors.New("root cause"))
	wrapped := errors.Join(errors.New("outer"), inner)
	msg, ok := TerminalMessage(wrapped)
	if !ok {
		t.Fatal("TerminalMessage returned ok=false for wrapped TerminalError, want true")
	}
	if msg != "inner message" {
		t.Errorf("TerminalMessage = %q, want %q", msg, "inner message")
	}
}

func TestTerminalMessage_ReconcileTerminalErrorNotMatched(t *testing.T) {
	err := reconcile.TerminalError(errors.New("raw cause"))
	_, ok := TerminalMessage(err)
	if ok {
		t.Error("TerminalMessage returned ok=true for a bare reconcile.TerminalError (no *TerminalError in chain), want false")
	}
}

func TestTerminalReason_Found(t *testing.T) {
	err := NewTerminalError("SpecValidationFailed", "spec validation failed", errors.New("bad field"))
	reason, ok := TerminalReason(err)
	if !ok {
		t.Fatal("TerminalReason returned ok=false, want true")
	}
	if reason != "SpecValidationFailed" {
		t.Errorf("TerminalReason = %q, want %q", reason, "SpecValidationFailed")
	}
}

func TestTerminalReason_NotFound(t *testing.T) {
	err := errors.New("plain error")
	_, ok := TerminalReason(err)
	if ok {
		t.Error("TerminalReason returned ok=true for a plain error, want false")
	}
}
