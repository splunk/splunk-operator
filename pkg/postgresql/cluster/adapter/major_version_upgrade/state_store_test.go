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
	"context"
	"errors"
	"testing"
	"time"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestMajorUpgradeInputFromClusterRequiresAllow(t *testing.T) {
	cluster := &enterprisev4.PostgresCluster{
		Spec: enterprisev4.PostgresClusterSpec{
			PostgresVersion: ptr.To("18"),
			PostgresMajorUpgradeConfig: &enterprisev4.PostgresMajorUpgradeConfig{
				Allow: ptr.To(false),
			},
		},
	}

	input, enabled, err := MajorUpgradeInputFromCluster(cluster)
	require.NoError(t, err)
	assert.False(t, enabled)
	assert.Empty(t, input.TargetPgVersion)
}

func TestMajorUpgradeInputFromClusterUsesPostgresVersionAsTarget(t *testing.T) {
	cluster := &enterprisev4.PostgresCluster{
		Spec: enterprisev4.PostgresClusterSpec{
			PostgresVersion: ptr.To("18"),
			PostgresMajorUpgradeConfig: &enterprisev4.PostgresMajorUpgradeConfig{
				Allow: ptr.To(true),
			},
		},
	}

	input, enabled, err := MajorUpgradeInputFromCluster(cluster)
	require.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, mvutypes.MajorUpgradeFlowPgUpgrade, input.Strategy)
	assert.Equal(t, "18", input.TargetPgVersion)
}

func TestMajorUpgradeStateStoreReadsClusterFromSpecification(t *testing.T) {
	reader := NewMajorUpgradeStateStore(fakeStateStore{
		spec: &enterprisev4.PostgresClusterSpec{
			PostgresVersion: ptr.To("18"),
			PostgresMajorUpgradeConfig: &enterprisev4.PostgresMajorUpgradeConfig{
				Allow: ptr.To(true),
			},
		},
	})

	input, enabled, err := reader.ReadMajorUpgradeIntent(t.Context())
	require.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, "18", input.TargetPgVersion)
}

func TestMajorUpgradeStateStoreComposesSpecificationStatusAndAnnotations(t *testing.T) {
	completed := string(mvutypes.Completed)
	target := "17"
	retryAt := "2026-06-24T10:00:00Z"
	reader := NewMajorUpgradeStateStore(fakeStateStore{
		spec: &enterprisev4.PostgresClusterSpec{
			PostgresVersion: ptr.To("18"),
			PostgresMajorUpgradeConfig: &enterprisev4.PostgresMajorUpgradeConfig{
				Allow: ptr.To(true),
			},
		},
		annotations: map[string]string{
			mvutypes.AnnotationMajorUpgradeRetryAt: retryAt,
		},
		entries: []enterprisev4.PostgresMajorUpgradeStatus{{
			Phase:           &completed,
			TargetPgVersion: &target,
		}},
	})

	input, enabled, err := reader.ReadMajorUpgradeIntent(t.Context())
	require.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, "17", input.SourcePgVersion)
	assert.Equal(t, "18", input.TargetPgVersion)
	require.NotNil(t, input.RetryRequestedAt)
	assert.Equal(t, retryAt, input.RetryRequestedAt.UTC().Format(time.RFC3339))
	require.Len(t, input.State, 1)
	assert.Equal(t, target, *input.State[0].TargetPgVersion)
}

func TestMajorUpgradeInputFromClusterSkipsWhenSourceMajorMatchesTargetMajor(t *testing.T) {
	completed := string(mvutypes.Completed)
	strategy := mvutypes.MajorUpgradeFlowPgUpgrade
	source := "17"
	target := "18"

	cluster := &enterprisev4.PostgresCluster{
		Spec: enterprisev4.PostgresClusterSpec{
			PostgresVersion: ptr.To("18.2"),
			PostgresMajorUpgradeConfig: &enterprisev4.PostgresMajorUpgradeConfig{
				Allow: ptr.To(true),
			},
		},
		Status: enterprisev4.PostgresClusterStatus{
			PostgresMajorUpgradeStatus: []enterprisev4.PostgresMajorUpgradeStatus{{
				Phase:           &completed,
				Strategy:        &strategy,
				SourcePgVersion: &source,
				TargetPgVersion: &target,
			}},
		},
	}

	input, enabled, err := MajorUpgradeInputFromCluster(cluster)
	require.NoError(t, err)
	assert.False(t, enabled)
	assert.Empty(t, input.TargetPgVersion)
}

func TestMajorUpgradeStateStoreReturnsSpecificationError(t *testing.T) {
	specErr := errors.New("get failed")
	reader := NewMajorUpgradeStateStore(fakeStateStore{specErr: specErr})

	input, enabled, err := reader.ReadMajorUpgradeIntent(t.Context())
	require.ErrorIs(t, err, specErr)
	assert.False(t, enabled)
	assert.Empty(t, input.TargetPgVersion)
}

func TestStateWithReportClearsTerminalFailureAfterRetry(t *testing.T) {
	source := "13"
	target := "14"
	strategy := mvutypes.MajorUpgradeFlowPgUpgrade
	phase := string(mvutypes.Failed)
	failedAt := metav1.NewTime(time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC))
	retryAt := metav1.NewTime(time.Date(2026, 6, 24, 10, 1, 0, 0, time.UTC))

	intent := mvutypes.Intent{
		Strategy:         strategy,
		SourcePgVersion:  source,
		TargetPgVersion:  target,
		RetryRequestedAt: &retryAt,
		State: []enterprisev4.PostgresMajorUpgradeStatus{{
			Phase:           &phase,
			Strategy:        &strategy,
			SourcePgVersion: &source,
			TargetPgVersion: &target,
			Conditions: []metav1.Condition{{
				Type:               mvutypes.ConditionMajorUpgradeTerminalFailure,
				LastTransitionTime: failedAt,
			}},
		}},
	}
	report := reconciliationTypes.Report{
		Name:   mvutypes.UseCaseName,
		Phase:  string(mvutypes.PreUpgradeBackup),
		Reason: mvutypes.ReasonBackupStatusMissing,
		Retry:  true,
	}

	next := stateWithReport(intent, report, nil)
	require.Len(t, next, 1)
	for _, condition := range next[0].Conditions {
		if condition.Type == mvutypes.ConditionMajorUpgradeTerminalFailure {
			t.Fatalf("terminal failure condition was not cleared: %#v", next[0].Conditions)
		}
	}
}

func TestStateWithReportSurfacesRetryableFailureForBlockingObstacle(t *testing.T) {
	source := "17"
	target := "18"
	strategy := mvutypes.MajorUpgradeFlowPgUpgrade

	intent := mvutypes.Intent{
		Strategy:        strategy,
		SourcePgVersion: source,
		TargetPgVersion: target,
	}
	report := reconciliationTypes.Report{
		Name:   mvutypes.UseCaseName,
		Phase:  string(mvutypes.PreUpgradeBackup),
		Reason: mvutypes.ReasonBackupStatusMissing,
		Retry:  true,
	}

	next := stateWithReport(intent, report, nil)
	require.Len(t, next, 1)
	condition := meta.FindStatusCondition(next[0].Conditions, mvutypes.ConditionMajorUpgradeRetryableFailure)
	require.NotNil(t, condition, "expected MajorUpgradeRetryableFailure for a blocking obstacle")
	assert.Equal(t, mvutypes.ReasonBackupStatusMissing, condition.Reason)
}

func TestStateWithReportClearsRetryableFailureWhenProgressResumes(t *testing.T) {
	source := "17"
	target := "18"
	strategy := mvutypes.MajorUpgradeFlowPgUpgrade
	phase := string(mvutypes.PreUpgradeBackup)

	intent := mvutypes.Intent{
		Strategy:        strategy,
		SourcePgVersion: source,
		TargetPgVersion: target,
		State: []enterprisev4.PostgresMajorUpgradeStatus{{
			Phase:           &phase,
			Strategy:        &strategy,
			SourcePgVersion: &source,
			TargetPgVersion: &target,
			Conditions: []metav1.Condition{{
				Type:   mvutypes.ConditionMajorUpgradeRetryableFailure,
				Status: metav1.ConditionTrue,
				Reason: mvutypes.ReasonBackupStatusMissing,
			}},
		}},
	}
	report := reconciliationTypes.Report{
		Name:   mvutypes.UseCaseName,
		Phase:  string(mvutypes.Upgrading),
		Reason: mvutypes.ReasonPgUpgradeStarted,
		Retry:  true,
	}

	next := stateWithReport(intent, report, nil)
	require.Len(t, next, 1)
	assert.Nil(t, meta.FindStatusCondition(next[0].Conditions, mvutypes.ConditionMajorUpgradeRetryableFailure),
		"retryable failure should be cleared once the upgrade progresses again")
	assert.NotNil(t, meta.FindStatusCondition(next[0].Conditions, mvutypes.ConditionMajorUpgradeProgressing))
}

func TestStateWithReportStartsFreshEntryForNewIntent(t *testing.T) {
	completed := string(mvutypes.Completed)
	oldSource := "16"
	oldTarget := "17"
	source := "17"
	target := "18"
	strategy := mvutypes.MajorUpgradeFlowPgUpgrade

	intent := mvutypes.Intent{
		Strategy:        strategy,
		SourcePgVersion: source,
		TargetPgVersion: target,
		State: []enterprisev4.PostgresMajorUpgradeStatus{{
			Phase:           &completed,
			Strategy:        &strategy,
			SourcePgVersion: &oldSource,
			TargetPgVersion: &oldTarget,
			Conditions: []metav1.Condition{{
				Type:    mvutypes.ConditionMajorUpgradeCompleted,
				Reason:  mvutypes.ReasonPgUpgradeFinalized,
				Status:  metav1.ConditionTrue,
				Message: mvutypes.MessagePgUpgradeFinalized,
			}},
		}},
	}
	report := reconciliationTypes.Report{
		Name:    mvutypes.UseCaseName,
		Phase:   string(mvutypes.Verifying),
		Reason:  mvutypes.ReasonPgUpgradeObservedComplete,
		Message: mvutypes.MessagePgUpgradeObservedComplete,
		Retry:   true,
	}

	next := stateWithReport(intent, report, nil)
	require.Len(t, next, 2)
	assert.Equal(t, oldSource, *next[0].SourcePgVersion)
	assert.Equal(t, oldTarget, *next[0].TargetPgVersion)
	assert.Equal(t, completed, *next[0].Phase)
	assert.Equal(t, source, *next[1].SourcePgVersion)
	assert.Equal(t, target, *next[1].TargetPgVersion)
	assert.Equal(t, string(mvutypes.Verifying), *next[1].Phase)
	require.Len(t, next[1].Conditions, 1)
	assert.Equal(t, mvutypes.ConditionMajorUpgradeProgressing, next[1].Conditions[0].Type)
}

func TestStateWithReportUpdatesMatchingEntryAndPreservesHistory(t *testing.T) {
	completed := string(mvutypes.Completed)
	upgrading := string(mvutypes.Upgrading)
	oldSource := "16"
	oldTarget := "17"
	source := "17"
	target := "18"
	strategy := mvutypes.MajorUpgradeFlowPgUpgrade

	intent := mvutypes.Intent{
		Strategy:        strategy,
		SourcePgVersion: source,
		TargetPgVersion: target,
		State: []enterprisev4.PostgresMajorUpgradeStatus{
			{
				Phase:           &completed,
				Strategy:        &strategy,
				SourcePgVersion: &oldSource,
				TargetPgVersion: &oldTarget,
			},
			{
				Phase:           &upgrading,
				Strategy:        &strategy,
				SourcePgVersion: &source,
				TargetPgVersion: &target,
			},
		},
	}
	report := reconciliationTypes.Report{
		Name:    mvutypes.UseCaseName,
		Phase:   string(mvutypes.Verifying),
		Reason:  mvutypes.ReasonPgUpgradeObservedComplete,
		Message: mvutypes.MessagePgUpgradeObservedComplete,
		Retry:   true,
	}

	next := stateWithReport(intent, report, nil)
	require.Len(t, next, 2)
	assert.Equal(t, oldSource, *next[0].SourcePgVersion)
	assert.Equal(t, oldTarget, *next[0].TargetPgVersion)
	assert.Equal(t, completed, *next[0].Phase)
	assert.Equal(t, source, *next[1].SourcePgVersion)
	assert.Equal(t, target, *next[1].TargetPgVersion)
	assert.Equal(t, string(mvutypes.Verifying), *next[1].Phase)
	require.Len(t, next[1].Conditions, 1)
	assert.Equal(t, mvutypes.ConditionMajorUpgradeProgressing, next[1].Conditions[0].Type)
}

// TestApplyBaselineWritesPostUpgradeNameOnlyWithCompletedReport locks the
// applyBaseline coupling: a Completed report + non-nil baseline must write to
// PostUpgrade, not PreUpgrade. Any change to the caller pairing (completedReport
// + post-upgrade BackupInfo) should break this test as intended.
func TestApplyBaselineWritesPostUpgradeNameOnlyWithCompletedReport(t *testing.T) {
	source := "17"
	target := "18"
	strategy := mvutypes.MajorUpgradeFlowPgUpgrade
	backupName := "my-cluster-post-upgrade-17-18"

	intent := mvutypes.Intent{
		Strategy:        strategy,
		SourcePgVersion: source,
		TargetPgVersion: target,
	}
	report := reconciliationTypes.Report{
		Name:  mvutypes.UseCaseName,
		Phase: string(mvutypes.Completed),
		Retry: false,
	}
	baseline := &mvutypes.BackupInfo{BackupName: backupName}

	next := stateWithReport(intent, report, baseline)
	require.Len(t, next, 1)
	require.NotNil(t, next[0].BackupNames, "BackupNames must be set")
	require.NotNil(t, next[0].BackupNames.PostUpgrade, "PostUpgrade name must be written for Completed report")
	assert.Equal(t, backupName, *next[0].BackupNames.PostUpgrade)
	assert.Nil(t, next[0].BackupNames.PreUpgrade, "PreUpgrade must not be written for Completed report")
}

// TestApplyBaselineWritesPreUpgradeNameForNonCompletedReport ensures a
// non-Completed phase writes the backup name to PreUpgrade.
func TestApplyBaselineWritesPreUpgradeNameForNonCompletedReport(t *testing.T) {
	source := "17"
	target := "18"
	strategy := mvutypes.MajorUpgradeFlowPgUpgrade
	backupName := "my-cluster-pre-upgrade-17-18"

	intent := mvutypes.Intent{
		Strategy:        strategy,
		SourcePgVersion: source,
		TargetPgVersion: target,
	}
	report := reconciliationTypes.Report{
		Name:  mvutypes.UseCaseName,
		Phase: string(mvutypes.PreUpgradeBackup),
		Retry: true,
	}
	baseline := &mvutypes.BackupInfo{BackupName: backupName}

	next := stateWithReport(intent, report, baseline)
	require.Len(t, next, 1)
	require.NotNil(t, next[0].BackupNames, "BackupNames must be set")
	require.NotNil(t, next[0].BackupNames.PreUpgrade, "PreUpgrade name must be written for non-Completed report")
	assert.Equal(t, backupName, *next[0].BackupNames.PreUpgrade)
	assert.Nil(t, next[0].BackupNames.PostUpgrade, "PostUpgrade must not be written for non-Completed report")
}

// TestRemoveConditionDoesNotMutateOriginalSlice verifies that removeCondition
// never modifies the caller's slice in-place, even when an element is removed.
func TestRemoveConditionDoesNotMutateOriginalSlice(t *testing.T) {
	original := []metav1.Condition{
		{Type: mvutypes.ConditionMajorUpgradeTerminalFailure, Status: metav1.ConditionTrue, Reason: "x", Message: "x"},
		{Type: mvutypes.ConditionMajorUpgradeProgressing, Status: metav1.ConditionTrue, Reason: "y", Message: "y"},
	}
	originalCopy := append([]metav1.Condition(nil), original...)

	result := removeCondition(original, mvutypes.ConditionMajorUpgradeTerminalFailure)

	// The original slice must be unchanged.
	assert.Equal(t, originalCopy, original, "removeCondition must not mutate the input slice")
	// The result has the condition removed.
	require.Len(t, result, 1)
	assert.Equal(t, mvutypes.ConditionMajorUpgradeProgressing, result[0].Type)
}

// TestMajorUpgradeInputFromPartsNilPostgresVersion covers the class-inheritance
// case where spec.PostgresVersion is nil — the input must report disabled so
// the use case is not activated before the version is resolved.
func TestMajorUpgradeInputFromPartsNilPostgresVersion(t *testing.T) {
	spec := &enterprisev4.PostgresClusterSpec{
		PostgresVersion: nil,
		PostgresMajorUpgradeConfig: &enterprisev4.PostgresMajorUpgradeConfig{
			Allow: ptr.To(true),
		},
	}
	intent, enabled, err := MajorUpgradeInputFromParts(spec, nil, nil, "17")
	require.NoError(t, err)
	if enabled {
		t.Fatalf("enabled = true, want false when PostgresVersion is nil")
	}
	if intent.TargetPgVersion != "" {
		t.Fatalf("TargetPgVersion = %q, want empty", intent.TargetPgVersion)
	}
}

// TestMajorUpgradeInputFromPartsNilPostgresVersionWithOverride verifies that
// NewMajorUpgradeStateStoreWithTarget patches a nil PostgresVersion from the
// class-inherited resolved version so the intent is enabled.
func TestMajorUpgradeInputFromPartsNilPostgresVersionWithOverride(t *testing.T) {
	reader := NewMajorUpgradeStateStoreWithTarget(fakeStateStore{
		spec: &enterprisev4.PostgresClusterSpec{
			PostgresVersion: nil,
			PostgresMajorUpgradeConfig: &enterprisev4.PostgresMajorUpgradeConfig{
				Allow: ptr.To(true),
			},
		},
		sourcePgVersion: "17",
	}, "18")

	intent, enabled, err := reader.ReadMajorUpgradeIntent(t.Context())
	require.NoError(t, err)
	if !enabled {
		t.Fatalf("enabled = false, want true when overrideTarget patches nil PostgresVersion")
	}
	if intent.TargetPgVersion != "18" {
		t.Fatalf("TargetPgVersion = %q, want %q", intent.TargetPgVersion, "18")
	}
}

// TestStateWithReportNilBaselineCompletedDoesNotWriteBackupName verifies that
// when the post-upgrade backup has no BackupInfo yet (nil baseline), Completed
// is not recorded and BackupNames is left untouched.
func TestStateWithReportNilBaselineCompletedDoesNotWriteBackupName(t *testing.T) {
	source := "17"
	target := "18"
	strategy := mvutypes.MajorUpgradeFlowPgUpgrade

	intent := mvutypes.Intent{
		Strategy:        strategy,
		SourcePgVersion: source,
		TargetPgVersion: target,
	}
	report := reconciliationTypes.Report{
		Name:  mvutypes.UseCaseName,
		Phase: string(mvutypes.Completed),
		Retry: false,
	}

	next := stateWithReport(intent, report, nil)
	require.Len(t, next, 1)
	if next[0].BackupNames != nil && next[0].BackupNames.PostUpgrade != nil {
		t.Fatalf("PostUpgrade backup name was written with nil baseline: %q", *next[0].BackupNames.PostUpgrade)
	}
}

// TestStateWithReportPhaseForDuplicateEntries verifies that when multiple status
// entries match the same source→target, stateWithReport updates the last one and
// leaves earlier ones intact.
func TestStateWithReportPhaseForDuplicateEntries(t *testing.T) {
	source := "17"
	target := "18"
	strategy := mvutypes.MajorUpgradeFlowPgUpgrade
	firstPhase := string(mvutypes.Failed)
	secondPhase := string(mvutypes.Scheduled)

	intent := mvutypes.Intent{
		Strategy:        strategy,
		SourcePgVersion: source,
		TargetPgVersion: target,
		State: []enterprisev4.PostgresMajorUpgradeStatus{
			{
				Phase:           &firstPhase,
				Strategy:        &strategy,
				SourcePgVersion: &source,
				TargetPgVersion: &target,
			},
			{
				Phase:           &secondPhase,
				Strategy:        &strategy,
				SourcePgVersion: &source,
				TargetPgVersion: &target,
			},
		},
	}
	report := reconciliationTypes.Report{
		Name:   mvutypes.UseCaseName,
		Phase:  string(mvutypes.Upgrading),
		Reason: mvutypes.ReasonPgUpgradeStarted,
		Retry:  true,
	}

	next := stateWithReport(intent, report, nil)
	require.Len(t, next, 2)
	// First entry is untouched.
	assert.Equal(t, firstPhase, *next[0].Phase)
	// Last matching entry is updated.
	assert.Equal(t, string(mvutypes.Upgrading), *next[1].Phase)
}

// TestPreUpgradeBackupNameSurvivesTerminalFailure verifies that when the upgrade
// transitions to Failed, the pre-upgrade backup name already written to status is
// preserved. This is the recovery anchor: the docs instruct operators to read
// status.postgresMajorUpgradeStatus[n].backupNames.preUpgrade to find the backup
// they must restore from. If stateWithReport zeroed it on failure the restore path
// would break silently.
func TestPreUpgradeBackupNameSurvivesTerminalFailure(t *testing.T) {
	source := "17"
	target := "18"
	strategy := mvutypes.MajorUpgradeFlowPgUpgrade
	preUpgradeName := "my-cluster-pre-upgrade-17-18"
	preUpgradePhase := string(mvutypes.PreUpgradeBackup)

	// Seed a prior state entry that already has the pre-upgrade backup name recorded.
	prior := enterprisev4.PostgresMajorUpgradeStatus{
		Phase:           &preUpgradePhase,
		Strategy:        &strategy,
		SourcePgVersion: &source,
		TargetPgVersion: &target,
		BackupNames:     &enterprisev4.UpgradeBackupNames{PreUpgrade: &preUpgradeName},
	}
	intent := mvutypes.Intent{
		Strategy:        strategy,
		SourcePgVersion: source,
		TargetPgVersion: target,
		State:           []enterprisev4.PostgresMajorUpgradeStatus{prior},
	}
	failedReport := reconciliationTypes.Report{
		Name:  mvutypes.UseCaseName,
		Phase: string(mvutypes.Failed),
		Retry: false,
	}

	next := stateWithReport(intent, failedReport, nil)
	require.Len(t, next, 1)
	require.NotNil(t, next[0].BackupNames, "BackupNames must not be nil after failure")
	require.NotNil(t, next[0].BackupNames.PreUpgrade, "PreUpgrade backup name must survive terminal failure")
	assert.Equal(t, preUpgradeName, *next[0].BackupNames.PreUpgrade)
	assert.Nil(t, next[0].BackupNames.PostUpgrade, "PostUpgrade must not be written on failure")
}

// TestRetryRequestedAfterTerminalFailureEmptyConditions verifies that when the
// status entry has no conditions at all (e.g. operator restarted before conditions
// were written), RetryRequestedAfterTerminalFailure returns false rather than
// allowing a spurious retry.
func TestRetryRequestedAfterTerminalFailureEmptyConditions(t *testing.T) {
	retryAt := metav1.NewTime(time.Date(2026, 6, 24, 10, 1, 0, 0, time.UTC))
	entry := enterprisev4.PostgresMajorUpgradeStatus{
		Conditions: nil, // no conditions written yet
	}
	if mvutypes.RetryRequestedAfterTerminalFailure(&retryAt, entry) {
		t.Fatalf("RetryRequestedAfterTerminalFailure = true, want false with no conditions")
	}
}

type fakeStateStore struct {
	spec            *enterprisev4.PostgresClusterSpec
	annotations     map[string]string
	entries         []enterprisev4.PostgresMajorUpgradeStatus
	sourcePgVersion string
	specErr         error
	statusErr       error
}

func (f fakeStateStore) GetSpecificationWithAnnotations(context.Context) (*enterprisev4.PostgresClusterSpec, map[string]string, error) {
	if f.specErr != nil {
		return nil, nil, f.specErr
	}
	if f.spec != nil {
		return f.spec, f.annotations, nil
	}
	return &enterprisev4.PostgresClusterSpec{}, f.annotations, nil
}

func (f fakeStateStore) GetMajorUpgradeStatus(context.Context) ([]enterprisev4.PostgresMajorUpgradeStatus, error) {
	return f.entries, f.statusErr
}

func (f fakeStateStore) SetMajorUpgradeStatus(context.Context, []enterprisev4.PostgresMajorUpgradeStatus) error {
	return f.statusErr
}

func (f fakeStateStore) GetSourcePgVersion(context.Context) (string, error) {
	return f.sourcePgVersion, f.statusErr
}
