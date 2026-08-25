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
	"errors"
	"fmt"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/logging"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	cnpginfra "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/cnpg"
	backuptypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/backup"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// onDemandBackupClient is the narrow slice of BackupBackend this adapter needs.
// BackupNow's created bool is intentionally unused: the backup name is
// deterministic and GetBackup is called immediately after to observe the result.
type onDemandBackupClient interface {
	BackupNow(ctx context.Context, owner client.Object, req backuptypes.BackupRequest) (created bool, err error)
	GetBackup(ctx context.Context, owner client.Object, name, namespace string) (backuptypes.BackupResult, bool, error)
}

type RollbackCapabilityAdapter struct {
	client     client.Client
	key        types.NamespacedName
	backend    onDemandBackupClient
	method     backuptypes.BackupMethod
	pluginName string
}

func NewRollbackCapabilityAdapter(c client.Client, scheme *runtime.Scheme, key types.NamespacedName, method backuptypes.BackupMethod, pluginName string) *RollbackCapabilityAdapter {
	return &RollbackCapabilityAdapter{
		client:     c,
		key:        key,
		backend:    cnpginfra.NewBackupBackend(c, scheme),
		method:     method,
		pluginName: pluginName,
	}
}

func (r *RollbackCapabilityAdapter) CreateBackup(ctx context.Context, intent mvutypes.Intent, generateBackupName func(mvutypes.Intent) string) (*mvutypes.BackupInfo, error) {
	owner := &platformv1alpha1.PostgresCluster{}
	if err := r.client.Get(ctx, r.key, owner); err != nil {
		return nil, fmt.Errorf("fetching PostgresCluster for rollback backup: %w", err)
	}

	// Backup names are deterministic per gate and source→target pair, making every reconcile idempotent
	backupName := fmt.Sprintf("%s-%s", owner.Name, generateBackupName(intent))
	result, found, err := r.backend.GetBackup(ctx, owner, backupName, r.key.Namespace)
	if err != nil {
		return nil, fmt.Errorf("observing rollback backup: %w", err)
	}
	if !found {
		cluster, err := cnpginfra.GetCnpgCluster(ctx, r.client, r.key)
		if err != nil {
			return nil, errors.Join(
				mvutypes.ErrRollbackCapabilityNotReady,
				fmt.Errorf("fetching CNPG Cluster for rollback backup: %w", err),
			)
		}
		if readinessErr := cnpginfra.BackupTargetReadiness(&cluster); readinessErr != nil {
			logging.FromContext(ctx).InfoContext(ctx, "waiting for CNPG backup target",
				"cluster", cluster.Name,
				"reason", readinessErr.Error())
			return nil, fmt.Errorf(
				"%w: CNPG Cluster %s backup target is not ready: %v",
				mvutypes.ErrRollbackCapabilityNotReady,
				cluster.Name,
				readinessErr,
			)
		}
	}

	req := backuptypes.BackupRequest{
		Name:            backupName,
		Namespace:       r.key.Namespace,
		CNPGClusterName: r.key.Name,
		Target:          "prefer-standby",
		Method:          r.method,
		PluginName:      r.pluginName,
	}

	if !found {
		if _, err := r.backend.BackupNow(ctx, owner, req); err != nil {
			return nil, fmt.Errorf("triggering rollback backup: %w", err)
		}

		result, found, err = r.backend.GetBackup(ctx, owner, backupName, r.key.Namespace)
		if err != nil {
			return nil, fmt.Errorf("observing rollback backup: %w", err)
		}
	}
	if !found {
		return nil, mvutypes.ErrBackupStatusMissing
	}
	if result.Failed {
		return nil, fmt.Errorf("%w: rollback backup %s failed: %s", mvutypes.ErrUpgradeFlowFailed, backupName, result.Error)
	}
	if !result.Done {
		return nil, mvutypes.ErrBackupStatusMissing
	}

	logging.FromContext(ctx).InfoContext(ctx, "backup section complete",
		"backup-name", backupName)

	backupStatus := &platformv1alpha1.BackupStatus{}
	switch r.method {
	case backuptypes.BackupMethodVolumeSnapshot:
		backupStatus.VolumeSnapshot = &platformv1alpha1.VolumeSnapshotBackupStatus{Enabled: true}
	case backuptypes.BackupMethodPlugin:
		backupStatus.ObjectStore = &platformv1alpha1.ObjectStoreBackupStatus{Enabled: true}
	}
	return &mvutypes.BackupInfo{
		BackupStatus: backupStatus,
		BackupName:   backupName,
	}, nil
}
