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
	"fmt"
	"slices"
	"strings"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type MajorUpgradeStateStore interface {
	GetSpecificationWithAnnotations(context.Context) (*platformv1alpha1.PostgresClusterSpec, map[string]string, error)
	GetMajorUpgradeStatus(context.Context) ([]platformv1alpha1.PostgresMajorUpgradeStatus, error)
	SetMajorUpgradeStatus(context.Context, []platformv1alpha1.PostgresMajorUpgradeStatus) error
	GetSourcePgVersion(context.Context) (string, error)
}

type majorUpgradeInfoStoreAdapter struct {
	store          MajorUpgradeStateStore
	overrideTarget string
}

func NewMajorUpgradeStateStore(store MajorUpgradeStateStore) *majorUpgradeInfoStoreAdapter {
	return &majorUpgradeInfoStoreAdapter{store: store}
}

func NewMajorUpgradeStateStoreWithTarget(store MajorUpgradeStateStore, targetVersion string) *majorUpgradeInfoStoreAdapter {
	return &majorUpgradeInfoStoreAdapter{store: store, overrideTarget: targetVersion}
}

func (r *majorUpgradeInfoStoreAdapter) ReadMajorUpgradeIntent(ctx context.Context) (mvutypes.Intent, bool, error) {
	spec, annotations, err := r.store.GetSpecificationWithAnnotations(ctx)
	if err != nil {
		return mvutypes.Intent{}, false, err
	}
	// Patch in the resolved target when the raw spec has no postgresVersion —
	// this happens when the version is inherited from a PostgresClusterClass.
	if spec != nil && spec.PostgresVersion == nil && r.overrideTarget != "" {
		spec = spec.DeepCopy()
		spec.PostgresVersion = &r.overrideTarget
	}
	entries, err := r.store.GetMajorUpgradeStatus(ctx)
	if err != nil {
		return mvutypes.Intent{}, false, err
	}
	sourceVersion, err := r.store.GetSourcePgVersion(ctx)
	if err != nil {
		return mvutypes.Intent{}, false, err
	}
	return MajorUpgradeInputFromParts(spec, entries, annotations, sourceVersion)
}

func (r *majorUpgradeInfoStoreAdapter) SaveMajorUpgradeProgress(ctx context.Context, intent mvutypes.Intent, progress mvutypes.Progress) error {
	if err := validateBlueGreenProgress(intent, progress); err != nil {
		return err
	}
	return r.store.SetMajorUpgradeStatus(ctx, stateWithProgress(intent, progress))
}

func MajorUpgradeInputFromCluster(cluster *platformv1alpha1.PostgresCluster) (mvutypes.Intent, bool, error) {
	if cluster == nil {
		return mvutypes.Intent{}, false, nil
	}

	return MajorUpgradeInputFromParts(&cluster.Spec, cluster.Status.PostgresMajorUpgradeStatus, cluster.Annotations, cluster.Status.CurrentPgVersion)
}

func MajorUpgradeInputFromParts(spec *platformv1alpha1.PostgresClusterSpec, entries []platformv1alpha1.PostgresMajorUpgradeStatus, annotations map[string]string, fallbackPgVersion string) (mvutypes.Intent, bool, error) {
	if spec == nil || spec.PostgresVersion == nil {
		return mvutypes.Intent{}, false, nil
	}
	if shouldObserveBlueGreenRearm(spec) {
		return blueGreenRearmIntent(entries, sourcePgVersion(entries, fallbackPgVersion), *spec.PostgresVersion)
	}
	if !majorUpgradeAllowed(spec.PostgresMajorUpgradeConfig) {
		return mvutypes.Intent{}, false, nil
	}

	retryRequestedAt, err := retryRequestedAt(annotations)
	if err != nil {
		return mvutypes.Intent{}, false, err
	}

	cfg := spec.PostgresMajorUpgradeConfig
	strategy := majorUpgradeStrategy(cfg)

	source := sourcePgVersion(entries, fallbackPgVersion)
	target := *spec.PostgresVersion
	if source != "" && samePostgresMajor(source, target) {
		return mvutypes.Intent{}, false, nil
	}

	intent := mvutypes.Intent{
		Strategy:         strategy,
		SourcePgVersion:  source,
		TargetPgVersion:  target,
		Policy:           mvutypes.DefaultUpgradePolicy(),
		State:            append([]platformv1alpha1.PostgresMajorUpgradeStatus(nil), entries...),
		RetryRequestedAt: retryRequestedAt,
	}
	if strategy == mvutypes.MajorUpgradeFlowBlueGreen {
		return mvutypes.Intent{}, false, nil
	}

	return intent, true, nil
}

func majorUpgradeAllowed(config *platformv1alpha1.PostgresMajorUpgradeConfig) bool {
	return config != nil && config.Allow != nil && *config.Allow
}

func majorUpgradeStrategy(config *platformv1alpha1.PostgresMajorUpgradeConfig) string {
	if config != nil && config.Strategy != nil && *config.Strategy != "" {
		return *config.Strategy
	}
	return mvutypes.MajorUpgradeFlowPgUpgrade
}

func samePostgresMajor(source, target string) bool {
	sourceMajor, _, _ := strings.Cut(source, ".")
	targetMajor, _, _ := strings.Cut(target, ".")
	return sourceMajor != "" && sourceMajor == targetMajor
}

func sourcePgVersion(entries []platformv1alpha1.PostgresMajorUpgradeStatus, fallbackPgVersion string) string {
	for i := range slices.Backward(entries) {
		entry := entries[i]
		unavailableBlueGreen := false
		if entry.BlueGreen != nil && entry.Phase != nil && *entry.Phase == string(mvutypes.Failed) {
			for _, condition := range entry.Conditions {
				if condition.Type == mvutypes.ConditionMajorUpgradeTerminalFailure &&
					condition.Reason == mvutypes.ReasonBlueGreenStrategyUnavailable {
					unavailableBlueGreen = true
					break
				}
			}
		}
		if entry.Phase != nil && *entry.Phase != string(mvutypes.Completed) && entry.SourcePgVersion != nil {
			if unavailableBlueGreen {
				continue
			}
			return *entry.SourcePgVersion
		}
	}
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.Phase != nil && *entry.Phase == string(mvutypes.Completed) && entry.TargetPgVersion != nil {
			return *entry.TargetPgVersion
		}
	}
	return fallbackPgVersion
}

func retryRequestedAt(annotations map[string]string) (*metav1.Time, error) {
	if annotations == nil {
		return nil, nil
	}

	value := annotations[mvutypes.AnnotationMajorUpgradeRetryAt]
	if value == "" {
		return nil, nil
	}

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("invalid %s annotation value %q: %w", mvutypes.AnnotationMajorUpgradeRetryAt, value, err)
	}

	retryAt := metav1.NewTime(parsed)
	return &retryAt, nil
}

func stateWithReport(intent mvutypes.Intent, report reconciliationTypes.Report, baseline *mvutypes.BackupInfo) []platformv1alpha1.PostgresMajorUpgradeStatus {
	return stateWithProgress(intent, mvutypes.Progress{Report: report, Baseline: baseline})
}

func stateWithProgress(intent mvutypes.Intent, progress mvutypes.Progress) []platformv1alpha1.PostgresMajorUpgradeStatus {
	current := currentOrNewEntry(intent.State, intent)
	current.SourcePgVersion = &intent.SourcePgVersion
	current.TargetPgVersion = &intent.TargetPgVersion
	current.Strategy = &intent.Strategy
	applyTimestamps(&current, progress.Report)
	if progress.Report.Phase != "" {
		current.Phase = &progress.Report.Phase
	}
	applyBaseline(&current, progress.Report, progress.Baseline)
	applyBlueGreenProgress(&current, intent, progress)
	applyConditions(&current, intent, progress.Report)
	return stateWithCurrentEntry(intent.State, intent, current)
}

func applyTimestamps(current *platformv1alpha1.PostgresMajorUpgradeStatus, report reconciliationTypes.Report) {
	if current.StartedAt == nil {
		now := metav1.Now()
		current.StartedAt = &now
	}
	if report.Phase == string(mvutypes.Completed) && current.CompletedAt == nil {
		now := metav1.Now()
		current.CompletedAt = &now
	}
}

// applyBaseline writes the backup name into the correct slot.
// The post-upgrade name is only written when report.Phase == Completed; callers
// MUST pass completedReport() together with the post-upgrade BackupInfo — if a
// non-Completed report is paired with a post-upgrade backup the name lands in
// PreUpgrade, corrupting status. This coupling is intentional but fragile: do
// not change the pairing without updating the corresponding test.
func applyBaseline(current *platformv1alpha1.PostgresMajorUpgradeStatus, report reconciliationTypes.Report, baseline *mvutypes.BackupInfo) {
	if baseline == nil {
		return
	}
	if baseline.BackupStatus != nil {
		current.BackupStatus = baseline.BackupStatus
	}
	if baseline.BackupName == "" {
		return
	}
	if current.BackupNames == nil {
		current.BackupNames = &platformv1alpha1.UpgradeBackupNames{}
	}
	if report.Phase == string(mvutypes.Completed) {
		current.BackupNames.PostUpgrade = &baseline.BackupName
	} else {
		current.BackupNames.PreUpgrade = &baseline.BackupName
	}
}

func applyConditions(current *platformv1alpha1.PostgresMajorUpgradeStatus, intent mvutypes.Intent, report reconciliationTypes.Report) {
	if mvutypes.RetryRequestedAfterTerminalFailure(intent.RetryRequestedAt, *current) && report.Phase != string(mvutypes.Failed) {
		current.Conditions = removeCondition(current.Conditions, mvutypes.ConditionMajorUpgradeTerminalFailure)
	}
	condition := conditionFromReport(report)
	// A retryable failure is transient: once the upgrade makes progress again,
	// drop any stale MajorUpgradeRetryableFailure so it does not linger.
	if condition.Type != mvutypes.ConditionMajorUpgradeRetryableFailure {
		current.Conditions = removeCondition(current.Conditions, mvutypes.ConditionMajorUpgradeRetryableFailure)
	}
	meta.SetStatusCondition(&current.Conditions, condition)
}

func currentOrNewEntry(entries []platformv1alpha1.PostgresMajorUpgradeStatus, intent mvutypes.Intent) platformv1alpha1.PostgresMajorUpgradeStatus {
	for i := len(entries) - 1; i >= 0; i-- {
		if mvutypes.MatchesIntent(entries[i], intent) {
			return *entries[i].DeepCopy()
		}
	}

	return platformv1alpha1.PostgresMajorUpgradeStatus{}
}

func stateWithCurrentEntry(entries []platformv1alpha1.PostgresMajorUpgradeStatus, intent mvutypes.Intent, current platformv1alpha1.PostgresMajorUpgradeStatus) []platformv1alpha1.PostgresMajorUpgradeStatus {
	next := append([]platformv1alpha1.PostgresMajorUpgradeStatus(nil), entries...)
	for i := len(next) - 1; i >= 0; i-- {
		if mvutypes.MatchesIntent(next[i], intent) {
			next[i] = current
			return compactBlueGreenHistory(next)
		}
	}
	return compactBlueGreenHistory(append(next, current))
}

func removeCondition(conditions []metav1.Condition, conditionType string) []metav1.Condition {
	filtered := make([]metav1.Condition, 0, len(conditions))
	for _, condition := range conditions {
		if condition.Type == conditionType {
			continue
		}
		filtered = append(filtered, condition)
	}
	return filtered
}

func conditionFromReport(report reconciliationTypes.Report) metav1.Condition {
	conditionType := mvutypes.ConditionMajorUpgradeProgressing
	status := metav1.ConditionTrue

	switch {
	case report.Phase == string(mvutypes.Completed):
		conditionType = mvutypes.ConditionMajorUpgradeCompleted
	case report.Phase == string(mvutypes.Failed):
		conditionType = mvutypes.ConditionMajorUpgradeTerminalFailure
	case report.Retry && mvutypes.IsRetryableFailureReason(report.Reason):
		conditionType = mvutypes.ConditionMajorUpgradeRetryableFailure
	}

	message := report.Message
	if message == "" {
		message = report.Reason
	}
	reason := report.Reason
	if reason == "" {
		reason = report.Phase
	}
	if message == "" {
		message = reason
	}

	return metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}
}
