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

// Package cnpg provides infrastructure adapters that implement core ports
// by talking to CNPG custom resources.
package cnpg

import (
	"context"
	"fmt"
	"sort"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	backuptypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/backup"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// backupBackend implements core.BackupBackend against CNPG Backup and
// ScheduledBackup resources.
type backupBackend struct {
	client client.Client
	scheme *runtime.Scheme
}

// NewBackupBackend returns a core.BackupBackend backed by CNPG resources.
func NewBackupBackend(c client.Client, scheme *runtime.Scheme) *backupBackend {
	return &backupBackend{client: c, scheme: scheme}
}

func (a *backupBackend) EnsureScheduled(ctx context.Context, owner client.Object, spec backuptypes.ScheduleSpec) (bool, error) {
	cnpgMethod, err := toCNPGBackupMethod(spec.Method)
	if err != nil {
		return false, err
	}

	var pluginCfg *cnpgv1.BackupPluginConfiguration
	if spec.Method == backuptypes.BackupMethodPlugin && spec.PluginName != "" {
		pluginCfg = &cnpgv1.BackupPluginConfiguration{Name: spec.PluginName}
	}

	desired := &cnpgv1.ScheduledBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: spec.Namespace,
		},
		Spec: cnpgv1.ScheduledBackupSpec{
			Schedule:             toSixFieldCron(spec.Schedule),
			Cluster:              cnpgv1.LocalObjectReference{Name: spec.CNPGClusterName},
			Method:               cnpgMethod,
			Target:               cnpgv1.BackupTarget(spec.Target),
			BackupOwnerReference: "cluster", // keeps CNPG Backup objects with the CNPG cluster owner, not with us
			PluginConfiguration:  pluginCfg,
		},
	}
	if err := ctrl.SetControllerReference(owner, desired, a.scheme); err != nil {
		return false, fmt.Errorf("setting controller reference on ScheduledBackup: %w", err)
	}

	existing := &cnpgv1.ScheduledBackup{}
	err = a.client.Get(ctx, types.NamespacedName{Name: spec.Name, Namespace: spec.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		createErr := a.client.Create(ctx, desired)
		if createErr == nil {
			return true, nil
		}
		// Another reconcile raced us to create — fetch and fall through to the update path.
		if !apierrors.IsAlreadyExists(createErr) {
			return false, fmt.Errorf("creating ScheduledBackup: %w", createErr)
		}
		if fetchErr := a.client.Get(ctx, types.NamespacedName{Name: spec.Name, Namespace: spec.Namespace}, existing); fetchErr != nil {
			return false, fmt.Errorf("re-fetching ScheduledBackup after AlreadyExists: %w", fetchErr)
		}
	} else if err != nil {
		return false, fmt.Errorf("getting ScheduledBackup: %w", err)
	}

	// Guard against a foreign (not controlled by this owner) object sharing the name.
	if controller := metav1.GetControllerOf(existing); controller != nil && controller.UID != owner.GetUID() {
		return false, fmt.Errorf("ScheduledBackup %s/%s already exists and is not controlled by this owner", spec.Namespace, spec.Name)
	}

	ownersBefore := existing.DeepCopy().OwnerReferences
	if err := ctrl.SetControllerReference(owner, existing, a.scheme); err != nil {
		return false, fmt.Errorf("repairing controller reference on ScheduledBackup: %w", err)
	}
	ownerChanged := !equality.Semantic.DeepEqual(ownersBefore, existing.OwnerReferences)
	specChanged := !equality.Semantic.DeepEqual(existing.Spec, desired.Spec)

	if !specChanged && !ownerChanged {
		return false, nil
	}

	existing.Spec = desired.Spec
	if err := a.client.Update(ctx, existing); err != nil {
		return false, fmt.Errorf("updating ScheduledBackup: %w", err)
	}
	return false, nil
}

func (a *backupBackend) DeleteScheduled(ctx context.Context, owner client.Object, name, namespace string) (bool, error) {
	sb := &cnpgv1.ScheduledBackup{}
	err := a.client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, sb)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("getting ScheduledBackup for deletion: %w", err)
	}
	// Only delete a ScheduledBackup this owner controls. A user- or
	// other-controller-owned object sharing the deterministic name must not be
	// deleted (mirrors the ObjectStore delete guard).
	if controller := metav1.GetControllerOf(sb); controller == nil || controller.UID != owner.GetUID() {
		return false, nil
	}
	if err := a.client.Delete(ctx, sb); err != nil && !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("deleting ScheduledBackup: %w", err)
	}
	return true, nil
}

func (a *backupBackend) GetSchedule(ctx context.Context, name, namespace string) (backuptypes.ScheduleResult, error) {
	sb := &cnpgv1.ScheduledBackup{}
	err := a.client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, sb)
	if apierrors.IsNotFound(err) {
		return backuptypes.ScheduleResult{Exists: false}, nil
	}
	if err != nil {
		return backuptypes.ScheduleResult{}, err
	}
	return backuptypes.ScheduleResult{
		Exists:           true,
		LastScheduleTime: sb.Status.LastScheduleTime,
		NextScheduleTime: sb.Status.NextScheduleTime,
	}, nil
}

func (a *backupBackend) BackupNow(ctx context.Context, owner client.Object, req backuptypes.BackupRequest) (bool, error) {
	cnpgMethod, err := toCNPGBackupMethod(req.Method)
	if err != nil {
		return false, err
	}

	// CNPG Backup spec is immutable, so the only safe idempotent operation is
	// create-if-absent. A Backup already present under this name is left as-is
	// only when it is controlled by the same owner — a foreign object sharing the
	// deterministic name must surface as an error rather than silently masquerade
	// as our backup.
	existing := &cnpgv1.Backup{}
	err = a.client.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, existing)
	if err == nil {
		if controller := metav1.GetControllerOf(existing); controller == nil || controller.UID != owner.GetUID() {
			return false, fmt.Errorf("Backup %s/%s already exists and is not controlled by this owner", req.Namespace, req.Name)
		}
		return false, nil
	}
	if !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("getting Backup: %w", err)
	}

	var pluginCfg *cnpgv1.BackupPluginConfiguration
	if req.Method == backuptypes.BackupMethodPlugin && req.PluginName != "" {
		pluginCfg = &cnpgv1.BackupPluginConfiguration{Name: req.PluginName}
	}

	desired := &cnpgv1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: req.Namespace,
		},
		Spec: cnpgv1.BackupSpec{
			Cluster:             cnpgv1.LocalObjectReference{Name: req.CNPGClusterName},
			Method:              cnpgMethod,
			Target:              cnpgv1.BackupTarget(req.Target),
			PluginConfiguration: pluginCfg,
		},
	}
	if err := ctrl.SetControllerReference(owner, desired, a.scheme); err != nil {
		return false, fmt.Errorf("setting controller reference on Backup: %w", err)
	}
	createErr := a.client.Create(ctx, desired)
	if createErr == nil {
		return true, nil
	}
	// Another reconcile raced us — re-fetch and verify ownership.
	if !apierrors.IsAlreadyExists(createErr) {
		return false, fmt.Errorf("creating Backup: %w", createErr)
	}
	if fetchErr := a.client.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, existing); fetchErr != nil {
		return false, fmt.Errorf("re-fetching Backup after AlreadyExists: %w", fetchErr)
	}
	if controller := metav1.GetControllerOf(existing); controller == nil || controller.UID != owner.GetUID() {
		return false, fmt.Errorf("Backup %s/%s already exists and is not controlled by this owner", req.Namespace, req.Name)
	}
	return false, nil
}

func (a *backupBackend) GetBackup(ctx context.Context, owner client.Object, name, namespace string) (backuptypes.BackupResult, bool, error) {
	backup := &cnpgv1.Backup{}
	err := a.client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, backup)
	if apierrors.IsNotFound(err) {
		return backuptypes.BackupResult{}, false, nil
	}
	if err != nil {
		return backuptypes.BackupResult{}, false, fmt.Errorf("getting Backup: %w", err)
	}
	// Treat an object not controlled by this owner as absent — the caller's
	// name-derivation may have collided with a foreign backup.
	if controller := metav1.GetControllerOf(backup); controller == nil || controller.UID != owner.GetUID() {
		return backuptypes.BackupResult{}, false, nil
	}
	return toBackupResult(backup), true, nil
}

func (a *backupBackend) ListBackups(ctx context.Context, owner client.Object, cnpgClusterName, namespace string) ([]backuptypes.BackupResult, error) {
	list := &cnpgv1.BackupList{}
	if err := a.client.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing Backups: %w", err)
	}
	results := make([]backuptypes.BackupResult, 0, len(list.Items))
	for i := range list.Items {
		backup := &list.Items[i]
		// Filter by the targeted CNPG cluster — the list API has no field selector
		// for spec.cluster.name so we filter in-process.
		if backup.Spec.Cluster.Name != cnpgClusterName {
			continue
		}
		// Only include backups this owner controls — foreign objects sharing the
		// namespace and cluster name must not appear in the owner's backup list.
		if controller := metav1.GetControllerOf(backup); controller == nil || controller.UID != owner.GetUID() {
			continue
		}
		results = append(results, toBackupResult(backup))
	}
	// Most recent first; name as secondary tiebreaker for deterministic order when
	// StartedAt is equal (including two nil values for not-yet-started backups).
	sort.SliceStable(results, func(i, j int) bool {
		si, sj := backupStartUnix(results[i]), backupStartUnix(results[j])
		if si != sj {
			return si > sj
		}
		return results[i].Name < results[j].Name
	})
	return results, nil
}

// backupStartUnix returns the backup start time in epoch seconds, or 0 when a
// backup has not started yet (so unstarted backups sort after started ones).
func backupStartUnix(r backuptypes.BackupResult) int64 {
	if r.StartedAt == nil {
		return 0
	}
	return r.StartedAt.Unix()
}

// toBackupResult translates a CNPG Backup object into the engine-agnostic
// observed-state DTO. An unrecognised method is left as backupMethodInvalid
// rather than propagating an error — callers observe phase/Done/Failed, not method.
func toBackupResult(backup *cnpgv1.Backup) backuptypes.BackupResult {
	method, _ := fromCNPGBackupMethod(backup.Spec.Method)
	res := backuptypes.BackupResult{
		Name:            backup.Name,
		CNPGClusterName: backup.Spec.Cluster.Name,
		Method:          method,
		Phase:           string(backup.Status.Phase),
		Done:            backup.Status.Phase == cnpgv1.BackupPhaseCompleted,
		Failed:          backup.Status.Phase == cnpgv1.BackupPhaseFailed,
		Error:           backup.Status.Error,
		BackupID:        backup.Status.BackupID,
		StartedAt:       backup.Status.StartedAt,
		StoppedAt:       backup.Status.StoppedAt,
	}
	for _, e := range backup.Status.BackupSnapshotStatus.Elements {
		res.SnapshotNames = append(res.SnapshotNames, e.Name)
	}
	return res
}

func toCNPGBackupMethod(m backuptypes.BackupMethod) (cnpgv1.BackupMethod, error) {
	switch m {
	case backuptypes.BackupMethodVolumeSnapshot:
		return cnpgv1.BackupMethodVolumeSnapshot, nil
	case backuptypes.BackupMethodPlugin:
		return cnpgv1.BackupMethodPlugin, nil
	default:
		return "", fmt.Errorf("unsupported backup method %d", m)
	}
}

func fromCNPGBackupMethod(m cnpgv1.BackupMethod) (backuptypes.BackupMethod, error) {
	switch m {
	case cnpgv1.BackupMethodVolumeSnapshot:
		return backuptypes.BackupMethodVolumeSnapshot, nil
	case cnpgv1.BackupMethodPlugin:
		return backuptypes.BackupMethodPlugin, nil
	case cnpgv1.BackupMethodBarmanObjectStore:
		// CNPG uses barmanObjectStore for legacy scheduled backups; we surface it as
		// BackupMethodPlugin since both represent object-store-based backups.
		return backuptypes.BackupMethodPlugin, nil
	default:
		return 0, fmt.Errorf("unsupported CNPG backup method %q", m)
	}
}

// toSixFieldCron converts a standard 5-field cron expression to the 6-field
// form CNPG expects (seconds field prepended as "0").
func toSixFieldCron(fiveField string) string {
	return "0 " + fiveField
}
