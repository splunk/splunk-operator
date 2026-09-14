/*
Copyright 2026.

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

package reconciliationTypes

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type Mode string

const (
	ModeConverged        Mode = "Converged"
	ModeWaiting          Mode = "Waiting"
	ModeRetryableRequeue Mode = "RetryableRequeue"
	ModeTerminalError    Mode = "TerminalError"
	ModeSilentStop       Mode = "SilentStop"
	ModeDeferred         Mode = "Deferred"
)

type StatusAction string

const (
	StatusNone               StatusAction = ""
	StatusApplyAndContinue   StatusAction = "ApplyAndContinue"
	StatusPersistAndContinue StatusAction = "PersistAndContinue"
	StatusPersistAndStop     StatusAction = "PersistAndStop"
)

// Outcome describes a database reconciliation result and the status action it requests.
type Outcome struct {
	mode Mode

	statusAction StatusAction

	phase           string
	condition       string
	conditionStatus metav1.ConditionStatus
	reason          string
	message         string

	result ctrl.Result

	err error
}

func (o Outcome) Mode() Mode { return o.mode }

func (o Outcome) StatusAction() StatusAction { return o.statusAction }

func (o Outcome) Phase() string { return o.phase }

func (o Outcome) Condition() string { return o.condition }

func (o Outcome) ConditionStatus() metav1.ConditionStatus { return o.conditionStatus }

func (o Outcome) Reason() string { return o.reason }

func (o Outcome) Message() string { return o.message }

func (o Outcome) Result() ctrl.Result { return o.result }

func (o Outcome) Err() error { return o.err }

// Validate rejects internally inconsistent outcomes before they reach status handling.
func (o Outcome) Validate(stepName string) error {
	if o.mode == "" {
		return fmt.Errorf("%s: outcome mode must be set", stepName)
	}
	if !isValidMode(o.mode) {
		return fmt.Errorf("%s: unknown outcome mode %q", stepName, o.mode)
	}
	if (o.mode == ModeRetryableRequeue || o.mode == ModeTerminalError) && o.err == nil {
		return fmt.Errorf("%s: %s outcome must set Err", stepName, o.mode)
	}
	if (o.mode == ModeWaiting || o.mode == ModeDeferred) && o.result.RequeueAfter <= 0 {
		return fmt.Errorf("%s: %s outcome must set RequeueAfter", stepName, o.mode)
	}
	if o.mode == ModeDeferred && o.statusAction != StatusNone {
		return fmt.Errorf("%s: deferred outcome must not handle status", stepName)
	}

	switch o.statusAction {
	case StatusNone:
		if o.phase != "" || o.condition != "" || o.conditionStatus != "" || o.reason != "" || o.message != "" {
			return fmt.Errorf("%s: outcome without status action must not set status fields", stepName)
		}
	case StatusApplyAndContinue, StatusPersistAndContinue:
		if o.mode != ModeConverged {
			return fmt.Errorf("%s: only converged outcomes may handle status and continue", stepName)
		}
		if err := validateStatusFields(stepName, o); err != nil {
			return err
		}
	case StatusPersistAndStop:
		if o.mode == ModeSilentStop {
			return fmt.Errorf("%s: silent stop must not persist status", stepName)
		}
		if err := validateStatusFields(stepName, o); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%s: unknown status action %q", stepName, o.statusAction)
	}

	return nil
}

func (o Outcome) IsClassifiedError() bool {
	return o.mode == ModeRetryableRequeue || o.mode == ModeTerminalError
}

// Converged reports that a step is done without a status action.
func Converged() Outcome {
	return Outcome{mode: ModeConverged}
}

// ConvergedApply reports convergence, applies status in memory, and continues.
func ConvergedApply(condition, reason, message, phase string) Outcome {
	return Outcome{
		mode: ModeConverged, statusAction: StatusApplyAndContinue, phase: phase,
		condition: condition, conditionStatus: metav1.ConditionTrue, reason: reason, message: message,
	}
}

// ConvergedStatus reports convergence, persists status, and continues.
func ConvergedStatus(condition, reason, message, phase string) Outcome {
	return Outcome{
		mode: ModeConverged, statusAction: StatusPersistAndContinue, phase: phase,
		condition: condition, conditionStatus: metav1.ConditionTrue, reason: reason, message: message,
	}
}

// ConvergedFlush reports convergence and marks the status flush point.
func ConvergedFlush(condition, reason, message, phase string) Outcome {
	return Outcome{
		mode: ModeConverged, statusAction: StatusPersistAndStop, phase: phase,
		condition: condition, conditionStatus: metav1.ConditionTrue, reason: reason, message: message,
	}
}

// Waiting reports an expected wait with a fixed requeue delay.
func Waiting(condition, reason, message, phase string, after time.Duration) Outcome {
	return WaitingWithStatus(condition, metav1.ConditionFalse, reason, message, phase, after)
}

// WaitingWithStatus reports an expected wait with an explicit condition status.
func WaitingWithStatus(condition string, conditionStatus metav1.ConditionStatus, reason, message, phase string, after time.Duration) Outcome {
	return Outcome{
		mode: ModeWaiting, statusAction: StatusPersistAndStop, phase: phase, condition: condition, conditionStatus: conditionStatus,
		reason: reason, message: message, result: ctrl.Result{RequeueAfter: after},
	}
}

// Deferred reports incomplete work whose prerequisites are not ready yet.
func Deferred(after time.Duration) Outcome {
	return Outcome{mode: ModeDeferred, result: ctrl.Result{RequeueAfter: after}}
}

// RetryableRequeue reports a transient error.
func RetryableRequeue(condition, reason, message, phase string, err error) Outcome {
	return Outcome{
		mode: ModeRetryableRequeue, statusAction: StatusPersistAndStop, phase: phase, condition: condition, conditionStatus: metav1.ConditionFalse,
		reason: reason, message: message, err: err,
	}
}

// TerminalError reports a user-actionable failure.
func TerminalError(condition, reason, message, phase string, err error) Outcome {
	if err != nil {
		err = reconcile.TerminalError(err)
	}
	return Outcome{
		mode: ModeTerminalError, statusAction: StatusPersistAndStop, phase: phase, condition: condition, conditionStatus: metav1.ConditionFalse,
		reason: reason, message: message, err: err,
	}
}

// SilentStop ends the pass immediately: no write, no requeue, no error.
func SilentStop() Outcome {
	return Outcome{mode: ModeSilentStop}
}

func validateStatusFields(stepName string, outcome Outcome) error {
	if outcome.phase == "" {
		return fmt.Errorf("%s: status action outcome must set Phase", stepName)
	}
	if outcome.condition == "" {
		return fmt.Errorf("%s: status action outcome must set Condition", stepName)
	}
	if outcome.reason == "" {
		return fmt.Errorf("%s: status action outcome must set Reason", stepName)
	}
	if outcome.conditionStatus == "" {
		return fmt.Errorf("%s: status action outcome must set ConditionStatus", stepName)
	}
	if !isValidConditionStatus(outcome.conditionStatus) {
		return fmt.Errorf("%s: status action outcome must set ConditionStatus to True, False, or Unknown", stepName)
	}
	return nil
}

func isValidConditionStatus(status metav1.ConditionStatus) bool {
	return status == metav1.ConditionTrue || status == metav1.ConditionFalse || status == metav1.ConditionUnknown
}

func isValidMode(mode Mode) bool {
	switch mode {
	case ModeConverged, ModeWaiting, ModeRetryableRequeue, ModeTerminalError, ModeSilentStop, ModeDeferred:
		return true
	default:
		return false
	}
}
