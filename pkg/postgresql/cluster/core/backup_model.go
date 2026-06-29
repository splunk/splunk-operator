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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultBackupSuffix = "-backup"

// objectStoreBackupSuffix names the barman object-store ScheduledBackup distinctly from the
// volume-snapshot one, so both can coexist when a cluster configures both providers.
const objectStoreBackupSuffix = "-backup-objectstore"

type backupEmitter interface {
	eventEmitter
	emitBackupReadyTransition(obj client.Object, conditions []metav1.Condition)
}

type backupModel struct {
	client       client.Client
	scheme       *runtime.Scheme
	events       backupEmitter
	updateStatus healthStatusUpdater
	cluster      *enterprisev4.PostgresCluster
	mergedConfig *MergedConfig
	contracts    *reconcileContracts
}

func newBackupModel(c client.Client, scheme *runtime.Scheme, events backupEmitter, updateStatus healthStatusUpdater, cluster *enterprisev4.PostgresCluster, mergedConfig *MergedConfig, contracts *reconcileContracts) *backupModel {
	return &backupModel{
		client:       c,
		scheme:       scheme,
		events:       events,
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
	// method/pluginCfg are the CNPG ScheduledBackup fields that select this provider.
	method    cnpgv1.BackupMethod
	pluginCfg *cnpgv1.BackupPluginConfiguration
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
			method: cnpgv1.BackupMethodVolumeSnapshot,
		})
	}
	if b.mergedConfig.CNPG.Backup.BarmanObjectStore != nil {
		providers = append(providers, backupProvider{
			kind:      providerObjectStore,
			sbName:    objectStoreBackupName(b.cluster.Name),
			method:    cnpgv1.BackupMethodPlugin,
			pluginCfg: &cnpgv1.BackupPluginConfiguration{Name: barmanCloudPluginName},
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
		if err := b.createOrUpdateScheduledBackup(ctx, p); err != nil {
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
		sb := &cnpgv1.ScheduledBackup{}
		if err := b.client.Get(ctx, types.NamespacedName{Name: p.sbName, Namespace: b.cluster.Namespace}, sb); err != nil {
			if apierrors.IsNotFound(err) {
				return newPendingHealth(backupReady, reasonScheduledBackupCreated, "Waiting for scheduled backup to appear"), nil
			}
			return newFailedHealth(backupReady, reasonScheduledBackupFailed, fmt.Sprintf("Failed to get scheduled backup: %v", err)), err
		}
		switch p.kind {
		case providerObjectStore:
			status.ObjectStore = &enterprisev4.ObjectStoreBackupStatus{
				Enabled:          true,
				LastScheduleTime: sb.Status.LastScheduleTime,
				NextScheduleTime: sb.Status.NextScheduleTime,
			}
		case providerVolumeSnapshot:
			status.VolumeSnapshot = &enterprisev4.VolumeSnapshotBackupStatus{
				Enabled:          true,
				LastScheduleTime: sb.Status.LastScheduleTime,
				NextScheduleTime: sb.Status.NextScheduleTime,
			}
		}
	}
	b.cluster.Status.BackupStatus = status

	b.events.emitBackupReadyTransition(b.cluster, oldConditions)
	return newReadyHealth(backupReady, reasonBackupConfigured, msgScheduledBackupReady), nil
}

func (b *backupModel) createOrUpdateScheduledBackup(ctx context.Context, provider backupProvider) error {
	schedule := toSixFieldCron(*b.mergedConfig.Spec.Backup.Schedule)

	// Target has a kubebuilder default but may be nil when constructed programmatically.
	target := cnpgv1.BackupTarget(ptr.Deref(b.mergedConfig.CNPG.Backup.Target, "prefer-standby"))

	desired := &cnpgv1.ScheduledBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      provider.sbName,
			Namespace: b.cluster.Namespace,
		},
		Spec: cnpgv1.ScheduledBackupSpec{
			Schedule:             schedule,
			Cluster:              cnpgv1.LocalObjectReference{Name: b.contracts.CNPGCluster.Name},
			Method:               provider.method,
			Target:               target,
			BackupOwnerReference: "cluster",
			PluginConfiguration:  provider.pluginCfg,
		},
	}
	if err := ctrl.SetControllerReference(b.cluster, desired, b.scheme); err != nil {
		return fmt.Errorf("setting controller reference on ScheduledBackup: %w", err)
	}

	existing := &cnpgv1.ScheduledBackup{}
	err := b.client.Get(ctx, types.NamespacedName{Name: provider.sbName, Namespace: b.cluster.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		if createErr := b.client.Create(ctx, desired); createErr != nil {
			return fmt.Errorf("creating ScheduledBackup: %w", createErr)
		}
		b.events.emitNormal(b.cluster, EventScheduledBackupCreated, "Scheduled backup created")
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting ScheduledBackup: %w", err)
	}

	ownersBefore := existing.DeepCopy().OwnerReferences
	if err := ctrl.SetControllerReference(b.cluster, existing, b.scheme); err != nil {
		return fmt.Errorf("repairing controller reference on ScheduledBackup: %w", err)
	}
	ownerChanged := !equality.Semantic.DeepEqual(ownersBefore, existing.OwnerReferences)
	specChanged := !equality.Semantic.DeepEqual(existing.Spec, desired.Spec)

	if !specChanged && !ownerChanged {
		return nil
	}

	existing.Spec = desired.Spec
	if err := b.client.Update(ctx, existing); err != nil {
		return fmt.Errorf("updating ScheduledBackup: %w", err)
	}
	return nil
}

// deleteScheduledBackups removes each named ScheduledBackup if present, treating absence as a
// no-op. Used both to tear down all backups when disabled and to GC a provider's ScheduledBackup
// when that provider is no longer configured.
func (b *backupModel) deleteScheduledBackups(ctx context.Context, names []string) error {
	for _, name := range names {
		sb := &cnpgv1.ScheduledBackup{}
		err := b.client.Get(ctx, types.NamespacedName{Name: name, Namespace: b.cluster.Namespace}, sb)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("getting ScheduledBackup for deletion: %w", err)
		}
		// Only delete a ScheduledBackup this PostgresCluster controls. A user- or
		// other-controller-owned object sharing the deterministic name must not be
		// deleted by this controller (mirrors the ObjectStore delete guard).
		if controller := metav1.GetControllerOf(sb); controller == nil || controller.UID != b.cluster.UID {
			continue
		}
		if err := b.client.Delete(ctx, sb); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting ScheduledBackup: %w", err)
		}
		b.events.emitNormal(b.cluster, EventScheduledBackupDeleted, "Scheduled backup deleted")
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
