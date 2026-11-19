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

package majorversionupgradetypes

import (
	"testing"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func cleanupEvidence() platformv1alpha1.PostgresMajorUpgradeStatus {
	bg := completionEvidence()
	now := bg.Completion.StartedAt
	bg.Completion.ReplicationRemovedAt = now.DeepCopy()
	bg.Completion.SourceCredentialsRetiredAt = now.DeepCopy()
	bg.Completion.CredentialsRemovedAt = now.DeepCopy()
	bg.Blue = &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{
		APIVersion: "postgresql.cnpg.io/v1", Kind: "Cluster", Namespace: "default", Name: "blue", UID: "blue-uid",
	}}
	bg.Green.Ref.APIVersion, bg.Green.Ref.Kind = bg.Blue.Ref.APIVersion, bg.Blue.Ref.Kind
	bg.CommitReceipt.Target = bg.Green.Ref.DeepCopy()
	bg.Cleanup = &platformv1alpha1.BlueGreenCleanupStatus{
		State: platformv1alpha1.BlueGreenCleanupStateCleaning, StartedAt: now.DeepCopy(),
		EnvironmentRef: bg.Blue.Ref.DeepCopy(), RetirementGeneration: new(int64(1)),
	}
	bg.Gates = &platformv1alpha1.BlueGreenGateStatus{Cleanup: &platformv1alpha1.BlueGreenGateConsumptionStatus{
		Generation: 3, ConsumedAt: now.DeepCopy(),
	}}
	return platformv1alpha1.PostgresMajorUpgradeStatus{
		Phase: new(string(Completed)), Strategy: new(MajorUpgradeFlowBlueGreen),
		SourcePgVersion: new("17"), TargetPgVersion: new("18"), BlueGreen: bg,
	}
}

func cancelledCleanupEvidence() platformv1alpha1.PostgresMajorUpgradeStatus {
	entry := cleanupEvidence()
	bg := entry.BlueGreen
	now := bg.Completion.StartedAt.DeepCopy()
	bg.Gates.Cancellation = &platformv1alpha1.BlueGreenCancellationStatus{
		RequestedAt: now, SourceAccessRestoredAt: now.DeepCopy(), ReplicationRemovedAt: now.DeepCopy(),
		CredentialsRemovedAt: now.DeepCopy(), GreenFencedAt: now.DeepCopy(), CompletedAt: now.DeepCopy(),
	}
	entry.Phase = new(string(Cancelled))
	bg.Completion, bg.Recoverability, bg.CommitReceipt, bg.Switchover = nil, nil, nil, nil
	bg.Green, bg.Cleanup.EnvironmentRef = nil, nil
	return entry
}

func TestCleanupRequiresTerminalProductionEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*platformv1alpha1.PostgresMajorUpgradeStatus)
	}{
		{"active", func(e *platformv1alpha1.PostgresMajorUpgradeStatus) { e.Phase = new(string(Upgrading)) }},
		{"failed", func(e *platformv1alpha1.PostgresMajorUpgradeStatus) { e.Phase = new(string(Failed)) }},
		{"recoverable", func(e *platformv1alpha1.PostgresMajorUpgradeStatus) { e.Phase = new(string(Recoverable)) }},
		{"wrong strategy", func(e *platformv1alpha1.PostgresMajorUpgradeStatus) { e.Strategy = new("pgUpgrade") }},
		{"missing attempt", func(e *platformv1alpha1.PostgresMajorUpgradeStatus) { e.BlueGreen.AttemptID = "" }},
		{"missing availability", func(e *platformv1alpha1.PostgresMajorUpgradeStatus) { e.BlueGreen.Switchover.TargetAvailableAt = nil }},
		{"missing WAL proof", func(e *platformv1alpha1.PostgresMajorUpgradeStatus) {
			e.BlueGreen.Recoverability.WALArchiveBaselineVerifiedAt = nil
		}},
		{"missing teardown", func(e *platformv1alpha1.PostgresMajorUpgradeStatus) {
			e.BlueGreen.Completion.ReplicationRemovedAt = nil
		}},
		{"missing credential proof", func(e *platformv1alpha1.PostgresMajorUpgradeStatus) {
			e.BlueGreen.Completion.CredentialsRemovedAt = nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			entry := cleanupEvidence()
			require.True(t, BlueGreenCleanupEligible(entry))
			test.change(&entry)
			assert.False(t, BlueGreenCleanupEligible(entry))
		})
	}
	cancelled := cancelledCleanupEvidence()
	require.True(t, BlueGreenCleanupEligible(cancelled))
	_, ref, err := BlueGreenRetirementEnvironments(cancelled)
	require.NoError(t, err)
	assert.Nil(t, ref)
	cancelled.BlueGreen.Gates.Cancellation.SourceAccessRestoredAt = nil
	assert.False(t, BlueGreenCleanupEligible(cancelled))
}

func TestCleanupSelectsOnlyTheRetiredRole(t *testing.T) {
	completed := cleanupEvidence()
	_, ref, err := BlueGreenRetirementEnvironments(completed)
	require.NoError(t, err)
	assert.Equal(t, completed.BlueGreen.Blue.Ref, *ref)
	completed.BlueGreen.Blue.Ref.UID = ""
	_, _, err = BlueGreenRetirementEnvironments(completed)
	require.Error(t, err)

	cancelled := cancelledCleanupEvidence()
	cancelled.BlueGreen.Green = &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: *cleanupEvidence().BlueGreen.Blue.Ref.DeepCopy()}
	cancelled.BlueGreen.Green.Ref.Name = "candidate"
	cancelled.BlueGreen.Green.Ref.UID = "candidate-uid"
	_, ref, err = BlueGreenRetirementEnvironments(cancelled)
	require.NoError(t, err)
	assert.Equal(t, "candidate", ref.Name)
}

func retirementProof() (platformv1alpha1.BlueGreenRetirementAcknowledgementStatus, *platformv1alpha1.PostgresDatabase) {
	resource := corev1.ObjectReference{APIVersion: "postgresql.cnpg.io/v1", Kind: "Database", Namespace: "default", Name: "old-db", UID: "provider-uid"}
	row := platformv1alpha1.BlueGreenRetirementAcknowledgementStatus{
		PostgresDatabaseName: "child", PostgresDatabaseUID: "child-uid", PostgresDatabaseGeneration: 2,
		DatabaseName: "app", Resources: []corev1.ObjectReference{resource},
	}
	child := &platformv1alpha1.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "default", UID: "child-uid", Generation: 2}}
	child.Status.EnvironmentRetirement = &platformv1alpha1.PostgresDatabaseEnvironmentRetirementStatus{
		ParentUID: "parent-uid", AttemptID: "attempt", RetirementGeneration: 1, ObservedDatabaseGeneration: 2,
		EnvironmentRef: *cleanupEvidence().BlueGreen.Blue.Ref.DeepCopy(),
		Databases:      []platformv1alpha1.DatabaseEnvironmentRetirementStatus{{DatabaseName: "app", Resources: []corev1.ObjectReference{resource}, AcknowledgedAt: new(metav1.Now())}},
	}
	return row, child
}

func TestRetirementRejectsStaleOrIncompleteProofs(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*platformv1alpha1.PostgresDatabase)
	}{
		{"missing proof", func(c *platformv1alpha1.PostgresDatabase) { c.Status.EnvironmentRetirement = nil }},
		{"parent", func(c *platformv1alpha1.PostgresDatabase) { c.Status.EnvironmentRetirement.ParentUID = "another" }},
		{"attempt", func(c *platformv1alpha1.PostgresDatabase) { c.Status.EnvironmentRetirement.AttemptID = "another" }},
		{"retirement generation", func(c *platformv1alpha1.PostgresDatabase) { c.Status.EnvironmentRetirement.RetirementGeneration++ }},
		{"environment UID", func(c *platformv1alpha1.PostgresDatabase) {
			c.Status.EnvironmentRetirement.EnvironmentRef.UID = "replacement"
		}},
		{"environment name", func(c *platformv1alpha1.PostgresDatabase) {
			c.Status.EnvironmentRetirement.EnvironmentRef.Name = "active"
		}},
		{"child UID", func(c *platformv1alpha1.PostgresDatabase) { c.UID = "replacement" }},
		{"child generation", func(c *platformv1alpha1.PostgresDatabase) { c.Generation++ }},
		{"child proof generation", func(c *platformv1alpha1.PostgresDatabase) {
			c.Status.EnvironmentRetirement.ObservedDatabaseGeneration++
		}},
		{"namespace", func(c *platformv1alpha1.PostgresDatabase) { c.Namespace = "other" }},
		{"database", func(c *platformv1alpha1.PostgresDatabase) {
			c.Status.EnvironmentRetirement.Databases[0].DatabaseName = "other"
		}},
		{"provider UID", func(c *platformv1alpha1.PostgresDatabase) {
			c.Status.EnvironmentRetirement.Databases[0].Resources[0].UID = "replacement"
		}},
		{"missing resource", func(c *platformv1alpha1.PostgresDatabase) {
			c.Status.EnvironmentRetirement.Databases[0].Resources = nil
		}},
		{"missing timestamp", func(c *platformv1alpha1.PostgresDatabase) {
			c.Status.EnvironmentRetirement.Databases[0].AcknowledgedAt = nil
		}},
		{"duplicate proof", func(c *platformv1alpha1.PostgresDatabase) {
			c.Status.EnvironmentRetirement.Databases = append(c.Status.EnvironmentRetirement.Databases, c.Status.EnvironmentRetirement.Databases[0])
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			row, child := retirementProof()
			cleanup := cleanupEvidence().BlueGreen.Cleanup
			require.True(t, RetirementAcknowledged("parent-uid", "attempt", cleanup, row, child))
			test.change(child)
			assert.False(t, RetirementAcknowledged("parent-uid", "attempt", cleanup, row, child))
		})
	}
	row, child := retirementProof()
	child.Status.EnvironmentRetirement.Databases[0].Resources[0].ResourceVersion = "newer"
	assert.True(t, RetirementAcknowledged(types.UID("parent-uid"), "attempt", cleanupEvidence().BlueGreen.Cleanup, row, child))
}

func TestCleanupProgressRequiresLatchedAndCompleteBarrier(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*platformv1alpha1.PostgresBlueGreenUpgradeStatus)
	}{
		{"gate", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Gates.Cleanup = nil }},
		{"consumed time", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Gates.Cleanup.ConsumedAt = nil }},
		{"start", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Cleanup.StartedAt = nil }},
		{"generation", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Cleanup.RetirementGeneration = new(int64(0))
		}},
		{"environment", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Cleanup.EnvironmentRef = bg.Green.Ref.DeepCopy()
		}},
		{"incomplete Cleaned", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Cleanup.State = platformv1alpha1.BlueGreenCleanupStateCleaned
		}},
		{"premature time", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Cleanup.CompletedAt = new(metav1.Now()) }},
		{"unacknowledged child", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			row, _ := retirementProof()
			bg.Cleanup.RetirementAcknowledgements = []platformv1alpha1.BlueGreenRetirementAcknowledgementStatus{row}
			bg.Cleanup.State = platformv1alpha1.BlueGreenCleanupStateCleaned
			bg.Cleanup.CompletedAt = new(metav1.Now())
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := cleanupEvidence()
			intent := Intent{Strategy: MajorUpgradeFlowBlueGreen, AttemptID: "attempt"}
			progress := Progress{BlueGreen: e.BlueGreen}
			progress.Report.Phase = string(Completed)
			require.NoError(t, ValidateBlueGreenProgress(intent, progress))
			test.change(e.BlueGreen)
			assert.Error(t, ValidateBlueGreenProgress(intent, progress))
		})
	}
	e := cancelledCleanupEvidence()
	progress := Progress{BlueGreen: e.BlueGreen}
	progress.Report.Phase = string(Cancelled)
	require.NoError(t, ValidateBlueGreenProgress(Intent{Strategy: MajorUpgradeFlowBlueGreen, AttemptID: "attempt"}, progress))
}

func TestCleanupAuthorizationAndRevisionsAreMonotonic(t *testing.T) {
	bg := cleanupEvidence().BlueGreen
	row, _ := retirementProof()
	bg.Cleanup.RetirementAcknowledgements = []platformv1alpha1.BlueGreenRetirementAcknowledgementStatus{row}
	for _, test := range []struct {
		name   string
		change func(*platformv1alpha1.PostgresBlueGreenUpgradeStatus)
	}{
		{"gate removed", func(n *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { n.Gates.Cleanup = nil }},
		{"gate generation", func(n *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { n.Gates.Cleanup.Generation++ }},
		{"start rewritten", func(n *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			n.Cleanup.StartedAt = new(metav1.NewTime(n.Cleanup.StartedAt.Add(time.Second)))
		}},
		{"environment replaced", func(n *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { n.Cleanup.EnvironmentRef.UID = "replacement" }},
		{"generation decreased", func(n *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			n.Cleanup.RetirementGeneration = new(int64(0))
		}},
		{"roster replaced", func(n *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			n.Cleanup.RetirementAcknowledgements[0].PostgresDatabaseUID = "replacement"
		}},
		{"resource replaced", func(n *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			n.Cleanup.RetirementAcknowledgements[0].Resources[0].UID = "replacement"
		}},
		{"proof reused", func(n *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			*n.Cleanup.RetirementGeneration++
			n.Cleanup.RetirementAcknowledgements[0].AcknowledgedAt = new(metav1.Now())
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			next := bg.DeepCopy()
			test.change(next)
			require.Error(t, preserveCleanupProgress(bg, next))
		})
	}
	next := bg.DeepCopy()
	next.Cleanup.RetirementAcknowledgements[0].AcknowledgedAt = new(metav1.Now())
	require.NoError(t, preserveCleanupProgress(bg, next))
	next.Cleanup.RetirementAcknowledgements[0].AcknowledgedAt = nil
	*next.Cleanup.RetirementGeneration++
	next.Cleanup.RetirementAcknowledgements[0].PostgresDatabaseGeneration++
	require.NoError(t, preserveCleanupProgress(bg, next))
}

func TestDeletedHistoricalChildRequiresParentAbsenceProof(t *testing.T) {
	entry := cleanupEvidence()
	row, child := retirementProof()
	row.PostgresDatabaseGeneration = 0
	entry.BlueGreen.Cleanup.RetirementAcknowledgements = []platformv1alpha1.BlueGreenRetirementAcknowledgementStatus{row}
	progress := Progress{BlueGreen: entry.BlueGreen}
	progress.Report.Phase = string(Completed)
	intent := Intent{Strategy: MajorUpgradeFlowBlueGreen, AttemptID: "attempt"}
	require.NoError(t, ValidateBlueGreenProgress(intent, progress))
	assert.False(t, RetirementAcknowledged("parent-uid", "attempt", entry.BlueGreen.Cleanup, row, child))
	entry.BlueGreen.Cleanup.State = platformv1alpha1.BlueGreenCleanupStateCleaned
	entry.BlueGreen.Cleanup.CompletedAt = new(metav1.Now())
	require.Error(t, ValidateBlueGreenProgress(intent, progress))
	entry.BlueGreen.Cleanup.RetirementAcknowledgements[0].PostgresDatabaseAbsentAt = new(metav1.Now())
	require.NoError(t, ValidateBlueGreenProgress(intent, progress))
	previous := entry.BlueGreen.DeepCopy()
	entry.BlueGreen.Cleanup.State = platformv1alpha1.BlueGreenCleanupStateCleaning
	require.Error(t, preserveCleanupProgress(previous, entry.BlueGreen))
}

func TestUnconsumedRetainedPlaceholderRemainsCompatible(t *testing.T) {
	entry := cleanupEvidence()
	entry.BlueGreen.Gates = nil
	entry.BlueGreen.Cleanup = &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateRetained}
	progress := Progress{BlueGreen: entry.BlueGreen}
	progress.Report.Phase = string(Completed)
	require.NoError(t, ValidateBlueGreenProgress(Intent{Strategy: MajorUpgradeFlowBlueGreen, AttemptID: "attempt"}, progress))
}

func TestLegacyCleanedSummaryCannotBeReopened(t *testing.T) {
	previous := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{Cleanup: &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateCleaned}}
	next := previous.DeepCopy()
	next.Cleanup.State = platformv1alpha1.BlueGreenCleanupStateCleaning
	require.Error(t, preserveCleanupProgress(previous, next))
}

func TestCleanupSelectionUsesOnlyTerminalRequestsOrDurablePermission(t *testing.T) {
	spec := &platformv1alpha1.PostgresClusterSpec{PostgresMajorUpgradeConfig: &platformv1alpha1.PostgresMajorUpgradeConfig{Allow: new(false), BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeConfig{Cleanup: new(true)}}}
	entry := cleanupEvidence()
	entry.BlueGreen.Gates.Cleanup = nil
	entry.BlueGreen.Cleanup = nil
	require.True(t, BlueGreenCleanupPending(spec, entry))
	assert.False(t, BlueGreenCleanupPending(nil, entry))
	for _, phase := range []Status{Scheduled, Preflight, Upgrading, Failed, Recoverable} {
		changed := *entry.DeepCopy()
		changed.Phase = new(string(phase))
		assert.False(t, BlueGreenCleanupPending(spec, changed), phase)
	}
	latched := cleanupEvidence()
	assert.True(t, BlueGreenCleanupPending(nil, latched))
	latched.BlueGreen.Cleanup.State = platformv1alpha1.BlueGreenCleanupStateCleanupFailed
	assert.True(t, BlueGreenCleanupPending(nil, latched))
	latched.BlueGreen.Cleanup.State = platformv1alpha1.BlueGreenCleanupStateCleaned
	assert.False(t, BlueGreenCleanupPending(spec, latched))
}

func TestRetiredResourceInventoryCannotBeDiscarded(t *testing.T) {
	previous := cleanupEvidence().BlueGreen
	ref := corev1.ObjectReference{APIVersion: "postgresql.cnpg.io/v1", Kind: "Pooler", Namespace: "default", Name: "retired-pooler", UID: "pooler-uid"}
	previous.Cleanup.RetiredResources = []corev1.ObjectReference{ref}
	next := previous.DeepCopy()
	next.Cleanup.RetiredResources = nil
	require.Error(t, preserveCleanupProgress(previous, next))
	next = previous.DeepCopy()
	next.Cleanup.RetiredResources = append(next.Cleanup.RetiredResources, corev1.ObjectReference{APIVersion: "v1", Kind: "Secret", Namespace: "default", Name: "late-artifact", UID: "secret-uid"})
	require.NoError(t, preserveCleanupProgress(previous, next))
	next.Cleanup.RetiredResources[0].UID = "replacement"
	require.Error(t, preserveCleanupProgress(previous, next))
}

func TestBlueGreenFinalizationSelection(t *testing.T) {
	parent := &platformv1alpha1.PostgresCluster{}
	assert.False(t, BlueGreenFinalizationRequired(parent))
	parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{Strategy: new("pgUpgrade")}}
	assert.False(t, BlueGreenFinalizationRequired(parent))
	parent.Status.PostgresMajorUpgradeStatus = append([]platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "historical"}}}, parent.Status.PostgresMajorUpgradeStatus...)
	assert.True(t, BlueGreenFinalizationRequired(parent))
	parent.Status.PostgresMajorUpgradeStatus = nil
	parent.Annotations = map[string]string{"platform.splunk.com/finalization-inventory": "forged inventory"}
	assert.False(t, BlueGreenFinalizationRequired(parent))
	parent.Status.Finalization = &platformv1alpha1.PostgresClusterFinalizationStatus{Version: 1}
	assert.True(t, BlueGreenFinalizationRequired(parent))
}
