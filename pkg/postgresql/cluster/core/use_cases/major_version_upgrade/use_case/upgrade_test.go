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
	"testing"
	"time"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	usecases "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases"
	pgupgradeflow "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/pg_upgrade"
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

func TestMajorUpgradeUseCasePrerequisitesPassWhenNotEnabled(t *testing.T) {
	// When the use case is not enabled (allow=false etc.), prerequisites are
	// irrelevant regardless of whether SourcePgVersion is populated.
	useCase := NewMajorUpgradeUseCase(
		majorUpgradeInfoStoreFunc(func(context.Context) (mvutypes.Intent, bool, error) {
			return mvutypes.Intent{}, false, nil
		}),
		nil, nil, pgupgradeflow.NoopNotifier(),
	)
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
		fakeInfoStore(intent), nil, &fakePgUpgrade{}, pgupgradeflow.NoopNotifier(),
	)
	report, err := useCase.Act(t.Context())
	if !errors.Is(err, mvutypes.ErrBackupProviderMissing) {
		t.Fatalf("Act() error = %v, want ErrBackupProviderMissing", err)
	}
	if report.Phase != string(mvutypes.Failed) || report.Retry {
		t.Fatalf("report = %#v, want terminal Failed report", report)
	}
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
		State: []enterprisev4.PostgresMajorUpgradeStatus{{
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
	if len(useCase.BlocksComponents()) == 0 {
		t.Fatalf("BlocksComponents() returned no blocks for active major upgrade without source")
	}
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
	)

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
	)

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
	)

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
	)

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
	)

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
	)

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
	)

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
	)

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
	useCase := NewMajorUpgradeUseCase(
		fakeInfoStore(intent),
		fakeBackupProvider{status: &mvutypes.BackupInfo{}},
		&fakePgUpgrade{},
		pgupgradeflow.NoopNotifier(),
	)

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
	useCase := NewMajorUpgradeUseCase(fakeInfoStore(intent), fakeBackupProvider{status: &mvutypes.BackupInfo{}}, &fakePgUpgrade{}, notifier)

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
	)

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
		State: []enterprisev4.PostgresMajorUpgradeStatus{{
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
	)

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
		State: []enterprisev4.PostgresMajorUpgradeStatus{{
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
func (f *fakePgUpgrade) VerifyUpgrade(context.Context) error { return nil }

type majorUpgradeInfoStoreFunc func(context.Context) (mvutypes.Intent, bool, error)

type fakeBackupProvider struct {
	status *mvutypes.BackupInfo
	err    error
}

func newTestUseCase(intent mvutypes.Intent) *MajorUpgradeUseCase {
	return NewMajorUpgradeUseCase(fakeInfoStore(intent), nil, nil, pgupgradeflow.NoopNotifier())
}

func fakeInfoStore(intent mvutypes.Intent) majorUpgradeInfoStoreFunc {
	return func(context.Context) (mvutypes.Intent, bool, error) {
		return intent, true, nil
	}
}

func (f majorUpgradeInfoStoreFunc) ReadMajorUpgradeIntent(ctx context.Context) (mvutypes.Intent, bool, error) {
	return f(ctx)
}

func (f majorUpgradeInfoStoreFunc) SaveMajorUpgradeProgress(context.Context, mvutypes.Intent, reconciliationTypes.Report, *mvutypes.BackupInfo) error {
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

// sequencedBackupProvider returns a different status per call so a single
// Act() can exercise both the pre-upgrade and post-upgrade backup gates.
type sequencedBackupProvider struct {
	statuses []*mvutypes.BackupInfo
	calls    int
}

func (s *sequencedBackupProvider) CreateBackup(context.Context, mvutypes.Intent, func(mvutypes.Intent) string) (*mvutypes.BackupInfo, error) {
	status := s.statuses[len(s.statuses)-1]
	if s.calls < len(s.statuses) {
		status = s.statuses[s.calls]
	}
	s.calls++
	return status, nil
}
