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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	cnpginfra "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/cnpg"
	corev1 "k8s.io/api/core/v1"
)

// Barman 0.15 injects this container either as a native or conventional sidecar.
const barmanCloudSidecar = "plugin-barman-cloud"

// targetBackupReady gates both scheduled and recovery backups on the actual
// rollout, rather than an old Healthy status alongside a new plugin spec.
func (c *BlueGreenCompletion) targetBackupReady(ctx context.Context, green *cnpgv1.Cluster) error {
	pods, err := c.targetBackupInstances(ctx, green)
	if err != nil {
		return err
	}
	for _, pod := range pods {
		ready, err := c.sql.TargetArchivingReady(ctx, green, pod.Status.PodIP)
		if err != nil {
			return fmt.Errorf("checking target archive settings on instance %q: %w", pod.Name, err)
		}
		if !ready {
			return fmt.Errorf("%w: waiting for effective target archive settings on instance %q", mvutypes.ErrUpgradeFlowPending, pod.Name)
		}
	}
	return nil
}

// targetBackupInstances also gates activation, when the sidecars must be ready
// but their WAL-archiver role is deliberately still disabled.
func (c *BlueGreenCompletion) targetBackupInstances(ctx context.Context, green *cnpgv1.Cluster) ([]corev1.Pod, error) {
	if !cnpginfra.ClusterReady(green) || green.Status.CurrentPrimary != green.Status.TargetPrimary || green.Status.TargetPrimary == "" {
		return nil, fmt.Errorf("%w: waiting for a stable, healthy target primary before enabling backups", mvutypes.ErrUpgradeFlowPending)
	}
	names, pods, err := healthyInstancePods(ctx, c.switchover.reader, green)
	if err != nil {
		return nil, err
	}
	if len(names) != green.Spec.Instances || !slices.Contains(names, green.Status.CurrentPrimary) {
		return nil, fmt.Errorf("%w: waiting for every target instance to finish the backup plugin rollout", mvutypes.ErrUpgradeFlowPending)
	}
	instances := make([]corev1.Pod, 0, len(names))
	for _, name := range names {
		pod := pods[name]
		if !barmanSidecarReady(&pod) {
			return nil, fmt.Errorf("%w: waiting for the target backup plugin on instance %q", mvutypes.ErrUpgradeFlowPending, name)
		}
		instances = append(instances, pod)
	}
	return instances, nil
}

func barmanSidecarReady(pod *corev1.Pod) bool {
	for _, container := range pod.Spec.InitContainers {
		if container.Name == barmanCloudSidecar && container.RestartPolicy != nil && *container.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			return runningBackupSidecar(pod.Status.InitContainerStatuses)
		}
	}
	for _, container := range pod.Spec.Containers {
		if container.Name == barmanCloudSidecar {
			return runningBackupSidecar(pod.Status.ContainerStatuses)
		}
	}
	return false
}

func runningBackupSidecar(statuses []corev1.ContainerStatus) bool {
	for _, status := range statuses {
		if status.Name == barmanCloudSidecar {
			return status.Ready && status.State.Running != nil
		}
	}
	return false
}
