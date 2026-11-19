/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package controller

import (
	"testing"

	certv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	clustercore "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCleanupFactoryRunsWithoutClassImageOrSQLCredentials(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, certv1.AddToScheme(scheme))
	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default", UID: "parent-uid", Generation: 3, Finalizers: []string{clustercore.PostgresClusterFinalizerName}}, Spec: platformv1alpha1.PostgresClusterSpec{Class: "missing", PostgresMajorUpgradeConfig: &platformv1alpha1.PostgresMajorUpgradeConfig{Allow: new(false), BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeConfig{Cleanup: new(true)}}}}
	owner := []metav1.OwnerReference{*metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}
	active := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: parent.Name, Namespace: parent.Namespace, UID: "blue-uid", OwnerReferences: owner}, Spec: cnpgv1.ClusterSpec{Instances: 1}, Status: cnpgv1.ClusterStatus{WriteService: "orders-rw", Phase: cnpgv1.PhaseHealthy, Instances: 1, ReadyInstances: 1, CurrentPrimary: "orders-1", InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"orders-1"}}}}
	now := metav1.Now()
	bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt", Blue: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Cluster", Name: active.Name, Namespace: active.Namespace, UID: active.UID}}, Gates: &platformv1alpha1.BlueGreenGateStatus{Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: &now, SourceAccessRestoredAt: &now, ReplicationRemovedAt: &now, CredentialsRemovedAt: &now, GreenFencedAt: &now, CompletedAt: &now}}}
	parent.Status.ProvisionerRef = bg.Blue.Ref.DeepCopy()
	parent.Status.Phase = new("Ready")
	parent.Status.Resources = &platformv1alpha1.PostgresClusterResources{ConfigMapRef: &corev1.LocalObjectReference{Name: "canonical"}}
	parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{Phase: new("Cancelled"), Strategy: new(mvutypes.MajorUpgradeFlowBlueGreen), SourcePgVersion: new("17"), TargetPgVersion: new("18"), BlueGreen: bg}}
	endpoint, err := pgconninfo.ServiceFQDN(active.Status.WriteService, parent.Namespace)
	require.NoError(t, err)
	canonical := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "canonical", Namespace: parent.Namespace, UID: "canonical-uid", OwnerReferences: owner}, Data: map[string]string{pgconninfo.KeyClusterRWEndpoint: endpoint}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(parent).WithObjects(parent, active, canonical).Build()
	r := &PostgresClusterReconciler{Client: c, APIReader: c, Scheme: scheme, Recorder: record.NewFakeRecorder(20)}
	for range 2 {
		// Ordinary maintenance may report missing configuration after cleanup advances.
		_, _ = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(parent)})
		require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
	}
	cleanup := parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Cleanup
	require.NotNil(t, cleanup)
	assert.Equal(t, platformv1alpha1.BlueGreenCleanupStateCleaned, cleanup.State)
	assert.Equal(t, "Cancelled", *parent.Status.PostgresMajorUpgradeStatus[0].Phase)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(active), &cnpgv1.Cluster{}))
}
