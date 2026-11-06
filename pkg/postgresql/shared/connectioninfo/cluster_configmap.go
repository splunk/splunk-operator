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
package connectioninfo

import (
	"maps"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	KeySuperUserName      = "SUPER_USER_NAME"
	KeySuperUserSecretRef = "SUPER_USER_SECRET_REF"
	DefaultSuperUserName  = "postgres"
	ServerCAKey           = "ca.crt"
	ClusterManagedBy      = "postgrescluster-controller"
)

// ClusterAccessKeys is the non-sensitive credential/CA selector schema.
func ClusterAccessKeys() []string {
	return []string{KeySuperUserName, KeySuperUserSecretRef, KeyServerCASecretRef}
}

// ClusterAccessMetadata publishes selectors, never credential values.
func ClusterAccessMetadata(user, secretRef, serverCARef string) map[string]string {
	data := map[string]string{KeySuperUserName: user, KeySuperUserSecretRef: secretRef}
	if serverCARef != "" {
		data[KeyServerCASecretRef] = serverCARef
	}
	return data
}

// ClusterEnvironmentAccess uses the standard CNPG superuser and CA key.
func ClusterEnvironmentAccess(secretName, caSecretName string) map[string]string {
	return ClusterAccessMetadata(DefaultSuperUserName, secretName, caSecretName+"/"+ServerCAKey)
}

func RequiredClusterKeys() []string {
	return append(RequiredKeys(), KeySuperUserName, KeySuperUserSecretRef)
}

// BuildClusterConfigMapData applies the same schema as canonical recovery.
func BuildClusterConfigMapData(endpoints Endpoints, user, secretRef, serverCARef string) (map[string]string, []string, error) {
	return BuildConfigMapData(endpoints, func(b *Builder) {
		b.SetRequired(KeySuperUserName, user)
		b.SetRequired(KeySuperUserSecretRef, secretRef)
		b.SetOptional(KeyServerCASecretRef, serverCARef)
	})
}

// NewClusterConfigMap creates the canonical projection with consistent ownership
// and management metadata. Callers apply their validated routing/publication stamp.
func NewClusterConfigMap(parent *platformv1alpha1.PostgresCluster, name string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: parent.Namespace,
		Labels:          map[string]string{"app.kubernetes.io/managed-by": ClusterManagedBy},
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))},
	}, Data: maps.Clone(data)}
}
