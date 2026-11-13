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
	"context"
	"encoding/json"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgtestfixture "github.com/splunk/splunk-operator/internal/testutil/postgres"
	clustercore "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	identityadapter "github.com/splunk/splunk-operator/pkg/postgresql/shared/adapter/identity"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func scheduledCancellationFixture(t *testing.T) (*PostgresClusterReconciler, *platformv1alpha1.PostgresCluster, *cnpgv1.Cluster, client.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, apps.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	parent := &platformv1alpha1.PostgresCluster{Name: "orders", Namespace: "default", UID: "parent-uid", Generation: 1, Finalizers: []string{clustercore.PostgresClusterFinalizerName},
		Spec:   platformv1alpha1.PostgresClusterSpec{Images: pgtestfixture.Images(), Class: "orders-class", Instances: new(int32(1)), Storage: new(resource.MustParse("10Gi")), PostgresVersion: new("18"), PostgresMajorUpgradeConfig: &platformv1alpha1.PostgresMajorUpgradeConfig{Allow: new(true), Strategy: new(mvutypes.MajorUpgradeFlowBlueGreen)}},
		Status: platformv1alpha1.PostgresClusterStatus{CurrentPgVersion: "17", Phase: new("Ready"), ObservedGeneration: new(int64(1)), Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready"}}}}
	owner := *metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))
	blue := &cnpgv1.Cluster{Name: parent.Name, Namespace: parent.Namespace, UID: "blue-uid", OwnerReferences: []metav1.OwnerReference{owner},
		Spec:   cnpgv1.ClusterSpec{Instances: 1, ImageName: pgtestfixture.ImageForVersion(platformv1alpha1.PostgresMajorVersion17), StorageConfiguration: cnpgv1.StorageConfiguration{Size: "10Gi"}, SuperuserSecret: &cnpgv1.LocalObjectReference{Name: "orders-secret"}},
		Status: cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy, Instances: 1, ReadyInstances: 1, CurrentPrimary: "orders-1", InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"orders-1"}}, PGDataImageInfo: &cnpgv1.ImageInfo{MajorVersion: 17}, WriteService: "orders-rw", ReadService: "orders-ro", Certificates: cnpgv1.CertificatesStatus{ServerTLSSecret: "orders-server", ServerCASecret: "orders-ca"}}}
	parent.Status.ProvisionerRef = &corev1.ObjectReference{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Cluster", Name: blue.Name, Namespace: blue.Namespace, UID: blue.UID}
	class := &platformv1alpha1.PostgresClusterClass{Name: parent.Spec.Class, Spec: platformv1alpha1.PostgresClusterClassSpec{Provisioner: cnpgProvisioner}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(parent, blue).WithObjects(parent, blue, class,
		&corev1.Secret{Name: "orders-secret", Namespace: parent.Namespace, OwnerReferences: []metav1.OwnerReference{owner}, Data: map[string][]byte{"password": []byte("test-password")}},
		&corev1.Secret{Name: "orders-ca", Namespace: parent.Namespace, Data: map[string][]byte{"ca.crt": []byte("test-ca")}},
	).WithIndex(&platformv1alpha1.PostgresDatabase{}, platformv1alpha1.PostgresDatabaseClusterRefNameField, func(o client.Object) []string {
		return []string{o.(*platformv1alpha1.PostgresDatabase).Spec.ClusterRef.Name}
	}).WithInterceptorFuncs(interceptor.Funcs{
		Apply: func(ctx context.Context, c client.WithWatch, obj runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
			// Model the parameter-only SSA API boundary without fake CNPG defaulting.
			data, err := json.Marshal(obj)
			if err != nil {
				return err
			}
			patch := new(cnpgv1.Cluster)
			if err := json.Unmarshal(data, patch); err != nil {
				return err
			}
			current := new(cnpgv1.Cluster)
			if err := c.Get(ctx, client.ObjectKeyFromObject(patch), current); err != nil {
				return err
			}
			current.Spec.PostgresConfiguration.Parameters = patch.Spec.PostgresConfiguration.Parameters
			return c.Update(ctx, current)
		},
	}).Build()
	r := &PostgresClusterReconciler{Client: c, APIReader: c, Scheme: scheme, Recorder: record.NewFakeRecorder(100), IdentityResolver: identityadapter.NewIdentityResolver()}
	// Exercise the real factory and initial Act writer; this is not a synthetic
	// Scheduled entry or an injected cancellation implementation.
	useCase, err := r.useCaseRegistry(client.ObjectKeyFromObject(parent), parent, clustercore.GetMergedConfig(class, parent))[mvutypes.UseCaseName]()
	require.NoError(t, err)
	require.NoError(t, useCase.Prerequisites(t.Context()))
	scheduled, err := useCase.Schedule(t.Context())
	require.NoError(t, err)
	require.True(t, scheduled)
	report, err := useCase.Act(t.Context())
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.Scheduled), report.Phase)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
	require.Len(t, parent.Status.PostgresMajorUpgradeStatus, 1)
	assert.Nil(t, parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Blue)
	parent.Spec.PostgresMajorUpgradeConfig.Allow = new(false)
	parent.Spec.PostgresMajorUpgradeConfig.BlueGreen = &platformv1alpha1.PostgresBlueGreenUpgradeConfig{Cancel: new(true)}
	parent.Generation++
	require.NoError(t, c.Update(t.Context(), parent))
	return r, parent, blue, c
}

func TestEarlyCancellationHandsBackRealScheduledSource(t *testing.T) {
	r, parent, blue, c := scheduledCancellationFixture(t)
	before := blue.Spec.DeepCopy()
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(parent)}
	for range 10 {
		_, err := r.Reconcile(t.Context(), req)
		require.NoError(t, err)
		require.NoError(t, c.Get(t.Context(), req.NamespacedName, parent))
		if *parent.Status.PostgresMajorUpgradeStatus[0].Phase == string(mvutypes.Cancelled) {
			break
		}
	}
	entry := parent.Status.PostgresMajorUpgradeStatus[0]
	require.Equal(t, string(mvutypes.Cancelled), *entry.Phase)
	require.NotNil(t, entry.BlueGreen.Blue, "handback must persist an exact source UID before completion")
	assert.Equal(t, blue.UID, entry.BlueGreen.Blue.Ref.UID)
	for range 3 {
		_, err := r.Reconcile(t.Context(), req)
		require.NoError(t, err)
	}
	require.NoError(t, c.Get(t.Context(), req.NamespacedName, parent))
	assert.True(t, meta.IsStatusConditionTrue(parent.Status.Conditions, "Ready"))
	assert.Equal(t, "17", parent.Status.CurrentPgVersion)
	assert.Equal(t, "18", *parent.Spec.PostgresVersion)
	assert.False(t, *parent.Spec.PostgresMajorUpgradeConfig.Allow)
	require.NotNil(t, parent.Status.Resources.ConfigMapRef)
	cm := new(corev1.ConfigMap)
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: parent.Namespace, Name: parent.Status.Resources.ConfigMapRef.Name}, cm))
	assert.Equal(t, "orders-rw.default.svc.cluster.local", cm.Data[pgconninfo.KeyClusterRWEndpoint])
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(blue), blue))
	assert.Equal(t, before.ImageName, blue.Spec.ImageName)
	assert.Equal(t, before.StorageConfiguration.Size, blue.Spec.StorageConfiguration.Size)
}

func TestEarlyCancellationRejectsReplacedOrChangedSource(t *testing.T) {
	for _, scenario := range []string{"replacement", "source major changed"} {
		t.Run(scenario, func(t *testing.T) {
			r, parent, blue, c := scheduledCancellationFixture(t)
			req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(parent)}
			for range 6 {
				_, err := r.Reconcile(t.Context(), req)
				require.NoError(t, err)
				require.NoError(t, c.Get(t.Context(), req.NamespacedName, parent))
				if parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Gates.Cancellation.CredentialsRemovedAt != nil {
					break
				}
			}
			require.Nil(t, parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Blue)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(blue), blue))
			if scenario == "replacement" {
				require.NoError(t, c.Delete(t.Context(), blue))
				blue.ResourceVersion, blue.UID = "", "replacement-uid"
				require.NoError(t, c.Create(t.Context(), blue))
			} else {
				blue.Status.PGDataImageInfo.MajorVersion = 18
				require.NoError(t, c.Status().Update(t.Context(), blue))
			}
			_, err := r.Reconcile(t.Context(), req)
			require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
			require.NoError(t, c.Get(t.Context(), req.NamespacedName, parent))
			assert.Nil(t, parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Blue)
			assert.Nil(t, parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Gates.Cancellation.CompletedAt)
		})
	}
}
