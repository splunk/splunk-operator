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

package resources

import (
	"maps"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewKVServiceTenantConfig builds the non-sensitive runtime membership projection.
func NewKVServiceTenantConfig(cr *enterpriseApi.KVService, mapping map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "splunk-" + cr.Name + "-kvservice-tenants", Namespace: cr.Namespace,
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(cr, enterpriseApi.GroupVersion.WithKind("KVService"))},
	}, Data: maps.Clone(mapping)}
}

// NewKVServiceTenantDatabase retains both the database CR and SQL data when its
// KVService is deleted. The annotation records management without a GC owner.
func NewKVServiceTenantDatabase(cr *enterpriseApi.KVService, tenant string) *platformApi.PostgresDatabase {
	return &platformApi.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{
		Name: util.KVServiceTenantDatabaseName(tenant), Namespace: cr.Namespace,
		Annotations: map[string]string{common.KVServiceTenantDatabaseOwnerAnnotation: cr.Namespace + "/" + cr.Name},
	}, Spec: platformApi.PostgresDatabaseSpec{
		ClusterRef: cr.Spec.Postgres.ClusterRef,
		Databases:  []platformApi.DatabaseDefinition{{Name: tenant, DeletionPolicy: "Retain", Extensions: []string{"pg_stat_statements", "pgcrypto"}}},
	}}
}

// NewKVServiceTenantKeyring builds the namespace-shared request keyring.
func NewKVServiceTenantKeyring(cr *enterpriseApi.KVService) *corev1.Secret {
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: common.KVServiceTenantVerificationKeysName, Namespace: cr.Namespace,
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(cr, enterpriseApi.GroupVersion.WithKind("KVService"))},
	}, Type: corev1.SecretTypeOpaque}
}

// KVServiceTenantCredential identifies a tenant's database-scoped owner Secret.
type KVServiceTenantCredential struct {
	Tenant    string
	SecretRef corev1.SecretKeySelector
}

// KVServiceTenantRuntime contains only pod wiring, never credential values.
type KVServiceTenantRuntime struct {
	Volumes      []corev1.Volume
	VolumeMounts []corev1.VolumeMount
	Environment  []corev1.EnvVar
}

// NewKVServiceTenantRuntime builds membership, keyring and database password mounts.
// Directory mounts allow runtime rereads after Kubernetes projects file updates.
// Reconciliation also requests rollouts for membership and keyring changes.
// Explicit modes avoid API-default rollout churn.
func NewKVServiceTenantRuntime(membership, keyring, serviceDatabase string, credentials []KVServiceTenantCredential) KVServiceTenantRuntime {
	mode := int32(0644)
	runtime := KVServiceTenantRuntime{
		Volumes: []corev1.Volume{
			{Name: "cmpk-tenant-map", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: membership}, DefaultMode: &mode}}},
			{Name: "cmpk-tenant-verification-keys", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: keyring, DefaultMode: &mode}}},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "cmpk-tenant-map", MountPath: "/var/run/kvservice/tenant-map", ReadOnly: true},
			{Name: "cmpk-tenant-verification-keys", MountPath: "/var/run/kvservice/tenant-verification-keys", ReadOnly: true},
		},
		Environment: []corev1.EnvVar{
			{Name: "KVSERVICE_CMPK_SERVICE_DATABASE", Value: serviceDatabase},
			{Name: "KVSERVICE_CMPK_TENANT_MAP_DIR", Value: "/var/run/kvservice/tenant-map"},
			{Name: "KVSERVICE_CMPK_TENANT_PASSWORD_DIR", Value: "/var/run/kvservice/tenant-passwords"},
			{Name: "KVSERVICE_CMPK_TENANT_VERIFICATION_KEYS_DIR", Value: "/var/run/kvservice/tenant-verification-keys"},
		},
	}
	for _, credential := range credentials {
		name := "cmpk-password-" + credential.Tenant
		runtime.Volumes = append(runtime.Volumes, corev1.Volume{Name: name, VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
			SecretName: credential.SecretRef.Name, DefaultMode: &mode, Items: []corev1.KeyToPath{{Key: credential.SecretRef.Key, Path: "password"}},
		}}})
		runtime.VolumeMounts = append(runtime.VolumeMounts, corev1.VolumeMount{Name: name, MountPath: "/var/run/kvservice/tenant-passwords/" + credential.Tenant, ReadOnly: true})
	}
	return runtime
}
