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
	"fmt"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationtypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	bluegreen "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/blue_green"
	pgupgradeflow "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/pg_upgrade"
)

type cleanupInfoStore interface {
	ReadMajorUpgradeIntent(context.Context) (mvutypes.Intent, bool, error)
	SaveMajorUpgradeProgress(context.Context, mvutypes.Intent, mvutypes.Progress) (mvutypes.Progress, error)
}

// CleanupUseCase retires terminal attempts without blocking production reconciliation.
type CleanupUseCase struct {
	store    cleanupInfoStore
	cleaner  bluegreen.Cleaner
	notifier pgupgradeflow.Notifier
}

func NewCleanupUseCase(store cleanupInfoStore, cleaner bluegreen.Cleaner, notifier pgupgradeflow.Notifier) *CleanupUseCase {
	return &CleanupUseCase{store: store, cleaner: cleaner, notifier: notifier}
}

func (*CleanupUseCase) Prerequisites(context.Context) error { return nil }
func (*CleanupUseCase) BlocksComponents() []string          { return nil }

func (h *CleanupUseCase) Schedule(ctx context.Context) (bool, error) {
	intent, enabled, err := h.store.ReadMajorUpgradeIntent(ctx)
	return err == nil && enabled && intent.RequiresBlueGreenCleanup && h.cleaner != nil, err
}

func (h *CleanupUseCase) Act(ctx context.Context) (reconciliationtypes.Report, error) {
	intent, enabled, err := h.store.ReadMajorUpgradeIntent(ctx)
	if err != nil {
		return mvutypes.ReportFromError(err), err
	}
	if !enabled || !intent.RequiresBlueGreenCleanup {
		return mvutypes.ReportFromError(mvutypes.ErrUpgradeIntentMissing), nil
	}
	var status *platformv1alpha1.PostgresBlueGreenUpgradeStatus
	phase := mvutypes.Scheduled
	for _, entry := range intent.State {
		if mvutypes.MatchesIntent(entry, intent) && entry.Phase != nil {
			status, phase = entry.BlueGreen.DeepCopy(), mvutypes.Status(*entry.Phase)
		}
	}
	if h.cleaner == nil || status == nil || phase != mvutypes.Completed && phase != mvutypes.Cancelled {
		return reconciliationtypes.Report{Name: mvutypes.UseCaseName, Phase: string(phase)}, fmt.Errorf("terminal blue/green cleanup runtime is unavailable")
	}
	outcome, cause := h.cleaner.AdvanceCleanup(ctx, intent)
	outcome.Report.Name, outcome.Report.Phase = mvutypes.UseCaseName, string(phase)
	if outcome.BlueGreen == nil {
		outcome.BlueGreen = status
	}
	if outcome.Report.Retry && outcome.Report.Sleep == nil {
		outcome.Report.Sleep = new(1)
	}
	progress, err := h.store.SaveMajorUpgradeProgress(ctx, intent, mvutypes.Progress{Report: outcome.Report, BlueGreen: outcome.BlueGreen})
	if err != nil {
		return outcome.Report, err
	}
	h.emitCleanupEvent(intent, progress)
	return progress.Report, cause
}

func (h *CleanupUseCase) emitCleanupEvent(intent mvutypes.Intent, progress mvutypes.Progress) {
	if h.notifier == nil || progress.BlueGreen == nil || progress.BlueGreen.Cleanup == nil {
		return
	}
	state := progress.BlueGreen.Cleanup.State
	for _, entry := range intent.State {
		if mvutypes.MatchesIntent(entry, intent) && entry.BlueGreen != nil && entry.BlueGreen.Cleanup != nil && entry.BlueGreen.Cleanup.State == state {
			return
		}
	}
	switch state {
	case platformv1alpha1.BlueGreenCleanupStateCleanupFailed:
		h.notifier.Warn("CleanupFailed", progress.Report.Message)
	case platformv1alpha1.BlueGreenCleanupStateCleaning:
		h.notifier.Inform("CleanupStarted", "Retiring the non-authoritative environment.")
	case platformv1alpha1.BlueGreenCleanupStateCleaned:
		h.notifier.Inform("CleanupCompleted", "Retained environment cleanup is verified.")
	}
}
