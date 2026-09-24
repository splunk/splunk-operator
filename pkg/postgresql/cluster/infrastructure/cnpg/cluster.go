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
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

const PostgresImageNameFormat = "ghcr.io/cloudnative-pg/postgresql:%s"

func PostgresImageName(version string) string {
	return fmt.Sprintf(PostgresImageNameFormat, version)
}

func ClusterReady(cluster *cnpgv1.Cluster) bool {
	return cluster != nil &&
		cluster.Status.Phase == cnpgv1.PhaseHealthy &&
		cluster.Status.Instances > 0 &&
		cluster.Status.ReadyInstances == cluster.Status.Instances &&
		cluster.Status.CurrentPrimary != ""
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
