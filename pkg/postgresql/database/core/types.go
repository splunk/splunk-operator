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
	"errors"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

// ReconcileContext bundles infrastructure dependencies injected by the controller.
type ReconcileContext struct {
	Client   client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Metrics  ports.Recorder
}

type reconcileDBPhases string
type conditionTypes string
type conditionReasons string
type clusterReadyStatus string
type reconcileConflictCategory string

const (
	retryDelay                = time.Second * 15
	clusterNotFoundRetryDelay = time.Second * 30
	roleCleanupTimeout        = time.Minute * 30

	rolesExist  = true
	rolesAbsent = false

	readOnlyEndpoint  string = "ro"
	readWriteEndpoint string = "rw"

	deletionPolicyRetain string = "Retain"

	postgresDatabaseFinalizerName string = "postgresdatabases.enterprise.splunk.com/finalizer"
	annotationRetainedFrom        string = "enterprise.splunk.com/retained-from"

	secretRoleAdmin   string = "admin"
	secretRoleRW      string = "rw"
	secretKeyPassword string = "password"
	secretKeyUsername string = "username"

	labelManagedBy  string = "app.kubernetes.io/managed-by"
	labelCNPGReload string = "cnpg.io/reload"

	// Password generation — no symbols for PostgreSQL connection string compatibility.
	passwordLength  = 32
	passwordDigits  = 8
	passwordSymbols = 0

	// Privileges failure handling.
	reconcileFailurePrivileges = "Privileges"

	// DB reconcile phases
	readyDBPhase        reconcileDBPhases = "Ready"
	pendingDBPhase      reconcileDBPhases = "Pending"
	provisioningDBPhase reconcileDBPhases = "Provisioning"
	failedDBPhase       reconcileDBPhases = "Failed"
	deletingDBPhase     reconcileDBPhases = "Deleting"

	// condition types
	clusterReady    conditionTypes = "ClusterReady"
	rolesReady      conditionTypes = "RolesReady"
	databasesReady  conditionTypes = "DatabasesReady"
	secretsReady    conditionTypes = "SecretsReady"
	configMapsReady conditionTypes = "ConfigMapsReady"
	privilegesReady conditionTypes = "PrivilegesReady"

	// condition reasons
	reasonClusterNotFound            conditionReasons = "ClusterNotFound"
	reasonClusterProvisioning        conditionReasons = "ClusterProvisioning"
	reasonClusterInfoFetchFailed     conditionReasons = "ClusterInfoFetchNotPossible"
	reasonClusterAvailable           conditionReasons = "ClusterAvailable"
	reasonDatabasesAvailable         conditionReasons = "DatabasesAvailable"
	reasonSecretsCreated             conditionReasons = "SecretsCreated"
	reasonSecretsCreationFailed      conditionReasons = "SecretsCreationFailed"
	reasonSecretsDriftDetected       conditionReasons = "SecretsDriftDetected"
	reasonExternalSecretMissing      conditionReasons = "ExternalSecretMissing"
	reasonExternalSecretInvalid      conditionReasons = "ExternalSecretInvalid"
	reasonExternalSecretMissingData  conditionReasons = "ExternalSecretMissingData"
	reasonExternalSecretMissingKeys  conditionReasons = "ExternalSecretMissingKeys"
	reasonExternalSecretMissingLabel conditionReasons = "ExternalSecretMissingReloadLabel"
	reasonWaitingForCNPG             conditionReasons = "WaitingForCNPG"
	reasonRolesCreationFailed        conditionReasons = "RolesCreationFailed"
	reasonRolesAvailable             conditionReasons = "RolesAvailable"
	reasonRoleConflict               conditionReasons = "RoleConflict"
	reasonRoleReconcileFailed        conditionReasons = "RoleReconcileFailed"
	reasonRoleCleanupWaiting         conditionReasons = "RoleCleanupWaitingForCluster"
	reasonRoleCleanupBlocked         conditionReasons = "RoleCleanupBlocked"
	reasonConfigMapsCreationFailed   conditionReasons = "ConfigMapsCreationFailed"
	reasonConfigMapsCreated          conditionReasons = "ConfigMapsCreated"
	reasonDatabaseReconcileFailed    conditionReasons = "DatabaseReconcileFailed"
	reasonPrivilegesGranted          conditionReasons = "PrivilegesGranted"
	reasonPrivilegesGrantFailed      conditionReasons = "PrivilegesGrantFailed"
	reasonPrivilegesTerminalFailure  conditionReasons = "PrivilegesTerminalFailure"

	// ClusterReady sentinel values returned by getClusterReadyStatus.
	ClusterNotReady         clusterReadyStatus = "NotReady"
	ClusterNoProvisionerRef clusterReadyStatus = "NoProvisionerRef"
	ClusterReady            clusterReadyStatus = "Ready"

	conflictDeletion               reconcileConflictCategory = "deletion"
	conflictFinalizer              reconcileConflictCategory = "finalizer"
	conflictClusterStatus          reconcileConflictCategory = "cluster_status"
	conflictRoleConflictStatus     reconcileConflictCategory = "role_conflict_status"
	conflictSecretsReconcile       reconcileConflictCategory = "secrets_reconcile"
	conflictSecretsStatus          reconcileConflictCategory = "secrets_status"
	conflictConfigMapsReconcile    reconcileConflictCategory = "configmaps_reconcile"
	conflictConfigMapsStatus       reconcileConflictCategory = "configmaps_status"
	conflictRolesStatus            reconcileConflictCategory = "roles_status"
	conflictCNPGDatabasesReconcile reconcileConflictCategory = "cnpg_databases_reconcile"
	conflictDatabasesStatus        reconcileConflictCategory = "databases_status"
	conflictPrivilegesStatus       reconcileConflictCategory = "privileges_status"
	conflictFinalStatus            reconcileConflictCategory = "final_status"
)

type clusterEndpoints = pgconninfo.Endpoints

// deletionPlan separates databases by their DeletionPolicy for the cleanup workflow.
type deletionPlan struct {
	retained []enterprisev4.DatabaseDefinition
	deleted  []enterprisev4.DatabaseDefinition
}

// ErrTerminal marks user-actionable errors where retrying the same spec is not expected to succeed.
var ErrTerminal = errors.New("terminal reconciliation error")

var errRoleCleanupPending = errors.New("waiting for managed role cleanup")
