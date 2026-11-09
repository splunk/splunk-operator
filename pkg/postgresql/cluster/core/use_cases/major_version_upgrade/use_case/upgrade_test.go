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

package majorversionupgrade

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgcconstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationtypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	usecases "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases"
	bluegreen "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/blue_green"
	pgupgradeflow "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/pg_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMajorUpgradeUseCasePrerequisitesFailWhenSourceVersionEmpty(t *testing.T) {
	intent := mvutypes.Intent{
		Strategy:        mvutypes.MajorUpgradeFlowPgUpgrade,
		TargetPgVersion: "18",
		// SourcePgVersion deliberately empty — CNPG has not written status yet
	}
	err := newTestUseCase(intent).Prerequisites(t.Context())
	if err == nil {
		t.Fatal("Prerequisites() = nil, want error when SourcePgVersion is empty")
	}
	if !errors.Is(err, usecases.ErrPrerequisiteNotReady) {
		t.Fatalf("Prerequisites() error = %v, want ErrPrerequisiteNotReady", err)
	}
}

func TestMajorUpgradeUseCasePrerequisitesPassWhenSourceVersionKnown(t *testing.T) {
	intent := mvutypes.Intent{
		Strategy:        mvutypes.MajorUpgradeFlowPgUpgrade,
		SourcePgVersion: "17",
		TargetPgVersion: "18",
	}
	if err := newTestUseCase(intent).Prerequisites(t.Context()); err != nil {
		t.Fatalf("Prerequisites() = %v, want nil", err)
	}
}

func TestBlueGreenWaitsForSourceReadinessBeforeBlockingComponents(t *testing.T) {
	intent := mvutypes.Intent{Strategy: mvutypes.MajorUpgradeFlowBlueGreen, SourcePgVersion: "17", TargetPgVersion: "18", AttemptID: "attempt-1"}
	store := readinessStore{majorUpgradeInfoStoreFunc: fakeInfoStore(intent), ready: false}
	useCase := NewMajorUpgradeUseCase(store, nil, nil, pgupgradeflow.NoopNotifier(), &fakeBlueGreenPreparer{}, nil, nil)
	require.ErrorIs(t, useCase.Prerequisites(t.Context()), usecases.ErrPrerequisiteNotReady)

	phase := string(mvutypes.Preflight)
	strategy := intent.Strategy
	intent.State = []platformv1alpha1.PostgresMajorUpgradeStatus{{
		Phase: &phase, Strategy: &strategy, SourcePgVersion: &intent.SourcePgVersion,
		TargetPgVersion: &intent.TargetPgVersion,
		BlueGreen:       &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: intent.AttemptID},
	}}
	store.majorUpgradeInfoStoreFunc = fakeInfoStore(intent)
	useCase = NewMajorUpgradeUseCase(store, nil, nil, pgupgradeflow.NoopNotifier(), &fakeBlueGreenPreparer{}, nil, nil)
	require.NoError(t, useCase.Prerequisites(t.Context()), "a durable attempt must continue even after the readiness cycle starts")

	failed := string(mvutypes.Failed)
	intent.State[0].Phase = &failed
	intent.State[0].Conditions = []metav1.Condition{{Type: mvutypes.ConditionMajorUpgradeTerminalFailure, LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Minute))}}
	retry := metav1.Now()
	intent.RetryRequestedAt = &retry
	store.majorUpgradeInfoStoreFunc = fakeInfoStore(intent)
	useCase = NewMajorUpgradeUseCase(store, nil, nil, pgupgradeflow.NoopNotifier(), &fakeBlueGreenPreparer{}, nil, nil)
	require.ErrorIs(t, useCase.Prerequisites(t.Context()), usecases.ErrPrerequisiteNotReady, "a retry must wait for source readiness again")
}

func TestMajorUpgradeUseCasePrerequisitesPassWhenNotEnabled(t *testing.T) {
	// When the use case is not enabled (allow=false etc.), prerequisites are
	// irrelevant regardless of whether SourcePgVersion is populated.
	useCase := NewMajorUpgradeUseCase(
		majorUpgradeInfoStoreFunc(func(context.Context) (mvutypes.Intent, bool, error) {
			return mvutypes.Intent{}, false, nil
		}),
		nil, nil, pgupgradeflow.NoopNotifier(), &fakeBlueGreenPreparer{}, nil, nil)

	if err := useCase.Prerequisites(t.Context()); err != nil {
		t.Fatalf("Prerequisites() = %v, want nil when use case not enabled", err)
	}
}

func TestMajorUpgradeUseCaseFailsWhenBackupProviderMissing(t *testing.T) {
	intent := mvutypes.Intent{
		Strategy:        mvutypes.MajorUpgradeFlowPgUpgrade,
		SourcePgVersion: "17",
		TargetPgVersion: "18",
	}

	useCase := NewMajorUpgradeUseCase(
		fakeInfoStore(intent), nil, &fakePgUpgrade{}, pgupgradeflow.NoopNotifier(), &fakeBlueGreenPreparer{}, nil, nil)

	report, err := useCase.Act(t.Context())
	if !errors.Is(err, mvutypes.ErrBackupProviderMissing) {
		t.Fatalf("Act() error = %v, want ErrBackupProviderMissing", err)
	}
	if report.Phase != string(mvutypes.Failed) || report.Retry {
		t.Fatalf("report = %#v, want terminal Failed report", report)
	}
}

func TestMajorUpgradeUseCasePreparesDeclaredBlueGreenRuntime(t *testing.T) {
	phase := string(mvutypes.Preflight)
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	source := "17"
	target := "18"
	intent := mvutypes.Intent{
		Strategy:        strategy,
		SourcePgVersion: source,
		TargetPgVersion: target,
		AttemptID:       "attempt-1",
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
			Phase:           &phase,
			Strategy:        &strategy,
			SourcePgVersion: &source,
			TargetPgVersion: &target,
			BlueGreen:       &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt-1"},
		}},
	}
	store := &blueGreenProgressStore{intent: intent}
	prepared := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: intent.AttemptID}
	preparer := &fakeBlueGreenPreparer{status: prepared}
	useCase := NewMajorUpgradeUseCase(store, fakeBackupProvider{status: &mvutypes.BackupInfo{}}, nil, pgupgradeflow.NoopNotifier(), preparer, nil, nil)

	report, err := useCase.Act(t.Context())
	if err != nil {
		t.Fatalf("Act() error = %v, want nil", err)
	}
	if report.Phase != string(mvutypes.Preflight) || !report.Retry {
		t.Fatalf("report = %#v, want retryable blue/green preparation report", report)
	}
	require.NotNil(t, store.progress.BlueGreen)
	assert.Equal(t, intent.AttemptID, store.progress.BlueGreen.AttemptID)
	assert.Equal(t, 1, preparer.calls)
}

func TestMajorUpgradeUseCaseEntersBlueGreenPreflightBeforePreparingSource(t *testing.T) {
	intent := mvutypes.Intent{Strategy: mvutypes.MajorUpgradeFlowBlueGreen, SourcePgVersion: "17", TargetPgVersion: "18", AttemptID: "attempt-1"}
	store := &blueGreenProgressStore{intent: intent}
	preparer := &fakeBlueGreenPreparer{}
	useCase := NewMajorUpgradeUseCase(store, fakeBackupProvider{status: &mvutypes.BackupInfo{}}, nil, pgupgradeflow.NoopNotifier(), preparer, nil, nil)

	report, err := useCase.Act(t.Context())
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.Scheduled), report.Phase)
	assert.Equal(t, mvutypes.ReasonUpgradeFlowPending, report.Reason)
	assert.Zero(t, preparer.calls, "source mutations begin only after Preflight is durable")
	assert.Nil(t, store.progress.BlueGreen)
}

func TestMajorUpgradeUseCaseDoesNotRecreateBackupAfterBlueGreenPreparation(t *testing.T) {
	phase := string(mvutypes.Preflight)
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	source := "17"
	target := "18"
	completedAt := metav1.Now()
	intent := mvutypes.Intent{
		Strategy: strategy, SourcePgVersion: source, TargetPgVersion: target, AttemptID: "attempt-1",
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
			Phase: &phase, Strategy: &strategy, SourcePgVersion: &source, TargetPgVersion: &target,
			BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
				AttemptID: "attempt-1",
				SourcePreparation: &platformv1alpha1.BlueGreenSourcePreparationStatus{
					CompletedAt: &completedAt,
				},
			},
		}},
	}
	store := &blueGreenProgressStore{intent: intent}
	useCase := NewMajorUpgradeUseCase(store, nil, nil, pgupgradeflow.NoopNotifier(), &fakeBlueGreenPreparer{}, &fakeBlueGreenCandidatePreparer{}, nil)

	report, err := useCase.Act(t.Context())
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.Upgrading), report.Phase)
	assert.True(t, report.Retry)
	assert.Nil(t, store.progress.Baseline)
}

func TestMajorUpgradeUseCaseRequiresBackupBeforeBlueGreenPreparation(t *testing.T) {
	intent := mvutypes.Intent{Strategy: mvutypes.MajorUpgradeFlowBlueGreen, SourcePgVersion: "17", TargetPgVersion: "18", AttemptID: "attempt-1"}
	useCase := NewMajorUpgradeUseCase(&blueGreenProgressStore{intent: intent}, nil, nil, pgupgradeflow.NoopNotifier(), &fakeBlueGreenPreparer{}, nil, nil)

	// A persisted scheduling entry exists before backup creation is attempted.
	useCase.store.(*blueGreenProgressStore).intent.State = []platformv1alpha1.PostgresMajorUpgradeStatus{{Strategy: new(intent.Strategy), SourcePgVersion: new(intent.SourcePgVersion), TargetPgVersion: new(intent.TargetPgVersion), Phase: new(string(mvutypes.Scheduled)), BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: intent.AttemptID}}}
	_, err := useCase.Act(t.Context())
	require.ErrorIs(t, err, mvutypes.ErrBackupProviderMissing)
}

func TestMajorUpgradeUseCaseRearmsBlueGreenOnlyDuringAct(t *testing.T) {
	source := "17"
	target := "18"
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	completed := string(mvutypes.Completed)
	store := &blueGreenRearmStore{intent: mvutypes.Intent{
		Strategy:               strategy,
		SourcePgVersion:        source,
		TargetPgVersion:        target,
		AttemptID:              "attempt-1",
		RequiresBlueGreenRearm: true,
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
			Phase:           &completed,
			Strategy:        &strategy,
			SourcePgVersion: &source,
			TargetPgVersion: &target,
			BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
				AttemptID: "attempt-1",
				Cleanup:   &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateCleaned},
			},
		}},
	}}
	useCase := NewMajorUpgradeUseCase(store, nil, nil, pgupgradeflow.NoopNotifier(), &fakeBlueGreenPreparer{}, nil, nil)

	require.NoError(t, useCase.Prerequisites(t.Context()))
	assert.Zero(t, store.rearmWrites, "Prerequisites must be read-only")

	scheduled, err := useCase.Schedule(t.Context())
	require.NoError(t, err)
	assert.True(t, scheduled)
	assert.Zero(t, store.rearmWrites, "Schedule must be read-only")
	assert.Empty(t, useCase.BlocksComponents(), "a status-only re-arm must not block components")

	report, err := useCase.Act(t.Context())
	require.NoError(t, err)
	assert.Equal(t, mvutypes.UseCaseName, report.Name)
	assert.False(t, report.Retry)
	assert.Equal(t, 1, store.rearmWrites)
	assert.Zero(t, store.progressWrites)
}

func TestMajorUpgradeUseCaseScheduleBlocksCompletedIntent(t *testing.T) {
	source := "13"
	target := "14"
	strategy := mvutypes.MajorUpgradeFlowPgUpgrade
	phase := string(mvutypes.Completed)

	intent := mvutypes.Intent{
		Strategy:        strategy,
		SourcePgVersion: source,
		TargetPgVersion: target,
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
			Phase:           &phase,
			Strategy:        &strategy,
			SourcePgVersion: &source,
			TargetPgVersion: &target,
		}},
	}

	scheduled, err := newTestUseCase(intent).Schedule(t.Context())
	if err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}
	if scheduled {
		t.Fatalf("Schedule() = true, want false")
	}
}

func TestMajorUpgradeUseCaseScheduleBlocksFailedIntentWithoutNewerRetryAnnotation(t *testing.T) {
	source := "13"
	target := "14"
	strategy := mvutypes.MajorUpgradeFlowPgUpgrade
	phase := string(mvutypes.Failed)
	failedAt := metav1.NewTime(time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC))
	retryAt := metav1.NewTime(time.Date(2026, 6, 24, 9, 59, 0, 0, time.UTC))

	intent := mvutypes.Intent{
		Strategy:        strategy,
		SourcePgVersion: source,
		TargetPgVersion: target,
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
			Phase:           &phase,
			Strategy:        &strategy,
			SourcePgVersion: &source,
			TargetPgVersion: &target,
			Conditions: []metav1.Condition{{
				Type:               mvutypes.ConditionMajorUpgradeTerminalFailure,
				LastTransitionTime: failedAt,
			}},
		}},
		RetryRequestedAt: &retryAt,
	}

	scheduled, err := newTestUseCase(intent).Schedule(t.Context())
	if err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}
	if scheduled {
		t.Fatalf("Schedule() = true, want false")
	}
}

func TestMajorUpgradeUseCaseScheduleAllowsFailedIntentWithNewerRetryAnnotation(t *testing.T) {
	source := "13"
	target := "14"
	strategy := mvutypes.MajorUpgradeFlowPgUpgrade
	phase := string(mvutypes.Failed)
	failedAt := metav1.NewTime(time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC))
	retryAt := metav1.NewTime(time.Date(2026, 6, 24, 10, 1, 0, 0, time.UTC))

	intent := mvutypes.Intent{
		Strategy:        strategy,
		SourcePgVersion: source,
		TargetPgVersion: target,
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
			Phase:           &phase,
			Strategy:        &strategy,
			SourcePgVersion: &source,
			TargetPgVersion: &target,
			Conditions: []metav1.Condition{{
				Type:               mvutypes.ConditionMajorUpgradeTerminalFailure,
				LastTransitionTime: failedAt,
			}},
		}},
		RetryRequestedAt: &retryAt,
	}

	scheduled, err := newTestUseCase(intent).Schedule(t.Context())
	if err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}
	if !scheduled {
		t.Fatalf("Schedule() = false, want true")
	}
}

func TestMajorUpgradeUseCaseBlocksComponentsBeforeSourceIsLatched(t *testing.T) {
	intent := mvutypes.Intent{
		Strategy:        mvutypes.MajorUpgradeFlowPgUpgrade,
		TargetPgVersion: "18",
	}

	useCase := newTestUseCase(intent)
	scheduled, err := useCase.Schedule(t.Context())
	if err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}
	if !scheduled {
		t.Fatalf("Schedule() = false, want true")
	}
	blocked := useCase.BlocksComponents()
	if len(blocked) == 0 {
		t.Fatalf("BlocksComponents() returned no blocks for active major upgrade without source")
	}

	blockedSet := make(map[string]struct{}, len(blocked))
	for _, c := range blocked {
		blockedSet[c] = struct{}{}
	}
	for _, want := range []string{
		pgcconstants.ComponentProvisioner,
		pgcconstants.ComponentManagedRoles,
		pgcconstants.ComponentPooler,
		pgcconstants.ComponentBackup,
		pgcconstants.ComponentConfigMap,
		pgcconstants.ComponentCustomMetrics,
	} {
		if _, ok := blockedSet[want]; !ok {
			t.Errorf("BlocksComponents() missing %q; got %v", want, blocked)
		}
	}
}

func TestMajorUpgradeUseCaseBlocksComponentsDuringBlueGreenPreparation(t *testing.T) {
	useCase := newTestUseCase(mvutypes.Intent{
		Strategy:        mvutypes.MajorUpgradeFlowBlueGreen,
		SourcePgVersion: "17",
		TargetPgVersion: "18",
		AttemptID:       "attempt-1",
	})
	require.True(t, func() bool {
		scheduled, err := useCase.Schedule(t.Context())
		require.NoError(t, err)
		return scheduled
	}())
	assert.NotEmpty(t, useCase.BlocksComponents())
}

func TestMajorUpgradeUseCaseBackupWaitReturnsReportWithoutError(t *testing.T) {
	intent := mvutypes.Intent{
		Strategy:        mvutypes.MajorUpgradeFlowPgUpgrade,
		SourcePgVersion: "17",
		TargetPgVersion: "18",
	}
	useCase := NewMajorUpgradeUseCase(
		fakeInfoStore(intent),
		fakeBackupProvider{err: mvutypes.ErrBackupStatusMissing},
		nil,
		pgupgradeflow.NoopNotifier(),
		&fakeBlueGreenPreparer{},
		nil, nil)

	report, err := useCase.Act(t.Context())
	if err != nil {
		t.Fatalf("Act() error = %v, want nil for expected backup wait", err)
	}
	if report.Phase != string(mvutypes.PreUpgradeBackup) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.PreUpgradeBackup)
	}
	if !report.Retry {
		t.Fatalf("expected retry report")
	}
}

func TestMajorUpgradeUseCasePreUpgradeBackupResumesFromScheduled(t *testing.T) {
	// If the operator restarts while the use case is in PreUpgradeBackup, the
	// strategy flow must not receive that phase — phaseForIntent normalises it to
	// Scheduled so pg_upgrade restarts cleanly from the top.
	intent := postUpgradeIntent(string(mvutypes.PreUpgradeBackup))
	intent.State[0].Phase = func() *string { s := string(mvutypes.PreUpgradeBackup); return &s }()

	useCase := NewMajorUpgradeUseCase(
		fakeInfoStore(intent),
		fakeBackupProvider{status: &mvutypes.BackupInfo{}},
		&fakePgUpgrade{},
		pgupgradeflow.NoopNotifier(),
		&fakeBlueGreenPreparer{},
		nil, nil)

	report, err := useCase.Act(t.Context())
	if err != nil {
		t.Fatalf("Act() error = %v, want nil", err)
	}
	if report.Phase == string(mvutypes.PreUpgradeBackup) {
		t.Fatalf("phase = %q: strategy flow must not resume into PreUpgradeBackup", report.Phase)
	}
	if !report.Retry {
		t.Fatalf("expected flow to retry after normalised resume")
	}
}

func TestMajorUpgradeUseCaseCompletedFlowHoldsInPostUpgradeBackup(t *testing.T) {
	// Start from Verifying: the strategy runs onVerifying → returns PostUpgradeBackup.
	// The use case persists that phase and does not proceed to Completed — it holds
	// for the next reconcile where the PostUpgradeBackup intercept will run.
	intent := postUpgradeIntent(string(mvutypes.Verifying))

	useCase := NewMajorUpgradeUseCase(
		fakeInfoStore(intent),
		fakeBackupProvider{status: &mvutypes.BackupInfo{}},
		&fakePgUpgrade{},
		pgupgradeflow.NoopNotifier(),
		&fakeBlueGreenPreparer{},
		nil, nil)

	report, err := useCase.Act(t.Context())
	if err != nil {
		t.Fatalf("Act() error = %v, want nil while waiting on post-upgrade backup", err)
	}
	if report.Phase != string(mvutypes.PostUpgradeBackup) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.PostUpgradeBackup)
	}
	if !report.Retry {
		t.Fatalf("PostUpgradeBackup report from strategy must retry: reconciler must requeue immediately to run the post-upgrade backup intercept")
	}
}

func TestMajorUpgradeUseCasePostUpgradeBackupCompletesWhenBaselineReady(t *testing.T) {
	postUpgradeBackup := string(mvutypes.PostUpgradeBackup)
	intent := postUpgradeIntent(postUpgradeBackup)

	// Already latched in PostUpgradeBackup with a fresh baseline available: the
	// use case completes without ever re-entering the strategy flow.
	driver := &fakePgUpgrade{}
	useCase := NewMajorUpgradeUseCase(
		fakeInfoStore(intent),
		fakeBackupProvider{status: &mvutypes.BackupInfo{}},
		driver,
		pgupgradeflow.NoopNotifier(),
		&fakeBlueGreenPreparer{},
		nil, nil)

	report, err := useCase.Act(t.Context())
	if err != nil {
		t.Fatalf("Act() error = %v", err)
	}
	if report.Phase != string(mvutypes.Completed) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.Completed)
	}
	if report.Retry {
		t.Fatalf("completed report should not retry")
	}
}

func TestMajorUpgradeUseCaseActFailsTerminalOnDowngrade(t *testing.T) {
	intent := mvutypes.Intent{
		Strategy:        mvutypes.MajorUpgradeFlowPgUpgrade,
		SourcePgVersion: "18",
		TargetPgVersion: "17",
	}
	useCase := NewMajorUpgradeUseCase(
		fakeInfoStore(intent),
		fakeBackupProvider{status: &mvutypes.BackupInfo{}},
		&fakePgUpgrade{},
		pgupgradeflow.NoopNotifier(),
		&fakeBlueGreenPreparer{},
		nil, nil)

	report, err := useCase.Act(t.Context())
	if err == nil {
		t.Fatalf("Act() error = nil, want terminal error for downgrade")
	}
	if report.Phase != string(mvutypes.Failed) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.Failed)
	}
	if report.Retry {
		t.Fatalf("downgrade report should be terminal, not retried")
	}
}

func TestMajorUpgradeUseCaseActFailsTerminalOnMultiMajorJump(t *testing.T) {
	intent := mvutypes.Intent{
		Strategy:        mvutypes.MajorUpgradeFlowPgUpgrade,
		SourcePgVersion: "15",
		TargetPgVersion: "18",
		Policy:          mvutypes.DefaultUpgradePolicy(),
	}
	useCase := NewMajorUpgradeUseCase(
		fakeInfoStore(intent),
		fakeBackupProvider{status: &mvutypes.BackupInfo{}},
		&fakePgUpgrade{},
		pgupgradeflow.NoopNotifier(),
		&fakeBlueGreenPreparer{},
		nil, nil)

	report, err := useCase.Act(t.Context())
	if err == nil {
		t.Fatalf("Act() error = nil, want terminal error for multi-major jump")
	}
	if report.Phase != string(mvutypes.Failed) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.Failed)
	}
}

func TestMajorUpgradeUseCaseActAllowsMultiMajorJumpWhenPolicyPermits(t *testing.T) {
	intent := mvutypes.Intent{
		Strategy:        mvutypes.MajorUpgradeFlowPgUpgrade,
		SourcePgVersion: "15",
		TargetPgVersion: "18",
		Policy:          mvutypes.UpgradePolicy{AllowDirectMultiMajorJump: true},
	}
	useCase := NewMajorUpgradeUseCase(
		fakeInfoStore(intent),
		fakeBackupProvider{status: &mvutypes.BackupInfo{}},
		&fakePgUpgrade{},
		pgupgradeflow.NoopNotifier(),
		&fakeBlueGreenPreparer{},
		nil, nil)

	report, err := useCase.Act(t.Context())
	if err != nil {
		t.Fatalf("Act() error = %v, want nil when multi-major jump explicitly allowed", err)
	}
	if report.Phase == string(mvutypes.Failed) {
		t.Fatalf("phase = Failed, policy override should have allowed the jump")
	}
}

// TestMajorUpgradeUseCaseActFailsTerminalOnUnknownSource guards the source-unknown
// validation bypass: a non-empty but unparseable source major must be rejected
// rather than silently skipping the downgrade and multi-major-jump checks. The
// empty-source case never reaches Act (Prerequisites defers it), so this exercises
// the only path where validateIntent runs with an unparseable source.
func TestMajorUpgradeUseCaseActFailsTerminalOnUnknownSource(t *testing.T) {
	intent := mvutypes.Intent{
		Strategy:        mvutypes.MajorUpgradeFlowPgUpgrade,
		SourcePgVersion: "garbage",
		TargetPgVersion: "18",
		Policy:          mvutypes.DefaultUpgradePolicy(),
	}
	useCase := NewMajorUpgradeUseCase(
		fakeInfoStore(intent),
		fakeBackupProvider{status: &mvutypes.BackupInfo{}},
		&fakePgUpgrade{},
		pgupgradeflow.NoopNotifier(),
		&fakeBlueGreenPreparer{},
		nil, nil)

	report, err := useCase.Act(t.Context())
	if err == nil {
		t.Fatalf("Act() error = nil, want terminal error for unknown source major")
	}
	if report.Phase != string(mvutypes.Failed) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.Failed)
	}
	if report.Retry {
		t.Fatalf("unknown-source report should be terminal, not retried")
	}
}

// TestMajorUpgradeUseCaseRetryAfterTerminalFailureResumesFlow exercises the
// operator-driven recovery path: a previous attempt ended in Failed, the user
// bumped the retry annotation, and the next reconcile must restart the strategy
// flow from a clean phase instead of handing the flow a Failed state it cannot
// resume (which would immediately re-fail).
func TestMajorUpgradeUseCaseRetryAfterTerminalFailureResumesFlow(t *testing.T) {
	source := "17"
	target := "18"
	strategy := mvutypes.MajorUpgradeFlowPgUpgrade
	phase := string(mvutypes.Failed)
	failedAt := metav1.NewTime(time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC))
	retryAt := metav1.NewTime(time.Date(2026, 6, 24, 10, 1, 0, 0, time.UTC))

	intent := mvutypes.Intent{
		Strategy:         strategy,
		SourcePgVersion:  source,
		TargetPgVersion:  target,
		RetryRequestedAt: &retryAt,
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
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
	useCase := NewMajorUpgradeUseCase(
		fakeInfoStore(intent),
		fakeBackupProvider{status: &mvutypes.BackupInfo{}},
		&fakePgUpgrade{},
		pgupgradeflow.NoopNotifier(),
		&fakeBlueGreenPreparer{},
		nil, nil)

	report, err := useCase.Act(t.Context())
	if err != nil {
		t.Fatalf("Act() error = %v, want nil: retry should resume the flow", err)
	}
	// Resuming from Scheduled runs the first flow step (preflight) and retries;
	// it must not bounce straight back to Failed.
	if report.Phase == string(mvutypes.Failed) {
		t.Fatalf("retry resumed into Failed (%q): flow was handed an unresumable phase", report.Reason)
	}
	if !report.Retry {
		t.Fatalf("expected resumed flow to request another reconcile")
	}
}

func TestMajorUpgradeUseCaseEmitsFailedEventOnFirstFailure(t *testing.T) {
	intent := mvutypes.Intent{
		Strategy:        mvutypes.MajorUpgradeFlowPgUpgrade,
		SourcePgVersion: "18",
		TargetPgVersion: "17", // downgrade → terminal failure
	}
	notifier := &fakeNotifier{}
	useCase := NewMajorUpgradeUseCase(fakeInfoStore(intent), fakeBackupProvider{status: &mvutypes.BackupInfo{}}, &fakePgUpgrade{}, notifier, &fakeBlueGreenPreparer{}, nil, nil)

	report, _ := useCase.Act(t.Context())
	if report.Phase != string(mvutypes.Failed) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.Failed)
	}
	if len(notifier.warnings) != 1 || notifier.warnings[0] != mvutypes.EventMajorUpgradeFailed {
		t.Fatalf("warnings = %v, want [%s]", notifier.warnings, mvutypes.EventMajorUpgradeFailed)
	}
	if len(notifier.informs) != 0 {
		t.Fatalf("unexpected informs on failure: %v", notifier.informs)
	}
}

func TestMajorUpgradeUseCaseEmitsPreUpgradeBackupStartedOnFirstEntry(t *testing.T) {
	intent := mvutypes.Intent{
		Strategy:        mvutypes.MajorUpgradeFlowPgUpgrade,
		SourcePgVersion: "17",
		TargetPgVersion: "18",
		// No State entries — raw phase is Scheduled; first reconcile enters PreUpgradeBackup.
	}
	notifier := &fakeNotifier{}
	useCase := NewMajorUpgradeUseCase(
		fakeInfoStore(intent),
		fakeBackupProvider{err: mvutypes.ErrBackupStatusMissing},
		nil,
		notifier,
		&fakeBlueGreenPreparer{},
		nil, nil)

	report, _ := useCase.Act(t.Context())
	if report.Phase != string(mvutypes.PreUpgradeBackup) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.PreUpgradeBackup)
	}
	if len(notifier.informs) != 1 || notifier.informs[0] != mvutypes.EventPreUpgradeBackupStarted {
		t.Fatalf("informs = %v, want [%s]", notifier.informs, mvutypes.EventPreUpgradeBackupStarted)
	}
}

func TestMajorUpgradeUseCaseDoesNotReemitPreUpgradeBackupStartedOnRetry(t *testing.T) {
	// Raw persisted phase is already PreUpgradeBackup — this is a retry reconcile.
	// The event must not re-fire.
	preUpgradeBackup := string(mvutypes.PreUpgradeBackup)
	source := "17"
	target := "18"
	strategy := mvutypes.MajorUpgradeFlowPgUpgrade
	intent := mvutypes.Intent{
		Strategy:        strategy,
		SourcePgVersion: source,
		TargetPgVersion: target,
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
			Phase:           &preUpgradeBackup,
			Strategy:        &strategy,
			SourcePgVersion: &source,
			TargetPgVersion: &target,
		}},
	}
	notifier := &fakeNotifier{}
	useCase := NewMajorUpgradeUseCase(
		fakeInfoStore(intent),
		fakeBackupProvider{err: mvutypes.ErrBackupStatusMissing},
		nil,
		notifier,
		&fakeBlueGreenPreparer{},
		nil, nil)

	report, _ := useCase.Act(t.Context())
	if report.Phase != string(mvutypes.PreUpgradeBackup) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.PreUpgradeBackup)
	}
	if len(notifier.informs) != 0 {
		t.Fatalf("event must not re-emit on retry: informs = %v", notifier.informs)
	}
}

func postUpgradeIntent(phase string) mvutypes.Intent {
	source := "17"
	target := "18"
	strategy := mvutypes.MajorUpgradeFlowPgUpgrade
	return mvutypes.Intent{
		Strategy:        strategy,
		SourcePgVersion: source,
		TargetPgVersion: target,
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
			Phase:           &phase,
			Strategy:        &strategy,
			SourcePgVersion: &source,
			TargetPgVersion: &target,
		}},
	}
}

type fakePgUpgrade struct{}

func (f *fakePgUpgrade) ApplyTargetImage(context.Context) error { return nil }
func (f *fakePgUpgrade) UpgradeComplete(context.Context) (bool, error) {
	return true, nil
}
func (f *fakePgUpgrade) VerifyUpgrade(context.Context) (bool, error) { return true, nil }

type majorUpgradeInfoStoreFunc func(context.Context) (mvutypes.Intent, bool, error)

type readinessStore struct {
	majorUpgradeInfoStoreFunc
	ready bool
}

func (s readinessStore) ReadyToStart(context.Context) (bool, error) { return s.ready, nil }

type fakeBackupProvider struct {
	status *mvutypes.BackupInfo
	err    error
}

func newTestUseCase(intent mvutypes.Intent) *MajorUpgradeUseCase {
	return NewMajorUpgradeUseCase(fakeInfoStore(intent), nil, nil, pgupgradeflow.NoopNotifier(), &fakeBlueGreenPreparer{}, nil, nil)
}

func fakeInfoStore(intent mvutypes.Intent) majorUpgradeInfoStoreFunc {
	return func(context.Context) (mvutypes.Intent, bool, error) {
		return intent, true, nil
	}
}

func (f majorUpgradeInfoStoreFunc) ReadMajorUpgradeIntent(ctx context.Context) (mvutypes.Intent, bool, error) {
	return f(ctx)
}

func (majorUpgradeInfoStoreFunc) ReadyToStart(context.Context) (bool, error) { return true, nil }

func (f majorUpgradeInfoStoreFunc) SaveMajorUpgradeProgress(_ context.Context, _ mvutypes.Intent, progress mvutypes.Progress) (mvutypes.Progress, error) {
	return progress, nil
}

func (f majorUpgradeInfoStoreFunc) SaveBlueGreenRearm(context.Context, mvutypes.Intent) error {
	return nil
}

type blueGreenRearmStore struct {
	intent         mvutypes.Intent
	progressWrites int
	rearmWrites    int
}

type blueGreenProgressStore struct {
	intent   mvutypes.Intent
	progress mvutypes.Progress
}

func TestCommittedBlueGreenFailureStaysScheduledAndFailsForward(t *testing.T) {
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	now := metav1.Now()
	intent := mvutypes.Intent{Strategy: strategy, SourcePgVersion: "17", TargetPgVersion: "18", AttemptID: "attempt",
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
			Strategy: &strategy, SourcePgVersion: new("17"), TargetPgVersion: new("18"), Phase: new(string(mvutypes.Failed)),
			BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt",
				SourcePreparation: &platformv1alpha1.BlueGreenSourcePreparationStatus{CompletedAt: &now},
				CommitReceipt:     &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CommittedAt: &now},
				Switchover:        &platformv1alpha1.BlueGreenSwitchoverStatus{StartedAt: &now}},
		}},
	}
	store := &blueGreenProgressStore{intent: intent}
	switchover := switchoverFunc(func(context.Context, mvutypes.Intent, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bluegreen.Outcome, error) {
		return bluegreen.Outcome{Report: reconciliationtypes.Report{Name: mvutypes.UseCaseName, Phase: string(mvutypes.Failed)}}, errors.New("temporary projection outage")
	})
	useCase := NewMajorUpgradeUseCase(store, nil, nil, pgupgradeflow.NoopNotifier(), nil, nil, switchover)
	scheduled, err := useCase.Schedule(t.Context())
	require.NoError(t, err)
	assert.True(t, scheduled)
	assert.NotEmpty(t, useCase.BlocksComponents())
	report, err := useCase.Act(t.Context())
	require.ErrorContains(t, err, "temporary projection outage")
	assert.Equal(t, string(mvutypes.SwitchingOver), report.Phase)
	assert.True(t, report.Retry)
	assert.Equal(t, string(mvutypes.SwitchingOver), store.progress.Report.Phase)
}

func TestAvailableBlueGreenNeverUsesLegacyBackupCompletion(t *testing.T) {
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	now := metav1.Now()
	intent := mvutypes.Intent{Strategy: strategy, SourcePgVersion: "17", TargetPgVersion: "18", AttemptID: "attempt",
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
			Strategy: &strategy, SourcePgVersion: new("17"), TargetPgVersion: new("18"), Phase: new(string(mvutypes.PostUpgradeBackup)),
			BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt",
				SourcePreparation: &platformv1alpha1.BlueGreenSourcePreparationStatus{CompletedAt: &now},
				Green:             &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}},
				CommitReceipt:     &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CommittedAt: &now, Target: &corev1.ObjectReference{Name: "green", UID: "green-uid"}, CanonicalConfigMap: &corev1.ObjectReference{Name: "access", UID: "canonical-uid"}},
				Switchover:        &platformv1alpha1.BlueGreenSwitchoverStatus{StartedAt: &now, TargetAvailableAt: &now, EndpointPublicationEventRef: &corev1.ObjectReference{Name: "publication-event"}}},
		}},
	}
	store := &blueGreenProgressStore{intent: intent}
	useCase := NewMajorUpgradeUseCase(store, nil, nil, pgupgradeflow.NoopNotifier(), nil, nil, switchoverFunc(func(_ context.Context, _ mvutypes.Intent, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bluegreen.Outcome, error) {
		return bluegreen.Outcome{BlueGreen: status, Report: reconciliationtypes.Report{Name: mvutypes.UseCaseName, Phase: string(mvutypes.PostUpgradeBackup), Retry: true}}, nil
	}))
	report, err := useCase.Act(t.Context())
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.PostUpgradeBackup), report.Phase)
	assert.True(t, report.Retry)
}

func TestRestoredSwitchoverRetryRequiresFreshSourceReadiness(t *testing.T) {
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	restored := metav1.NewTime(time.Now().Add(-time.Minute))
	retryAt := metav1.Now()
	intent := mvutypes.Intent{Strategy: strategy, SourcePgVersion: "17", TargetPgVersion: "18", AttemptID: "attempt", RetryRequestedAt: &retryAt,
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
			Strategy: &strategy, SourcePgVersion: new("17"), TargetPgVersion: new("18"), Phase: new(string(mvutypes.Failed)),
			Conditions: []metav1.Condition{{Type: mvutypes.ConditionMajorUpgradeTerminalFailure, LastTransitionTime: restored}},
			BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt",
				Gates:      &platformv1alpha1.BlueGreenGateStatus{Switchover: &platformv1alpha1.BlueGreenGateConsumptionStatus{Generation: 3, ConsumedAt: &restored}},
				Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{StartedAt: &restored, SourceFenceRestoredAt: &restored}},
		}},
	}
	store := readinessStore{majorUpgradeInfoStoreFunc: fakeInfoStore(intent), ready: false}
	useCase := NewMajorUpgradeUseCase(store, nil, nil, pgupgradeflow.NoopNotifier(), nil, nil, nil)
	require.ErrorIs(t, useCase.Prerequisites(t.Context()), usecases.ErrPrerequisiteNotReady)
	store.ready = true
	useCase.store = store
	require.NoError(t, useCase.Prerequisites(t.Context()))
	scheduled, err := useCase.Schedule(t.Context())
	require.NoError(t, err)
	assert.True(t, scheduled)
	assert.NotEmpty(t, useCase.BlocksComponents())
}

func TestRestoredBlueGreenKeepsCredentialsAndObjectStoreBlocked(t *testing.T) {
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	for _, stage := range []mvutypes.CutoverStage{mvutypes.CutoverNone, mvutypes.CutoverRestored} {
		t.Run(fmt.Sprint(stage), func(t *testing.T) {
			status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt"}
			if stage == mvutypes.CutoverRestored {
				status.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{SourceFenceRestoredAt: new(metav1.Now())}
			}
			intent := mvutypes.Intent{Strategy: strategy, SourcePgVersion: "17", TargetPgVersion: "18", AttemptID: "attempt", State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
				Strategy: &strategy, SourcePgVersion: new("17"), TargetPgVersion: new("18"), Phase: new(string(mvutypes.SwitchingOver)), BlueGreen: status,
			}}}
			require.Equal(t, stage, mvutypes.CutoverStageForIntent(intent))
			useCase := newTestUseCase(intent)
			scheduled, err := useCase.Schedule(t.Context())
			require.NoError(t, err)
			require.True(t, scheduled)
			blocked := useCase.BlocksComponents()
			for _, name := range []string{pgcconstants.ComponentSecret, pgcconstants.ComponentObjectStore} {
				assert.Equal(t, stage == mvutypes.CutoverRestored, slices.Contains(blocked, name), "component %s", name)
			}
			assert.Contains(t, blocked, pgcconstants.ComponentManagedRoles)
			assert.Contains(t, blocked, pgcconstants.ComponentCustomMetrics)
		})
	}
}

func TestBlueGreenKeepsOrdinaryServerTLSBlockedThroughCutoverAndHandoff(t *testing.T) {
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	for _, phase := range []mvutypes.Status{mvutypes.Preflight, mvutypes.ReadyForSwitchover, mvutypes.SwitchingOver, mvutypes.PostUpgradeBackup} {
		t.Run(string(phase), func(t *testing.T) {
			intent := mvutypes.Intent{Strategy: strategy, SourcePgVersion: "17", TargetPgVersion: "18", AttemptID: "attempt", State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
				Strategy: &strategy, SourcePgVersion: new("17"), TargetPgVersion: new("18"), Phase: new(string(phase)), BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt"},
			}}}
			useCase := newTestUseCase(intent)
			scheduled, err := useCase.Schedule(t.Context())
			require.NoError(t, err)
			assert.True(t, scheduled)
			assert.Contains(t, useCase.BlocksComponents(), pgcconstants.ComponentServerTLS)
		})
	}
	pgUpgrade := newTestUseCase(mvutypes.Intent{Strategy: mvutypes.MajorUpgradeFlowPgUpgrade, SourcePgVersion: "17", TargetPgVersion: "18"})
	_, err := pgUpgrade.Schedule(t.Context())
	require.NoError(t, err)
	assert.NotContains(t, pgUpgrade.BlocksComponents(), pgcconstants.ComponentServerTLS)
}

type switchoverFunc func(context.Context, mvutypes.Intent, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bluegreen.Outcome, error)

func (f switchoverFunc) AdvanceSwitchover(ctx context.Context, intent mvutypes.Intent, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bluegreen.Outcome, error) {
	return f(ctx, intent, status)
}

func (s *blueGreenProgressStore) ReadMajorUpgradeIntent(context.Context) (mvutypes.Intent, bool, error) {
	return s.intent, true, nil
}

func (*blueGreenProgressStore) ReadyToStart(context.Context) (bool, error) { return true, nil }

func (s *blueGreenProgressStore) SaveMajorUpgradeProgress(_ context.Context, _ mvutypes.Intent, progress mvutypes.Progress) (mvutypes.Progress, error) {
	s.progress = progress
	return progress, nil
}

func (s *blueGreenProgressStore) SaveBlueGreenRearm(context.Context, mvutypes.Intent) error {
	return nil
}

type fakeBlueGreenPreparer struct {
	status *platformv1alpha1.PostgresBlueGreenUpgradeStatus
	err    error
	calls  int
}

type fakeBlueGreenCandidatePreparer struct {
	status *platformv1alpha1.PostgresBlueGreenUpgradeStatus
	err    error
	calls  int
}

func (p *fakeBlueGreenCandidatePreparer) PrepareCandidate(context.Context, mvutypes.Intent) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	p.calls++
	return p.status, p.err
}

func (p *fakeBlueGreenPreparer) Prepare(context.Context, mvutypes.Intent) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	p.calls++
	return p.status, p.err
}

func (s *blueGreenRearmStore) ReadMajorUpgradeIntent(context.Context) (mvutypes.Intent, bool, error) {
	return s.intent, true, nil
}

func (*blueGreenRearmStore) ReadyToStart(context.Context) (bool, error) { return true, nil }

func (s *blueGreenRearmStore) SaveMajorUpgradeProgress(_ context.Context, _ mvutypes.Intent, progress mvutypes.Progress) (mvutypes.Progress, error) {
	s.progressWrites++
	return progress, nil
}

func (s *blueGreenRearmStore) SaveBlueGreenRearm(context.Context, mvutypes.Intent) error {
	s.rearmWrites++
	return nil
}

func (f fakeBackupProvider) CreateBackup(context.Context, mvutypes.Intent, func(mvutypes.Intent) string) (*mvutypes.BackupInfo, error) {
	return f.status, f.err
}

type fakeNotifier struct {
	informs  []string
	warnings []string
}

func (n *fakeNotifier) Inform(reason, _ string) { n.informs = append(n.informs, reason) }
func (n *fakeNotifier) Warn(reason, _ string)   { n.warnings = append(n.warnings, reason) }
