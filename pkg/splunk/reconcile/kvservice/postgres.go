// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kvservice

import (
	"context"
	"fmt"
	"slices"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// ensurePostgresReady consumes an existing PostgresDatabase without changing it.
// Logical database readiness and published connection references are authoritative;
// KVService does not inspect or watch the underlying PostgresCluster.
func ensurePostgresReady(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService) (*k8sops.KVServicePostgresDependencies, bool, string, error) {
	spec := cr.Spec.Postgres
	if spec == nil {
		return nil, true, "", nil
	}
	if !config.DefaultMutableFeatureGate.Enabled(config.PostgresController) {
		message := fmt.Sprintf("spec.postgres requires the %s feature gate", config.PostgresController)
		return nil, false, "", splcommon.NewTerminalError(splcommon.EventReasonValidateSpecFailed, message, nil)
	}
	if spec.DatabaseRef != nil && spec.DatabaseRef.Name == "" {
		return nil, false, "", splcommon.NewTerminalError(splcommon.EventReasonValidateSpecFailed, "spec.postgres.databaseRef.name must not be empty when databaseRef is specified", nil)
	}
	databaseName := k8sops.KVServicePostgresLogicalDatabaseName(cr)
	databaseKey := types.NamespacedName{Namespace: cr.Namespace, Name: k8sops.KVServicePostgresDatabaseResourceName(cr)}
	database := &platformApi.PostgresDatabase{}
	if err := c.Get(ctx, databaseKey, database); err != nil {
		if apierrors.IsNotFound(err) {
			// A dependency may be created after its consumer. Report Pending rather
			// than an API failure; database events and the normal requeue retry it.
			cr.Status.Phase = enterpriseApi.PhasePending
			return nil, false, fmt.Sprintf("waiting for PostgresDatabase %q", databaseKey.Name), nil
		}
		return nil, false, "", fmt.Errorf("get PostgresDatabase %s for KVService: %w", databaseKey, err)
	}
	if database.DeletionTimestamp != nil {
		cr.Status.Phase = enterpriseApi.PhasePending
		message := fmt.Sprintf("PostgresDatabase %q is being deleted; KVService cannot use it until it is recreated and ready", database.Name)
		return nil, false, message, nil
	}
	if slices.IndexFunc(database.Spec.Databases, func(definition platformApi.DatabaseDefinition) bool { return definition.Name == databaseName }) < 0 {
		message := fmt.Sprintf("referenced PostgresDatabase %q does not define logical database %q; configure it before using it with KVService", database.Name, databaseName)
		return nil, false, "", splcommon.NewTerminalError(splcommon.EventReasonValidateSpecFailed, message, nil)
	}
	if database.Status.ObservedGeneration == nil || *database.Status.ObservedGeneration != database.Generation {
		cr.Status.Phase = enterpriseApi.PhasePending
		message := fmt.Sprintf("waiting for PostgresDatabase %q to observe generation %d", database.Name, database.Generation)
		return nil, false, message, nil
	}
	dependencies, ready, err := k8sops.ResolveKVServicePostgresDependencies(ctx, c, cr, database, databaseName)
	if err != nil {
		return nil, false, "", fmt.Errorf("resolve connection dependencies for PostgresDatabase %s database %q: %w", databaseKey, databaseName, err)
	}
	if ready {
		return dependencies, true, "", nil
	}
	if database.Status.Phase != nil && *database.Status.Phase == "Failed" {
		cr.Status.Phase = enterpriseApi.PhaseError
		return nil, false, fmt.Sprintf("PostgresDatabase %q failed while provisioning database %q", database.Name, databaseName), nil
	}
	cr.Status.Phase = enterpriseApi.PhasePending
	return nil, false, fmt.Sprintf("waiting for logical database %q in PostgresDatabase %q to become ready and publish connection references", databaseName, database.Name), nil
}
