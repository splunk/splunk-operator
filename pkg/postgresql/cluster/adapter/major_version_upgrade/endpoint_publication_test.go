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
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEndpointProofResolverUsesGreenWithoutBlueAfterCommit(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	green := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "green", Namespace: "ns", UID: "green-uid"},
		Spec:       cnpgv1.ClusterSpec{Instances: 1},
		Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy, ReadyInstances: 1, WriteService: "green-rw"},
	}
	objects := []client.Object{green}
	portName := "postgres"
	portNumber := int32(5432)
	protocol := corev1.ProtocolTCP
	for _, side := range []string{"rw", "r"} {
		name := "green-" + side
		selector := map[string]string{"cnpg.io/cluster": "green"}
		if side == "rw" {
			selector["cnpg.io/instanceRole"] = "primary"
		} else {
			selector["cnpg.io/podRole"] = "instance"
		}
		uid := types.UID(name + "-uid")
		objects = append(objects,
			&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", UID: uid}, Spec: corev1.ServiceSpec{Selector: selector, Ports: []corev1.ServicePort{{Name: portName, Protocol: protocol, Port: portNumber, TargetPort: intstr.FromInt32(portNumber)}}}},
		)
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	resolver := &EndpointProofResolver{Reader: reader}
	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "ns"}}
	bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		Blue:               &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "blue", UID: "blue-uid"}},
		Green:              &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: green.UID}},
		CandidateEndpoints: &platformv1alpha1.BlueGreenCandidateEndpointsStatus{DirectService: &corev1.ObjectReference{Name: "green-rw", Namespace: "ns", UID: "green-rw-uid"}},
	}
	data, uid, err := resolver.ResolveGreen(t.Context(), parent, bg)
	require.NoError(t, err)
	require.Equal(t, green.UID, uid)
	require.Equal(t, "green-rw.ns.svc.cluster.local", data[pgconninfo.KeyClusterRWEndpoint])
	_, err = resolver.Resolve(t.Context(), parent, bg)
	require.Error(t, err, "precommit proof still requires blue")
	blue := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "blue", Namespace: "ns", UID: "blue-uid"},
		Status:     cnpgv1.ClusterStatus{WriteService: "blue-rw", ReadyInstances: 1},
	}
	require.NoError(t, reader.Create(t.Context(), blue))
	proof, err := resolver.Resolve(t.Context(), parent, bg)
	require.NoError(t, err)
	require.Equal(t, "blue-rw.ns.svc.cluster.local", proof.Blue[pgconninfo.KeyClusterRWEndpoint])
	require.Equal(t, green.UID, proof.GreenUID)
	require.Equal(t, data, proof.Green)

	bg.Green.Ref.Name = "missing"
	_, err = resolver.Resolve(t.Context(), parent, bg)
	require.Error(t, err, "a missing target Cluster must block a combined proof")
	bg.Green.Ref.Name = "green"
	bg.CandidateEndpoints.DirectService = nil
	_, _, err = resolver.ResolveGreen(t.Context(), parent, bg)
	require.ErrorContains(t, err, "direct Service reference are required")
	bg.CandidateEndpoints.DirectService = &corev1.ObjectReference{Name: "green-rw", Namespace: "ns", UID: "green-rw-uid"}
	parent.Status.ConnectionPoolerStatus = &platformv1alpha1.ConnectionPoolerStatus{Enabled: true}
	poolerData, _, err := resolver.ResolveGreen(t.Context(), parent, bg)
	require.NoError(t, err)
	require.Contains(t, poolerData, pgconninfo.KeyPoolerRWEndpoint)
	require.Contains(t, poolerData, pgconninfo.KeyPoolerROEndpoint)

	bg.Green.Ref.UID = "recreated-uid"
	_, _, err = resolver.ResolveGreen(t.Context(), parent, bg)
	require.ErrorContains(t, err, "identity or health changed")
	_, _, err = (&EndpointProofResolver{}).ResolveGreen(t.Context(), parent, bg)
	require.ErrorContains(t, err, "API reader")
}

func TestEndpointProofResolverRejectsBlueIdentityAndIncompleteRouting(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	blue := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "blue", Namespace: "ns", UID: "live-uid"},
		Status:     cnpgv1.ClusterStatus{WriteService: "blue-rw", ReadyInstances: 2},
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(blue).Build()
	resolver := &EndpointProofResolver{Reader: reader}
	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "ns"}}
	bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		Blue: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "blue", UID: blue.UID}},
	}
	_, err := resolver.ResolveBlue(t.Context(), parent, bg)
	require.ErrorContains(t, err, "read service name is required")

	bg.Blue.Ref.UID = "stale-uid"
	_, err = resolver.ResolveBlue(t.Context(), parent, bg)
	require.ErrorContains(t, err, "UID changed")

	bg.Blue.Ref.Name = "missing"
	_, err = resolver.ResolveBlue(t.Context(), parent, bg)
	require.Error(t, err, "a missing source Cluster cannot produce proof")

	bg.Blue = nil
	_, err = resolver.ResolveBlue(t.Context(), parent, bg)
	require.ErrorContains(t, err, "identity and API reader")

	bg.Blue = &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "blue", UID: blue.UID}}
	blue.Status.ReadService = "blue-ro"
	require.NoError(t, reader.Update(t.Context(), blue))
	parent.Status.ConnectionPoolerStatus = &platformv1alpha1.ConnectionPoolerStatus{Enabled: true, ReadWriteEnabled: true, ReadOnlyEnabled: true}
	data, err := resolver.ResolveBlue(t.Context(), parent, bg)
	require.NoError(t, err)
	require.Equal(t, "blue-ro.ns.svc.cluster.local", data[pgconninfo.KeyClusterROEndpoint])
	require.Equal(t, "blue-pooler-rw.ns.svc.cluster.local", data[pgconninfo.KeyPoolerRWEndpoint])
	require.Equal(t, "blue-pooler-ro.ns.svc.cluster.local", data[pgconninfo.KeyPoolerROEndpoint])
}

func TestEndpointCoordinatorStoreReadsObjectsAndWritesStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "ns", UID: "parent-uid"}}
	child := &platformv1alpha1.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "database", Namespace: "ns", UID: "db-uid"}}
	other := &platformv1alpha1.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "other-ns"}}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "database-conn", Namespace: "ns", UID: "cm-uid"}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent, child, other, cm).WithStatusSubresource(parent).Build()
	store := &EndpointCoordinatorStore{Writer: client, Reader: client}
	ctx := t.Context()

	gotParent, err := store.ReadParent(ctx, types.NamespacedName{Namespace: "ns", Name: "parent"})
	require.NoError(t, err)
	require.Equal(t, parent.UID, gotParent.UID)
	children, err := store.ListChildren(ctx, "ns")
	require.NoError(t, err)
	require.Len(t, children, 1)
	require.Equal(t, child.UID, children[0].UID)
	gotCM, err := store.ReadConfigMap(ctx, types.NamespacedName{Namespace: "ns", Name: "database-conn"})
	require.NoError(t, err)
	require.Equal(t, cm.UID, gotCM.UID)

	gotParent.Status.ConnectionPoolerStatus = &platformv1alpha1.ConnectionPoolerStatus{Enabled: true}
	require.NoError(t, store.WriteParentStatus(ctx, gotParent))
	stored, err := store.ReadParent(ctx, types.NamespacedName{Namespace: "ns", Name: "parent"})
	require.NoError(t, err)
	require.Equal(t, gotParent.Status.ConnectionPoolerStatus, stored.Status.ConnectionPoolerStatus)

	_, err = store.ReadParent(ctx, types.NamespacedName{Namespace: "ns", Name: "missing"})
	require.Error(t, err)
	_, err = store.ReadConfigMap(ctx, types.NamespacedName{Namespace: "ns", Name: "missing"})
	require.Error(t, err)
}
