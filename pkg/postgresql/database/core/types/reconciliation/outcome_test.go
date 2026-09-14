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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestOutcomeValidateRejectsMalformedValues(t *testing.T) {
	tests := []struct {
		name    string
		outcome Outcome
		wantErr string
	}{
		{
			name:    "empty mode",
			outcome: Outcome{},
			wantErr: "outcome mode must be set",
		},
		{
			name:    "unknown mode",
			outcome: Outcome{mode: "Bogus"},
			wantErr: "unknown outcome mode",
		},
		{
			name:    "status fields without status action",
			outcome: Outcome{mode: ModeConverged, phase: "Ready"},
			wantErr: "without status action",
		},
		{
			name: "status action without phase",
			outcome: Outcome{
				mode: ModeConverged, statusAction: StatusPersistAndContinue,
				condition: "Ready", conditionStatus: metav1.ConditionTrue,
			},
			wantErr: "must set Phase",
		},
		{
			name:    "status action without reason",
			outcome: ConvergedFlush("Ready", "", "ready", "Ready"),
			wantErr: "must set Reason",
		},
		{
			name:    "deferred without requeue",
			outcome: Deferred(0),
			wantErr: "Deferred outcome must set RequeueAfter",
		},
		{
			name: "deferred with status action",
			outcome: Outcome{
				mode: ModeDeferred, statusAction: StatusPersistAndStop,
				result: ctrl.Result{RequeueAfter: time.Second},
			},
			wantErr: "deferred outcome must not handle status",
		},
		{
			name:    "retryable without error",
			outcome: RetryableRequeue("Ready", "Retry", "retry", "Provisioning", nil),
			wantErr: "outcome must set Err",
		},
		{
			name:    "terminal without error",
			outcome: TerminalError("Ready", "Terminal", "terminal", "Failed", nil),
			wantErr: "outcome must set Err",
		},
		{
			name:    "waiting without requeue",
			outcome: Waiting("Ready", "Pending", "waiting", "Pending", 0),
			wantErr: "Waiting outcome must set RequeueAfter",
		},
		{
			name:    "invalid condition status",
			outcome: WaitingWithStatus("Ready", metav1.ConditionStatus("Maybe"), "Pending", "waiting", "Pending", time.Second),
			wantErr: "ConditionStatus to True, False, or Unknown",
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			err := tst.outcome.Validate("step")
			require.Error(t, err)
			require.Contains(t, err.Error(), tst.wantErr)
		})
	}
}

func TestOutcomeValidateAcceptsConstructedValues(t *testing.T) {
	wantErr := assertError{}
	for _, outcome := range []Outcome{
		Converged(),
		ConvergedApply("Ready", "Ready", "ready", "Ready"),
		ConvergedStatus("Ready", "Ready", "ready", "Provisioning"),
		ConvergedFlush("Ready", "Ready", "ready", "Ready"),
		Waiting("Ready", "Pending", "waiting", "Pending", time.Second),
		WaitingWithStatus("Ready", metav1.ConditionUnknown, "Pending", "waiting", "Provisioning", time.Second),
		Deferred(time.Second),
		RetryableRequeue("Ready", "Retry", "retry", "Provisioning", wantErr),
		TerminalError("Ready", "Terminal", "terminal", "Failed", wantErr),
		SilentStop(),
	} {
		require.NoError(t, outcome.Validate("step"))
	}
}

type assertError struct{}

func (assertError) Error() string { return "assert error" }
