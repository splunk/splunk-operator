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
	"slices"
	"testing"

	certv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	tlsport "github.com/splunk/splunk-operator/pkg/postgresql/cluster/ports/tls"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func cleanupInventoryFixture(t *testing.T) (*runtime.Scheme, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus, *cnpgv1.Cluster, corev1.ObjectReference) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, snapshotv1.AddToScheme(scheme))
	require.NoError(t, certv1.AddToScheme(scheme))
	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "ns", UID: "parent-uid"}}
	active := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "green", Namespace: "ns", UID: "green-uid"},
		Spec: cnpgv1.ClusterSpec{Certificates: &cnpgv1.CertificatesConfiguration{ServerTLSSecret: "active-tls", ServerCASecret: "shared-ca"}, Managed: &cnpgv1.ManagedConfiguration{Roles: []cnpgv1.RoleConfiguration{{Name: "app", PasswordSecret: &cnpgv1.LocalObjectReference{Name: "app-password"}}}}},
	}
	blue := corev1.ObjectReference{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Cluster", Namespace: "ns", Name: "blue", UID: "blue-uid"}
	bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt", Blue: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: blue}, Green: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: testResourceReference(t, active)}}
	return scheme, parent, bg, active, blue
}

func TestCleanupRosterIncludesChangedAndDeletedChildren(t *testing.T) {
	scheme, parent, bg, _, environment := cleanupInventoryFixture(t)
	child := &platformv1alpha1.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "ns", UID: "child-uid", Generation: 4}, Spec: platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: parent.Name}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "new-definition"}}}}
	declaration := &cnpgv1.Database{ObjectMeta: metav1.ObjectMeta{Name: "arbitrary", Namespace: "ns", UID: "database-uid", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(child, platformv1alpha1.GroupVersion.WithKind("PostgresDatabase"))}}, Spec: cnpgv1.DatabaseSpec{Name: "removed-definition", ClusterRef: corev1.LocalObjectReference{Name: environment.Name}}}
	bg.DatabaseReplication = []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{PostgresDatabaseName: "deleted", PostgresDatabaseUID: "deleted-uid", DatabaseName: "historical"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(child, declaration).Build()
	rows, err := cleanupInventory(t.Context(), c, parent, bg, &environment)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, "new-definition", rows[0].DatabaseName)
	assert.EqualValues(t, 4, rows[0].PostgresDatabaseGeneration)
	assert.Equal(t, "removed-definition", rows[1].DatabaseName)
	require.Len(t, rows[1].Resources, 1)
	assert.Equal(t, declaration.UID, rows[1].Resources[0].UID)
	assert.Equal(t, "historical", rows[2].DatabaseName)
	assert.Zero(t, rows[2].PostgresDatabaseGeneration)
	bg.Cleanup = &platformv1alpha1.BlueGreenCleanupStatus{RetirementAcknowledgements: rows}
	require.NoError(t, c.Delete(t.Context(), declaration))
	repeated, err := cleanupInventory(t.Context(), c, parent, bg, &environment)
	require.NoError(t, err)
	assert.Equal(t, rows, repeated)
}

func TestCleanupRosterRejectsForeignAndReplacedDeclarations(t *testing.T) {
	scheme, parent, bg, _, environment := cleanupInventoryFixture(t)
	child := &platformv1alpha1.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "foreign", Namespace: "ns", UID: "foreign-uid"}, Spec: platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "other"}}}
	database := &cnpgv1.Database{ObjectMeta: metav1.ObjectMeta{Name: "provider", Namespace: "ns", UID: "provider-uid", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(child, platformv1alpha1.GroupVersion.WithKind("PostgresDatabase"))}}, Spec: cnpgv1.DatabaseSpec{Name: "app", ClusterRef: corev1.LocalObjectReference{Name: environment.Name}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(child, database).Build()
	_, err := cleanupInventory(t.Context(), c, parent, bg, &environment)
	require.ErrorContains(t, err, "unrelated child")
	original := testResourceReference(t, database)
	replacement := original
	replacement.UID = "replacement"
	_, _, err = mergeCleanupReferences([]corev1.ObjectReference{original}, []corev1.ObjectReference{replacement})
	require.ErrorContains(t, err, "replaced")
}

func TestCleanupRootsProtectPromotedResourcesAndBackupAnchor(t *testing.T) {
	scheme, parent, bg, active, environment := cleanupInventoryFixture(t)
	owner := []metav1.OwnerReference{*metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}
	metadata := func(name string) metav1.ObjectMeta {
		return metav1.ObjectMeta{Name: name, Namespace: "ns", UID: types.UID(name), OwnerReferences: owner, Labels: map[string]string{blueGreenAttemptLabel: bg.AttemptID}}
	}
	activePooler := &cnpgv1.Pooler{ObjectMeta: metadata("promoted-pooler"), Spec: cnpgv1.PoolerSpec{Cluster: cnpgv1.LocalObjectReference{Name: active.Name}}}
	oldPooler := &cnpgv1.Pooler{ObjectMeta: metadata("arbitrary-retired-pooler"), Spec: cnpgv1.PoolerSpec{Cluster: cnpgv1.LocalObjectReference{Name: environment.Name}}}
	anchor := &cnpgv1.Backup{ObjectMeta: metadata("target-anchor"), Spec: cnpgv1.BackupSpec{Cluster: cnpgv1.LocalObjectReference{Name: active.Name}}}
	disposable := &corev1.Secret{ObjectMeta: metadata("arbitrary-temporary-secret")}
	foreign := &corev1.Secret{ObjectMeta: metadata("foreign")}
	foreign.OwnerReferences = []metav1.OwnerReference{{UID: "another-parent", Controller: new(true)}}

	objects := []client.Object{activePooler, oldPooler, anchor, disposable, foreign}
	for _, name := range []string{"active-tls", "shared-ca", "app-password"} {
		objects = append(objects, &corev1.Secret{ObjectMeta: metadata(name)})
	}
	labels := map[string]string{tlsport.ClusterUIDLabel: string(parent.UID), tlsport.PurposeLabel: tlsport.ServerTLSPurpose}
	retiredCert := &certv1.Certificate{ObjectMeta: metadata("old-cert"), Spec: certv1.CertificateSpec{SecretName: "retired-tls"}}
	retiredCert.Labels = labels
	retiredTLS := &corev1.Secret{ObjectMeta: metadata("retired-tls"), Type: corev1.SecretTypeTLS}
	retiredTLS.Labels = labels
	retiredTLS.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(retiredCert, certv1.SchemeGroupVersion.WithKind("Certificate"))}
	promotedCert := &certv1.Certificate{ObjectMeta: metadata("active-cert"), Spec: certv1.CertificateSpec{SecretName: "active-tls"}}
	promotedCert.Labels = labels
	objects = append(objects, retiredCert, promotedCert, retiredTLS)
	parent.Status.Resources = &platformv1alpha1.PostgresClusterResources{ConfigMapRef: &corev1.LocalObjectReference{Name: "canonical"}}
	publishedCA := &corev1.Secret{ObjectMeta: metadata("published-ca")}
	canonical := &corev1.ConfigMap{ObjectMeta: metadata("canonical"), Data: map[string]string{pgconninfo.KeyServerCASecretRef: "published-ca/ca.crt"}}
	objects = append(objects, publishedCA, canonical)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	protected, err := publishedCleanupSecrets(t.Context(), c, parent)
	require.NoError(t, err)
	roots, err := cleanupRoots(t.Context(), c, parent, bg, active, &environment, protected)
	require.NoError(t, err)
	names := []string{}
	for _, ref := range roots {
		names = append(names, ref.Name)
	}
	assert.ElementsMatch(t, []string{environment.Name, oldPooler.Name, disposable.Name, retiredCert.Name, retiredTLS.Name}, names)
	bg.Cleanup = &platformv1alpha1.BlueGreenCleanupStatus{RetiredResources: roots}
	require.NoError(t, c.Delete(t.Context(), oldPooler))
	repeated, err := cleanupRoots(t.Context(), c, parent, bg, active, &environment, protected)
	require.NoError(t, err)
	assert.Equal(t, roots, repeated)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(anchor), &cnpgv1.Backup{}))
}

func TestCleanupRetiresOnlyUnprotectedManagedCACopy(t *testing.T) {
	for _, test := range []struct {
		name    string
		change  func(*testing.T, *cleanupFixture, *corev1.Secret)
		retired bool
	}{
		{name: "retired candidate CA", retired: true},
		{name: "active CA", change: func(t *testing.T, f *cleanupFixture, ca *corev1.Secret) {
			f.active.Spec.Certificates.ServerCASecret = ca.Name
			require.NoError(t, f.client.Update(t.Context(), f.active))
		}},
		{name: "published CA", change: func(t *testing.T, f *cleanupFixture, ca *corev1.Secret) {
			canonical := &corev1.ConfigMap{}
			require.NoError(t, f.client.Get(t.Context(), client.ObjectKey{Namespace: f.parent.Namespace, Name: "canonical"}, canonical))
			canonical.Data[pgconninfo.KeyServerCASecretRef] = ca.Name + "/ca.crt"
			require.NoError(t, f.client.Update(t.Context(), canonical))
		}},
		{name: "wrong namespace", change: func(_ *testing.T, _ *cleanupFixture, ca *corev1.Secret) { ca.Namespace = "other" }},
		{name: "unrelated name", change: func(_ *testing.T, _ *cleanupFixture, ca *corev1.Secret) { ca.Name = "unrelated-ca" }},
		{name: "wrong parent UID", change: func(_ *testing.T, _ *cleanupFixture, ca *corev1.Secret) { ca.Labels[tlsport.ClusterUIDLabel] = "other" }},
		{name: "wrong parent name", change: func(_ *testing.T, _ *cleanupFixture, ca *corev1.Secret) {
			ca.Labels[tlsport.ClusterNameLabel] = "other"
		}},
		{name: "wrong attempt", change: func(_ *testing.T, _ *cleanupFixture, ca *corev1.Secret) { ca.Labels[blueGreenAttemptLabel] = "other" }},
		{name: "unmanaged", change: func(_ *testing.T, _ *cleanupFixture, ca *corev1.Secret) {
			delete(ca.Labels, "app.kubernetes.io/managed-by")
		}},
		{name: "wrong purpose", change: func(_ *testing.T, _ *cleanupFixture, ca *corev1.Secret) { delete(ca.Labels, tlsport.PurposeLabel) }},
		{name: "foreign owner", change: func(_ *testing.T, _ *cleanupFixture, ca *corev1.Secret) {
			ca.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: "other", UID: "other"}}
		}},
		{name: "contains private key", change: func(_ *testing.T, _ *cleanupFixture, ca *corev1.Secret) { ca.Data["ca.key"] = []byte("private") }},
		{name: "empty CA", change: func(_ *testing.T, _ *cleanupFixture, ca *corev1.Secret) { ca.Data["ca.crt"] = nil }},
		{name: "wrong type", change: func(_ *testing.T, _ *cleanupFixture, ca *corev1.Secret) { ca.Type = corev1.SecretTypeTLS }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newCleanupFixture(t)
			bg := f.parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen
			identity := tlsport.Identity{ClusterName: f.parent.Name, ClusterUID: f.parent.UID, AdditionalLabels: map[string]string{blueGreenAttemptLabel: bg.AttemptID}}
			ca := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: f.retired.Name + "-server-ca", Namespace: f.parent.Namespace, UID: "ca-copy-uid", Labels: identity.Labels()}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"ca.crt": []byte("public CA")}}
			if test.change != nil {
				test.change(t, f, ca)
			}
			require.NoError(t, f.client.Create(t.Context(), ca))
			bg = f.retirementStatus(t)
			found := slices.ContainsFunc(bg.Cleanup.RetiredResources, func(ref corev1.ObjectReference) bool { return ref.UID == ca.UID })
			assert.Equal(t, test.retired, found)
			done := false
			for range 6 {
				if done = f.retire(t); done {
					break
				}
			}
			require.True(t, done)
			err := f.client.Get(t.Context(), client.ObjectKeyFromObject(ca), &corev1.Secret{})
			if test.retired {
				assert.True(t, apierrors.IsNotFound(err))
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.active), &cnpgv1.Cluster{}))
		})
	}
}

func testResourceReference(t *testing.T, object client.Object) corev1.ObjectReference {
	t.Helper()
	ref, err := resourceReference(object)
	require.NoError(t, err)
	return ref
}
