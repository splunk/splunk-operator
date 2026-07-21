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
	"strconv"
	"strings"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	usecases "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases"
	pgupgradeflow "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/pg_upgrade"
)

type upgradeFlow interface {
	Upgrade(context.Context) (reconciliationTypes.Report, error)
}

type MajorUpgradeUseCase struct {
	store     majorUpgradeInfoStore
	rollback  backupProvider
	pgUpgrade pgupgradeflow.PgUpgrade
	notifier  pgupgradeflow.Notifier

	intent mvutypes.Intent
	active bool
}

func NewMajorUpgradeUseCase(store majorUpgradeInfoStore, rollback backupProvider, pgUpgrade pgupgradeflow.PgUpgrade, notifier pgupgradeflow.Notifier) *MajorUpgradeUseCase {
	return &MajorUpgradeUseCase{store: store, rollback: rollback, pgUpgrade: pgUpgrade, notifier: notifier}
}

func (h *MajorUpgradeUseCase) Prerequisites(ctx context.Context) error {
	intent, enabled, err := h.readIntent(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	if intent.SourcePgVersion == "" {
		return errors.Join(usecases.ErrPrerequisiteNotReady, errors.New("source PostgreSQL version not yet written to status"))
	}
	return nil
}

func (h *MajorUpgradeUseCase) Schedule(ctx context.Context) (bool, error) {
	intent, enabled, err := h.readIntent(ctx)
	if err != nil || !enabled {
		h.active = false
		return false, err
	}
	h.intent = intent
	h.active = false

	for _, entry := range intent.State {
		if !mvutypes.MatchesIntent(entry, intent) || entry.Phase == nil {
			continue
		}
		switch *entry.Phase {
		case string(mvutypes.Completed):
			return false, nil
		case string(mvutypes.Failed):
			if mvutypes.RetryRequestedAfterTerminalFailure(intent.RetryRequestedAt, entry) {
				h.active = true
				return true, nil
			}
			return false, nil
		}
	}

	h.active = true
	return true, nil
}

func (h *MajorUpgradeUseCase) BlocksComponents() []string {
	if h == nil || !h.active {
		return nil
	}
	return []string{
		pgcConstants.ComponentProvisioner,
		pgcConstants.ComponentManagedRoles,
		pgcConstants.ComponentPooler,
		pgcConstants.ComponentBackup,
		pgcConstants.ComponentConfigMap,
	}
}

func (h *MajorUpgradeUseCase) Act(ctx context.Context) (reconciliationTypes.Report, error) {

	intent, enabled, err := h.readIntent(ctx)
	logger := logging.FromContext(ctx)

	if err != nil {
		return mvutypes.ReportFromError(err), err
	}
	if !enabled {
		return mvutypes.ReportFromError(mvutypes.ErrUpgradeIntentMissing), nil
	}
	h.intent = intent

	if err := h.validateIntent(); err != nil {
		return h.finish(ctx, mvutypes.ReportFromError(err), nil, err)
	}

	strategy := h.strategyFor(h.intent)
	if strategy == nil {
		err := errors.Join(mvutypes.ErrUnsupportedUpgradeStrategy, errors.New(h.intent.Strategy))
		return h.finish(ctx, mvutypes.ReportFromError(err), nil, err)
	}

	if h.rollback == nil {
		err := mvutypes.ErrBackupProviderMissing
		return h.finish(ctx, mvutypes.ReportFromError(err), nil, err)
	}

	currentPhase := phaseForIntent(h.intent.State, h.intent)
	if currentPhase == mvutypes.PostUpgradeBackup {
		logger.InfoContext(ctx, "major version upgrade post upgrade reconciliation")
		return h.postUpgradeBackup(ctx)
	}

	backupStatus, err := h.rollback.CreateBackup(ctx, h.intent, mvutypes.PreUpgradeBackupName)
	if err != nil {
		report, cause := resolveBackupErr(err, mvutypes.ErrPreUpgradeBackupNotReady)
		return h.finish(ctx, report, backupStatus, cause)
	}

	if backupStatus == nil {
		err := mvutypes.ErrPreUpgradeBackupNotReady
		report := mvutypes.ReportFromError(err)
		return h.finish(ctx, report, nil, reportCause(err, report))
	}

	upgrade, err := strategy.Upgrade(ctx)
	if err != nil {
		return h.finish(ctx, mvutypes.ReportFromError(err), backupStatus, err)
	}

	if upgrade.Name == "" {
		upgrade.Name = mvutypes.UseCaseName
	}

	return h.finish(ctx, upgrade, backupStatus, nil)
}

func (h *MajorUpgradeUseCase) postUpgradeBackup(ctx context.Context) (reconciliationTypes.Report, error) {
	backupStatus, err := h.rollback.CreateBackup(ctx, h.intent, mvutypes.PostUpgradeBackupName)
	if err != nil {
		report, cause := resolveBackupErr(err, mvutypes.ErrPostUpgradeBackupNotReady)
		return h.finish(ctx, report, backupStatus, cause)
	}
	if backupStatus == nil {
		report := mvutypes.ReportFromError(mvutypes.ErrPostUpgradeBackupNotReady)
		return h.finish(ctx, report, nil, reportCause(nil, report))
	}

	return h.finish(ctx, completedReport(), backupStatus, nil)
}

func completedReport() reconciliationTypes.Report {
	return reconciliationTypes.Report{
		Name:    mvutypes.UseCaseName,
		Phase:   string(mvutypes.Completed),
		Reason:  mvutypes.ReasonPgUpgradeFinalized,
		Message: mvutypes.MessagePgUpgradeFinalized,
		Retry:   false,
	}
}

// resolveBackupErr maps a backup error to a report and its propagation cause.
// Terminal errors (ErrUpgradeFlowFailed) pass through unchanged; all others are
// wrapped in retryableSentinel so ReportFromError maps them to a retryable phase.
func resolveBackupErr(err, retryableSentinel error) (reconciliationTypes.Report, error) {
	if errors.Is(err, mvutypes.ErrUpgradeFlowFailed) {
		return mvutypes.ReportFromError(err), err
	}
	report := mvutypes.ReportFromError(errors.Join(retryableSentinel, err))
	return report, reportCause(err, report)
}

func reportCause(err error, report reconciliationTypes.Report) error {
	if report.Retry &&
		(report.Phase == string(mvutypes.PreUpgradeBackup) ||
			report.Phase == string(mvutypes.PostUpgradeBackup) ||
			report.Phase == string(mvutypes.Upgrading)) {
		return nil
	}
	return err
}

func (h *MajorUpgradeUseCase) validateIntent() error {
	src := majorVersion(h.intent.SourcePgVersion)
	tgt := majorVersion(h.intent.TargetPgVersion)
	if tgt <= 0 {
		// Target major is unparseable: nothing meaningful to validate yet.
		return nil
	}
	if src <= 0 {
		// Source major is unknown/unparseable. Refuse rather than silently
		// skipping the downgrade and multi-major-jump guards below — an
		// unknown source must never be treated as "anything goes".
		return errors.Join(mvutypes.ErrInvalidUpgradeIntent, errors.New(
			"current PostgreSQL major version is unknown"))
	}
	if tgt < src {
		return errors.Join(mvutypes.ErrInvalidUpgradeIntent, fmt.Errorf(
			"requested PostgreSQL major %d is a downgrade from current major %d", tgt, src))
	}
	if tgt-src > 1 && !h.intent.Policy.AllowDirectMultiMajorJump {
		return errors.Join(mvutypes.ErrInvalidUpgradeIntent, fmt.Errorf(
			"requested PostgreSQL major %d skips intermediate versions from current major %d", tgt, src))
	}
	return nil
}

func majorVersion(version string) int {
	major, _, _ := strings.Cut(version, ".")
	parsed, err := strconv.Atoi(major)
	if err != nil {
		return 0
	}
	return parsed
}

func (h *MajorUpgradeUseCase) readIntent(ctx context.Context) (mvutypes.Intent, bool, error) {
	if h.store == nil {
		return mvutypes.Intent{}, false, errors.Join(mvutypes.ErrStateTemporarilyUnavailable, errors.New("major upgrade info store is not configured"))
	}
	return h.store.ReadMajorUpgradeIntent(ctx)
}

func (h *MajorUpgradeUseCase) strategyFor(intent mvutypes.Intent) upgradeFlow {
	switch intent.Strategy {
	case mvutypes.MajorUpgradeFlowPgUpgrade:
		return pgupgradeflow.NewPgUpgradeFlow(h.pgUpgrade, phaseForIntent(intent.State, intent))
	default:
		return nil
	}
}

func phaseForIntent(entries []enterprisev4.PostgresMajorUpgradeStatus, intent mvutypes.Intent) mvutypes.Status {
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if !mvutypes.MatchesIntent(entry, intent) || entry.Phase == nil {
			continue
		}
		phase := mvutypes.Status(*entry.Phase)
		switch {
		// A terminal failure that the operator has asked to retry restarts from a
		// clean Scheduled phase: the strategy flow has no Failed state to resume,
		// so feeding it Failed would just re-fail. This mirrors the retry gate in
		// Schedule, which re-activates the use case on the same signal.
		case phase == mvutypes.Failed &&
			mvutypes.RetryRequestedAfterTerminalFailure(intent.RetryRequestedAt, entry):
			return mvutypes.Scheduled
		// PreUpgradeBackup is owned by the outer use case, not the strategy flow.
		// If we resume into this phase the flow should restart pg_upgrade from the top.
		case phase == mvutypes.PreUpgradeBackup:
			return mvutypes.Scheduled
		}
		return phase
	}
	return mvutypes.Scheduled
}

func (h *MajorUpgradeUseCase) rawPersistedPhase() mvutypes.Status {
	for i := len(h.intent.State) - 1; i >= 0; i-- {
		entry := h.intent.State[i]
		if !mvutypes.MatchesIntent(entry, h.intent) || entry.Phase == nil {
			continue
		}
		return mvutypes.Status(*entry.Phase)
	}
	return mvutypes.Scheduled
}

func (h *MajorUpgradeUseCase) finish(ctx context.Context, report reconciliationTypes.Report, baseline *mvutypes.BackupInfo, cause error) (reconciliationTypes.Report, error) {
	h.emitPhaseEvent(report)
	if h.store != nil {
		if err := h.store.SaveMajorUpgradeProgress(ctx, h.intent, report, baseline); err != nil {
			return report, err
		}
	}
	return report, cause
}

func (h *MajorUpgradeUseCase) emitPhaseEvent(report reconciliationTypes.Report) {
	if mvutypes.Status(report.Phase) == h.rawPersistedPhase() {
		return
	}
	switch mvutypes.Status(report.Phase) {
	case mvutypes.PreUpgradeBackup:
		h.notifier.Inform(mvutypes.EventPreUpgradeBackupStarted, mvutypes.MessagePreUpgradeBackupStarted)
	case mvutypes.Preflight:
		h.notifier.Inform(mvutypes.EventMajorUpgradeScheduled, mvutypes.MessageMajorUpgradeScheduled)
	case mvutypes.Upgrading:
		h.notifier.Inform(mvutypes.EventMajorUpgradeStarted, mvutypes.MessageMajorUpgradeStarted)
	case mvutypes.PostUpgradeBackup:
		h.notifier.Inform(mvutypes.EventPostUpgradeBackupStarted, mvutypes.MessagePostUpgradeBackupStarted)
	case mvutypes.Completed:
		h.notifier.Inform(mvutypes.EventMajorUpgradeCompleted, mvutypes.MessageMajorUpgradeCompleted)
	case mvutypes.Failed:
		h.notifier.Warn(mvutypes.EventMajorUpgradeFailed, fmt.Sprintf(mvutypes.MessageMajorUpgradeFailed, report.Message))
	}
}
