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

package majorupgradeadapter

import (
	"context"
	"slices"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestCandidatePoolerEndpointsResolvePreparedTopology(t *testing.T) {
	for _, tc := range []struct {
		name     string
		sides    []cnpgv1.PoolerType
		selected cnpgv1.PoolerType
	}{
		{name: "RO only", sides: []cnpgv1.PoolerType{cnpgv1.PoolerTypeRO}, selected: cnpgv1.PoolerTypeRO},
		{name: "RW only", sides: []cnpgv1.PoolerType{cnpgv1.PoolerTypeRW}, selected: cnpgv1.PoolerTypeRW},
		{name: "RO before RW", sides: []cnpgv1.PoolerType{cnpgv1.PoolerTypeRO, cnpgv1.PoolerTypeRW}, selected: cnpgv1.PoolerTypeRW},
		{name: "RW before RO", sides: []cnpgv1.PoolerType{cnpgv1.PoolerTypeRW, cnpgv1.PoolerTypeRO}, selected: cnpgv1.PoolerTypeRW},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, appsv1.AddToScheme(scheme))
			require.NoError(t, cnpgv1.AddToScheme(scheme))
			rwEnabled := slices.Contains(tc.sides, cnpgv1.PoolerTypeRW)
			roEnabled := slices.Contains(tc.sides, cnpgv1.PoolerTypeRO)
			parent := &platformv1alpha1.PostgresCluster{
				Name: "orders", Namespace: "default", UID: "parent-uid",
				Status: platformv1alpha1.PostgresClusterStatus{ConnectionPoolerStatus: &platformv1alpha1.ConnectionPoolerStatus{
					Enabled: true, ReadWriteEnabled: rwEnabled, ReadOnlyEnabled: roEnabled,
				}},
			}
			source := &cnpgv1.Cluster{Name: "blue", Namespace: parent.Namespace}
			green := &cnpgv1.Cluster{
				Name: "green", Namespace: parent.Namespace, UID: "green-uid",
				Spec:   cnpgv1.ClusterSpec{Instances: 2},
				Status: cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy, ReadyInstances: 2, WriteService: "green-rw", ReadService: "green-r"},
			}
			preparer := &BlueGreenCandidatePreparer{cluster: parent}
			objects := []client.Object{green}
			order := make(map[cnpgv1.PoolerType]int, len(tc.sides))
			for index, side := range tc.sides {
				order[side] = index
				sourcePooler := &cnpgv1.Pooler{
					Name: "blue-pooler-" + string(side), Namespace: parent.Namespace,
					OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))},
					Spec: cnpgv1.PoolerSpec{Cluster: cnpgv1.LocalObjectReference{Name: source.Name}, Type: side,
						Instances: new(int32(1)), PgBouncer: &cnpgv1.PgBouncerSpec{PoolMode: cnpgv1.PgBouncerPoolModeSession}},
				}
				pooler := preparer.buildCandidatePooler(sourcePooler, green.Name, candidatePoolerName(green.Name, string(side)), "attempt-1")
				pooler.UID = types.UID(pooler.Name + "-uid")
				pooler.Status = cnpgv1.PoolerStatus{Phase: cnpgv1.PoolerPhaseActive, Instances: 1}
				selector := map[string]string{cnpgutils.PgbouncerNameLabel: pooler.Name}
				owner := []metav1.OwnerReference{*metav1.NewControllerRef(pooler, cnpgv1.SchemeGroupVersion.WithKind("Pooler"))}
				service := &corev1.Service{
					Name: pooler.Name, Namespace: parent.Namespace, UID: types.UID(pooler.Name + "-service-uid"), OwnerReferences: owner,
					Spec: corev1.ServiceSpec{Selector: selector, Ports: []corev1.ServicePort{{
						Name: "pgbouncer", Port: 5432, TargetPort: intstr.FromString("pgbouncer"), Protocol: corev1.ProtocolTCP,
					}}},
				}
				deployment := &appsv1.Deployment{
					Name: pooler.Name, Namespace: parent.Namespace, UID: types.UID(pooler.Name + "-deployment-uid"), Generation: 1, OwnerReferences: owner,
					Spec: appsv1.DeploymentSpec{Replicas: new(int32(1)), Selector: &metav1.LabelSelector{MatchLabels: selector},
						Template: corev1.PodTemplateSpec{Labels: selector, Spec: corev1.PodSpec{Containers: []corev1.Container{{
							Name: "pgbouncer", Image: "pgbouncer:test", Ports: []corev1.ContainerPort{{Name: "pgbouncer", ContainerPort: 5432}},
						}}}}},
					Status: appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1},
				}
				objects = append(objects, sourcePooler, pooler, service, deployment)
			}
			for _, side := range []string{"rw", "r", "ro"} {
				selector := map[string]string{"cnpg.io/cluster": green.Name}
				switch side {
				case "rw":
					selector["cnpg.io/instanceRole"] = "primary"
				case "ro":
					selector["cnpg.io/instanceRole"] = "replica"
				default:
					selector["cnpg.io/podRole"] = "instance"
				}
				objects = append(objects, &corev1.Service{
					Name: "green-" + side, Namespace: parent.Namespace, UID: types.UID("green-" + side + "-uid"),
					Spec: corev1.ServiceSpec{Selector: selector, Ports: []corev1.ServicePort{{
						Name: "postgres", Port: 5432, TargetPort: intstr.FromInt32(5432), Protocol: corev1.ProtocolTCP,
					}}},
				})
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if err := c.List(ctx, list, opts...); err != nil {
						return err
					}
					if poolers, ok := list.(*cnpgv1.PoolerList); ok {
						slices.SortFunc(poolers.Items, func(a, b cnpgv1.Pooler) int { return order[a.Spec.Type] - order[b.Spec.Type] })
					}
					return nil
				},
			}).Build()
			preparer.client = c
			status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{Green: &platformv1alpha1.BlueGreenEnvironmentStatus{
				Ref: corev1.ObjectReference{Name: green.Name, UID: green.UID},
			}}
			resolver := &EndpointProofResolver{Reader: c}
			for range 2 {
				ready, err := preparer.reconcileCandidateEndpoints(t.Context(), source, green, "attempt-1", status)
				require.NoError(t, err)
				require.True(t, ready)
				require.NotNil(t, status.CandidateEndpoints.PoolerService)
				require.Equal(t, "green-pooler-"+string(tc.selected), status.CandidateEndpoints.PoolerService.Name)
				data, uid, err := resolver.ResolveGreen(t.Context(), parent, status)
				require.NoError(t, err)
				require.Equal(t, green.UID, uid)
				require.Equal(t, rwEnabled, data[pgconninfo.KeyPoolerRWEndpoint] != "")
				require.Equal(t, roEnabled, data[pgconninfo.KeyPoolerROEndpoint] != "")
			}
		})
	}
}
