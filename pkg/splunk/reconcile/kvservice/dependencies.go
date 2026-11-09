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
	"github.com/splunk/splunk-operator/pkg/logging"
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

// ensureTenantPostgresReady gates deployment only on the service database.
// Tenant-local failures remain visible in status and never suspend other tenants.
func ensureTenantPostgresReady(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService) (*k8sops.KVServicePostgresDependencies, bool, string, error) {
	if !config.DefaultMutableFeatureGate.Enabled(config.PostgresController) {
		return nil, false, "", tenantTerminal(fmt.Sprintf("Enable the %s feature gate on the Operator (--feature-gates=%s=true) to provision databases for KVService %s/%s.", config.PostgresController, config.PostgresController, cr.Namespace, cr.Name), fmt.Errorf("PostgreSQL provisioning is disabled for KVService %s/%s", cr.Namespace, cr.Name))
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
			cr.Status.Phase = enterpriseApi.PhasePending
			return nil, false, reservation.Error(), nil
		}
		return nil, false, "", fmt.Errorf("check tenant namespace reservation: %w", err)
	}
	mapping, err := k8sops.DiscoverKVServiceTenants(ctx, c, cr)
	var discovery *k8sops.TenantDiscoveryError
	if err != nil && !errors.As(err, &discovery) {
		return nil, false, "", fmt.Errorf("discover KVService tenants: %w", err)
	}
	statuses := []enterpriseApi.KVServiceTenantStatus{}
	if discovery != nil {
		statuses = append(statuses, discovery.Blocked...)
	}
	// Removals and authentication updates are published before service readiness is checked.
	keyring, keyErr := reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	if keyring == nil {
		var reservation *tenantReservationError
		if errors.As(keyErr, &reservation) {
			cr.Status.Phase = enterpriseApi.PhasePending
			return nil, false, reservation.Error(), nil
		}
		return nil, false, "", fmt.Errorf("publish tenant verification keys: %w", keyErr)
	}
	var keyFailures tenantKeyErrors
	if keyErr != nil && !errors.As(keyErr, &keyFailures) {
		return nil, false, "", fmt.Errorf("reconcile tenant verification keys: %w", keyErr)
	}
	serviceDB, serviceReady, serviceErr := ensureDatabase(ctx, c, cr, resources.NewKVServiceSystemDatabase(cr), services)
	var service *k8sops.KVServicePostgresDependencies
	if serviceErr == nil && serviceReady {
		var info *platformApi.DatabaseInfo
		service, info, serviceErr = k8sops.ResolveKVServiceTenantDependencies(ctx, c, cr, serviceDB, splcommon.KVServiceSystemDatabase)
		serviceReady = info != nil && serviceErr == nil
	}
	ready := make(map[string]string)
	var databases []tenantDatabase
	for _, tenant := range slices.Sorted(maps.Keys(mapping)) {
		status := enterpriseApi.KVServiceTenantStatus{Name: tenant, Namespace: mapping[tenant], DatabaseName: splutil.KVServiceTenantDatabaseName(tenant), Reason: "DatabasePending", Message: "Waiting for tenant database readiness and connection references"}
		db, dbReady, dbErr := ensureDatabase(ctx, c, cr, resources.NewKVServiceTenantDatabase(cr, tenant), services)
		if dbErr != nil {
			status.Message = "Tenant database is unavailable; check its status and Operator logs"
			if message, terminal := splcommon.TerminalMessage(dbErr); terminal {
				status.Reason = "DatabaseConflict"
				status.Message = message
			}
			logging.FromContext(ctx).ErrorContext(ctx, "tenant database reconciliation failed", "tenant", tenant, "namespace", status.Namespace, "error", dbErr)
		} else if authErr := keyFailures[tenant]; authErr != nil {
			status.Reason = "AuthenticationPending"
			status.Message = "Waiting for the tenant namespace authentication Secret"
			if message, terminal := splcommon.TerminalMessage(authErr); terminal {
				status.Reason = "AuthenticationInvalid"
				status.Message = message
			}
			if !apierrors.IsNotFound(authErr) {
				logging.FromContext(ctx).ErrorContext(ctx, "tenant authentication Secret unavailable", "tenant", tenant,
					"namespace", status.Namespace, "secret", splcommon.GetNamespaceScopedSecretName(status.Namespace), "error", authErr)
			}
		} else if dbReady {
			snapshot, info, resolveErr := k8sops.ResolveKVServiceTenantDependencies(ctx, c, cr, db, tenant)
			if resolveErr != nil {
				status.Reason = "CredentialsPending"
				status.Message = "Waiting for usable tenant connection ConfigMap and database credentials"
				logging.FromContext(ctx).ErrorContext(ctx, "tenant connection dependencies unavailable", "tenant", tenant, "error", resolveErr)
			} else if info != nil {
				status.Ready = true
				status.Reason = "Ready"
				status.Message = "Tenant database and authentication dependencies are ready"
				ready[tenant] = mapping[tenant]
				databases = append(databases, tenantDatabase{tenant: tenant, dependencies: snapshot, info: info})
			}
		}
		statuses = append(statuses, status)
	}
	slices.SortFunc(statuses, func(a, b enterpriseApi.KVServiceTenantStatus) int { return strings.Compare(a.Namespace, b.Namespace) })
	cr.Status.Tenants = statuses
	runtimeConfig, err := k8sops.ApplyKVServiceTenantConfig(ctx, c, cr, ready)
	if err != nil {
		return nil, false, "", fmt.Errorf("publish tenant membership: %w", err)
	}
	if serviceErr != nil {
		return nil, false, "", fmt.Errorf("prepare service database %s/%s: %w", cr.Namespace, splcommon.KVServiceSystemDatabaseResource, serviceErr)
	}
	if !serviceReady {
		cr.Status.Phase = enterpriseApi.PhasePending
		return nil, false, "waiting for service database kvservicesystem and its connection credentials", nil
	}
	dependencies, err := buildTenantRuntime(cr, ready, databases, keyring, service, runtimeConfig.Name)
	if err != nil {
		return nil, false, "", fmt.Errorf("build tenant runtime: %w", err)
	}
	return dependencies, true, "", nil
}

// tenantDatabase keeps an eligible tenant and its validated connection snapshot together.
type tenantDatabase struct {
	tenant       string
	dependencies *k8sops.KVServicePostgresDependencies
	info         *platformApi.DatabaseInfo
}

func ensureDatabase(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService, desired *platformApi.PostgresDatabase, services []enterpriseApi.KVService) (*platformApi.PostgresDatabase, bool, error) {
	db := &platformApi.PostgresDatabase{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(desired), db); apierrors.IsNotFound(err) {
		if err := c.Create(ctx, desired); err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, false, fmt.Errorf("create PostgresDatabase %s/%s: %w", desired.Namespace, desired.Name, err)
		}
		return nil, false, nil
	} else if err != nil {
		return nil, false, fmt.Errorf("get PostgresDatabase %s/%s: %w", desired.Namespace, desired.Name, err)
	}
	if err := reconcileTenantDatabaseOwnership(ctx, c, cr, db, desired.Spec.Databases[0].Name, services); err != nil {
		return nil, false, fmt.Errorf("validate PostgresDatabase %s/%s: %w", db.Namespace, db.Name, err)
	}
	ready := db.DeletionTimestamp == nil && db.Status.ObservedGeneration != nil && *db.Status.ObservedGeneration == db.Generation && db.Status.Phase != nil && *db.Status.Phase == "Ready"
	return db, ready, nil
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

func buildTenantRuntime(cr *enterpriseApi.KVService, mapping map[string]string, databases []tenantDatabase, keyring *corev1.Secret, dependencies *k8sops.KVServicePostgresDependencies, membership string) (*k8sops.KVServicePostgresDependencies, error) {
	var credentials []resources.KVServiceTenantCredential
	configs := []string{splcommon.KVServiceSystemDatabase + ":" + dependencies.ConfigHash}
	revisions := []string{dependencies.AdminUserSecretRevision}
	for _, entry := range databases {
		configs = append(configs, entry.tenant+":"+entry.dependencies.ConfigHash)
		revisions = append(revisions, entry.dependencies.AdminUserSecretRevision)
		credentials = append(credentials, resources.KVServiceTenantCredential{Tenant: entry.tenant, SecretRef: *entry.info.AdminUserSecretRef})
	}
	runtime := resources.NewKVServiceTenantRuntime(membership, keyring.Name, splcommon.KVServiceSystemDatabase, credentials)
	dependencies.Volumes = runtime.Volumes
	dependencies.VolumeMounts = runtime.VolumeMounts
	dependencies.Environment = append(dependencies.Environment, runtime.Environment...)
	hash := func(values []string) string {
		digest := sha256.Sum256([]byte(strings.Join(values, "\n")))
		return hex.EncodeToString(digest[:])
	}
	mappingJSON, err := json.Marshal(mapping)
	if err != nil {
		return nil, fmt.Errorf("encode tenant membership for KVService %s/%s: %w", cr.Namespace, cr.Name, err)
	}
	dependencies.ConfigHash = hash(append(configs, string(mappingJSON)))
	dependencies.AdminUserSecretRevision = hash(revisions)
	dependencies.TenantKeyringRevision = fmt.Sprintf("%s:%s", keyring.UID, keyring.ResourceVersion)
	return dependencies, nil
}
