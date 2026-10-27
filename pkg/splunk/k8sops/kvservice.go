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

package k8sops

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/logging"
	pgdatabase "github.com/splunk/splunk-operator/pkg/postgresql/database/core"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
)

const (
	// KVServicePostgresDefaultDatabaseResource is the default existing PostgresDatabase name.
	KVServicePostgresDefaultDatabaseResource = "kvstore-database"
	// KVServicePostgresDefaultDatabaseName is the default logical database name.
	KVServicePostgresDefaultDatabaseName = "kvstore"
	kvServicePass4SymmKey                = "pass4SymmKey"
)

// KVServicePostgresDependencies contains the validated PostgreSQL workload
// configuration resolved during KVService reconciliation.
type KVServicePostgresDependencies struct {
	Environment             []corev1.EnvVar
	ConfigHash              string
	AdminUserSecretRevision string
	Pass4SymmKeyRevision    string
}

// KVServicePostgresDatabaseResourceName returns the configured or default database resource name.
func KVServicePostgresDatabaseResourceName(cr *enterpriseApi.KVService) string {
	if cr == nil || cr.Spec.Postgres == nil || cr.Spec.Postgres.DatabaseRef == nil {
		return KVServicePostgresDefaultDatabaseResource
	}
	return cr.Spec.Postgres.DatabaseRef.Name
}

// KVServicePostgresLogicalDatabaseName returns the configured or default logical database name.
func KVServicePostgresLogicalDatabaseName(cr *enterpriseApi.KVService) string {
	if cr == nil || cr.Spec.Postgres == nil {
		return KVServicePostgresDefaultDatabaseName
	}
	return cmp.Or(cr.Spec.Postgres.DatabaseName, KVServicePostgresDefaultDatabaseName)
}

// GetKVServiceDeployment resolves API-backed configuration before returning a complete workload.
func GetKVServiceDeployment(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService, extraEnv []corev1.EnvVar) (*appsv1.Deployment, error) {
	var postgresDependencies *KVServicePostgresDependencies
	var err error
	if cr.Spec.Postgres != nil {
		database := &platformApi.PostgresDatabase{}
		databaseKey := types.NamespacedName{
			Namespace: cr.Namespace,
			Name:      KVServicePostgresDatabaseResourceName(cr),
		}
		if err := c.Get(ctx, databaseKey, database); err != nil {
			return nil, fmt.Errorf("get PostgresDatabase %s for KVService deployment: %w", databaseKey, err)
		}
		var ready bool
		postgresDependencies, ready, err = ResolveKVServicePostgresDependencies(ctx, c, cr, database, KVServicePostgresLogicalDatabaseName(cr))
		if err != nil {
			return nil, fmt.Errorf("resolve PostgreSQL dependencies for KVService %q: %w", cr.Name, err)
		}
		if !ready {
			return nil, fmt.Errorf("postgres database resource %q does not publish ready connection references for database %q", database.Name, KVServicePostgresLogicalDatabaseName(cr))
		}
	}
	return getKVServiceDeployment(ctx, c, cr, postgresDependencies, extraEnv)
}

// GetKVServiceDeploymentWithPostgresDependencies builds a Deployment using the
// dependency snapshot already validated by the KVService reconciler.
func GetKVServiceDeploymentWithPostgresDependencies(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService, postgresDependencies *KVServicePostgresDependencies, extraEnv []corev1.EnvVar) (*appsv1.Deployment, error) {
	if cr.Spec.Postgres != nil && postgresDependencies == nil {
		return nil, fmt.Errorf("validated PostgreSQL dependencies are required for KVService %q", cr.Name)
	}
	return getKVServiceDeployment(ctx, c, cr, postgresDependencies, extraEnv)
}

func getKVServiceDeployment(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService, postgresDependencies *KVServicePostgresDependencies, extraEnv []corev1.EnvVar) (*appsv1.Deployment, error) {
	desired := resources.NewKVServiceDeployment(cr)
	current := &appsv1.Deployment{}
	err := c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: desired.Name}, current)
	if err != nil && !k8serrors.IsNotFound(err) {
		return nil, fmt.Errorf("get KVService deployment %q: %w", desired.Name, err)
	}
	if err == nil {
		if owner := metav1.GetControllerOf(current); owner != nil && owner.UID != cr.UID {
			return nil, fmt.Errorf("deployment %s is controlled by another resource", current.Name)
		}
		desired.ObjectMeta = *current.ObjectMeta.DeepCopy()
		found := false
		for _, ref := range desired.OwnerReferences {
			if ref.UID == cr.UID {
				found = true
			}
		}
		if !found {
			desired.OwnerReferences = append(desired.OwnerReferences, splcommon.AsOwner(cr, true))
		}
	}
	if cr.Spec.ServiceAccount != "" {
		if _, err := GetServiceAccount(ctx, c, types.NamespacedName{Namespace: cr.Namespace, Name: cr.Spec.ServiceAccount}); err != nil {
			return nil, fmt.Errorf("get KVService service account %q: %w", cr.Spec.ServiceAccount, err)
		}
	}
	configureKVServiceConfigMapRevisions(ctx, c, &desired.Spec.Template, cr)
	desired.Spec.Template.Spec.Containers[0].Env = append(desired.Spec.Template.Spec.Containers[0].Env, extraEnv...)
	if postgresDependencies != nil {
		desired.Spec.Template.Spec.Containers[0].Env = append(desired.Spec.Template.Spec.Containers[0].Env, postgresDependencies.Environment...)
	}
	if cr.Spec.Postgres != nil {
		annotateKVServicePostgresDependencies(&desired.Spec.Template, postgresDependencies)
	}
	return desired, nil
}

func annotateKVServicePostgresDependencies(template *corev1.PodTemplateSpec, dependencies *KVServicePostgresDependencies) {
	if template.Annotations == nil {
		template.Annotations = make(map[string]string)
	}
	template.Annotations["enterprise.splunk.com/kvservice-postgres-config"] = dependencies.ConfigHash
	template.Annotations["enterprise.splunk.com/kvservice-postgres-admin-secret"] = dependencies.AdminUserSecretRevision
	template.Annotations["enterprise.splunk.com/kvservice-pass4-secret"] = dependencies.Pass4SymmKeyRevision
}

func hashKVServiceData(data map[string]string, keys ...string) string {
	slices.Sort(keys)
	h := sha256.New()
	for _, key := range keys {
		fmt.Fprintf(h, "%d:%s:%d:", len(key), key, len(data[key]))
		_, _ = h.Write([]byte(data[key]))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// kvServiceSecretRevision identifies a Secret snapshot without exposing a
// fingerprint of credential data. Metadata-only updates also trigger a rollout.
func kvServiceSecretRevision(secret *corev1.Secret, key string) string {
	return fmt.Sprintf("%s/%s:%s:%s:%s", secret.Namespace, secret.Name, secret.UID, secret.ResourceVersion, key)
}

// ResolveKVServicePostgresDependencies validates one database status snapshot and
// resolves the environment, ConfigMap hash, and Secret revision metadata.
func ResolveKVServicePostgresDependencies(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService, database *platformApi.PostgresDatabase, databaseName string) (*KVServicePostgresDependencies, bool, error) {
	infoIndex := slices.IndexFunc(database.Status.Databases, func(info platformApi.DatabaseInfo) bool {
		return info.Name == databaseName
	})
	if infoIndex < 0 {
		return nil, false, nil
	}
	info := &database.Status.Databases[infoIndex]
	if !info.Ready || info.ConfigMapRef == nil || info.AdminUserSecretRef == nil {
		return nil, false, nil
	}
	configMap := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: info.ConfigMapRef.Name}, configMap); err != nil {
		return nil, false, fmt.Errorf("get PostgreSQL connection ConfigMap %q: %w", info.ConfigMapRef.Name, err)
	}
	for _, key := range []string{pgconninfo.KeyClusterRWEndpoint, pgconninfo.KeyDefaultClusterPort, pgdatabase.ConfigMapKeyAdminUser} {
		if configMap.Data[key] == "" {
			return nil, false, fmt.Errorf("config map %q must contain non-empty key %q for KVService PostgreSQL configuration", configMap.Name, key)
		}
	}
	adminSecret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: info.AdminUserSecretRef.Name}, adminSecret); err != nil {
		return nil, false, fmt.Errorf("get PostgreSQL admin Secret %q: %w", info.AdminUserSecretRef.Name, err)
	}
	adminPassword := adminSecret.Data[info.AdminUserSecretRef.Key]
	if len(adminPassword) == 0 {
		return nil, false, fmt.Errorf("secret %q must contain non-empty key %q for KVService PostgreSQL configuration", adminSecret.Name, info.AdminUserSecretRef.Key)
	}
	namespaceSecret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: splcommon.GetNamespaceScopedSecretName(cr.Namespace)}, namespaceSecret); err != nil {
		return nil, false, fmt.Errorf("get namespace authentication Secret for KVService %q: %w", cr.Name, err)
	}
	if len(namespaceSecret.Data[kvServicePass4SymmKey]) == 0 {
		return nil, false, fmt.Errorf("namespace Secret %q must contain non-empty key %q", namespaceSecret.Name, kvServicePass4SymmKey)
	}
	configMapRef := corev1.LocalObjectReference{Name: info.ConfigMapRef.Name}
	adminUserSecretRef := info.AdminUserSecretRef
	namespaceSecretName := splcommon.GetNamespaceScopedSecretName(cr.Namespace)
	env := []corev1.EnvVar{
		{Name: "KVSERVICE_SINGLE_TENANT_K8S_MODE", Value: "true"},
		{Name: "KVSERVICE_SINGLE_TENANT_PASS4_SYMM_KEY", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: namespaceSecretName}, Key: kvServicePass4SymmKey}}},
		{Name: "KVSERVICE_DB_HOST", ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: configMapRef, Key: pgconninfo.KeyClusterRWEndpoint}}},
		{Name: "KVSERVICE_DB_PORT", ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: configMapRef, Key: pgconninfo.KeyDefaultClusterPort}}},
		{Name: "KVSERVICE_SINGLE_TENANT_PRIMARY_HOST", ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: configMapRef, Key: pgconninfo.KeyClusterRWEndpoint}}},
		{Name: "KVSERVICE_SINGLE_TENANT_PRIMARY_PORT", ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: configMapRef, Key: pgconninfo.KeyDefaultClusterPort}}},
		{Name: "KVSERVICE_SINGLE_TENANT_REPLICA_HOST", ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: configMapRef, Key: pgconninfo.KeyClusterRWEndpoint}}},
		{Name: "KVSERVICE_SINGLE_TENANT_REPLICA_PORT", ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: configMapRef, Key: pgconninfo.KeyDefaultClusterPort}}},
		{Name: "KVSERVICE_DB_ROOT_USER", ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: configMapRef, Key: pgdatabase.ConfigMapKeyAdminUser}}},
		{Name: "KVSERVICE_DB_SERVICE_USER", ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: configMapRef, Key: pgdatabase.ConfigMapKeyAdminUser}}},
		{Name: "KVSERVICE_DB_PASSWORD", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: adminUserSecretRef}},
		{Name: "KVSERVICE_DB_SERVICE_PASSWORD", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: adminUserSecretRef}},
	}
	return &KVServicePostgresDependencies{
		Environment:             env,
		ConfigHash:              hashKVServiceData(configMap.Data, pgconninfo.KeyClusterRWEndpoint, pgconninfo.KeyDefaultClusterPort, pgdatabase.ConfigMapKeyAdminUser),
		AdminUserSecretRevision: kvServiceSecretRevision(adminSecret, info.AdminUserSecretRef.Key),
		Pass4SymmKeyRevision:    kvServiceSecretRevision(namespaceSecret, kvServicePass4SymmKey),
	}, true, nil
}

// UpdateKVServiceStatus retries conflicts without overwriting concurrent spec updates.
func UpdateKVServiceStatus(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &enterpriseApi.KVService{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name}, latest); err != nil {
			return fmt.Errorf("get KVService %q for status update: %w", cr.Name, err)
		}
		cr.Status.DeepCopyInto(&latest.Status)
		if err := c.Status().Update(ctx, latest); err != nil {
			return fmt.Errorf("update KVService %q status: %w", cr.Name, err)
		}
		return nil
	})
}

// DeleteKVServiceCR removes the current CR as an owner of the shared KVService CR.
// If the shared KVService has no owners left, it is deleted.
func DeleteKVServiceCR(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject) error {
	logger := logging.FromContext(ctx).With("func", "DeleteKVServiceCR", "kind", cr.GetObjectKind().GroupVersionKind().Kind, "name", cr.GetName(), "namespace", cr.GetNamespace())

	kvService := &enterpriseApi.KVService{}
	namespacedName := types.NamespacedName{
		Namespace: cr.GetNamespace(),
		Name:      splcommon.KVServiceCrName,
	}
	if err := c.Get(ctx, namespacedName, kvService); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		logger.ErrorContext(ctx, "failed to get KVService CR", "error", err)
		return fmt.Errorf("get shared KVService %s: %w", namespacedName, err)
	}

	if kvService.GetUID() == cr.GetUID() {
		return nil
	}

	ownerRefs := kvService.GetOwnerReferences()
	remainingRefs := removeOwnerReference(ownerRefs, cr)
	if len(remainingRefs) == len(ownerRefs) {
		return nil
	}

	if len(remainingRefs) == 0 {
		if err := c.Delete(ctx, kvService); err != nil && !k8serrors.IsNotFound(err) {
			logger.ErrorContext(ctx, "failed to delete KVService CR", "error", err)
			return fmt.Errorf("delete shared KVService %s: %w", namespacedName, err)
		}
		return nil
	}

	kvService.SetOwnerReferences(remainingRefs)
	if err := c.Update(ctx, kvService); err != nil {
		logger.ErrorContext(ctx, "failed to update KVService owner references", "error", err)
		return fmt.Errorf("update shared KVService %s owner references: %w", namespacedName, err)
	}
	return nil
}

func removeOwnerReference(ownerRefs []metav1.OwnerReference, cr splcommon.MetaObject) []metav1.OwnerReference {
	remainingRefs := make([]metav1.OwnerReference, 0, len(ownerRefs))
	for _, ref := range ownerRefs {
		if ref.UID != cr.GetUID() {
			remainingRefs = append(remainingRefs, ref)
		}
	}
	return remainingRefs
}

// configureKVServiceConfigMapRevisions stamps content hashes for customer-provided ConfigMap volumes.
func configureKVServiceConfigMapRevisions(ctx context.Context, client splcommon.ControllerClient, podTemplateSpec *corev1.PodTemplateSpec, cr *enterpriseApi.KVService) {
	logger := logging.FromContext(ctx).With("func", "configureKVServiceConfigMapRevisions")
	spec := &cr.Spec
	// Stamp splcommon.ConfigMapRevAnnotationPrefix+<vol-name> annotation for each user-supplied
	// ConfigMap volume using a content hash rather than ResourceVersion. ResourceVersion changes
	// on any metadata update (labels, annotations) and would cause spurious pod rolls; the hash
	// only changes when the mounted data itself changes.
	// The annotation key uses the volume name (a valid DNS label, ≤63 chars) as the suffix,
	// not the ConfigMap name, which can exceed Kubernetes' 63-char annotation-suffix limit.
	// Projected volumes that reference ConfigMaps are handled via the Sources loop.
	for _, vol := range spec.Volumes {
		switch {
		case vol.ConfigMap != nil:
			cmNS := types.NamespacedName{Namespace: cr.GetNamespace(), Name: vol.ConfigMap.Name}
			cm, err := GetConfigMap(ctx, client, cmNS)
			if err != nil {
				logger.ErrorContext(ctx, "Failed to fetch ConfigMap for restart annotation", "volume", vol.Name, "error", err)
				break
			}
			if cm.Annotations[splcommon.ConfigMapRestartOptOutAnnotation] == "false" {
				// Consumer handles dynamic reload; skip the restart-triggering annotation.
				break
			}
			hash, err := GetConfigMapDataHash(ctx, client, cmNS, vol.ConfigMap.Items)
			if err == nil {
				podTemplateSpec.ObjectMeta.Annotations[splcommon.ConfigMapRevAnnotationPrefix+vol.Name] = hash
			} else {
				logger.ErrorContext(ctx, "Failed to get ConfigMap data hash for annotation", "volume", vol.Name, "error", err)
			}
		case vol.Projected != nil:
			for i, src := range vol.Projected.Sources {
				if src.ConfigMap == nil {
					continue
				}
				cmNS := types.NamespacedName{Namespace: cr.GetNamespace(), Name: src.ConfigMap.Name}
				cm, err := GetConfigMap(ctx, client, cmNS)
				if err != nil {
					logger.ErrorContext(ctx, "Failed to fetch projected ConfigMap for restart annotation", "volume", vol.Name, "configMap", src.ConfigMap.Name, "error", err)
					continue
				}
				if cm.Annotations[splcommon.ConfigMapRestartOptOutAnnotation] == "false" {
					continue
				}
				hash, err := GetConfigMapDataHash(ctx, client, cmNS, src.ConfigMap.Items)
				if err == nil {
					// Build a collision-free annotation key suffix ≤63 chars.
					// vol.Name is a DNS label (≤63 chars); appending ".<n>" can push past the
					// Kubernetes annotation name-segment limit. When the combined length exceeds
					// 63, replace vol.Name with "p.<8-hex-digest>" — the "p." prefix contains a
					// dot, which is legal in annotation name segments but cannot appear in a
					// Kubernetes DNS-label volume name, making hashed keys structurally distinct
					// from any real short volume name and preventing false collisions.
					idxStr := strconv.Itoa(i)
					volNamePart := vol.Name
					if len(volNamePart)+1+len(idxStr) > 63 {
						sum := sha256.Sum256([]byte(vol.Name))
						volNamePart = "p." + hex.EncodeToString(sum[:])[:8]
					}
					podTemplateSpec.ObjectMeta.Annotations[splcommon.ConfigMapRevAnnotationPrefix+volNamePart+"."+idxStr] = hash
				} else {
					logger.ErrorContext(ctx, "Failed to get ConfigMap data hash for projected annotation", "volume", vol.Name, "configMap", src.ConfigMap.Name, "error", err)
				}
			}
		}
	}

}
