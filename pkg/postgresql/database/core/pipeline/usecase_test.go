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
package pipeline

import (
	"context"
	"errors"
	"testing"

	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
	"github.com/stretchr/testify/require"
)

type fakeUseCase struct {
	prerequisitesErr  error
	prerequisitesFunc func(*Contracts) error
	scheduleResult    bool
	scheduleErr       error
	scheduleFunc      func(*Contracts) (bool, error)
	actOutcome        reconciliationTypes.Outcome
	actErr            error
	actFunc           func(*Contracts) (reconciliationTypes.Outcome, error)

	prerequisitesCalls int
	scheduleCalls      int
	actCalls           int
}

func (u *fakeUseCase) Prerequisites(_ context.Context, contracts *Contracts) error {
	u.prerequisitesCalls++
	if u.prerequisitesFunc != nil {
		return u.prerequisitesFunc(contracts)
	}
	return u.prerequisitesErr
}

func (u *fakeUseCase) Schedule(_ context.Context, contracts *Contracts) (bool, error) {
	u.scheduleCalls++
	if u.scheduleFunc != nil {
		return u.scheduleFunc(contracts)
	}
	return u.scheduleResult, u.scheduleErr
}

func (u *fakeUseCase) Act(_ context.Context, contracts *Contracts) (reconciliationTypes.Outcome, error) {
	u.actCalls++
	if u.actFunc != nil {
		return u.actFunc(contracts)
	}
	return u.actOutcome, u.actErr
}

func TestUseCaseStep_DeferredWhenPrerequisiteNotReady(t *testing.T) {
	uc := &fakeUseCase{prerequisitesErr: ErrPrerequisiteNotReady, scheduleResult: true}
	step := NewUseCaseStep("upgrade", uc, nil, nil)

	contracts := NewContracts()
	require.NoError(t, step.Reconcile(context.Background(), contracts))
	outcome, err := step.Observe(context.Background(), contracts, nil)
	require.NoError(t, err)
	require.Equal(t, reconciliationTypes.ModeDeferred, outcome.Mode())
	require.Equal(t, reconciliationTypes.StatusNone, outcome.StatusAction())
	require.Equal(t, runtimeDependencyRequeueAfter, outcome.Result().RequeueAfter)
	require.Zero(t, uc.scheduleCalls, "Schedule must not be reached when a prerequisite defers the use case")
	require.Zero(t, uc.actCalls)
}

func TestUseCaseStep_DeferredUseCaseLetsPipelineContinue(t *testing.T) {
	uc := &fakeUseCase{prerequisitesErr: ErrPrerequisiteNotReady, scheduleResult: true}
	useCaseStep := NewUseCaseStep("upgrade", uc, nil, []ContractKey{ContractDatabaseRWPrivilegesReady})
	dependent := &fakeStep{name: "dependent", requires: []ContractKey{ContractDatabaseRWPrivilegesReady}, observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
		t.Fatal("dependent steps must not run until the deferred use case publishes its contract")
		return reconciliationTypes.Converged(), nil
	}}
	independent := convergedStep("independent")

	outcome, err := Run(context.Background(), []Step{useCaseStep, dependent, independent}, func(context.Context, reconciliationTypes.Outcome) error {
		t.Fatal("deferred use cases must not request a status action")
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, reconciliationTypes.ModeDeferred, outcome.Mode())
	require.Equal(t, reconciliationTypes.StatusNone, outcome.StatusAction())
	require.Equal(t, runtimeDependencyRequeueAfter, outcome.Result().RequeueAfter)
	require.Zero(t, dependent.observeCalls)
	require.Equal(t, 1, independent.observeCalls)
	require.Zero(t, uc.scheduleCalls)
	require.Zero(t, uc.actCalls)
}

func TestUseCaseStep_PropagatesGenuinePrerequisiteError(t *testing.T) {
	wantErr := errors.New("cannot read upstream state")
	uc := &fakeUseCase{prerequisitesErr: wantErr}
	step := NewUseCaseStep("upgrade", uc, nil, nil)

	contracts := NewContracts()
	err := step.Reconcile(context.Background(), contracts)
	require.ErrorIs(t, err, wantErr)
	outcome, observeErr := step.Observe(context.Background(), contracts, err)
	require.ErrorIs(t, observeErr, wantErr)
	require.Equal(t, reconciliationTypes.Outcome{}, outcome)
}

func TestUseCaseStep_NotScheduledConvergesWithoutActing(t *testing.T) {
	uc := &fakeUseCase{scheduleResult: false}
	step := NewUseCaseStep("upgrade", uc, nil, nil)

	contracts := NewContracts()
	require.NoError(t, step.Reconcile(context.Background(), contracts))
	outcome, err := step.Observe(context.Background(), contracts, nil)
	require.NoError(t, err)
	require.Equal(t, reconciliationTypes.ModeConverged, outcome.Mode())
	require.Equal(t, reconciliationTypes.StatusNone, outcome.StatusAction())
	require.Zero(t, uc.actCalls, "Act must not run when Schedule reports no work this pass")
}

func TestUseCaseStep_UnscheduledProviderPublishesSatisfiedContractForConsumer(t *testing.T) {
	uc := &fakeUseCase{scheduleFunc: func(contracts *Contracts) (bool, error) {
		contracts.RWPrivilegesReady = &RWPrivilegesReadyContract{}
		return false, nil
	}}
	provider := NewUseCaseStep("rw-privilege-bootstrap", uc, nil, []ContractKey{ContractDatabaseRWPrivilegesReady})
	consumer := &fakeStep{name: "custom-metrics", requires: []ContractKey{ContractDatabaseRWPrivilegesReady}, observeFunc: func(contracts *Contracts, _ error) (reconciliationTypes.Outcome, error) {
		require.NotNil(t, contracts.RWPrivilegesReady)
		return reconciliationTypes.Converged(), nil
	}}

	outcome, err := Run(context.Background(), []Step{provider, consumer}, func(context.Context, reconciliationTypes.Outcome) error { return nil })
	require.NoError(t, err)
	require.Equal(t, reconciliationTypes.ModeConverged, outcome.Mode())
	require.Equal(t, 1, consumer.observeCalls)
	require.Equal(t, 1, uc.scheduleCalls)
	require.Zero(t, uc.actCalls)
}

func TestUseCaseStep_UnscheduledProviderWithoutContractIsRejected(t *testing.T) {
	uc := &fakeUseCase{scheduleResult: false}
	provider := NewUseCaseStep("rw-privilege-bootstrap", uc, nil, []ContractKey{ContractDatabaseRWPrivilegesReady})
	consumer := &fakeStep{name: "custom-metrics", requires: []ContractKey{ContractDatabaseRWPrivilegesReady}, observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
		t.Fatal("consumer must not run when provider did not publish its contract")
		return reconciliationTypes.Outcome{}, nil
	}}

	_, err := Run(context.Background(), []Step{provider, consumer}, func(context.Context, reconciliationTypes.Outcome) error { return nil })
	require.Error(t, err)
	require.Contains(t, err.Error(), `converged without providing declared contract "database.rw-privileges.ready"`)
	require.Zero(t, consumer.observeCalls)
	require.Equal(t, 1, uc.scheduleCalls)
	require.Zero(t, uc.actCalls)
}

func TestUseCaseStep_ScheduledReportsActsOutcome(t *testing.T) {
	want := reconciliationTypes.RetryableRequeue("Upgrade", "InProgress", "step 2 of 5", "Provisioning", errors.New("keep going"))
	uc := &fakeUseCase{scheduleResult: true, actOutcome: want}
	step := NewUseCaseStep("upgrade", uc, nil, nil)

	contracts := NewContracts()
	require.NoError(t, step.Reconcile(context.Background(), contracts))
	outcome, err := step.Observe(context.Background(), contracts, nil)
	require.NoError(t, err)
	require.Equal(t, want, outcome, "Observe must report exactly what Act decided, not reinterpret it")
}

func TestUseCaseStep_ActErrorWithClassifiedOutcomeIsHandledByRunner(t *testing.T) {
	tests := []struct {
		name     string
		outcome  func(error) reconciliationTypes.Outcome
		wantMode reconciliationTypes.Mode
	}{
		{
			name: "retryable",
			outcome: func(err error) reconciliationTypes.Outcome {
				return reconciliationTypes.RetryableRequeue("Upgrade", "InProgress", "step 2 of 5", "Provisioning", err)
			},
			wantMode: reconciliationTypes.ModeRetryableRequeue,
		},
		{
			name: "terminal",
			outcome: func(err error) reconciliationTypes.Outcome {
				return reconciliationTypes.TerminalError("Upgrade", "Terminal", "manual intervention required", "Failed", err)
			},
			wantMode: reconciliationTypes.ModeTerminalError,
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			wantErr := errors.New(tst.name + " failure")
			uc := &fakeUseCase{scheduleResult: true, actOutcome: tst.outcome(wantErr), actErr: wantErr}
			step := NewUseCaseStep("upgrade", uc, nil, nil)
			unreached := convergedStep("unreached")

			var persisted []reconciliationTypes.Outcome
			outcome, err := Run(context.Background(), []Step{step, unreached}, func(_ context.Context, o reconciliationTypes.Outcome) error {
				persisted = append(persisted, o)
				return nil
			})

			require.ErrorIs(t, err, wantErr)
			require.Equal(t, tst.wantMode, outcome.Mode())
			require.Len(t, persisted, 1)
			require.Equal(t, tst.wantMode, persisted[0].Mode())
			require.Equal(t, reconciliationTypes.StatusPersistAndStop, persisted[0].StatusAction())
			require.Zero(t, unreached.observeCalls)
		})
	}
}

func TestUseCaseStep_ResultStateIsPerContracts(t *testing.T) {
	want := reconciliationTypes.ConvergedStatus("Upgrade", "Ready", "done", "Ready")
	uc := &fakeUseCase{scheduleResult: true, actOutcome: want}
	step := NewUseCaseStep("upgrade", uc, nil, nil)

	firstContracts := NewContracts()
	require.NoError(t, step.Reconcile(context.Background(), firstContracts))

	uc.scheduleResult = false
	secondContracts := NewContracts()
	require.NoError(t, step.Reconcile(context.Background(), secondContracts))

	firstOutcome, err := step.Observe(context.Background(), firstContracts, nil)
	require.NoError(t, err)
	require.Equal(t, want, firstOutcome)

	secondOutcome, err := step.Observe(context.Background(), secondContracts, nil)
	require.NoError(t, err)
	require.Equal(t, reconciliationTypes.ModeConverged, secondOutcome.Mode())
	require.Equal(t, reconciliationTypes.StatusNone, secondOutcome.StatusAction())
}

func TestUseCaseStep_ActErrorPropagatesAsGenuineFailure(t *testing.T) {
	wantErr := errors.New("act blew up")
	uc := &fakeUseCase{scheduleResult: true, actErr: wantErr}
	step := NewUseCaseStep("upgrade", uc, nil, nil)

	contracts := NewContracts()
	err := step.Reconcile(context.Background(), contracts)
	require.ErrorIs(t, err, wantErr)
	_, observeErr := step.Observe(context.Background(), contracts, err)
	require.ErrorIs(t, observeErr, wantErr)
}

var _ UseCase = (*fakeUseCase)(nil)
