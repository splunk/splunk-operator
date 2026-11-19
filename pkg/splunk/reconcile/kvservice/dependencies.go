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
		fmt.Sprintf(
			"Keyring Secret %s/%s is not owned by KVService %s/%s. Resolve the Secret ownership conflict before retrying.",
			cr.Namespace, keyring.Name, cr.Namespace, cr.Name,
		),
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
		fmt.Sprintf(
			"Namespace %q contains multiple KVServices. Keep only one KVService and wait for the others to finish deleting; existing serving resources are left unchanged.",
			cr.Namespace,
		),
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

// tenantPostgresResult separates service readiness from tenant progress.
type tenantPostgresResult struct {
	dependencies          *k8sops.KVServicePostgresDependencies
	ready                 bool
	message               string
	tenantsEvaluated      bool
	retryTenants          bool
	clientProtocolChecked bool
	protocolMismatches    []string
}

// ensureTenantPostgresReady gates deployment only on the service database.
// Tenant-local failures remain visible in status and never suspend other tenants.
func ensureTenantPostgresReady(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService) (tenantPostgresResult, error) {
	var result tenantPostgresResult
	if !config.DefaultMutableFeatureGate.Enabled(config.PostgresController) {
		return result, tenantTerminal(
			fmt.Sprintf(
				"Enable the %s feature gate on the Operator (--feature-gates=%s=true) to provision databases for KVService %s/%s.",
				config.PostgresController, config.PostgresController, cr.Namespace, cr.Name,
			),
			fmt.Errorf("PostgreSQL provisioning is disabled for KVService %s/%s", cr.Namespace, cr.Name))
	}
	services, err := k8sops.ListKVServices(ctx, c, client.InNamespace(cr.Namespace))
	if err != nil {
		return result, fmt.Errorf("list KVServices in hosting namespace %q: %w", cr.Namespace, err)
	}
	if err := validateTenantSingleton(cr, services); err != nil {
		return result, fmt.Errorf("validate KVService uniqueness: %w", err)
	}
	if err := checkTenantReservation(ctx, c, cr); err != nil {
		var reservation *tenantReservationError
		if errors.As(err, &reservation) {
			cr.Status.Phase = enterpriseApi.PhasePending
			result.message = reservation.Error()
			return result, nil
		}
		return result, fmt.Errorf("check tenant namespace reservation: %w", err)
	}
	discovery, err := k8sops.DiscoverKVServiceTenants(ctx, c, cr)
	if err != nil {
		return result, fmt.Errorf("discover KVService tenants: %w", err)
	}
	result.clientProtocolChecked = true
	result.protocolMismatches = discovery.ProtocolMismatches
	mapping := discovery.Members
	statuses := slices.Clone(discovery.Blocked)
	// Removals and authentication updates are published before service readiness is checked.
	keyring, keyFailures, keyErr := reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	if keyErr != nil {
		var reservation *tenantReservationError
		if errors.As(keyErr, &reservation) {
			cr.Status.Phase = enterpriseApi.PhasePending
			result.message = reservation.Error()
			return result, nil
		}
		return result, fmt.Errorf("publish tenant verification keys: %w", keyErr)
	}
	serviceDB, serviceReady, serviceErr := ensureDatabase(ctx, c, cr,
		resources.NewKVServiceSystemDatabase(cr), splcommon.KVServiceSystemDatabase, services)
	var service *k8sops.KVServicePostgresDependencies
	if serviceErr == nil && serviceReady {
		var info *platformApi.DatabaseInfo
		service, info, serviceErr = k8sops.ResolveKVServiceTenantDependencies(ctx, c, cr, serviceDB, splcommon.KVServiceSystemDatabase)
		serviceReady = service != nil && info != nil && serviceErr == nil
	}
	ready := make(map[string]string)
	var databases []tenantDatabase
	for _, tenant := range slices.Sorted(maps.Keys(mapping)) {
		db, dbReady, dbErr := ensureDatabase(ctx, c, cr,
			resources.NewKVServiceTenantDatabase(cr, tenant), tenant, services)
		evaluation := evaluateTenant(tenant, mapping[tenant], databaseResult{ready: dbReady, err: dbErr}, keyFailures[tenant], func() resolvedTenantDependencies {
			dependencies, info, err := k8sops.ResolveKVServiceTenantDependencies(ctx, c, cr, db, tenant)
			return resolvedTenantDependencies{dependencies: dependencies, info: info, err: err}
		})
		if evaluation.diagnostic != nil {
			args := []any{"tenant", tenant, "namespace", mapping[tenant],
				"database", evaluation.status.DatabaseName, "reason", evaluation.status.Reason,
				"error", evaluation.diagnostic}
			if evaluation.secret != "" {
				args = append(args, "secret", evaluation.secret)
			}
			logger := logging.FromContext(ctx)
			if apierrors.IsNotFound(evaluation.diagnostic) || slices.Contains(cr.Status.Tenants, evaluation.status) {
				logger.DebugContext(ctx, "tenant dependencies pending", args...)
			} else {
				logger.ErrorContext(ctx, "tenant dependencies unavailable", args...)
			}
		}
		result.retryTenants = result.retryTenants || evaluation.retry
		statuses = append(statuses, evaluation.status)
		if evaluation.database != nil {
			ready[tenant] = mapping[tenant]
			databases = append(databases, *evaluation.database)
		}
	}
	slices.SortFunc(statuses, func(a, b enterpriseApi.KVServiceTenantStatus) int { return strings.Compare(a.Namespace, b.Namespace) })
	if statuses == nil {
		statuses = []enterpriseApi.KVServiceTenantStatus{}
	}
	runtimeConfig, err := k8sops.ApplyKVServiceTenantConfig(ctx, c, cr, ready)
	if err != nil {
		return result, fmt.Errorf("publish tenant membership: %w", err)
	}
	cr.Status.Tenants = statuses
	result.tenantsEvaluated = true
	if serviceErr != nil {
		return result, fmt.Errorf("prepare service database %s/%s: %w", cr.Namespace, splcommon.KVServiceSystemDatabaseResource, serviceErr)
	}
	if !serviceReady {
		cr.Status.Phase = enterpriseApi.PhasePending
		result.message = "waiting for service database kvservicesystem and its connection credentials"
		return result, nil
	}
	result.dependencies, err = buildTenantRuntime(cr, ready, databases, keyring, service, runtimeConfig.Name)
	if err != nil {
		return result, fmt.Errorf("build tenant runtime: %w", err)
	}
	result.ready = true
	return result, nil
}

type databaseResult struct {
	ready bool
	err   error
}

type resolvedTenantDependencies struct {
	dependencies *k8sops.KVServicePostgresDependencies
	info         *platformApi.DatabaseInfo
	err          error
}

type tenantEvaluation struct {
	status     enterpriseApi.KVServiceTenantStatus
	database   *tenantDatabase
	retry      bool
	diagnostic error
	secret     string
}

// evaluateTenant owns tenant outcome precedence and resolves credentials only after
// database and authentication prerequisites pass.
func evaluateTenant(tenant, namespace string, db databaseResult, authErr error, resolve func() resolvedTenantDependencies) tenantEvaluation {
	result := tenantEvaluation{status: enterpriseApi.KVServiceTenantStatus{
		Name: tenant, Namespace: namespace, DatabaseName: splutil.KVServiceTenantDatabaseName(tenant),
	}}
	switch {
	case db.err != nil:
		result.status.Reason = enterpriseApi.KVServiceTenantDatabasePending
		result.status.Message = "Tenant database is unavailable; check its status and Operator logs"
		result.retry = true
		result.diagnostic = db.err
		if message, terminal := splcommon.TerminalMessage(db.err); terminal {
			result.status.Reason = enterpriseApi.KVServiceTenantDatabaseConflict
			result.status.Message = message
			result.retry = false
		}
	case authErr != nil:
		result.status.Reason = enterpriseApi.KVServiceTenantAuthenticationPending
		result.status.Message = "Waiting for the tenant namespace authentication Secret"
		result.retry = true
		result.diagnostic = authErr
		result.secret = splcommon.GetNamespaceScopedSecretName(namespace)
		if message, terminal := splcommon.TerminalMessage(authErr); terminal {
			result.status.Reason = enterpriseApi.KVServiceTenantAuthenticationInvalid
			result.status.Message = message
			result.retry = false
		}
	case !db.ready:
		result.status.Reason = enterpriseApi.KVServiceTenantDatabasePending
		result.status.Message = "Waiting for tenant database readiness"
		result.retry = true
	default:
		resolved := resolve()
		if resolved.err != nil || resolved.info == nil || resolved.dependencies == nil {
			result.status.Reason = enterpriseApi.KVServiceTenantCredentialsPending
			result.status.Message = "Waiting for usable tenant connection ConfigMap and database credentials"
			result.retry = true
			result.diagnostic = resolved.err
			return result
		}
		result.status.Ready = true
		result.status.Reason = enterpriseApi.KVServiceTenantReady
		result.status.Message = "Tenant database and authentication dependencies are ready"
		result.database = &tenantDatabase{tenant: tenant, dependencies: resolved.dependencies, info: resolved.info}
	}
	return result
}

// tenantDatabase keeps an eligible tenant and its validated connection snapshot together.
type tenantDatabase struct {
	tenant       string
	dependencies *k8sops.KVServicePostgresDependencies
	info         *platformApi.DatabaseInfo
}

func ensureDatabase(
	ctx context.Context,
	c splcommon.ControllerClient,
	cr *enterpriseApi.KVService,
	desired *platformApi.PostgresDatabase,
	databaseName string,
	services []enterpriseApi.KVService,
) (*platformApi.PostgresDatabase, bool, error) {
	db := &platformApi.PostgresDatabase{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(desired), db); apierrors.IsNotFound(err) {
		if err := c.Create(ctx, desired); err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, false, fmt.Errorf("create PostgresDatabase %s/%s: %w", desired.Namespace, desired.Name, err)
		}
		return nil, false, nil
	} else if err != nil {
		return nil, false, fmt.Errorf("get PostgresDatabase %s/%s: %w", desired.Namespace, desired.Name, err)
	}
	if err := reconcileManagedDatabaseOwnership(ctx, c, cr, db, databaseName, services); err != nil {
		return nil, false, fmt.Errorf("validate PostgresDatabase %s/%s: %w", db.Namespace, db.Name, err)
	}
	ready := db.DeletionTimestamp == nil &&
		db.Status.ObservedGeneration != nil &&
		*db.Status.ObservedGeneration == db.Generation &&
		db.Status.Phase != nil && *db.Status.Phase == "Ready"
	return db, ready, nil
}

// classifyManagedDatabase is pure policy: foreign and incompatible resources are never adopted.
func classifyManagedDatabase(db *platformApi.PostgresDatabase, cr *enterpriseApi.KVService, databaseName string, services []enterpriseApi.KVService) (bool, error) {
	subject := fmt.Sprintf("Tenant %q", databaseName)
	repair := fmt.Sprintf(
		"Correct the tenant Namespace's %s backend assignment if it selects the wrong service; if the database is unrelated, choose a unique %s tenant label.",
		splcommon.AnnotationKVServiceAddr, splcommon.LabelTenant,
	)
	if databaseName == splcommon.KVServiceSystemDatabase {
		subject = "The KVService system database"
		repair = "Review the existing system database's management assignment and controller ownership against this KVService."
	}
	if owner := metav1.GetControllerOf(db); owner != nil {
		return false, tenantTerminal(
			fmt.Sprintf(
				"%s requires PostgresDatabase %s/%s for KVService %s/%s, but that database has a Kubernetes controller reference to %s %q. %s The existing database is left unchanged.",
				subject, db.Namespace, db.Name, cr.Namespace, cr.Name, owner.Kind, owner.Name, repair,
			),
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
			fmt.Sprintf(
				"%s requires PostgresDatabase %s/%s, but it is not managed by KVService %s/%s. %s The existing database is left unchanged.",
				subject, db.Namespace, db.Name, cr.Namespace, cr.Name, repair,
			),
			fmt.Errorf(
				"PostgresDatabase %s/%s management annotation is %q; expected %q or a retained assignment whose previous manager is gone",
				db.Namespace, db.Name, db.Annotations[splcommon.KVServiceTenantDatabaseOwnerAnnotation], cr.Namespace+"/"+cr.Name,
			))
	}
	if db.Spec.ClusterRef.Name != cr.Spec.Postgres.ClusterRef.Name ||
		len(db.Spec.Databases) != 1 ||
		db.Spec.Databases[0].Name != databaseName ||
		db.Spec.Databases[0].DeletionPolicy != "Retain" {
		return false, tenantTerminal(
			fmt.Sprintf(
				"%s has incompatible configuration in PostgresDatabase %s/%s. It must reference PostgresCluster %q, contain only database %q, and use deletionPolicy Retain.",
				subject, db.Namespace, db.Name, cr.Spec.Postgres.ClusterRef.Name, databaseName,
			),
			fmt.Errorf("PostgresDatabase %s/%s does not match the required cluster, logical database, and Retain policy", db.Namespace, db.Name))
	}

	return db.Annotations[splcommon.KVServiceTenantDatabaseOwnerAnnotation] != cr.Namespace+"/"+cr.Name, nil
}

func reconcileManagedDatabaseOwnership(
	ctx context.Context,
	c splcommon.ControllerClient,
	cr *enterpriseApi.KVService,
	db *platformApi.PostgresDatabase,
	databaseName string,
	services []enterpriseApi.KVService,
) error {
	adopt, err := classifyManagedDatabase(db, cr, databaseName, services)
	if err != nil {
		return fmt.Errorf("validate managed database assignment: %w", err)
	}
	if !adopt {
		return nil
	}
	if db.Annotations == nil {
		db.Annotations = make(map[string]string)
	}
	db.Annotations[splcommon.KVServiceTenantDatabaseOwnerAnnotation] = cr.Namespace + "/" + cr.Name
	if err := c.Update(ctx, db); err != nil {
		return fmt.Errorf("update managed database assignment %s/%s: %w", db.Namespace, db.Name, err)
	}
	return nil
}

func buildTenantRuntime(
	cr *enterpriseApi.KVService,
	ready map[string]string,
	databases []tenantDatabase,
	keyring *corev1.Secret,
	service *k8sops.KVServicePostgresDependencies,
	membership string,
) (*k8sops.KVServicePostgresDependencies, error) {
	var credentials []resources.KVServiceTenantCredential
	configs := []string{splcommon.KVServiceSystemDatabase + ":" + service.ConfigHash}
	revisions := []string{service.AdminUserSecretRevision}
	for _, entry := range databases {
		configs = append(configs, entry.tenant+":"+entry.dependencies.ConfigHash)
		revisions = append(revisions, entry.dependencies.AdminUserSecretRevision)
		credentials = append(credentials, resources.KVServiceTenantCredential{Tenant: entry.tenant, SecretRef: *entry.info.AdminUserSecretRef})
	}
	runtime := resources.NewKVServiceTenantRuntime(membership, keyring.Name, splcommon.KVServiceSystemDatabase, credentials)
	// Keep the service-database snapshot separate from the Deployment dependencies.
	dependencies := *service
	dependencies.Volumes = runtime.Volumes
	dependencies.VolumeMounts = runtime.VolumeMounts
	dependencies.Environment = append(slices.Clone(service.Environment), runtime.Environment...)
	hash := func(values []string) string {
		digest := sha256.Sum256([]byte(strings.Join(values, "\n")))
		return hex.EncodeToString(digest[:])
	}
	mappingJSON, err := json.Marshal(ready)
	if err != nil {
		return nil, fmt.Errorf("encode tenant membership for KVService %s/%s: %w", cr.Namespace, cr.Name, err)
	}
	dependencies.ConfigHash = hash(append(configs, string(mappingJSON)))
	dependencies.AdminUserSecretRevision = hash(revisions)
	dependencies.TenantKeyringRevision = fmt.Sprintf("%s:%s", keyring.UID, keyring.ResourceVersion)
	return &dependencies, nil
}
