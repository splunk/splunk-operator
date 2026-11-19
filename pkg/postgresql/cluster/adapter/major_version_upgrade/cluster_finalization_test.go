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
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	certv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	tlsport "github.com/splunk/splunk-operator/pkg/postgresql/cluster/ports/tls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func finalizationFixture(t *testing.T, policy string) (*cleanupFixture, *ClusterFinalizer) {
	t.Helper()
	f := newCleanupFixture(t)
	f.parent.Spec.ClusterDeletionPolicy = new(policy)
	f.parent.Finalizers = []string{"postgresclusters.platform.splunk.com/finalizer"}
	require.NoError(t, f.client.Update(t.Context(), f.parent))
	require.NoError(t, f.client.Delete(t.Context(), f.parent))
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.parent), f.parent))
	runtime := NewClusterFinalizer(f.client, f.client, nil)
	return f, runtime
}
func finalizationStep(t *testing.T, f *cleanupFixture, runtime *ClusterFinalizer) (bool, error) {
	t.Helper()
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.parent), f.parent))
	done, err := runtime.AdvanceFinalization(t.Context(), f.parent)
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.parent), f.parent))
	return done, err
}

func TestFinalizationDeleteContinuesWithoutSQLAndWaitsForStorage(t *testing.T) {
	f, runtime := finalizationFixture(t, "Delete")
	calls := 0
	runtime.teardown = func(ctx context.Context, _ *platformv1alpha1.PostgresCluster) (bool, error) {
		calls++
		_, ok := ctx.Deadline()
		require.True(t, ok)
		return false, errors.New("unreachable SQL")
	}
	owner := []metav1.OwnerReference{*metav1.NewControllerRef(f.retired, cnpgv1.SchemeGroupVersion.WithKind("Cluster"))}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "retired-data", Namespace: "ns", UID: "pvc-uid", OwnerReferences: owner}}
	anchor := &cnpgv1.Backup{ObjectMeta: metav1.ObjectMeta{Name: "anchor", Namespace: "ns", UID: "anchor-uid", OwnerReferences: owner}, Spec: cnpgv1.BackupSpec{Cluster: cnpgv1.LocalObjectReference{Name: f.retired.Name}}}
	require.NoError(t, f.client.Create(t.Context(), pvc))
	anchor.OwnerReferences[0].Controller = nil
	require.NoError(t, f.client.Create(t.Context(), anchor))
	foreign := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "foreign", Namespace: "ns", UID: "foreign-uid", Labels: map[string]string{blueGreenAttemptLabel: "attempt"}}}
	require.NoError(t, f.client.Create(t.Context(), foreign))
	snapshot := &snapshotv1.VolumeSnapshot{ObjectMeta: metav1.ObjectMeta{Name: "recovery-data", Namespace: "ns", UID: "snapshot-uid", OwnerReferences: owner}}
	require.NoError(t, f.client.Create(t.Context(), snapshot))
	for range 12 {
		done, err := finalizationStep(t, f, runtime)
		require.NoError(t, err)
		assert.False(t, done)
	}
	assert.Equal(t, 1, calls, "a bounded SQL failure is persisted before deletion proceeds")
	require.True(t, apierrors.IsNotFound(f.client.Get(t.Context(), client.ObjectKeyFromObject(f.active), &cnpgv1.Cluster{})))
	require.True(t, apierrors.IsNotFound(f.client.Get(t.Context(), client.ObjectKeyFromObject(f.retired), &cnpgv1.Cluster{})))
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(anchor), anchor))
	assert.Empty(t, anchor.OwnerReferences)
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(snapshot), snapshot))
	assert.Empty(t, snapshot.OwnerReferences)
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(foreign), &corev1.Secret{}))
	require.NoError(t, f.client.Delete(t.Context(), pvc))
	done, err := finalizationStep(t, f, runtime)
	require.NoError(t, err)
	assert.True(t, done)
	require.NotNil(t, f.parent.Status.Finalization)
	assert.True(t, f.parent.Status.Finalization.SQLWarning)
}

func TestFinalizationRetainResumesCompletionWithoutRetiredCredentials(t *testing.T) {
	for _, test := range []struct {
		name                string
		checkpoint, blocked bool
	}{
		{"checkpoint lost", false, false},
		{"checkpoint saved", true, false},
		{"production configuration changed", true, false},
		{"missing SQL proof", false, true},
		{"fence drift", false, true},
		{"late declaration", false, true},
		{"receipt target changed", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, completion, sql := completionFixture(t)
			backup := waitForTargetBackup(t, f, completion)
			completeTargetBackup(t, f, backup)
			sql.archived = true
			completionStep(t, f, completion)
			completionStep(t, f, completion)
			require.NotNil(t, f.status().Completion.ReplicationRemovedAt)
			_, err := completion.teardown.deleteSecrets(t.Context(), "attempt")
			require.NoError(t, err)
			f.mutate(func(parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
				parent.Status.Resources = &platformv1alpha1.PostgresClusterResources{ConfigMapRef: &corev1.LocalObjectReference{Name: bg.CommitReceipt.CanonicalConfigMap.Name}}
				if test.checkpoint {
					bg.Completion.CredentialsRemovedAt = new(metav1.Now())
				}
				if test.name == "missing SQL proof" {
					bg.Completion.ReplicationRemovedAt = nil
				}
				if test.name == "receipt target changed" {
					bg.CommitReceipt.Target.UID = "replacement"
				}
				if test.name == "production configuration changed" {
					parent.Status.PostgresMajorUpgradeStatus[0].Phase = new("Completed")
					bg.Completion.SourceCredentialsRetiredAt = new(metav1.Now())
				}
			})
			if test.name == "production configuration changed" {
				green := &cnpgv1.Cluster{}
				require.NoError(t, f.runtime.client.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "green"}, green))
				green.Spec.PostgresConfiguration.PgHBA = append(green.Spec.PostgresConfiguration.PgHBA, "hostssl all reporting 10.0.0.0/24 scram-sha-256")
				green.Spec.Managed.Roles = append(green.Spec.Managed.Roles, cnpgv1.RoleConfiguration{Name: "reporting", Login: true})
				require.NoError(t, f.runtime.client.Update(t.Context(), green))
			}
			if test.name == "fence drift" {
				blue := &cnpgv1.Cluster{}
				require.NoError(t, f.runtime.client.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "blue"}, blue))
				blue.Spec.PostgresConfiguration.PgHBA = nil
				require.NoError(t, f.runtime.client.Update(t.Context(), blue))
			}
			require.NoError(t, appsv1.AddToScheme(f.runtime.client.Scheme()))
			require.NoError(t, certv1.AddToScheme(f.runtime.client.Scheme()))
			objects, err := namespaceObjects(t.Context(), f.runtime.client, f.runtime.key.Namespace, &cnpgv1.BackupList{}, &cnpgv1.ScheduledBackupList{}, &cnpgv1.PoolerList{}, &corev1.ConfigMapList{}, &corev1.SecretList{})
			require.NoError(t, err)
			for _, object := range objects {
				if object.GetUID() == "" {
					object.SetUID(types.UID("test-" + object.GetName()))
					require.NoError(t, f.runtime.client.Update(t.Context(), object))
				}
			}
			parent := &platformv1alpha1.PostgresCluster{}
			require.NoError(t, f.runtime.client.Get(t.Context(), f.runtime.key, parent))
			parent.Spec.ClusterDeletionPolicy = new("Retain")
			parent.Finalizers = []string{"test/finalizer"}
			require.NoError(t, f.runtime.client.Update(t.Context(), parent))
			require.NoError(t, f.runtime.client.Delete(t.Context(), parent))
			if test.name == "late declaration" {
				late := &cnpgv1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "late", Namespace: parent.Namespace, UID: "late-uid", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}}, Spec: cnpgv1.SubscriptionSpec{ClusterRef: corev1.LocalObjectReference{Name: "green"}}}
				require.NoError(t, f.runtime.client.Create(t.Context(), late))
			}
			runtime := NewClusterFinalizer(f.runtime.client, f.runtime.reader, nil)
			done := false
			for range 20 {
				require.NoError(t, f.runtime.client.Get(t.Context(), f.runtime.key, parent))
				done, err = runtime.AdvanceFinalization(t.Context(), parent)
				if test.blocked && err != nil {
					break
				}
				require.NoError(t, err, "the SQL checkpoint must avoid already deleted control credentials")
				if done {
					break
				}
			}
			if test.blocked {
				require.Error(t, err)
				green := &cnpgv1.Cluster{}
				require.NoError(t, f.runtime.client.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "green"}, green))
				assert.True(t, metav1.IsControlledBy(green, parent))
				return
			}
			require.True(t, done)
			if test.name != "production configuration changed" {
				assert.Equal(t, string(mvutypes.Recoverable), *parent.Status.PostgresMajorUpgradeStatus[0].Phase)
			}
			bg := parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen
			for _, environment := range []*platformv1alpha1.BlueGreenEnvironmentStatus{bg.Blue, bg.Green} {
				cluster := &cnpgv1.Cluster{}
				require.NoError(t, f.runtime.client.Get(t.Context(), client.ObjectKey{Namespace: parent.Namespace, Name: environment.Ref.Name}, cluster))
				assert.Empty(t, cluster.OwnerReferences)
			}
		})
	}
}

func TestFinalizationRetainBlocksThenPreservesCredentialsAndRenewingTLS(t *testing.T) {
	f, runtime := finalizationFixture(t, "Retain")
	safe := false
	runtime.teardown = func(context.Context, *platformv1alpha1.PostgresCluster) (bool, error) {
		if !safe {
			return false, errors.New("SQL proof unavailable")
		}
		return true, nil
	}
	owner := []metav1.OwnerReference{*metav1.NewControllerRef(f.parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}
	credential := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "app-password", Namespace: "ns", UID: "password-uid", OwnerReferences: owner}, Data: map[string][]byte{"password": []byte("unchanged")}}
	certificate := &certv1.Certificate{ObjectMeta: metav1.ObjectMeta{Name: "renewing-leaf", Namespace: "ns", UID: "cert-uid", OwnerReferences: owner}, Spec: certv1.CertificateSpec{SecretName: "leaf"}}
	leaf := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "leaf", Namespace: "ns", UID: "leaf-uid", Labels: map[string]string{tlsport.ClusterUIDLabel: string(f.parent.UID), tlsport.PurposeLabel: tlsport.ServerTLSPurpose}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(certificate, certv1.SchemeGroupVersion.WithKind("Certificate"))}}, Type: corev1.SecretTypeTLS}
	require.NoError(t, f.client.Create(t.Context(), credential))
	require.NoError(t, f.client.Create(t.Context(), certificate))
	require.NoError(t, f.client.Create(t.Context(), leaf))
	_, err := finalizationStep(t, f, runtime)
	require.NoError(t, err)
	_, err = finalizationStep(t, f, runtime)
	require.ErrorContains(t, err, "SQL proof unavailable")
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(credential), credential))
	assert.True(t, metav1.IsControlledBy(credential, f.parent))
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.active), f.active))
	assert.True(t, metav1.IsControlledBy(f.active, f.parent))
	safe = true
	completed := false
	for range 16 {
		// Reconstruct the adapter to exercise durable progress after every effect.
		next := NewClusterFinalizer(f.client, f.client, nil)
		next.teardown = runtime.teardown
		completed, err = finalizationStep(t, f, next)
		require.NoError(t, err)
		if completed {
			break
		}
	}
	assert.True(t, completed)
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(credential), credential))
	assert.Empty(t, credential.OwnerReferences)
	assert.Equal(t, []byte("unchanged"), credential.Data["password"])
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(certificate), certificate))
	assert.Empty(t, certificate.OwnerReferences)
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(leaf), leaf))
	assert.Equal(t, certificate.UID, metav1.GetControllerOf(leaf).UID)
	for _, environment := range []*cnpgv1.Cluster{f.active, f.retired} {
		require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(environment), environment))
		assert.Empty(t, environment.OwnerReferences)
	}
}

func TestFinalizationRejectsReplacedInventoryAndChangedPolicy(t *testing.T) {
	for _, change := range []string{"UID", "policy"} {
		t.Run(change, func(t *testing.T) {
			f, runtime := finalizationFixture(t, "Retain")
			runtime.teardown = func(context.Context, *platformv1alpha1.PostgresCluster) (bool, error) {
				t.Fatal("SQL must not run after identity drift")
				return false, nil
			}
			_, err := finalizationStep(t, f, runtime)
			require.NoError(t, err)
			if change == "UID" {
				require.NoError(t, f.client.Delete(t.Context(), f.retired))
				replacement := f.retired.DeepCopy()
				replacement.ResourceVersion = ""
				replacement.UID = "replacement-uid"
				require.NoError(t, f.client.Create(t.Context(), replacement))
			} else {
				require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.parent), f.parent))
				f.parent.Spec.ClusterDeletionPolicy = new("Delete")
				require.NoError(t, f.client.Update(t.Context(), f.parent))
			}
			_, err = finalizationStep(t, f, runtime)
			require.Error(t, err)
		})
	}
}

func TestFinalizationRetainsSQLBeforeReleasingOnlyCNPGFinalizer(t *testing.T) {
	f, runtime := finalizationFixture(t, "Delete")
	runtime.teardown = func(context.Context, *platformv1alpha1.PostgresCluster) (bool, error) { return true, nil }
	declaration := &cnpgv1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "arbitrary-subscription", Namespace: "ns", UID: "sub-uid", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(f.parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}, Finalizers: []string{cnpgutils.SubscriptionFinalizerName, "another-controller"}}, Spec: cnpgv1.SubscriptionSpec{ClusterRef: corev1.LocalObjectReference{Name: f.retired.Name}}}
	require.NoError(t, f.client.Create(t.Context(), declaration))
	for range 4 {
		done, err := finalizationStep(t, f, runtime)
		require.NoError(t, err)
		assert.False(t, done)
	}
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(declaration), declaration))
	assert.Equal(t, cnpgv1.SubscriptionReclaimRetain, declaration.Spec.ReclaimPolicy)
	assert.ElementsMatch(t, []string{"another-controller"}, declaration.Finalizers)
	require.NotNil(t, declaration.DeletionTimestamp)
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.active), &cnpgv1.Cluster{}))
}

func TestFinalizationRetainWaitsForCNPGWriter(t *testing.T) {
	for _, declaration := range []client.Object{
		&cnpgv1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "subscription", Generation: 1}, Status: cnpgv1.SubscriptionStatus{ObservedGeneration: 1}},
		&cnpgv1.Publication{ObjectMeta: metav1.ObjectMeta{Name: "publication", Generation: 1}, Status: cnpgv1.PublicationStatus{ObservedGeneration: 1}},
	} {
		t.Run(declaration.GetName(), func(t *testing.T) {
			f, runtime := finalizationFixture(t, "Retain")
			calls := 0
			runtime.teardown = func(context.Context, *platformv1alpha1.PostgresCluster) (bool, error) { calls++; return true, nil }
			finalizer := cnpgutils.SubscriptionFinalizerName
			if _, ok := declaration.(*cnpgv1.Publication); ok {
				finalizer = cnpgutils.PublicationFinalizerName
			}
			retainReplicationDeclaration(declaration)
			declaration.SetNamespace("ns")
			declaration.SetUID(types.UID(declaration.GetName()))
			declaration.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(f.parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))})
			declaration.SetFinalizers([]string{finalizer})
			require.NoError(t, f.client.Create(t.Context(), declaration))
			for range 4 {
				done, err := finalizationStep(t, f, runtime)
				require.NoError(t, err)
				assert.False(t, done)
			}
			require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(declaration), declaration))
			assert.NotNil(t, declaration.GetDeletionTimestamp())
			assert.Contains(t, declaration.GetFinalizers(), finalizer)
			assert.Zero(t, calls, "Retain SQL proof waits for CNPG's acknowledgement")
		})
	}
}

func TestFinalizationHonorsCopiedCARetentionPolicy(t *testing.T) {
	for _, policy := range []platformv1alpha1.PostgresCertificateRetentionPolicy{platformv1alpha1.PostgresCertificateRetentionPolicyRetain, platformv1alpha1.PostgresCertificateRetentionPolicyDelete} {
		t.Run(string(policy), func(t *testing.T) {
			f, runtime := finalizationFixture(t, "Delete")
			runtime.teardown = func(context.Context, *platformv1alpha1.PostgresCluster) (bool, error) { return true, nil }
			require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.parent), f.parent))
			f.parent.Spec.Class = "ca-class"
			require.NoError(t, f.client.Update(t.Context(), f.parent))
			class := &platformv1alpha1.PostgresClusterClass{ObjectMeta: metav1.ObjectMeta{Name: "ca-class"}, Spec: platformv1alpha1.PostgresClusterClassSpec{TLS: &platformv1alpha1.PostgresClusterClassTLS{Certificates: &platformv1alpha1.PostgresCertificateConfig{SecretRetentionPolicy: &policy}}}}
			ca := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: f.active.Spec.Certificates.ServerCASecret, Namespace: f.parent.Namespace, UID: "ca-uid", Labels: tlsport.Identity{ClusterName: f.parent.Name, Namespace: f.parent.Namespace, ClusterUID: f.parent.UID}.Labels()}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"ca.crt": []byte("unchanged")}}
			require.NoError(t, f.client.Create(t.Context(), class))
			require.NoError(t, f.client.Create(t.Context(), ca))
			require.NoError(t, f.client.Delete(t.Context(), f.active))
			_, err := finalizationStep(t, f, runtime)
			require.NoError(t, err)
			require.NoError(t, f.client.Delete(t.Context(), class))
			completed := false
			for range 15 {
				completed, err = finalizationStep(t, f, runtime)
				require.NoError(t, err)
				if completed {
					break
				}
			}
			require.True(t, completed)
			err = f.client.Get(t.Context(), client.ObjectKeyFromObject(ca), &corev1.Secret{})
			if policy == platformv1alpha1.PostgresCertificateRetentionPolicyDelete {
				assert.True(t, apierrors.IsNotFound(err))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFinalizationRechecksSQLAfterNewDeclaration(t *testing.T) {
	f, runtime := finalizationFixture(t, "Retain")
	calls := 0
	runtime.teardown = func(context.Context, *platformv1alpha1.PostgresCluster) (bool, error) { calls++; return true, nil }
	_, err := finalizationStep(t, f, runtime)
	require.NoError(t, err)
	_, err = finalizationStep(t, f, runtime)
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
	late := &cnpgv1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "late", Namespace: "ns", UID: "late-uid", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(f.parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}}}
	require.NoError(t, f.client.Create(t.Context(), late))
	for range 8 {
		_, err = finalizationStep(t, f, runtime)
		require.NoError(t, err)
		if calls == 2 {
			break
		}
	}
	assert.Equal(t, 2, calls)
}

func TestFinalizationRejectsWritersForCleanedAttempts(t *testing.T) {
	for _, historical := range []bool{false, true} {
		for _, declaration := range []client.Object{&cnpgv1.Subscription{}, &cnpgv1.Publication{}} {
			t.Run(fmt.Sprintf("%T/historical=%t", declaration, historical), func(t *testing.T) {
				f, runtime := finalizationFixture(t, "Retain")
				bg := f.parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen
				bg.Cleanup = &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateCleaned, CompletedAt: new(metav1.Now())}
				if historical {
					f.parent.Status.PostgresMajorUpgradeStatus = append(f.parent.Status.PostgresMajorUpgradeStatus, platformv1alpha1.PostgresMajorUpgradeStatus{Phase: new("Completed"), Strategy: new("pgUpgrade")})
				}
				require.NoError(t, f.client.Status().Update(t.Context(), f.parent))
				for range 2 {
					_, err := finalizationStep(t, f, runtime)
					require.NoError(t, err)
				}
				require.True(t, f.parent.Status.Finalization.SQLDone)
				declaration.SetName("late-writer")
				declaration.SetNamespace(f.parent.Namespace)
				declaration.SetUID("late-writer")
				declaration.SetLabels(map[string]string{blueGreenAttemptLabel: bg.AttemptID})
				declaration.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(f.parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))})
				require.NoError(t, f.client.Create(t.Context(), declaration))
				_, err := finalizationStep(t, f, runtime)
				require.NoError(t, err, "persist the contradiction before any effects")
				require.True(t, f.parent.Status.Finalization.SQLBlocked)
				require.False(t, f.parent.Status.Finalization.SQLDone)
				_, err = finalizationStep(t, f, runtime)
				require.ErrorContains(t, err, "cleaned attempt")
				require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(declaration), declaration))
				assert.Nil(t, declaration.GetDeletionTimestamp())
				// Removing the declaration externally cannot erase the observed SQL risk.
				require.NoError(t, f.client.Delete(t.Context(), declaration))
				restarted := NewClusterFinalizer(f.client, f.client, nil)
				_, err = finalizationStep(t, f, restarted)
				require.ErrorContains(t, err, "cleaned attempt")
				for _, cluster := range []*cnpgv1.Cluster{f.active, f.retired} {
					require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(cluster), cluster))
					assert.True(t, metav1.IsControlledBy(cluster, f.parent))
				}
			})
		}
	}
}

func TestFinalizationIgnoresForgedAnnotationAuthority(t *testing.T) {
	for _, policy := range []string{"Delete", "Retain"} {
		t.Run(policy, func(t *testing.T) {
			f, runtime := finalizationFixture(t, policy)
			victims := []client.Object{
				&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unrelated-secret", Namespace: f.parent.Namespace, UID: "secret-victim"}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "unrelated-config", Namespace: f.parent.Namespace, UID: "config-victim"}},
				&cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "unrelated-environment", Namespace: f.parent.Namespace, UID: "cluster-victim"}},
			}
			for _, object := range victims {
				require.NoError(t, f.client.Create(t.Context(), object))
			}
			refs, _, err := runtime.discover(t.Context(), f.parent, nil)
			require.NoError(t, err)
			refs, err = mergeRetirementRoots(refs, nil, victims...)
			require.NoError(t, err)
			history, err := json.Marshal(f.parent.Status.PostgresMajorUpgradeStatus)
			require.NoError(t, err)
			forged, err := json.Marshal(finalizationInventory{Version: 1, ParentUID: f.parent.UID, Policy: policy, Resources: refs,
				HistoryHash: fmt.Sprintf("%x", sha256.Sum256(history)), SQLDone: true, DeleteCA: true})
			require.NoError(t, err)
			f.parent.Annotations = map[string]string{"platform.splunk.com/finalization-inventory": string(forged)}
			require.NoError(t, f.client.Update(t.Context(), f.parent))
			for _, object := range victims {
				assert.False(t, finalizationOwns(f.parent, object))
			}
			calls := 0
			runtime.teardown = func(context.Context, *platformv1alpha1.PostgresCluster) (bool, error) { calls++; return true, nil }
			completed := false
			for range 20 {
				completed, err = finalizationStep(t, f, runtime)
				require.NoError(t, err)
				if completed {
					break
				}
			}
			require.True(t, completed)
			assert.Equal(t, 1, calls, "annotation SQLDone must not bypass the real checkpoint")
			for _, object := range victims {
				actual := object.DeepCopyObject().(client.Object)
				require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(object), actual))
				assert.Equal(t, object.GetUID(), actual.GetUID())
				assert.Nil(t, actual.GetDeletionTimestamp())
				assert.NotContains(t, f.parent.Status.Finalization.Resources, testResourceReference(t, object))
			}
		})
	}
}
