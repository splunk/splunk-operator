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
	client           client.Client
	scheme           *runtime.Scheme
	events           backupEmitter
	updateStatus     healthStatusUpdater
	cluster          *enterprisev4.PostgresCluster
	mergedConfig     *MergedConfig
	backupEnabled    bool
	backupConfigured bool

	health     componentHealth
	actuateErr error
}

func newBackupModel(c client.Client, scheme *runtime.Scheme, events backupEmitter, updateStatus healthStatusUpdater, cluster *enterprisev4.PostgresCluster, mergedConfig *MergedConfig, backupEnabled bool, backupConfigured bool) *backupModel {
	return &backupModel{
		client:           c,
		scheme:           scheme,
		events:           events,
		updateStatus:     updateStatus,
		cluster:          cluster,
		mergedConfig:     mergedConfig,
		backupEnabled:    backupEnabled,
		backupConfigured: backupConfigured,
	}
}

func (b *backupModel) Name() string { return pgcConstants.ComponentBackup }

func (b *backupModel) EvaluatePrerequisites(_ context.Context) prerequisiteDecision {
	return prerequisiteDecision{Allowed: true}
}

func (b *backupModel) Actuate(ctx context.Context) {
	b.actuateErr = nil
	if !b.backupEnabled {
		if err := b.deleteScheduledBackup(ctx); err != nil {
			b.health.State = pgcConstants.Failed
			b.health.Reason = reasonScheduledBackupFailed
			b.health.Message = fmt.Sprintf(msgFmtScheduledBackupFailed, err)
			b.health.Phase = failedClusterPhase
			b.actuateErr = err
			return
		}
		b.cluster.Status.BackupStatus = nil
		return
	}

	if !b.backupConfigured {
		b.health.State = pgcConstants.Failed
		b.health.Reason = reasonBackupVolumeSnapshotMissing
		b.health.Message = string(msgBackupVolumeSnapshotMissing)
		b.health.Phase = failedClusterPhase
		b.actuateErr = fmt.Errorf("backup enabled without cnpg.backup.volumeSnapshot configuration")
		return
	}

	if err := b.createOrUpdateScheduledBackup(ctx); err != nil {
		b.events.emitWarning(b.cluster, EventBackupReconcileFailed, fmt.Sprintf("Failed to reconcile scheduled backup: %v", err))
		b.health.State = pgcConstants.Failed
		b.health.Reason = reasonScheduledBackupFailed
		b.health.Message = fmt.Sprintf(msgFmtScheduledBackupFailed, err)
		b.health.Phase = failedClusterPhase
		b.actuateErr = err
		return
	}
}

func (b *backupModel) Converge(ctx context.Context) (health componentHealth, err error) {
	b.health.Condition = backupReady
	oldConditions := append([]metav1.Condition(nil), b.cluster.Status.Conditions...)
	defer func() {
		statusErr := writeComponentStatus(b.updateStatus, b.health)
		if statusErr != nil {
			if err != nil {
				err = errors.Join(err, statusErr)
			} else {
				err = statusErr
			}
		}
		health = b.health
	}()

	if b.actuateErr != nil {
		return b.health, b.actuateErr
	}

	if !b.backupEnabled {
		b.health.State = pgcConstants.Ready
		b.health.Reason = reasonBackupDisabled
		b.health.Message = msgBackupDisabled
		b.health.Phase = readyClusterPhase
		b.health.Result = ctrl.Result{}
		return b.health, nil
	}

	scheduledBackup := &cnpgv1.ScheduledBackup{}
	sbName := scheduledBackupName(b.cluster.Name)
	if err := b.client.Get(ctx, types.NamespacedName{Name: sbName, Namespace: b.cluster.Namespace}, scheduledBackup); err != nil {
		if apierrors.IsNotFound(err) {
			b.health.State = pgcConstants.Pending
			b.health.Reason = reasonScheduledBackupCreated
			b.health.Message = "Waiting for scheduled backup to appear"
			b.health.Phase = provisioningClusterPhase
			b.health.Result = ctrl.Result{RequeueAfter: retryDelay}
			return b.health, nil
		}
		b.health.State = pgcConstants.Failed
		b.health.Reason = reasonScheduledBackupFailed
		b.health.Message = fmt.Sprintf("Failed to get scheduled backup: %v", err)
		b.health.Phase = failedClusterPhase
		return b.health, err
	}

	oldBackupStatus := b.cluster.Status.BackupStatus
	b.cluster.Status.BackupStatus = &enterprisev4.BackupStatus{
		VolumeSnapshot: &enterprisev4.VolumeSnapshotBackupStatus{
			Enabled:          true,
			LastScheduleTime: scheduledBackup.Status.LastScheduleTime,
			NextScheduleTime: scheduledBackup.Status.NextScheduleTime,
		},
	}
	if !equality.Semantic.DeepEqual(oldBackupStatus, b.cluster.Status.BackupStatus) {
		if err := b.client.Status().Update(ctx, b.cluster); err != nil {
			b.health.State = pgcConstants.Failed
			b.health.Reason = reasonScheduledBackupFailed
			b.health.Message = fmt.Sprintf("Failed to update backup status: %v", err)
			b.health.Phase = failedClusterPhase
			return b.health, err
		}
	}

	b.events.emitBackupReadyTransition(b.cluster, oldConditions)
	b.health.State = pgcConstants.Ready
	b.health.Reason = reasonBackupConfigured
	b.health.Message = msgScheduledBackupReady
	b.health.Phase = readyClusterPhase
	b.health.Result = ctrl.Result{}
	return b.health, nil
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
			Cluster:              cnpgv1.LocalObjectReference{Name: b.cluster.Name},
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
