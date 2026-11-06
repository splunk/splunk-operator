// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kvservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func tenantTerminal(message string, cause error) error {
	return splcommon.NewTerminalError(splcommon.EventReasonValidateSpecFailed, message, cause)
}

// tenantWriteError adds operation context without changing error classification.
func tenantWriteError(operation string, err error) error {
	return fmt.Errorf("%s: %w", operation, err)
}

// tenantReservationError represents contention, which must retry when another
// KVService or its garbage-collected keyring releases the namespace.
type tenantReservationError struct{ message string }

func (e *tenantReservationError) Error() string { return e.message }

func validateTenantKeyringOwner(cr *enterpriseApi.KVService, keyring *corev1.Secret) error {
	owner := metav1.GetControllerOf(keyring)
	if owner != nil && owner.UID == cr.UID {
		return nil
	}
	if owner != nil && owner.Kind == "KVService" && owner.APIVersion == enterpriseApi.GroupVersion.String() {
		return &tenantReservationError{fmt.Sprintf("namespace %q is reserved by KVService %q; waiting for its keyring to be released", cr.Namespace, owner.Name)}
	}
	return tenantTerminal(
		fmt.Sprintf("Keyring Secret %s/%s is not owned by KVService %s/%s. Resolve the Secret ownership conflict before retrying.", cr.Namespace, keyring.Name, cr.Namespace, cr.Name),
		fmt.Errorf("verification-key Secret %s/%s has controller owner %v; expected KVService UID %s", cr.Namespace, keyring.Name, owner, cr.UID))
}

// Duplicate CRs can exist when admission is bypassed or two creates race.
// Do not select a winner or mutate serving resources while that conflict exists.
func validateTenantSingleton(cr *enterpriseApi.KVService, services []enterpriseApi.KVService) error {
	var conflicts []string
	for _, service := range services {
		if service.Name != cr.Name {
			conflicts = append(conflicts, service.Name)
		}
	}
	if len(conflicts) == 0 {
		return nil
	}
	slices.Sort(conflicts)
	return tenantTerminal(
		fmt.Sprintf("Namespace %q contains multiple KVServices. Keep only one KVService and wait for the others to finish deleting; existing serving resources are left unchanged.", cr.Namespace),
		fmt.Errorf("KVService %s/%s conflicts with KVServices %q in its hosting namespace", cr.Namespace, cr.Name, conflicts))
}

// checkTenantReservation protects an acquired keyring while its previous owner
// is being deleted, including when concurrent admissions both saw an empty namespace.
func checkTenantReservation(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService) error {
	keyring := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: splcommon.KVServiceTenantVerificationKeysName}, keyring); err == nil {
		if err := validateTenantKeyringOwner(cr, keyring); err != nil {
			return fmt.Errorf("validate tenant keyring ownership: %w", err)
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get tenant keyring reservation %s/%s: %w", cr.Namespace, splcommon.KVServiceTenantVerificationKeysName, err)
	}
	return nil
}

// revokeAllTenants fails closed on invalid authoritative metadata.
// Transient discovery API errors never call this: they retain the last good state.
// Write failures preserve their terminal or retryable classification.
func revokeAllTenants(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService) error {
	if err := k8sops.SuspendKVService(ctx, c, cr); err != nil {
		return tenantWriteError("suspend KVService after invalid tenant discovery", err)
	}
	if _, err := reconcileKVServiceTenantKeys(ctx, c, cr, map[string]string{}); err != nil {
		return tenantWriteError("revoke tenant keys", err)
	}
	if _, err := k8sops.ApplyKVServiceTenantConfig(ctx, c, cr, map[string]string{}); err != nil {
		return tenantWriteError("revoke tenant membership", err)
	}
	cr.Status.ReadyReplicas = 0
	return nil
}

func ensureTenantPostgresReady(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService) (*k8sops.KVServicePostgresDependencies, bool, string, error) {
	if !config.DefaultMutableFeatureGate.Enabled(config.PostgresController) {
		return nil, false, "", tenantTerminal(
			fmt.Sprintf("Enable the %s feature gate on the Operator (--feature-gates=%s=true) to provision tenant databases for KVService %s/%s.", config.PostgresController, config.PostgresController, cr.Namespace, cr.Name),
			fmt.Errorf("PostgreSQL tenant provisioning is disabled for KVService %s/%s: feature gate %s is false", cr.Namespace, cr.Name, config.PostgresController))
	}
	pending := func(message string) (*k8sops.KVServicePostgresDependencies, bool, string, error) {
		cr.Status.Phase = enterpriseApi.PhasePending
		return nil, false, message, nil
	}
	services, err := k8sops.ListKVServices(ctx, c, client.InNamespace(cr.Namespace))
	if err != nil {
		return nil, false, "", fmt.Errorf("list KVServices in hosting namespace %q: %w", cr.Namespace, err)
	}
	if err := validateTenantSingleton(cr, services); err != nil {
		return nil, false, "", fmt.Errorf("validate KVService uniqueness: %w", err)
	}
	if err := checkTenantReservation(ctx, c, cr); err != nil {
		var reservation *tenantReservationError
		if errors.As(err, &reservation) {
			return pending(reservation.Error())
		}
		return nil, false, "", fmt.Errorf("check tenant namespace reservation: %w", err)
	}
	mapping, err := k8sops.DiscoverKVServiceTenants(ctx, c, cr)
	if err != nil {
		if _, terminal := splcommon.TerminalMessage(err); terminal {
			if revokeErr := revokeAllTenants(ctx, c, cr); revokeErr != nil {
				return nil, false, "", fmt.Errorf("revoke tenants after invalid discovery: %w", revokeErr)
			}
		}
		return nil, false, "", fmt.Errorf("discover KVService tenants: %w", err)
	}
	tenants := slices.Sorted(maps.Keys(mapping))
	if len(tenants) == 0 {
		if err := revokeAllTenants(ctx, c, cr); err != nil {
			return nil, false, "", fmt.Errorf("revoke tenants after empty discovery: %w", err)
		}
		return pending("waiting for enabled tenant namespaces selecting this KVService")
	}
	// Resolve keys before gating on PostgreSQL so removals and rotations do not
	// depend on database health. Defer key errors until all DB CRs are requested.
	keyring, keyErr := reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	var reservation *tenantReservationError
	if errors.As(keyErr, &reservation) {
		return pending(reservation.Error())
	}
	databases, waiting, err := ensureTenantDatabases(ctx, c, cr, tenants, services)
	if err != nil {
		return nil, false, "", fmt.Errorf("ensure tenant databases: %w", err)
	}
	if onlyMissingTenantSecrets(keyErr) {
		return pending("waiting for tenant namespace authentication Secrets")
	}
	if keyErr != nil {
		return nil, false, "", fmt.Errorf("reconcile tenant verification keys: %w", keyErr)
	}
	if len(waiting) > 0 {
		return pending("waiting for tenant databases: " + strings.Join(waiting, ", "))
	}
	dependencies, ready, message, err := buildTenantRuntime(ctx, c, cr, tenants, mapping, databases, keyring)
	if err != nil {
		return nil, false, "", fmt.Errorf("build tenant runtime: %w", err)
	}
	return dependencies, ready, message, nil
}

// tenantDatabase keeps a tenant and its validated database snapshot together.
type tenantDatabase struct {
	tenant   string
	database *platformApi.PostgresDatabase
}

func ensureTenantDatabases(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService, tenants []string, services []enterpriseApi.KVService) ([]tenantDatabase, []string, error) {
	var databases []tenantDatabase
	var waiting []string
	for _, tenant := range tenants {
		db := &platformApi.PostgresDatabase{}
		key := types.NamespacedName{Namespace: cr.Namespace, Name: splutil.KVServiceTenantDatabaseName(tenant)}
		if err := c.Get(ctx, key, db); apierrors.IsNotFound(err) {
			db = resources.NewKVServiceTenantDatabase(cr, tenant)
			if err := c.Create(ctx, db); err != nil && !apierrors.IsAlreadyExists(err) {
				return nil, nil, fmt.Errorf("create tenant database %s/%s for tenant %q: %w", cr.Namespace, key.Name, tenant, err)
			}
			waiting = append(waiting, tenant)
			continue
		} else if err != nil {
			return nil, nil, fmt.Errorf("get tenant database %s/%s for tenant %q: %w", cr.Namespace, key.Name, tenant, err)
		}
		if err := reconcileTenantDatabaseOwnership(ctx, c, cr, db, tenant, services); err != nil {
			return nil, nil, fmt.Errorf("reconcile database %s/%s for tenant %q: %w", db.Namespace, db.Name, tenant, err)
		}
		if db.DeletionTimestamp != nil || db.Status.ObservedGeneration == nil || *db.Status.ObservedGeneration != db.Generation || db.Status.Phase == nil || *db.Status.Phase != "Ready" {
			waiting = append(waiting, tenant)
		}
		databases = append(databases, tenantDatabase{tenant: tenant, database: db})
	}
	return databases, waiting, nil
}

// classifyTenantDatabase is pure policy: foreign and incompatible resources are never adopted.
func classifyTenantDatabase(db *platformApi.PostgresDatabase, cr *enterpriseApi.KVService, tenant string, services []enterpriseApi.KVService) (bool, error) {
	if owner := metav1.GetControllerOf(db); owner != nil {
		return false, tenantTerminal(
			fmt.Sprintf("Tenant %q requires PostgresDatabase %s/%s for KVService %s/%s, but that database has a Kubernetes controller reference to %s %q. Correct the tenant Namespace's %s backend assignment if it selects the wrong service; if the database belongs to another tenant, choose a unique %s tenant label. The existing database is left unchanged.", tenant, db.Namespace, db.Name, cr.Namespace, cr.Name, owner.Kind, owner.Name, splcommon.AnnotationKVServiceAddr, splcommon.LabelTenant),
			fmt.Errorf("PostgresDatabase %s/%s has controller owner %s/%s (UID %s)", db.Namespace, db.Name, owner.Kind, owner.Name, owner.UID))
	}
	managed := db.Annotations[splcommon.KVServiceTenantDatabaseOwnerAnnotation] == cr.Namespace+"/"+cr.Name
	// A replacement namespace-primary KVService can reuse retained resources,
	// but only after their previous manager is gone and without adopting foreign owners.
	if !managed && metav1.GetControllerOf(db) == nil {
		previous, sameNamespace := strings.CutPrefix(db.Annotations[splcommon.KVServiceTenantDatabaseOwnerAnnotation], cr.Namespace+"/")
		if !sameNamespace {
			previous = ""
		}

		if previous != "" && !slices.ContainsFunc(services, func(service enterpriseApi.KVService) bool { return service.Name == previous }) {
			managed = true
		}
	}
	if !managed {
		return false, tenantTerminal(
			fmt.Sprintf("Tenant %q requires PostgresDatabase %s/%s, but it is not managed by KVService %s/%s. Correct the tenant Namespace's %s backend assignment if it selects the wrong service; if the database is unrelated, choose a unique %s tenant label. The existing database is left unchanged.", tenant, db.Namespace, db.Name, cr.Namespace, cr.Name, splcommon.AnnotationKVServiceAddr, splcommon.LabelTenant),
			fmt.Errorf("PostgresDatabase %s/%s management annotation is %q; expected %q or a retained assignment whose previous manager is gone", db.Namespace, db.Name, db.Annotations[splcommon.KVServiceTenantDatabaseOwnerAnnotation], cr.Namespace+"/"+cr.Name))
	}
	if db.Spec.ClusterRef.Name != cr.Spec.Postgres.ClusterRef.Name || len(db.Spec.Databases) != 1 || db.Spec.Databases[0].Name != tenant || db.Spec.Databases[0].DeletionPolicy != "Retain" {
		return false, tenantTerminal(
			fmt.Sprintf("Tenant database %s/%s has incompatible configuration. It must reference PostgresCluster %q, contain only database %q, and use deletionPolicy Retain.", db.Namespace, db.Name, cr.Spec.Postgres.ClusterRef.Name, tenant),
			fmt.Errorf("PostgresDatabase %s/%s does not match the required cluster, tenant database, and Retain policy", db.Namespace, db.Name))
	}

	return db.Annotations[splcommon.KVServiceTenantDatabaseOwnerAnnotation] != cr.Namespace+"/"+cr.Name, nil
}

func reconcileTenantDatabaseOwnership(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService, db *platformApi.PostgresDatabase, tenant string, services []enterpriseApi.KVService) error {
	adopt, err := classifyTenantDatabase(db, cr, tenant, services)
	if err != nil {
		return fmt.Errorf("validate tenant database assignment: %w", err)
	}
	if !adopt {
		return nil
	}
	if db.Annotations == nil {
		db.Annotations = make(map[string]string)
	}
	db.Annotations[splcommon.KVServiceTenantDatabaseOwnerAnnotation] = cr.Namespace + "/" + cr.Name
	if err := c.Update(ctx, db); err != nil {
		return fmt.Errorf("update tenant database management assignment %s/%s: %w", db.Namespace, db.Name, err)
	}
	return nil
}

func buildTenantRuntime(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService, tenants []string, mapping map[string]string, databases []tenantDatabase, keyring *corev1.Secret) (*k8sops.KVServicePostgresDependencies, bool, string, error) {
	pending := func(message string) (*k8sops.KVServicePostgresDependencies, bool, string, error) {
		cr.Status.Phase = enterpriseApi.PhasePending
		return nil, false, message, nil
	}
	runtimeConfig, err := k8sops.ApplyKVServiceTenantConfig(ctx, c, cr, mapping)
	if err != nil {
		return nil, false, "", fmt.Errorf("publish tenant membership: %w", err)
	}
	dependencies := &k8sops.KVServicePostgresDependencies{}
	var credentials []resources.KVServiceTenantCredential
	var configs, revisions []string
	for i := range databases {
		entry := &databases[i]
		tenant, db := entry.tenant, entry.database
		snapshot, info, err := k8sops.ResolveKVServiceTenantDependencies(ctx, c, cr, db, tenant)
		if err != nil {
			return nil, false, "", fmt.Errorf("resolve database connection for tenant %q: %w", tenant, err)
		}
		if info == nil {
			return pending("waiting for connection references for tenant " + tenant)
		}
		if i == 0 {
			dependencies.Environment = snapshot.Environment
		}
		configs = append(configs, tenant+":"+snapshot.ConfigHash)
		revisions = append(revisions, snapshot.AdminUserSecretRevision)
		credentials = append(credentials, resources.KVServiceTenantCredential{Tenant: tenant, SecretRef: *info.AdminUserSecretRef})
	}
	runtime := resources.NewKVServiceTenantRuntime(runtimeConfig.Name, keyring.Name, tenants[0], credentials)
	dependencies.Volumes = runtime.Volumes
	dependencies.VolumeMounts = runtime.VolumeMounts
	dependencies.Environment = append(dependencies.Environment, runtime.Environment...)

	hash := func(values []string) string {
		digest := sha256.Sum256([]byte(strings.Join(values, "\n")))
		return hex.EncodeToString(digest[:])
	}
	mappingJSON, err := json.Marshal(mapping)
	if err != nil {
		return nil, false, "", fmt.Errorf("encode tenant membership for KVService %s/%s: %w", cr.Namespace, cr.Name, err)
	}
	// Membership and keyring revisions deliberately trigger pod rollouts even though
	// the runtime can reread mounted files. Pending dependencies can defer reaching
	// this point; neither file propagation nor a rollout is an immediate revocation barrier.
	dependencies.ConfigHash = hash(append(configs, string(mappingJSON)))
	dependencies.AdminUserSecretRevision = hash(revisions)
	dependencies.TenantKeyringRevision = fmt.Sprintf("%s:%s", keyring.UID, keyring.ResourceVersion)
	return dependencies, true, "", nil
}
