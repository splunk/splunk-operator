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

package cnpg

import (
	"context"
	"fmt"
	"slices"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const PostgresImageNameFormat = "ghcr.io/cloudnative-pg/postgresql:%s"

func PostgresImageName(version string) string {
	return fmt.Sprintf(PostgresImageNameFormat, version)
}

// GetCnpgCluster reads the provider Cluster at key.
func GetCnpgCluster(ctx context.Context, c client.Client, key types.NamespacedName) (cnpgv1.Cluster, error) {
	cluster := cnpgv1.Cluster{}
	if err := c.Get(ctx, key, &cluster); err != nil {
		return cnpgv1.Cluster{}, err
	}
	return cluster, nil
}

func ClusterReady(cluster *cnpgv1.Cluster) bool {
	return cluster != nil &&
		cluster.Status.Phase == cnpgv1.PhaseHealthy &&
		cluster.Status.Instances > 0 &&
		cluster.Status.ReadyInstances == cluster.Status.Instances &&
		cluster.Status.CurrentPrimary != ""
}

// PrimaryReady reports whether CNPG has a healthy primary that can serve the
// upgraded cluster. Replica recovery remains part of normal cluster readiness.
func PrimaryReady(cluster *cnpgv1.Cluster) bool {
	if cluster == nil || cluster.Status.Phase != cnpgv1.PhaseHealthy || cluster.Status.CurrentPrimary == "" {
		return false
	}
	return slices.Contains(cluster.Status.InstancesStatus[cnpgv1.PodHealthy], cluster.Status.CurrentPrimary)
}

// BackupTargetReadiness verifies CNPG's primary fallback for a prefer-standby
// backup. It intentionally does not require every replica to be healthy.
func BackupTargetReadiness(cluster *cnpgv1.Cluster) error {
	if cluster == nil {
		return fmt.Errorf("CNPG cluster is missing")
	}
	if cluster.Status.TargetPrimary == "" {
		return fmt.Errorf("target primary is not published")
	}

	healthy := cluster.Status.InstancesStatus[cnpgv1.PodHealthy]
	if !slices.Contains(healthy, cluster.Status.TargetPrimary) {
		return fmt.Errorf("target primary %q is not in the published healthy instances %v",
			cluster.Status.TargetPrimary, healthy)
	}
	return nil
}

func ClusterBlockingError(cluster *cnpgv1.Cluster) error {
	if cluster == nil {
		return fmt.Errorf("cnpg cluster is missing")
	}

	switch cluster.Status.Phase {
	case cnpgv1.PhaseWaitingForUser:
		return fmt.Errorf("cnpg cluster requires user action: %s", cluster.Status.PhaseReason)
	case cnpgv1.PhaseUnrecoverable:
		return fmt.Errorf("cnpg cluster is unrecoverable: %s", cluster.Status.PhaseReason)
	case cnpgv1.PhaseCannotCreateClusterObjects:
		return fmt.Errorf("cnpg cluster cannot create required objects: %s", cluster.Status.PhaseReason)
	case cnpgv1.PhaseUnknownPlugin, cnpgv1.PhaseFailurePlugin:
		return fmt.Errorf("cnpg cluster plugin failure: %s", cluster.Status.PhaseReason)
	case cnpgv1.PhaseImageCatalogError, cnpgv1.PhaseArchitectureBinaryMissing:
		return fmt.Errorf("cnpg cluster image error: %s", cluster.Status.PhaseReason)
	default:
		return nil
	}
}
