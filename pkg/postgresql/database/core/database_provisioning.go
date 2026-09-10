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
	"fmt"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DatabaseProvisioner applies logical databases through a provider and
// observes the exact generations returned by that apply.
type DatabaseProvisioner interface {
	Inspect(ctx context.Context, target dbtypes.ProvisionerTarget, databases []dbtypes.DatabaseIdentity) (dbtypes.Observation, error)
	Apply(ctx context.Context, target dbtypes.ProvisionerTarget, desired []dbtypes.DesiredDatabase) (dbtypes.ApplyResult, error)
	Observe(ctx context.Context, target dbtypes.ProvisionerTarget, expected []dbtypes.ExpectedDatabase) (dbtypes.Observation, error)
}

type databaseProvisioningResult struct {
	adopted      []string
	notReady     []string
	reasons      map[string]string
	bootstrap    []platformv1alpha1.DatabaseDefinition
	databaseUIDs map[string]k8stypes.UID
}

func reconcileDatabaseProvisioning(
	ctx context.Context,
	provisioner DatabaseProvisioner,
	postgresDB *platformv1alpha1.PostgresDatabase,
	providerClusterName string,
) (databaseProvisioningResult, error) {
	if provisioner == nil {
		return databaseProvisioningResult{}, fmt.Errorf("database provisioner is not configured")
	}
	target := databaseProvisionerTarget(postgresDB, providerClusterName)
	current, err := provisioner.Inspect(ctx, target, databaseIdentities(postgresDB))
	if err != nil {
		return databaseProvisioningResult{}, fmt.Errorf("inspecting provider databases: %w", err)
	}
	bootstrapCompleted := databaseBootstrapCompletion(postgresDB, current)
	result, err := reconcileDesiredDatabaseState(
		ctx,
		provisioner,
		target,
		desiredDatabasesForProvisioning(postgresDB, bootstrapCompleted),
	)
	if err != nil {
		return databaseProvisioningResult{}, err
	}
	result.bootstrap = databasesRequiringPrivilegeBootstrap(postgresDB, bootstrapCompleted)
	return result, nil
}

func reconcileRequestedClosedState(
	ctx context.Context,
	provisioner DatabaseProvisioner,
	postgresDB *platformv1alpha1.PostgresDatabase,
	providerClusterName string,
) (bool, error) {
	if provisioner == nil {
		return false, fmt.Errorf("database provisioner is not configured")
	}
	desired := desiredClosedDatabases(postgresDB)
	if len(desired) == 0 {
		return true, nil
	}
	result, err := reconcileDesiredDatabaseState(
		ctx,
		provisioner,
		databaseProvisionerTarget(postgresDB, providerClusterName),
		desired,
	)
	if err != nil {
		return false, fmt.Errorf("applying requested closed database state: %w", err)
	}
	return len(result.notReady) == 0, nil
}

func databaseProvisionerTarget(
	postgresDB *platformv1alpha1.PostgresDatabase,
	providerClusterName string,
) dbtypes.ProvisionerTarget {
	return dbtypes.ProvisionerTarget{
		Namespace:            postgresDB.Namespace,
		PostgresDatabaseName: postgresDB.Name,
		PostgresDatabaseUID:  string(postgresDB.UID),
		ProviderClusterName:  providerClusterName,
	}
}

func reconcileDesiredDatabaseState(
	ctx context.Context,
	provisioner DatabaseProvisioner,
	target dbtypes.ProvisionerTarget,
	desired []dbtypes.DesiredDatabase,
) (databaseProvisioningResult, error) {
	applied, err := provisioner.Apply(ctx, target, desired)
	if err != nil {
		return databaseProvisioningResult{}, err
	}
	observed, err := provisioner.Observe(ctx, target, applied.Expected)
	if err != nil {
		return databaseProvisioningResult{}, err
	}

	result := databaseProvisioningResult{
		adopted:      applied.Adopted,
		reasons:      make(map[string]string),
		databaseUIDs: make(map[string]k8stypes.UID),
	}
	byName := make(map[string]dbtypes.ObservedDatabase, len(observed.Databases))
	for _, database := range observed.Databases {
		byName[database.Name] = database
	}
	for _, expected := range applied.Expected {
		database, found := byName[expected.Name]
		if found && database.Found {
			result.databaseUIDs[expected.Name] = k8stypes.UID(database.UID)
		}
		switch {
		case !found || !database.Found:
			result.notReady = append(result.notReady, expected.Name)
			result.reasons[expected.Name] = reasonCNPGDatabaseNotFound
		case database.ObservedGeneration != expected.Generation:
			result.notReady = append(result.notReady, expected.Name)
			result.reasons[expected.Name] = reasonCNPGDatabaseApplying
		case !database.Applied:
			result.notReady = append(result.notReady, expected.Name)
			if database.Message != "" {
				result.reasons[expected.Name] = database.Message
			} else {
				result.reasons[expected.Name] = reasonCNPGDatabaseApplying
			}
		}
	}
	return result, nil
}

func desiredDatabasesForProvisioning(
	postgresDB *platformv1alpha1.PostgresDatabase,
	bootstrapCompleted map[string]bool,
) []dbtypes.DesiredDatabase {
	desired := make([]dbtypes.DesiredDatabase, 0, len(postgresDB.Spec.Databases))
	for _, database := range postgresDB.Spec.Databases {
		allowConnections := cloneBool(database.AllowConnections)
		if allowConnections != nil && !*allowConnections && !bootstrapCompleted[database.Name] {
			// SOK must connect to a new database once to establish application
			// privileges. The requested false value is applied after that bootstrap.
			allowConnections = boolPointer(true)
		}
		desired = append(desired, desiredDatabase(postgresDB.Name, database, allowConnections))
	}
	return desired
}

func desiredClosedDatabases(postgresDB *platformv1alpha1.PostgresDatabase) []dbtypes.DesiredDatabase {
	desired := make([]dbtypes.DesiredDatabase, 0, len(postgresDB.Spec.Databases))
	for _, database := range postgresDB.Spec.Databases {
		if database.AllowConnections != nil && !*database.AllowConnections {
			desired = append(desired, desiredDatabase(postgresDB.Name, database, cloneBool(database.AllowConnections)))
		}
	}
	return desired
}

func desiredDatabase(
	postgresDatabaseName string,
	database platformv1alpha1.DatabaseDefinition,
	allowConnections *bool,
) dbtypes.DesiredDatabase {
	reclaim := dbtypes.ReclaimDelete
	if database.DeletionPolicy == deletionPolicyRetain {
		reclaim = dbtypes.ReclaimRetain
	}
	return dbtypes.DesiredDatabase{
		ResourceName: cnpgDatabaseName(postgresDatabaseName, database.Name),
		Name:         database.Name,
		Owner:        EffectiveRoleNames(database).Admin,
		Reclaim:      reclaim,
		Extensions:   append([]string(nil), database.Extensions...),
		Creation: dbtypes.DatabaseCreationOptions{
			Template:         database.Template,
			Encoding:         database.Encoding,
			Locale:           database.Locale,
			LocaleProvider:   database.LocaleProvider,
			LocaleCollate:    database.LocaleCollate,
			LocaleCType:      database.LocaleCType,
			ICULocale:        database.ICULocale,
			ICURules:         database.ICURules,
			BuiltinLocale:    database.BuiltinLocale,
			CollationVersion: database.CollationVersion,
		},
		Mutable: dbtypes.DatabaseMutableOptions{
			IsTemplate:       cloneBool(database.IsTemplate),
			AllowConnections: allowConnections,
			ConnectionLimit:  cloneInt32(database.ConnectionLimit),
			Tablespace:       database.Tablespace,
		},
	}
}

func databaseIdentities(postgresDB *platformv1alpha1.PostgresDatabase) []dbtypes.DatabaseIdentity {
	result := make([]dbtypes.DatabaseIdentity, 0, len(postgresDB.Spec.Databases))
	for _, database := range postgresDB.Spec.Databases {
		result = append(result, dbtypes.DatabaseIdentity{
			Name: database.Name, ResourceName: cnpgDatabaseName(postgresDB.Name, database.Name),
		})
	}
	return result
}

func databaseBootstrapCompletion(
	postgresDB *platformv1alpha1.PostgresDatabase,
	observation dbtypes.Observation,
) map[string]bool {
	current := make(map[string]dbtypes.ObservedDatabase, len(observation.Databases))
	for _, database := range observation.Databases {
		current[database.Name] = database
	}

	result := make(map[string]bool, len(postgresDB.Status.Databases))
	for _, database := range postgresDB.Status.Databases {
		if !databaseProvisioned(database) {
			continue
		}
		provider, found := current[database.Name]
		if !found || !provider.Found {
			continue
		}
		result[database.Name] = database.DatabaseUID != "" && string(database.DatabaseUID) == provider.UID
	}
	return result
}

func requiresClosedDatabaseFinalization(databases []platformv1alpha1.DatabaseDefinition) bool {
	for _, database := range databases {
		if database.AllowConnections != nil && !*database.AllowConnections {
			return true
		}
	}
	return false
}

func databasesRequiringPrivilegeBootstrap(
	postgresDB *platformv1alpha1.PostgresDatabase,
	bootstrapCompleted map[string]bool,
) []platformv1alpha1.DatabaseDefinition {
	result := make([]platformv1alpha1.DatabaseDefinition, 0, len(postgresDB.Spec.Databases))
	for _, database := range postgresDB.Spec.Databases {
		if !bootstrapCompleted[database.Name] {
			result = append(result, database)
		}
	}
	return result
}

func bootstrapDatabaseReasons(databases []platformv1alpha1.DatabaseDefinition) map[string]string {
	reasons := make(map[string]string, len(databases))
	for _, database := range databases {
		reasons[database.Name] = "Waiting for initial application privileges"
	}
	return reasons
}

// persistDatabaseBootstrapCompletion records successful grants before a
// requested closed state is applied on the next reconciliation.
func persistDatabaseBootstrapCompletion(
	ctx context.Context,
	c client.Client,
	postgresDB *platformv1alpha1.PostgresDatabase,
	bootstrapped []platformv1alpha1.DatabaseDefinition,
	databaseUIDs map[string]k8stypes.UID,
) error {
	before := postgresDB.Status.DeepCopy()
	applyStatus(
		postgresDB,
		databasesReady,
		metav1.ConditionFalse,
		reasonWaitingForCNPG,
		"Waiting for CNPG to apply the final database connection policy",
		provisioningDBPhase,
	)
	recordDatabaseBootstrapCompletion(postgresDB, bootstrapped, databaseUIDs)
	closed := make(map[string]struct{}, len(bootstrapped))
	for _, database := range bootstrapped {
		if database.AllowConnections != nil && !*database.AllowConnections {
			closed[database.Name] = struct{}{}
		}
	}
	for i := range postgresDB.Status.Databases {
		info := &postgresDB.Status.Databases[i]
		if _, found := closed[info.Name]; !found {
			continue
		}
		info.Ready = false
		info.Message = reasonCNPGDatabaseApplying
	}
	postgresDB.Status.ObservedGeneration = &postgresDB.Generation
	if equality.Semantic.DeepEqual(*before, postgresDB.Status) {
		return nil
	}
	return c.Status().Update(ctx, postgresDB)
}

func recordDatabaseBootstrapCompletion(
	postgresDB *platformv1alpha1.PostgresDatabase,
	bootstrapped []platformv1alpha1.DatabaseDefinition,
	databaseUIDs map[string]k8stypes.UID,
) {
	targets := make(map[string]struct{}, len(bootstrapped))
	for _, database := range bootstrapped {
		targets[database.Name] = struct{}{}
	}
	for i := range postgresDB.Status.Databases {
		info := &postgresDB.Status.Databases[i]
		if _, found := targets[info.Name]; !found {
			continue
		}
		info.DatabaseRef = &corev1.LocalObjectReference{Name: cnpgDatabaseName(postgresDB.Name, info.Name)}
		info.DatabaseUID = databaseUIDs[info.Name]
	}
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	return boolPointer(*value)
}

func boolPointer(value bool) *bool {
	return &value
}

func cloneInt32(value *int32) *int32 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
