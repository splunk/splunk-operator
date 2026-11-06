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
	"strings"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgpostgres "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	bluegreen "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/blue_green"
	identityadapter "github.com/splunk/splunk-operator/pkg/postgresql/shared/adapter/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestSourceLogicalSlotSynchronizationPatchesNativePrerequisitesAtomically(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	parent := &platformv1alpha1.PostgresCluster{Name: "orders", Namespace: "default"}
	source := &cnpgv1.Cluster{Name: "orders", Namespace: parent.Namespace, Spec: cnpgv1.ClusterSpec{ImageName: "postgres:17", PostgresConfiguration: cnpgv1.PostgresConfiguration{Parameters: map[string]string{"work_mem": "16MB", "hot_standby_feedback": "off"}}}}
	patches := 0
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(source).WithInterceptorFuncs(interceptor.Funcs{Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
		patches++
		cluster := obj.(*cnpgv1.Cluster)
		require.True(t, cluster.Spec.ReplicationSlots.HighAvailability.SynchronizeLogicalDecoding)
		for _, parameter := range []string{"hot_standby_feedback", "sync_replication_slots"} {
			enabled, err := cnpgpostgres.ParsePostgresConfigBoolean(cluster.Spec.PostgresConfiguration.Parameters[parameter])
			require.NoError(t, err)
			require.True(t, enabled, "CNPG admission must see %s in the same update", parameter)
		}
		return c.Patch(ctx, obj, patch, opts...)
	}}).Build()
	preparer := &BlueGreenPreparer{client: c, key: client.ObjectKeyFromObject(parent), cluster: parent}
	before, err := preparer.configurationFingerprint(source, nil)
	require.NoError(t, err)
	require.NoError(t, preparer.enableLogicalSlotSynchronization(t.Context(), source))
	assert.Equal(t, 1, patches)
	live := new(cnpgv1.Cluster)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(source), live))
	assert.Equal(t, "16MB", live.Spec.PostgresConfiguration.Parameters["work_mem"])
	assert.Equal(t, "on", live.Spec.PostgresConfiguration.Parameters["hot_standby_feedback"])
	assert.Equal(t, "on", live.Spec.PostgresConfiguration.Parameters["sync_replication_slots"])
	after, err := preparer.baseline().configurationFingerprint(live, nil)
	require.NoError(t, err)
	assert.Equal(t, before, after, "the captured baseline must equal the complete accepted source overlay")
	require.NoError(t, preparer.enableLogicalSlotSynchronization(t.Context(), live))
	assert.Equal(t, 1, patches, "the complete native configuration is idempotent")
}

func TestLogicalSlotSynchronizationRepairsMissingSettingsAndPreservesEnabledAliases(t *testing.T) {
	for _, test := range []struct {
		name       string
		parameters map[string]string
		already    bool
	}{
		{"missing", nil, false},
		{"disabled", map[string]string{"hot_standby_feedback": "off", "sync_replication_slots": "false"}, false},
		{"already enabled aliases", map[string]string{"hot_standby_feedback": "true", "sync_replication_slots": "1"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			spec := cnpgv1.ClusterSpec{ReplicationSlots: &cnpgv1.ReplicationSlotsConfiguration{HighAvailability: &cnpgv1.ReplicationSlotsHAConfiguration{SynchronizeLogicalDecoding: true}}, PostgresConfiguration: cnpgv1.PostgresConfiguration{Parameters: test.parameters}}
			assert.Equal(t, test.already, setLogicalSlotSynchronization(&spec))
			assert.True(t, setLogicalSlotSynchronization(&spec))
			if test.already {
				assert.Equal(t, "true", spec.PostgresConfiguration.Parameters["hot_standby_feedback"])
				assert.Equal(t, "1", spec.PostgresConfiguration.Parameters["sync_replication_slots"])
			}
		})
	}
	spec := cnpgv1.ClusterSpec{ReplicationSlots: &cnpgv1.ReplicationSlotsConfiguration{HighAvailability: &cnpgv1.ReplicationSlotsHAConfiguration{Enabled: new(false), SynchronizeLogicalDecoding: true}}}
	assert.False(t, setLogicalSlotSynchronization(&spec))
	assert.True(t, spec.ReplicationSlots.HighAvailability.GetEnabled(), "native synchronization requires CNPG HA slot management")
	assert.True(t, setLogicalSlotSynchronization(&spec))
}

func TestBlueGreenPreparerReconcilesSourceArtifactsIdempotently(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, networkingv1.AddToScheme(scheme))
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	const namespace = "default"
	const sourceName = "source-cnpg"
	parent := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: namespace, Generation: 7},
		Status: platformv1alpha1.PostgresClusterStatus{
			ProvisionerRef: &corev1.ObjectReference{Name: sourceName, Namespace: namespace},
		},
	}
	source := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: sourceName, Namespace: namespace, UID: types.UID("source-uid")},
		Spec:       cnpgv1.ClusterSpec{SuperuserSecret: &cnpgv1.LocalObjectReference{Name: sourceName + "-superuser"}, StorageConfiguration: cnpgv1.StorageConfiguration{Size: "10Gi"}},
		Status: cnpgv1.ClusterStatus{
			Phase:           cnpgv1.PhaseHealthy,
			CurrentPrimary:  "source-cnpg-1",
			InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"source-cnpg-1"}},
		},
	}
	database := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-db", Namespace: namespace, UID: types.UID("database-uid")},
		Spec: platformv1alpha1.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: parent.Name},
			Databases:  []platformv1alpha1.DatabaseDefinition{{Name: "orders"}},
		},
	}
	sourceSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: sourceName + "-superuser", Namespace: namespace, UID: types.UID("source-secret")}, Data: map[string][]byte{"password": []byte("not-used-by-fake-preflight")}}
	sourcePod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: sourceName + "-1", Namespace: namespace, Labels: map[string]string{"cnpg.io/cluster": sourceName}}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(source, database, sourceSecret, sourcePod).Build()
	targetStorage := resource.MustParse("10Gi")
	preparer := NewBlueGreenPreparer(client, types.NamespacedName{Name: parent.Name, Namespace: namespace}, parent, identityadapter.NewIdentityResolver(), BlueGreenPreflightPlan{TargetConfiguration: &platformv1alpha1.PostgresClusterSpec{Storage: &targetStorage}})
	preparer.preflight = successfulSourcePreflight{}
	intent := mvutypes.Intent{Strategy: mvutypes.MajorUpgradeFlowBlueGreen, AttemptID: "attempt-1", SourcePgVersion: "17.1", TargetPgVersion: "18"}
	entry, err := bluegreen.New(mvutypes.PreUpgradeBackup, preparer, nil).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.Preflight), entry.Report.Phase)
	intent.State = stateWithProgress(intent, mvutypes.Progress{Report: entry.Report})
	require.NotNil(t, intent.State[0].BlueGreen)
	assert.Nil(t, intent.State[0].BlueGreen.Blue)

	status, err := preparer.Prepare(t.Context(), intent)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowPending)
	require.NotNil(t, status, "publisher preparation records source ownership before waiting for CNPG")
	require.NotNil(t, status.Blue)
	require.NotNil(t, status.Gates)
	require.NotNil(t, status.Gates.Allow)
	require.NotNil(t, status.SourceConfigurationFingerprint)
	baselineFingerprint := *status.SourceConfigurationFingerprint
	require.Len(t, status.DatabaseReplication, 1)
	require.Len(t, status.CredentialFingerprints, 1)
	publicationKey := types.NamespacedName{Name: publicationName(parent.Name, "orders"), Namespace: namespace}
	require.True(t, apierrors.IsNotFound(client.Get(t.Context(), publicationKey, &cnpgv1.Publication{})), "source artifacts must wait for the durable claim")
	require.True(t, apierrors.IsNotFound(client.Get(t.Context(), types.NamespacedName{Name: replicationSecretName(parent.Name, intent.AttemptID), Namespace: namespace}, &corev1.Secret{})))
	unchangedSource := &cnpgv1.Cluster{}
	require.NoError(t, client.Get(t.Context(), types.NamespacedName{Name: sourceName, Namespace: namespace}, unchangedSource))
	assert.Nil(t, unchangedSource.Spec.ReplicationSlots)
	phase := string(mvutypes.Preflight)
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	intent.State = []platformv1alpha1.PostgresMajorUpgradeStatus{{
		Phase:           &phase,
		Strategy:        &strategy,
		SourcePgVersion: &intent.SourcePgVersion,
		TargetPgVersion: &intent.TargetPgVersion,
		BlueGreen:       status,
	}}
	preparer.preflight = failingSourcePreflight{}
	failedStatus, err := preparer.Prepare(t.Context(), intent)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.Nil(t, failedStatus, "an inspection failure must not publish a new preparation status")
	preparer.preflight = successfulSourcePreflight{}
	*preparer.plan.TargetConfiguration.Storage = resource.MustParse("9Gi")
	_, err = preparer.Prepare(t.Context(), intent)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed, "the target plan must stay frozen after source claim")
	require.True(t, apierrors.IsNotFound(client.Get(t.Context(), publicationKey, &cnpgv1.Publication{})))
	*preparer.plan.TargetConfiguration.Storage = resource.MustParse("10Gi")
	_, err = preparer.Prepare(t.Context(), intent)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowPending)
	require.NoError(t, client.Get(t.Context(), publicationKey, &cnpgv1.Publication{}))
	changedSource := &cnpgv1.Cluster{}
	require.NoError(t, client.Get(t.Context(), types.NamespacedName{Name: sourceName, Namespace: namespace}, changedSource))
	changedSource.UID = types.UID("replacement-source-uid")
	require.NoError(t, client.Update(t.Context(), changedSource))
	_, err = preparer.Prepare(t.Context(), intent)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed, "a replacement source must not inherit the prior claim")
	changedSource.UID = source.UID
	require.NoError(t, client.Update(t.Context(), changedSource))

	status, err = preparer.Prepare(t.Context(), intent)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowPending)
	require.Len(t, status.CredentialFingerprints, 2)
	intent.State[0].BlueGreen = status

	publication := &cnpgv1.Publication{}
	require.NoError(t, client.Get(t.Context(), publicationKey, publication))
	applied := true
	publication.Status.ObservedGeneration = publication.Generation
	publication.Status.Applied = &applied
	require.NoError(t, client.Update(t.Context(), publication))

	status, err = preparer.Prepare(t.Context(), intent)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "attempt-1", status.AttemptID)
	require.NotNil(t, status.Blue)
	assert.Equal(t, sourceName, status.Blue.Ref.Name)
	require.NotNil(t, status.Gates)
	require.NotNil(t, status.Gates.Allow)
	assert.Equal(t, int64(7), status.Gates.Allow.Generation)
	require.Len(t, status.DatabaseReplication, 1)
	assert.Equal(t, "orders", status.DatabaseReplication[0].DatabaseName)
	require.NotNil(t, status.DatabaseReplication[0].SourceSchemaFingerprint)
	assert.Equal(t, "schema-orders", *status.DatabaseReplication[0].SourceSchemaFingerprint)
	require.Len(t, status.CredentialFingerprints, 2)
	require.NotNil(t, status.SourceConfigurationFingerprint)
	assert.Equal(t, baselineFingerprint, *status.SourceConfigurationFingerprint, "the completion receipt retains the claimed source baseline")
	require.NotNil(t, status.SourcePreparation)
	require.NotNil(t, status.SourcePreparation.CompletedAt)
	intent.State[0].BlueGreen = status

	updatedSource := &cnpgv1.Cluster{}
	require.NoError(t, client.Get(t.Context(), types.NamespacedName{Name: sourceName, Namespace: namespace}, updatedSource))
	require.NotNil(t, updatedSource.Spec.ReplicationSlots)
	require.NotNil(t, updatedSource.Spec.ReplicationSlots.HighAvailability)
	assert.True(t, updatedSource.Spec.ReplicationSlots.HighAvailability.SynchronizeLogicalDecoding)
	assert.Equal(t, "on", updatedSource.Spec.PostgresConfiguration.Parameters["hot_standby_feedback"])
	assert.Equal(t, "on", updatedSource.Spec.PostgresConfiguration.Parameters["sync_replication_slots"])
	assert.Nil(t, updatedSource.Spec.ReplicationSlots.SynchronizeReplicas)

	require.NoError(t, client.Get(t.Context(), publicationKey, publication))
	assert.Equal(t, sourceName, publication.Spec.ClusterRef.Name)
	assert.Equal(t, "orders", publication.Spec.DBName)
	assert.Equal(t, "attempt-1", publication.Labels[blueGreenAttemptLabel])
	require.Len(t, publication.OwnerReferences, 1)
	assert.Equal(t, parent.Name, publication.OwnerReferences[0].Name)

	replicationSecret := &corev1.Secret{}
	require.NoError(t, client.Get(t.Context(), types.NamespacedName{Name: replicationSecretName(parent.Name, intent.AttemptID), Namespace: namespace}, replicationSecret))
	assert.Equal(t, "attempt-1", replicationSecret.Labels[blueGreenAttemptLabel])
	assert.NotEmpty(t, replicationSecret.Data[replicationSecretPasswordKey])

	applied = false
	publication.Status.Applied = &applied
	require.NoError(t, client.Update(t.Context(), publication))
	status, err = preparer.Prepare(t.Context(), intent)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowPending)
	require.Len(t, status.DatabaseReplication, 1, "a pending retry must preserve the durable database baseline")

	applied = true
	publication.Status.Applied = &applied
	require.NoError(t, client.Update(t.Context(), publication))
	_, err = preparer.Prepare(t.Context(), intent)
	require.NoError(t, err, "a restart must reuse the attempt-scoped publication")
}

func TestBlueGreenPreparerRejectsForeignPublication(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default", UID: types.UID("parent-uid")}}
	database := declaredDatabase{name: "orders"}
	foreign := &cnpgv1.Publication{
		ObjectMeta: metav1.ObjectMeta{Name: publicationName(parent.Name, database.name), Namespace: parent.Namespace, Labels: map[string]string{blueGreenAttemptLabel: "attempt-1"}},
		Spec:       cnpgv1.PublicationSpec{ClusterRef: corev1.LocalObjectReference{Name: "source"}, Name: publicationName(parent.Name, database.name), DBName: database.name, Target: cnpgv1.PublicationTarget{AllTables: true}},
	}
	preparer := &BlueGreenPreparer{client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(foreign).Build(), key: types.NamespacedName{Name: parent.Name, Namespace: parent.Namespace}, cluster: parent}
	_, err := preparer.reconcilePublication(t.Context(), "source", database, "attempt-1")
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}

func TestPublicationNameFitsPostgresIdentifierLimit(t *testing.T) {
	firstCluster := strings.Repeat("a", 50)
	name := publicationName(firstCluster, "orders")
	assert.Len(t, name, postgresIdentifierMaxLength)
	assert.True(t, strings.HasPrefix(name, strings.Repeat("a", 49)+"-bg-"))
	assert.NotEqual(t, name, publicationName(strings.Repeat("a", 49)+"b", "orders"))
}

func TestCredentialSecretNamesIncludesDeclaredAndObservedCredentials(t *testing.T) {
	source := &cnpgv1.Cluster{Spec: cnpgv1.ClusterSpec{SuperuserSecret: &cnpgv1.LocalObjectReference{Name: "superuser"}}, Status: cnpgv1.ClusterStatus{Certificates: cnpgv1.CertificatesStatus{CertificatesConfiguration: cnpgv1.CertificatesConfiguration{ServerCASecret: "server-ca"}}}}
	database := declaredDatabase{
		name: "orders",
		definition: platformv1alpha1.DatabaseDefinition{PasswordConfig: &platformv1alpha1.PasswordConfig{
			ExternalAdminSecretRef: corev1.LocalObjectReference{Name: "admin"},
			ExternalRWSecretRef:    corev1.LocalObjectReference{Name: "rw"},
		}},
		parent: platformv1alpha1.PostgresDatabase{Status: platformv1alpha1.PostgresDatabaseStatus{Databases: []platformv1alpha1.DatabaseInfo{
			{Name: "unrelated", AdminUserSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "ignored"}}},
			{Name: "orders", AdminUserSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "admin"}}, RWUserSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "observed-rw"}}},
		}}},
	}
	assert.Equal(t, []string{"admin", "observed-rw", "replication", "rw", "server-ca", "superuser"}, credentialSecretNames(source, []declaredDatabase{database}, "replication"))
}

func TestBlueGreenPreparerRejectsReusedAuthoritativeSourceName(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	parent := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default"},
		Status: platformv1alpha1.PostgresClusterStatus{
			ProvisionerRef: &corev1.ObjectReference{Name: "source", Namespace: "default", UID: types.UID("expected")},
		},
	}
	source := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: "default", UID: types.UID("reused")}}
	preparer := NewBlueGreenPreparer(fake.NewClientBuilder().WithScheme(scheme).WithObjects(source).Build(), types.NamespacedName{Name: parent.Name, Namespace: parent.Namespace}, parent, identityadapter.NewIdentityResolver(), BlueGreenPreflightPlan{})
	_, err := preparer.Prepare(t.Context(), mvutypes.Intent{AttemptID: "attempt-1", SourcePgVersion: "17"})
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}

func TestBlueGreenPreparerRejectsDuplicateDatabaseDeclarations(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	first := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "first", Namespace: "default"},
		Spec: platformv1alpha1.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "orders"},
			Databases:  []platformv1alpha1.DatabaseDefinition{{Name: "orders"}},
		},
	}
	second := first.DeepCopy()
	second.Name = "second"
	preparer := NewBlueGreenPreparer(fake.NewClientBuilder().WithScheme(scheme).WithObjects(first, second).Build(), types.NamespacedName{Name: "orders", Namespace: "default"}, nil, nil, BlueGreenPreflightPlan{})
	_, err := preparer.managedDatabases(t.Context())
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}
