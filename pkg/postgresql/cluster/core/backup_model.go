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
package core

import (
	"context"
	"errors"
	"fmt"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/backuptypes"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BackupBackend is the secondary (driven) port the backup component calls out
// through. The concrete adapter talks to CNPG; callers depend only on this
// interface, expressed in the operator's own vocabulary. It is defined here,
// next to its sole consumer (backupModel), so the port lives at its point of
// use while the value objects it exchanges live in the leaf backuptypes package.
type BackupBackend interface {
	// EnsureScheduled creates or updates the scheduled backup to match spec,
	// owned by owner. It is idempotent.
	EnsureScheduled(ctx context.Context, owner client.Object, spec backuptypes.ScheduleSpec) error
	// DeleteScheduled removes the scheduled backup if it exists and is controlled
	// by owner. It is a no-op when the object is absent or owned by someone else.
	DeleteScheduled(ctx context.Context, owner client.Object, name, namespace string) error
	// GetSchedule returns the observed state of the scheduled backup.
	// No owner filter is applied — the same name can only exist once per
	// namespace, and callers need to observe the object regardless of who owns it
	// (e.g. to surface conflicts in status).
	GetSchedule(ctx context.Context, name, namespace string) (backuptypes.ScheduleResult, error)

	// BackupNow creates a one-shot backup owned by owner if it does not already
	// exist. It is idempotent on req.Name: a backup with the same name is never
	// recreated or mutated (CNPG Backup spec is immutable).
	BackupNow(ctx context.Context, owner client.Object, req backuptypes.BackupRequest) error
	// GetBackup returns the observed state of a single backup that is controlled
	// by owner, or (zero, false) when no Backup object with that name exists or
	// the object is not controlled by owner.
	GetBackup(ctx context.Context, owner client.Object, name, namespace string) (backuptypes.BackupResult, bool, error)
	// ListBackups returns the observed state of every backup targeting the named
	// CNPG cluster in the namespace that is controlled by owner, most recent first.
	ListBackups(ctx context.Context, owner client.Object, cnpgClusterName, namespace string) ([]backuptypes.BackupResult, error)
}

const defaultBackupSuffix = "-backup"

// objectStoreBackupSuffix names the barman object-store ScheduledBackup distinctly from the
// volume-snapshot one, so both can coexist when a cluster configures both providers.
const objectStoreBackupSuffix = "-backup-objectstore"

type backupEmitter interface {
	eventEmitter
	emitBackupReadyTransition(obj client.Object, conditions []metav1.Condition)
}

type backupModel struct {
	events       backupEmitter
	backend      BackupBackend
	updateStatus healthStatusUpdater
	cluster      *enterprisev4.PostgresCluster
	mergedConfig *MergedConfig
	contracts    *reconcileContracts
}

func newBackupModel(backend BackupBackend, events backupEmitter, updateStatus healthStatusUpdater, cluster *enterprisev4.PostgresCluster, mergedConfig *MergedConfig, contracts *reconcileContracts) *backupModel {
	return &backupModel{
		events:       events,
		backend:      backend,
		updateStatus: updateStatus,
		cluster:      cluster,
		mergedConfig: mergedConfig,
		contracts:    contracts,
	}
}

func (b *backupModel) backupEnabled() bool {
	return b.mergedConfig.Spec.Backup != nil &&
		b.mergedConfig.Spec.Backup.Enabled != nil &&
		*b.mergedConfig.Spec.Backup.Enabled
}

// backupProvider describes one active backup provider and the ScheduledBackup it drives.
// A cluster may configure volume snapshots, barman object store, or both — each gets its own
// ScheduledBackup so the two never collide on a single object.
type backupProvider struct {
	// kind distinguishes the provider for status reporting.
	kind backupProviderKind
	// sbName is the deterministic ScheduledBackup name for this provider.
	sbName string
	// method/pluginName are the engine-agnostic fields that select this provider;
	// the backend translates them into the concrete CNPG ScheduledBackup.
	method     backuptypes.BackupMethod
	pluginName string
}

type backupProviderKind int

const (
	providerVolumeSnapshot backupProviderKind = iota
	providerObjectStore
)

// activeBackupProviders returns the providers configured for an enabled backup, in a stable
// order. Empty when backup is disabled or no provider is configured.
func (b *backupModel) activeBackupProviders() []backupProvider {
	if !b.backupEnabled() || b.mergedConfig.CNPG == nil || b.mergedConfig.CNPG.Backup == nil {
		return nil
	}
	var providers []backupProvider
	if b.mergedConfig.CNPG.Backup.VolumeSnapshot != nil {
		providers = append(providers, backupProvider{
			kind:   providerVolumeSnapshot,
			sbName: scheduledBackupName(b.cluster.Name),
			method: backuptypes.BackupMethodVolumeSnapshot,
		})
	}
	if b.mergedConfig.CNPG.Backup.BarmanObjectStore != nil {
		providers = append(providers, backupProvider{
			kind:       providerObjectStore,
			sbName:     objectStoreBackupName(b.cluster.Name),
			method:     backuptypes.BackupMethodPlugin,
			pluginName: barmanCloudPluginName,
		})
	}
	return providers
}

// allScheduledBackupNames lists every ScheduledBackup name the model may own, so the reconcile
// can garbage-collect the ones whose provider is no longer configured.
func (b *backupModel) allScheduledBackupNames() []string {
	return []string{
		scheduledBackupName(b.cluster.Name),
		objectStoreBackupName(b.cluster.Name),
	}
}

// backupConfigured reports whether the class defines a backup provider
// (volume snapshot or barman object store) for the enabled backup.
func (b *backupModel) backupConfigured() bool {
	return b.mergedConfig.CNPG != nil &&
		b.mergedConfig.CNPG.Backup != nil &&
		(b.mergedConfig.CNPG.Backup.VolumeSnapshot != nil ||
			b.mergedConfig.CNPG.Backup.BarmanObjectStore != nil)
}

func (b *backupModel) Name() string            { return pgcConstants.ComponentBackup }
func (b *backupModel) Requires() []contractKey { return []contractKey{contractCNPGCluster} }
func (b *backupModel) Provides() []contractKey { return nil }

func (b *backupModel) CheckContracts() error {
	if !checkContractsFromRequirements(b.Requires(), b.contracts) {
		return errContractsNotReady
	}
	return nil
}

// barmanObjectStoreCfg returns the BarmanObjectStore config from MergedConfig, or nil if not set.
// Used by both backupModel and objectStoreModel to avoid duplicating the nil-guard chain.
func barmanObjectStoreCfg(cfg *MergedConfig) *enterprisev4.CNPGBarmanObjectStoreConfig {
	if cfg == nil || cfg.CNPG == nil || cfg.CNPG.Backup == nil {
		return nil
	}
	return cfg.CNPG.Backup.BarmanObjectStore
}

func backupIsEnabled(cfg *MergedConfig) bool {
	return cfg != nil &&
		cfg.Spec != nil &&
		cfg.Spec.Backup != nil &&
		cfg.Spec.Backup.Enabled != nil &&
		*cfg.Spec.Backup.Enabled
}

func activeBarmanObjectStoreCfg(cfg *MergedConfig) *enterprisev4.CNPGBarmanObjectStoreConfig {
	if !backupIsEnabled(cfg) {
		return nil
	}
	return barmanObjectStoreCfg(cfg)
}

func (b *backupModel) Reconcile(ctx context.Context) error {
	if !b.backupEnabled() {
		if err := b.deleteScheduledBackups(ctx, b.allScheduledBackupNames()); err != nil {
			return newReconcileFailure(reasonScheduledBackupFailed, err)
		}
		return nil
	}

	if !b.backupConfigured() {
		return newReconcileFailure(reasonBackupProviderMissing, fmt.Errorf("backup enabled without cnpg.backup.volumeSnapshot or cnpg.backup.barmanObjectStore configuration"))
	}

	// Reconcile the ScheduledBackup for each configured provider, then garbage-collect any
	// ScheduledBackup whose provider is no longer configured (e.g. a provider was removed, or a
	// legacy barman-only cluster used the bare volume-snapshot name before suffixing).
	desired := make(map[string]struct{})
	for _, p := range b.activeBackupProviders() {
		desired[p.sbName] = struct{}{}
		if err := b.backend.EnsureScheduled(ctx, b.cluster, b.scheduleSpec(p)); err != nil {
			return newReconcileFailure(reasonScheduledBackupFailed, err)
		}
	}
	var stale []string
	for _, name := range b.allScheduledBackupNames() {
		if _, keep := desired[name]; !keep {
			stale = append(stale, name)
		}
	}
	if err := b.deleteScheduledBackups(ctx, stale); err != nil {
		return newReconcileFailure(reasonScheduledBackupFailed, err)
	}
	return nil
}

// scheduleSpec builds the backend-agnostic ScheduleSpec for a provider from the merged config.
func (b *backupModel) scheduleSpec(provider backupProvider) backuptypes.ScheduleSpec {
	return backuptypes.ScheduleSpec{
		Name:            provider.sbName,
		Namespace:       b.cluster.Namespace,
		CNPGClusterName: b.contracts.CNPGCluster.Name,
		// Schedule has a kubebuilder default but may be nil when constructed programmatically.
		Schedule: ptr.Deref(b.mergedConfig.Spec.Backup.Schedule, ""),
		// Target has a kubebuilder default but may be nil when constructed programmatically.
		Target:     ptr.Deref(b.mergedConfig.CNPG.Backup.Target, "prefer-standby"),
		Method:     provider.method,
		PluginName: provider.pluginName,
	}
}

func (b *backupModel) Observe(ctx context.Context, reconcileErr error) (componentHealth, error) {
	before := b.cluster.Status.DeepCopy()
	health, err := b.computeHealth(ctx, reconcileErr)
	statusErr := writeComponentStatus(b.updateStatus, before, health)
	return health, errors.Join(err, statusErr)
}

func (b *backupModel) computeHealth(ctx context.Context, reconcileErr error) (componentHealth, error) {
	oldConditions := append([]metav1.Condition(nil), b.cluster.Status.Conditions...)

	if h, err, ok := classifyReconcileErr(reconcileErr, backupReady, b.events, b.cluster, EventBackupReconcileFailed, "scheduled backup"); ok {
		return h, err
	}

	if !b.backupEnabled() {
		b.cluster.Status.BackupStatus = nil
		return newReadyHealth(backupReady, reasonBackupDisabled, msgBackupDisabled), nil
	}

	// Build status per configured provider. Each provider has its own ScheduledBackup; report
	// Pending if any expected one has not appeared yet, Failed on a get error.
	status := &enterprisev4.BackupStatus{}
	for _, p := range b.activeBackupProviders() {
		schedule, err := b.backend.GetSchedule(ctx, p.sbName, b.cluster.Namespace)
		if err != nil {
			return newFailedHealth(backupReady, reasonScheduledBackupFailed, fmt.Sprintf("Failed to get scheduled backup: %v", err)), err
		}
		if !schedule.Exists {
			return newPendingHealth(backupReady, reasonScheduledBackupCreated, "Waiting for scheduled backup to appear"), nil
		}
		switch p.kind {
		case providerObjectStore:
			status.ObjectStore = &enterprisev4.ObjectStoreBackupStatus{
				Enabled:          true,
				LastScheduleTime: schedule.LastScheduleTime,
				NextScheduleTime: schedule.NextScheduleTime,
			}
		case providerVolumeSnapshot:
			status.VolumeSnapshot = &enterprisev4.VolumeSnapshotBackupStatus{
				Enabled:          true,
				LastScheduleTime: schedule.LastScheduleTime,
				NextScheduleTime: schedule.NextScheduleTime,
			}
		}
	}
	b.cluster.Status.BackupStatus = status

	b.events.emitBackupReadyTransition(b.cluster, oldConditions)
	return newReadyHealth(backupReady, reasonBackupConfigured, msgScheduledBackupReady), nil
}

// deleteScheduledBackups removes each named ScheduledBackup if present, treating absence as a
// no-op. Used both to tear down all backups when disabled and to GC a provider's ScheduledBackup
// when that provider is no longer configured. The backend only deletes objects this
// PostgresCluster controls.
func (b *backupModel) deleteScheduledBackups(ctx context.Context, names []string) error {
	for _, name := range names {
		if err := b.backend.DeleteScheduled(ctx, b.cluster, name, b.cluster.Namespace); err != nil {
			return err
		}
	}
	return nil
}

func scheduledBackupName(clusterName string) string {
	return clusterName + defaultBackupSuffix
}

func objectStoreBackupName(clusterName string) string {
	return clusterName + objectStoreBackupSuffix
}

func toSixFieldCron(fiveField string) string {
	return "0 " + fiveField
}
