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
package clusterreadiness

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type readerFunc func(context.Context, string, string) (ResolvedClusterFacts, error)

func (f readerFunc) Read(ctx context.Context, namespace, name string) (ResolvedClusterFacts, error) {
	return f(ctx, namespace, name)
}

func TestGateObserve(t *testing.T) {
	transient := errors.New("apiserver unavailable")
	readyFacts := ResolvedClusterFacts{
		Name: "primary", Namespace: "dbs", Lifecycle: LifecycleReady, Recovery: RecoveryNone,
		Provider: &ProviderReference{Kind: ProviderCNPG, Name: "primary-cnpg", Namespace: "dbs"},
	}

	tests := []struct {
		name             string
		input            Input
		facts            ResolvedClusterFacts
		err              error
		wantMode         reconciliationTypes.Mode
		wantAction       reconciliationTypes.StatusAction
		wantStatus       metav1.ConditionStatus
		wantReason       string
		wantMessage      string
		wantPhase        string
		wantRequeueAfter time.Duration
		wantErr          error
	}{
		{
			name:        "ready cluster continues with authoritative facts",
			facts:       readyFacts,
			wantMode:    reconciliationTypes.ModeConverged,
			wantAction:  reconciliationTypes.StatusPersistAndContinue,
			wantStatus:  metav1.ConditionTrue,
			wantReason:  reasonClusterAvailable,
			wantMessage: messageClusterAvailable,
			wantPhase:   phaseProvisioning,
		},
		{
			name:             "ready phase without provider waits for provider prerequisite",
			facts:            ResolvedClusterFacts{Lifecycle: LifecycleReady, Recovery: RecoveryNone},
			wantMode:         reconciliationTypes.ModeWaiting,
			wantAction:       reconciliationTypes.StatusPersistAndStop,
			wantStatus:       metav1.ConditionFalse,
			wantReason:       reasonClusterProvisioning,
			wantMessage:      messageClusterProvisioning,
			wantPhase:        phasePending,
			wantRequeueAfter: reconciliationTypes.ReadinessRetryDelay,
		},
		{
			name:             "provisioning cluster waits",
			facts:            ResolvedClusterFacts{Lifecycle: "Provisioning", Recovery: RecoveryNone},
			wantMode:         reconciliationTypes.ModeWaiting,
			wantAction:       reconciliationTypes.StatusPersistAndStop,
			wantStatus:       metav1.ConditionFalse,
			wantReason:       reasonClusterProvisioning,
			wantMessage:      messageClusterProvisioning,
			wantPhase:        phasePending,
			wantRequeueAfter: reconciliationTypes.ReadinessRetryDelay,
		},
		{
			name:             "recovery after ready is reported distinctly",
			input:            Input{WasReady: true},
			facts:            ResolvedClusterFacts{Lifecycle: "Pending", Recovery: RecoveryInProgress},
			wantMode:         reconciliationTypes.ModeWaiting,
			wantAction:       reconciliationTypes.StatusPersistAndStop,
			wantStatus:       metav1.ConditionFalse,
			wantReason:       reasonClusterRecovery,
			wantMessage:      messageClusterRecovery,
			wantPhase:        phasePending,
			wantRequeueAfter: reconciliationTypes.ReadinessRetryDelay,
		},
		{
			name:             "existing recovery reason remains recovery",
			input:            Input{PreviousClusterReadyReason: reasonClusterRecovery},
			facts:            ResolvedClusterFacts{Lifecycle: "Pending", Recovery: RecoveryInProgress},
			wantMode:         reconciliationTypes.ModeWaiting,
			wantAction:       reconciliationTypes.StatusPersistAndStop,
			wantStatus:       metav1.ConditionFalse,
			wantReason:       reasonClusterRecovery,
			wantMessage:      messageClusterRecovery,
			wantPhase:        phasePending,
			wantRequeueAfter: reconciliationTypes.ReadinessRetryDelay,
		},
		{
			name:             "ordinary not ready cluster is provisioning",
			facts:            ResolvedClusterFacts{Lifecycle: "Pending", Recovery: RecoveryInProgress},
			wantMode:         reconciliationTypes.ModeWaiting,
			wantAction:       reconciliationTypes.StatusPersistAndStop,
			wantStatus:       metav1.ConditionFalse,
			wantReason:       reasonClusterProvisioning,
			wantMessage:      messageClusterProvisioning,
			wantPhase:        phasePending,
			wantRequeueAfter: reconciliationTypes.ReadinessRetryDelay,
		},
		{
			name:             "missing cluster has dedicated wait",
			err:              fmt.Errorf("read: %w", ErrClusterNotFound),
			wantMode:         reconciliationTypes.ModeWaiting,
			wantAction:       reconciliationTypes.StatusPersistAndStop,
			wantStatus:       metav1.ConditionFalse,
			wantReason:       reasonClusterNotFound,
			wantMessage:      messageClusterNotFound,
			wantPhase:        phasePending,
			wantRequeueAfter: reconciliationTypes.ClusterNotFoundRetryDelay,
		},
		{
			name:        "transient read retains error for controller backoff",
			err:         transient,
			wantMode:    reconciliationTypes.ModeRetryableRequeue,
			wantAction:  reconciliationTypes.StatusPersistAndStop,
			wantStatus:  metav1.ConditionFalse,
			wantReason:  reasonClusterInfoFetchFailed,
			wantMessage: messageClusterInfoFetchFailed,
			wantPhase:   phasePending,
			wantErr:     transient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gate := New(readerFunc(func(_ context.Context, namespace, name string) (ResolvedClusterFacts, error) {
				assert.Equal(t, "dbs", namespace)
				assert.Equal(t, "primary", name)
				return tt.facts, tt.err
			}))

			result := gate.Observe(t.Context(), Input{Namespace: "dbs", Name: "primary", WasReady: tt.input.WasReady, PreviousClusterReadyReason: tt.input.PreviousClusterReadyReason})
			outcome := result.Outcome
			require.NoError(t, outcome.Validate("cluster readiness"))
			assert.Equal(t, tt.wantMode, outcome.Mode())
			assert.Equal(t, tt.wantAction, outcome.StatusAction())
			assert.Equal(t, tt.wantStatus, outcome.ConditionStatus())
			assert.Equal(t, tt.wantReason, outcome.Reason())
			assert.Equal(t, tt.wantMessage, outcome.Message())
			assert.Equal(t, tt.wantPhase, outcome.Phase())
			assert.Equal(t, tt.wantRequeueAfter, outcome.Result().RequeueAfter)
			if tt.wantErr != nil {
				assert.ErrorIs(t, outcome.Err(), tt.wantErr)
			}
			if tt.wantMode == reconciliationTypes.ModeConverged {
				assert.Equal(t, readyFacts.Provider, result.Facts.Provider)
			}
		})
	}
}

func TestGateObserveWithoutReaderReportsConfigurationError(t *testing.T) {
	outcome := New(nil).Observe(t.Context(), Input{Namespace: "dbs", Name: "primary"}).Outcome

	require.NoError(t, outcome.Validate("cluster readiness"))
	assert.Equal(t, reconciliationTypes.ModeRetryableRequeue, outcome.Mode())
	assert.Equal(t, reconciliationTypes.StatusPersistAndStop, outcome.StatusAction())
	assert.Equal(t, reasonClusterReaderNotConfigured, outcome.Reason())
	assert.Equal(t, messageClusterReaderNotConfigured, outcome.Message())
	assert.ErrorIs(t, outcome.Err(), ErrClusterReaderNotConfigured)
}
