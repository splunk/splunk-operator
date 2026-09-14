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
package k8s

import (
	"context"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClusterSnapshot is the provider-shaped result of one Kubernetes read. The
// database adapter translates it into consumer-owned facts.
type ClusterSnapshot struct {
	Name      string
	Namespace string

	Phase          *string
	Conditions     []metav1.Condition
	ProvisionerRef *corev1.ObjectReference

	ManagedRolesStatus     *platformv1alpha1.ManagedRolesStatus
	ConnectionPoolerStatus *platformv1alpha1.ConnectionPoolerStatus
	Resources              *platformv1alpha1.PostgresClusterResources
	CustomMetricsStatus    *platformv1alpha1.CustomMetricsStatus
}

// ClusterReader performs Kubernetes reads for PostgresCluster snapshots.
type ClusterReader struct {
	reader client.Reader
}

// NewClusterReader returns a Kubernetes-backed PostgresCluster reader.
func NewClusterReader(reader client.Reader) ClusterReader {
	return ClusterReader{reader: reader}
}

// Read returns the status fields consumed by the database adapter.
func (r ClusterReader) Read(ctx context.Context, namespace, name string) (ClusterSnapshot, error) {
	cluster := &platformv1alpha1.PostgresCluster{}
	if err := r.reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, cluster); err != nil {
		return ClusterSnapshot{}, err
	}

	status := cluster.Status.DeepCopy()
	return ClusterSnapshot{
		Name:                   cluster.Name,
		Namespace:              cluster.Namespace,
		Phase:                  status.Phase,
		Conditions:             status.Conditions,
		ProvisionerRef:         status.ProvisionerRef,
		ManagedRolesStatus:     status.ManagedRolesStatus,
		ConnectionPoolerStatus: status.ConnectionPoolerStatus,
		Resources:              status.Resources,
		CustomMetricsStatus:    status.CustomMetricsStatus,
	}, nil
}
