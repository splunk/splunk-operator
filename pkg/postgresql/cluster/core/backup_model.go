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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultBackupSuffix = "-backup"

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

func (b *backupModel) backupConfigured() bool {
	return b.mergedConfig.CNPG != nil &&
		b.mergedConfig.CNPG.Backup != nil &&
		b.mergedConfig.CNPG.Backup.VolumeSnapshot != nil
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

func (b *backupModel) Reconcile(ctx context.Context) error {

	if !b.backupEnabled() {
		if err := b.deleteScheduledBackup(ctx); err != nil {
			return newReconcileFailure(reasonScheduledBackupFailed, err)
		}
		return nil
	}

	if !b.backupConfigured() {
		return newReconcileFailure(reasonBackupVolumeSnapshotMissing, fmt.Errorf("backup enabled without cnpg.backup.volumeSnapshot configuration"))
	}

	if err := b.createOrUpdateScheduledBackup(ctx); err != nil {
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

	scheduledBackup := &cnpgv1.ScheduledBackup{}
	sbName := scheduledBackupName(b.cluster.Name)
	if err := b.client.Get(ctx, types.NamespacedName{Name: sbName, Namespace: b.cluster.Namespace}, scheduledBackup); err != nil {
		if apierrors.IsNotFound(err) {
			return newPendingHealth(backupReady, reasonScheduledBackupCreated, "Waiting for scheduled backup to appear"), nil
		}
		return newFailedHealth(backupReady, reasonScheduledBackupFailed, fmt.Sprintf("Failed to get scheduled backup: %v", err)), err
	}

	b.cluster.Status.BackupStatus = &enterprisev4.BackupStatus{
		VolumeSnapshot: &enterprisev4.VolumeSnapshotBackupStatus{
			Enabled:          true,
			LastScheduleTime: scheduledBackup.Status.LastScheduleTime,
			NextScheduleTime: scheduledBackup.Status.NextScheduleTime,
		},
	}

	b.events.emitBackupReadyTransition(b.cluster, oldConditions)
	return newReadyHealth(backupReady, reasonBackupConfigured, msgScheduledBackupReady), nil
}

func (b *backupModel) createOrUpdateScheduledBackup(ctx context.Context) error {
	sbName := scheduledBackupName(b.cluster.Name)
	schedule := toSixFieldCron(*b.mergedConfig.Spec.Backup.Schedule)

	target := cnpgv1.BackupTarget(*b.mergedConfig.CNPG.Backup.Target)

	desired := &cnpgv1.ScheduledBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sbName,
			Namespace: b.cluster.Namespace,
		},
		Spec: cnpgv1.ScheduledBackupSpec{
			Schedule:             schedule,
			Cluster:              cnpgv1.LocalObjectReference{Name: b.contracts.CNPGCluster.Name},
			Method:               cnpgv1.BackupMethodVolumeSnapshot,
			Target:               target,
			BackupOwnerReference: "cluster",
		},
	}
	if err := ctrl.SetControllerReference(b.cluster, desired, b.scheme); err != nil {
		return fmt.Errorf("setting controller reference on ScheduledBackup: %w", err)
	}

	existing := &cnpgv1.ScheduledBackup{}
	err := b.client.Get(ctx, types.NamespacedName{Name: sbName, Namespace: b.cluster.Namespace}, existing)
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

func (b *backupModel) deleteScheduledBackup(ctx context.Context) error {
	sb := &cnpgv1.ScheduledBackup{}
	sbName := scheduledBackupName(b.cluster.Name)
	err := b.client.Get(ctx, types.NamespacedName{Name: sbName, Namespace: b.cluster.Namespace}, sb)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting ScheduledBackup for deletion: %w", err)
	}
	if err := b.client.Delete(ctx, sb); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting ScheduledBackup: %w", err)
	}
	b.events.emitNormal(b.cluster, EventScheduledBackupDeleted, "Scheduled backup deleted")
	return nil
}

func scheduledBackupName(clusterName string) string {
	return clusterName + defaultBackupSuffix
}

func toSixFieldCron(fiveField string) string {
	return "0 " + fiveField
}
