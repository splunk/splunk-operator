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
	dbclusterinfo "github.com/splunk/splunk-operator/pkg/postgresql/database/ports/clusterinfo"
	identitytypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type readerFunc func(context.Context, string, string) (dbclusterinfo.ResolvedClusterFacts, error)

func (f readerFunc) Read(ctx context.Context, namespace, name string) (dbclusterinfo.ResolvedClusterFacts, error) {
	return f(ctx, namespace, name)
}

func TestGateObserve(t *testing.T) {
	transient := errors.New("apiserver unavailable")
	readyFacts := dbclusterinfo.ResolvedClusterFacts{
		Name: "primary", Namespace: "dbs", Lifecycle: dbclusterinfo.LifecycleReady, Recovery: dbclusterinfo.RecoveryNone,
		Cluster: readyClusterCard("primary", "dbs", "primary-cnpg"),
	}

	tests := []struct {
		name             string
		input            Input
		facts            dbclusterinfo.ResolvedClusterFacts
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
			name:             "ready phase without cluster card waits for identity prerequisite",
			facts:            dbclusterinfo.ResolvedClusterFacts{Lifecycle: dbclusterinfo.LifecycleReady, Recovery: dbclusterinfo.RecoveryNone},
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
			facts:            dbclusterinfo.ResolvedClusterFacts{Lifecycle: "Provisioning", Recovery: dbclusterinfo.RecoveryNone},
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
			facts:            dbclusterinfo.ResolvedClusterFacts{Lifecycle: "Pending", Recovery: dbclusterinfo.RecoveryInProgress},
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
			facts:            dbclusterinfo.ResolvedClusterFacts{Lifecycle: "Pending", Recovery: dbclusterinfo.RecoveryInProgress},
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
			facts:            dbclusterinfo.ResolvedClusterFacts{Lifecycle: "Pending", Recovery: dbclusterinfo.RecoveryInProgress},
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
			err:              fmt.Errorf("read: %w", dbclusterinfo.ErrClusterNotFound),
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
			gate := New(readerFunc(func(_ context.Context, namespace, name string) (dbclusterinfo.ResolvedClusterFacts, error) {
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
				assert.Equal(t, readyFacts.Cluster, result.Facts.Cluster)
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
	assert.ErrorIs(t, outcome.Err(), dbclusterinfo.ErrClusterReaderNotConfigured)
}

func readyClusterCard(logicalName, namespace, authoritativeName string) *identitytypes.ClusterCard {
	authoritative := identitytypes.Environment{
		Identity: identitytypes.ObjectIdentity{
			APIVersion: "postgresql.cnpg.io/v1",
			Kind:       "Cluster",
			Name:       authoritativeName,
			Namespace:  namespace,
		},
		Role:  identitytypes.EnvironmentRoleAuthoritative,
		Scope: identitytypes.NamingScopeConventional,
	}
	return &identitytypes.ClusterCard{
		Logical: identitytypes.ObjectIdentity{
			APIVersion: "platform.splunk.com/v1alpha1",
			Kind:       "PostgresCluster",
			Name:       logicalName,
			Namespace:  namespace,
		},
		Authoritative: authoritative,
		Managed:       []identitytypes.Environment{authoritative},
	}
}
