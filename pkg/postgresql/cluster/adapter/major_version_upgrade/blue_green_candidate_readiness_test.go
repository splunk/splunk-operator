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

package majorupgradeadapter

import (
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCandidatePluginStartupRecoveryHasDurableDeadline(t *testing.T) {
	created := time.Date(2026, 11, 19, 13, 32, 29, 0, time.UTC)
	candidate := &cnpgv1.Cluster{
		CreationTimestamp: metav1.NewTime(created),
		Status:            cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseFailurePlugin, PhaseReason: `Role "green-barman-cloud" not found`},
	}
	for _, elapsed := range []time.Duration{0, 25 * time.Second, candidatePluginStartupGracePeriod - time.Nanosecond} {
		err := candidateBlockingError(candidate.DeepCopy(), created.Add(elapsed))
		require.ErrorIs(t, err, mvutypes.ErrCandidatePluginPending)
		assert.False(t, mvutypes.IsTerminalFlowFailure(err), "re-entry must let CNPG recover without an explicit retry")
	}
	for _, elapsed := range []time.Duration{candidatePluginStartupGracePeriod, 2 * candidatePluginStartupGracePeriod} {
		err := candidateBlockingError(candidate.DeepCopy(), created.Add(elapsed))
		require.ErrorIs(t, err, mvutypes.ErrCandidatePluginTimedOut)
		assert.True(t, mvutypes.IsTerminalFlowFailure(err), "the same candidate cannot reset the deadline")
		assert.NotErrorIs(t, err, mvutypes.ErrCandidateInvalid, "dependency failure does not invalidate imported data")
	}
	candidate.Status.Phase = cnpgv1.PhaseHealthy
	require.NoError(t, candidateBlockingError(candidate, created.Add(2*candidatePluginStartupGracePeriod)), "a corrected dependency can be explicitly retried even after its original startup window")
}

func TestCandidateStartupGraceDoesNotHidePermanentFailures(t *testing.T) {
	created := metav1.Now()
	for _, phase := range []string{
		cnpgv1.PhaseUnknownPlugin, cnpgv1.PhaseWaitingForUser, cnpgv1.PhaseUnrecoverable,
		cnpgv1.PhaseCannotCreateClusterObjects, cnpgv1.PhaseImageCatalogError, cnpgv1.PhaseArchitectureBinaryMissing,
	} {
		t.Run(phase, func(t *testing.T) {
			candidate := &cnpgv1.Cluster{CreationTimestamp: created, Status: cnpgv1.ClusterStatus{Phase: phase}}
			err := candidateBlockingError(candidate, created.Time)
			require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
			assert.NotErrorIs(t, err, mvutypes.ErrCandidatePluginPending)
		})
	}
	require.ErrorIs(t, candidateBlockingError(&cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseFailurePlugin}}, created.Time), mvutypes.ErrCandidatePluginTimedOut, "missing durable creation time cannot grant an unbounded wait")
}
