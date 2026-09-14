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
	"time"

	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type fakeStep struct {
	name         string
	requires     []ContractKey
	provides     []ContractKey
	observeFunc  func(contracts *Contracts, mutationErr error) (reconciliationTypes.Outcome, error)
	observeCalls int
}

func (f *fakeStep) Name() string            { return f.name }
func (f *fakeStep) Requires() []ContractKey { return f.requires }
func (f *fakeStep) Provides() []ContractKey { return f.provides }
func (f *fakeStep) Observe(_ context.Context, contracts *Contracts, mutationErr error) (reconciliationTypes.Outcome, error) {
	f.observeCalls++
	return f.observeFunc(contracts, mutationErr)
}

type mutatingFakeStep struct {
	*fakeStep
	reconcileErr   error
	reconcileFunc  func(contracts *Contracts) error
	reconcileCalls int
}

func (f *mutatingFakeStep) Reconcile(_ context.Context, contracts *Contracts) error {
	f.reconcileCalls++
	if f.reconcileFunc != nil {
		return f.reconcileFunc(contracts)
	}
	return f.reconcileErr
}

func convergedStep(name string) *fakeStep {
	return &fakeStep{
		name: name,
		observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
			return reconciliationTypes.Converged(), nil
		},
	}
}

func TestRun_AllConvergedReturnsBareConverged(t *testing.T) {
	s1, s2 := convergedStep("first"), convergedStep("second")
	outcome, err := Run(context.Background(), []Step{s1, s2}, func(context.Context, reconciliationTypes.Outcome) error {
		t.Fatal("persist must not be called when no step requests it")
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, reconciliationTypes.ModeConverged, outcome.Mode())
	require.Equal(t, 1, s1.observeCalls)
	require.Equal(t, 1, s2.observeCalls)
}

func TestRun_PersistAndContinueWritesOutcomeAndContinues(t *testing.T) {
	next := convergedStep("next")
	clusterGate := &fakeStep{
		name: "cluster-gate",
		observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
			return reconciliationTypes.ConvergedStatus("ClusterReady", "Available", "Cluster is operational", "Provisioning"), nil
		},
	}

	var persisted []reconciliationTypes.Outcome
	outcome, err := Run(context.Background(), []Step{clusterGate, next}, func(_ context.Context, o reconciliationTypes.Outcome) error {
		persisted = append(persisted, o)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, reconciliationTypes.ModeConverged, outcome.Mode())
	require.Equal(t, 1, next.observeCalls)
	require.Len(t, persisted, 1)
	require.Equal(t, reconciliationTypes.StatusPersistAndContinue, persisted[0].StatusAction())
	require.Equal(t, "Provisioning", persisted[0].Phase())
}

func TestRun_RejectsUnflushedApplyAndContinueAtEnd(t *testing.T) {
	next := convergedStep("next")
	privileges := &fakeStep{
		name: "privileges",
		observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
			return reconciliationTypes.ConvergedApply("PrivilegesReady", "Granted", "granted", "Ready"), nil
		},
	}

	var handled []reconciliationTypes.Outcome
	outcome, err := Run(context.Background(), []Step{privileges, next}, func(_ context.Context, o reconciliationTypes.Outcome) error {
		handled = append(handled, o)
		return nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "status applied in memory but not persisted")
	require.Equal(t, reconciliationTypes.ModeConverged, outcome.Mode())
	require.Equal(t, 1, next.observeCalls)
	require.Len(t, handled, 1)
	require.Equal(t, reconciliationTypes.StatusApplyAndContinue, handled[0].StatusAction())
	require.Equal(t, "PrivilegesReady", handled[0].Condition())
	require.Equal(t, "Ready", handled[0].Phase())
}

func TestRun_WaitingPersistsExplicitPendingPhaseAndStops(t *testing.T) {
	reached := convergedStep("unreached")
	waiting := &fakeStep{
		name: "waiting",
		observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
			return reconciliationTypes.Waiting("ClusterReady", "ClusterNotFound", "Cluster CR not found", "Pending", 15*time.Second), nil
		},
	}

	var persisted []reconciliationTypes.Outcome
	outcome, err := Run(context.Background(), []Step{waiting, reached}, func(_ context.Context, o reconciliationTypes.Outcome) error {
		persisted = append(persisted, o)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, reconciliationTypes.ModeWaiting, outcome.Mode())
	require.Equal(t, ctrl.Result{RequeueAfter: 15 * time.Second}, outcome.Result())
	require.Zero(t, reached.observeCalls, "a step after a non-converged outcome must not run")
	require.Len(t, persisted, 1)
	require.Equal(t, reconciliationTypes.StatusPersistAndStop, persisted[0].StatusAction())
	require.Equal(t, "Pending", persisted[0].Phase())
}

func TestRun_WaitingWithStatusPersistsExplicitConditionStatus(t *testing.T) {
	waiting := &fakeStep{
		name: "waiting",
		observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
			return reconciliationTypes.WaitingWithStatus("CustomMetricsReady", metav1.ConditionUnknown, "Pending", "waiting for acknowledgement", "Provisioning", 15*time.Second), nil
		},
	}

	var persisted []reconciliationTypes.Outcome
	outcome, err := Run(context.Background(), []Step{waiting}, func(_ context.Context, o reconciliationTypes.Outcome) error {
		persisted = append(persisted, o)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, reconciliationTypes.ModeWaiting, outcome.Mode())
	require.Equal(t, ctrl.Result{RequeueAfter: 15 * time.Second}, outcome.Result())
	require.Len(t, persisted, 1)
	require.Equal(t, metav1.ConditionUnknown, persisted[0].ConditionStatus())
	require.Equal(t, "CustomMetricsReady", persisted[0].Condition())
}

func TestRun_RetryableRequeuePersistsStatusAndPropagatesError(t *testing.T) {
	wantErr := errors.New("transient failure")
	failing := &fakeStep{
		name: "failing",
		observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
			return reconciliationTypes.RetryableRequeue("CredentialsReady", "Transient", "will retry", "Provisioning", wantErr), nil
		},
	}

	var persisted []reconciliationTypes.Outcome
	outcome, err := Run(context.Background(), []Step{failing}, func(_ context.Context, o reconciliationTypes.Outcome) error {
		persisted = append(persisted, o)
		return nil
	})
	require.ErrorIs(t, err, wantErr)
	require.Equal(t, reconciliationTypes.ModeRetryableRequeue, outcome.Mode())
	require.False(t, errors.Is(err, reconcile.TerminalError(nil)), "retryable must not be classified as terminal")
	require.Len(t, persisted, 1)
	require.Equal(t, "Provisioning", persisted[0].Phase())
}

func TestRun_TerminalErrorPersistsStatusAndWrapsReconcileTerminalError(t *testing.T) {
	wantErr := errors.New("manual intervention required")
	failing := &fakeStep{
		name: "failing",
		observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
			return reconciliationTypes.TerminalError("PrivilegesReady", "Terminal", "give up", "Failed", wantErr), nil
		},
	}

	var persisted []reconciliationTypes.Outcome
	outcome, err := Run(context.Background(), []Step{failing}, func(_ context.Context, o reconciliationTypes.Outcome) error {
		persisted = append(persisted, o)
		return nil
	})
	require.True(t, errors.Is(err, reconcile.TerminalError(nil)), "terminal outcomes must wrap reconcile.TerminalError")
	require.ErrorIs(t, err, wantErr)
	require.Equal(t, reconciliationTypes.ModeTerminalError, outcome.Mode())
	require.Len(t, persisted, 1)
	require.Equal(t, "Failed", persisted[0].Phase())
}

func TestRun_TerminalErrorConstructorWithNilErrorIsRejected(t *testing.T) {
	buggy := &fakeStep{
		name: "buggy",
		observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
			return reconciliationTypes.TerminalError("PrivilegesReady", "Terminal", "give up", "Failed", nil), nil
		},
	}

	_, err := Run(context.Background(), []Step{buggy}, func(context.Context, reconciliationTypes.Outcome) error {
		t.Fatal("status handler must not run for a terminal outcome without an error")
		return nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "outcome must set Err")
}

func TestRun_SilentStopReturnsNilErrorAndNoRequeue(t *testing.T) {
	reached := convergedStep("unreached")
	stop := &fakeStep{
		name: "stop",
		observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
			return reconciliationTypes.SilentStop(), nil
		},
	}

	persistCalls := 0
	outcome, err := Run(context.Background(), []Step{stop, reached}, func(context.Context, reconciliationTypes.Outcome) error {
		persistCalls++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, reconciliationTypes.ModeSilentStop, outcome.Mode())
	require.Equal(t, ctrl.Result{}, outcome.Result())
	require.Zero(t, persistCalls)
	require.Zero(t, reached.observeCalls)
}

func TestRun_WellBehavedMutatingStepReconciles(t *testing.T) {
	step := &mutatingFakeStep{fakeStep: &fakeStep{
		name: "mutator",
		observeFunc: func(_ *Contracts, mutationErr error) (reconciliationTypes.Outcome, error) {
			require.NoError(t, mutationErr)
			return reconciliationTypes.Converged(), nil
		},
	}}

	_, err := Run(context.Background(), []Step{step}, func(context.Context, reconciliationTypes.Outcome) error { return nil })
	require.NoError(t, err)
	require.Equal(t, 1, step.reconcileCalls)
	require.Equal(t, 1, step.observeCalls)
}

func TestRun_MutatingStepReconcileErrorReachesObserve(t *testing.T) {
	wantErr := errors.New("reconcile failed")
	var observedWith error
	step := &mutatingFakeStep{
		fakeStep: &fakeStep{
			name: "mutator",
			observeFunc: func(_ *Contracts, mutationErr error) (reconciliationTypes.Outcome, error) {
				observedWith = mutationErr
				return reconciliationTypes.RetryableRequeue("MutatorReady", "Retry", "retry", "Provisioning", mutationErr), nil
			},
		},
		reconcileErr: wantErr,
	}

	_, err := Run(context.Background(), []Step{step}, func(context.Context, reconciliationTypes.Outcome) error { return nil })
	require.ErrorIs(t, err, wantErr)
	require.ErrorIs(t, observedWith, wantErr)
}

func TestRun_RejectsContractOrderViolation(t *testing.T) {
	consumer := &fakeStep{name: "consumer", requires: []ContractKey{ContractDatabaseCNPGDatabasesReady}, observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
		t.Fatal("must not run before order is validated")
		return reconciliationTypes.Outcome{}, nil
	}}
	producer := &fakeStep{name: "producer", provides: []ContractKey{ContractDatabaseCNPGDatabasesReady}, observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
		t.Fatal("must not run before order is validated")
		return reconciliationTypes.Outcome{}, nil
	}}

	_, err := Run(context.Background(), []Step{consumer, producer}, func(context.Context, reconciliationTypes.Outcome) error { return nil })
	require.Error(t, err)
	require.Contains(t, err.Error(), `requires contract "database.cnpg-databases.ready"`)
	require.Zero(t, consumer.observeCalls)
	require.Zero(t, producer.observeCalls)
}

func TestRun_PassesContractsBetweenSteps(t *testing.T) {
	producer := &fakeStep{name: "producer", provides: []ContractKey{ContractDatabaseCNPGDatabasesReady}, observeFunc: func(c *Contracts, _ error) (reconciliationTypes.Outcome, error) {
		c.CNPGDatabasesReady = &CNPGDatabasesReadyContract{}
		return reconciliationTypes.Converged(), nil
	}}
	consumer := &fakeStep{name: "consumer", requires: []ContractKey{ContractDatabaseCNPGDatabasesReady}, observeFunc: func(c *Contracts, _ error) (reconciliationTypes.Outcome, error) {
		require.NotNil(t, c.CNPGDatabasesReady)
		return reconciliationTypes.Converged(), nil
	}}

	_, err := Run(context.Background(), []Step{producer, consumer}, func(context.Context, reconciliationTypes.Outcome) error { return nil })
	require.NoError(t, err)
	require.Equal(t, 1, producer.observeCalls)
	require.Equal(t, 1, consumer.observeCalls)
}

func TestRun_PassesContractsFromMutationToObserve(t *testing.T) {
	step := &mutatingFakeStep{
		fakeStep: &fakeStep{
			name:     "mutator",
			provides: []ContractKey{ContractDatabaseRWPrivilegesReady},
			observeFunc: func(c *Contracts, mutationErr error) (reconciliationTypes.Outcome, error) {
				require.NoError(t, mutationErr)
				require.NotNil(t, c.RWPrivilegesReady)
				return reconciliationTypes.Converged(), nil
			},
		},
		reconcileFunc: func(c *Contracts) error {
			c.RWPrivilegesReady = &RWPrivilegesReadyContract{}
			return nil
		},
	}

	_, err := Run(context.Background(), []Step{step}, func(context.Context, reconciliationTypes.Outcome) error { return nil })
	require.NoError(t, err)
	require.Equal(t, 1, step.reconcileCalls)
}

func TestRun_RejectsConvergedStepThatDoesNotPublishDeclaredContract(t *testing.T) {
	producer := &fakeStep{
		name:     "producer",
		provides: []ContractKey{ContractDatabaseCNPGDatabasesReady},
		observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
			return reconciliationTypes.Converged(), nil
		},
	}
	consumer := &fakeStep{name: "consumer", requires: []ContractKey{ContractDatabaseCNPGDatabasesReady}, observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
		t.Fatal("consumer must not run after producer failed to publish")
		return reconciliationTypes.Outcome{}, nil
	}}

	_, err := Run(context.Background(), []Step{producer, consumer}, func(context.Context, reconciliationTypes.Outcome) error { return nil })
	require.Error(t, err)
	require.Contains(t, err.Error(), `converged without providing declared contract "database.cnpg-databases.ready"`)
	require.Zero(t, consumer.observeCalls)
}

func TestRun_CreatesFreshContractsForEachPass(t *testing.T) {
	var firstContracts, secondContracts *Contracts
	producer := &fakeStep{name: "producer", provides: []ContractKey{ContractDatabaseCNPGDatabasesReady}, observeFunc: func(c *Contracts, _ error) (reconciliationTypes.Outcome, error) {
		if firstContracts == nil {
			firstContracts = c
		} else {
			secondContracts = c
		}
		c.CNPGDatabasesReady = &CNPGDatabasesReadyContract{}
		return reconciliationTypes.Converged(), nil
	}}
	consumer := &fakeStep{name: "consumer", requires: []ContractKey{ContractDatabaseCNPGDatabasesReady}, observeFunc: func(c *Contracts, _ error) (reconciliationTypes.Outcome, error) {
		require.NotNil(t, c.CNPGDatabasesReady)
		return reconciliationTypes.Converged(), nil
	}}

	_, err := Run(context.Background(), []Step{producer, consumer}, func(context.Context, reconciliationTypes.Outcome) error { return nil })
	require.NoError(t, err)
	_, err = Run(context.Background(), []Step{producer, consumer}, func(context.Context, reconciliationTypes.Outcome) error { return nil })
	require.NoError(t, err)
	require.NotNil(t, firstContracts)
	require.NotNil(t, secondContracts)
	require.NotSame(t, firstContracts, secondContracts)
}

func TestRun_RejectsInvalidConditionStatus(t *testing.T) {
	buggy := &fakeStep{name: "buggy", observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
		return reconciliationTypes.WaitingWithStatus("Ready", metav1.ConditionStatus("Maybe"), "Pending", "waiting", "Pending", time.Second), nil
	}}

	_, err := Run(context.Background(), []Step{buggy}, func(context.Context, reconciliationTypes.Outcome) error {
		t.Fatal("status handler must not run for an invalid condition status")
		return nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ConditionStatus to True, False, or Unknown")
}

func TestRun_RejectsDeferredOutcomeWithoutRequeueAfter(t *testing.T) {
	buggy := &fakeStep{name: "buggy", observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
		return reconciliationTypes.Deferred(0), nil
	}}

	_, err := Run(context.Background(), []Step{buggy}, func(context.Context, reconciliationTypes.Outcome) error {
		t.Fatal("status handler must not run for an invalid deferred outcome")
		return nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Deferred outcome must set RequeueAfter")
}

func TestRun_DeferredOutcomePreservesExplicitRequeueAfter(t *testing.T) {
	provider := &fakeStep{name: "deferred-provider", provides: []ContractKey{ContractDatabaseCNPGDatabasesReady}, observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
		return reconciliationTypes.Deferred(time.Minute), nil
	}}
	dependent := &fakeStep{name: "dependent", requires: []ContractKey{ContractDatabaseCNPGDatabasesReady}, observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
		t.Fatal("dependent step must not run when its runtime contract is missing")
		return reconciliationTypes.Outcome{}, nil
	}}
	independent := convergedStep("independent")

	outcome, err := Run(context.Background(), []Step{provider, dependent, independent}, func(context.Context, reconciliationTypes.Outcome) error {
		t.Fatal("deferred outcomes must not request a status action")
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, reconciliationTypes.ModeDeferred, outcome.Mode())
	require.Equal(t, time.Minute, outcome.Result().RequeueAfter)
	require.Zero(t, dependent.observeCalls)
	require.Equal(t, 1, independent.observeCalls)
}

func TestRun_DeferredOutcomesReturnShortestRequeueAfter(t *testing.T) {
	slow := &fakeStep{name: "slow-deferred", observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
		return reconciliationTypes.Deferred(time.Minute), nil
	}}
	fast := &fakeStep{name: "fast-deferred", observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
		return reconciliationTypes.Deferred(5 * time.Second), nil
	}}

	outcome, err := Run(context.Background(), []Step{slow, fast}, func(context.Context, reconciliationTypes.Outcome) error {
		t.Fatal("deferred outcomes must not request a status action")
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, reconciliationTypes.ModeDeferred, outcome.Mode())
	require.Equal(t, 5*time.Second, outcome.Result().RequeueAfter)
}

func TestRun_RejectsErrorOutcomesWithoutErrors(t *testing.T) {
	tests := []struct {
		name    string
		outcome reconciliationTypes.Outcome
	}{
		{
			name:    "retryable",
			outcome: reconciliationTypes.RetryableRequeue("Ready", "Retry", "retry", "Provisioning", nil),
		},
		{
			name:    "terminal",
			outcome: reconciliationTypes.TerminalError("Ready", "Terminal", "terminal", "Failed", nil),
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			buggy := &fakeStep{name: "buggy", observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
				return tst.outcome, nil
			}}

			_, err := Run(context.Background(), []Step{buggy}, func(context.Context, reconciliationTypes.Outcome) error {
				t.Fatal("status handler must not run for a malformed error outcome")
				return nil
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), "outcome must set Err")
		})
	}
}

func TestRun_RejectsWaitingOutcomeWithoutRequeueAfter(t *testing.T) {
	waiting := &fakeStep{name: "waiting", observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
		return reconciliationTypes.Waiting("Ready", "Pending", "waiting", "Pending", 0), nil
	}}

	_, err := Run(context.Background(), []Step{waiting}, func(context.Context, reconciliationTypes.Outcome) error {
		t.Fatal("status handler must not run for a waiting outcome without a retry delay")
		return nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Waiting outcome must set RequeueAfter")
}

func TestRun_AccumulateThenFlush_PersistsExactlyOnce(t *testing.T) {
	var log []string

	privileges := &fakeStep{name: "privileges", observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
		log = append(log, "privileges-accumulated")
		return reconciliationTypes.ConvergedApply("PrivilegesReady", "Granted", "granted", "Ready"), nil
	}}
	metrics := &fakeStep{name: "metrics", observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
		log = append(log, "metrics-accumulated")
		return reconciliationTypes.ConvergedApply("CustomMetricsReady", "Ready", "ready", "Ready"), nil
	}}
	flush := &mutatingFakeStep{fakeStep: &fakeStep{
		name: "flush",
		observeFunc: func(_ *Contracts, mutationErr error) (reconciliationTypes.Outcome, error) {
			require.NoError(t, mutationErr)
			return reconciliationTypes.ConvergedFlush("Ready", "AllConverged", "database is ready", "Ready"), nil
		},
	}}

	var applied []reconciliationTypes.Outcome
	persistCalls := 0
	outcome, err := Run(context.Background(), []Step{privileges, metrics, flush}, func(_ context.Context, o reconciliationTypes.Outcome) error {
		switch o.StatusAction() {
		case reconciliationTypes.StatusApplyAndContinue:
			applied = append(applied, o)
		case reconciliationTypes.StatusPersistAndStop:
			require.Equal(t, []string{"privileges-accumulated", "metrics-accumulated"}, log,
				"both accumulate steps must have run before persist is called")
			persistCalls++
		default:
			t.Fatalf("unexpected status action %q", o.StatusAction())
		}
		return nil
	})
	require.NoError(t, err)

	require.Equal(t, 1, persistCalls, "the flush point must persist exactly once, not once per accumulating step")
	require.Len(t, applied, 2)
	require.Equal(t, "PrivilegesReady", applied[0].Condition())
	require.Equal(t, "CustomMetricsReady", applied[1].Condition())
	require.Equal(t, reconciliationTypes.ModeConverged, outcome.Mode())
	require.Equal(t, 1, flush.reconcileCalls)
}

func TestRun_AccumulateThenFlush_ReturnsPersistError(t *testing.T) {
	flush := &mutatingFakeStep{fakeStep: &fakeStep{
		name: "flush",
		observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
			return reconciliationTypes.ConvergedFlush("Ready", "AllConverged", "database is ready", "Ready"), nil
		},
	}}

	persistErr := errors.New("status update conflict")
	_, err := Run(context.Background(), []Step{flush}, func(context.Context, reconciliationTypes.Outcome) error { return persistErr })
	require.ErrorIs(t, err, persistErr)
}

func TestRun_FlushStopsThePipeline(t *testing.T) {
	flush := &fakeStep{name: "flush", observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
		return reconciliationTypes.ConvergedFlush("Ready", "AllConverged", "database is ready", "Ready"), nil
	}}
	afterFlush := convergedStep("after-flush")

	persistCalls := 0
	outcome, err := Run(context.Background(), []Step{flush, afterFlush}, func(context.Context, reconciliationTypes.Outcome) error {
		persistCalls++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, persistCalls)
	require.Equal(t, reconciliationTypes.StatusPersistAndStop, outcome.StatusAction())
	require.Zero(t, afterFlush.observeCalls, "no step may run after the flush point persists the authoritative Ready status")
}

func TestRun_RejectsFinalFlushAfterRuntimeDependencySkip(t *testing.T) {
	provider := &fakeStep{name: "deferred-provider", provides: []ContractKey{ContractDatabaseCNPGDatabasesReady}, observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
		return reconciliationTypes.Deferred(time.Second), nil
	}}
	dependent := &fakeStep{name: "dependent", requires: []ContractKey{ContractDatabaseCNPGDatabasesReady}, observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
		t.Fatal("dependent step must not run when its runtime contract is missing")
		return reconciliationTypes.Outcome{}, nil
	}}
	flush := &fakeStep{name: "ready-flush", observeFunc: func(*Contracts, error) (reconciliationTypes.Outcome, error) {
		return reconciliationTypes.ConvergedFlush("Ready", "AllConverged", "database is ready", "Ready"), nil
	}}

	_, err := Run(context.Background(), []Step{provider, dependent, flush}, func(context.Context, reconciliationTypes.Outcome) error {
		t.Fatal("final flush must not persist after a runtime dependency skip")
		return nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "final converged status cannot be persisted")
	require.Zero(t, dependent.observeCalls)
}
