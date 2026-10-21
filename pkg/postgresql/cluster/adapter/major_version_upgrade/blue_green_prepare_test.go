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
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	bluegreen "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/blue_green"
	tlsport "github.com/splunk/splunk-operator/pkg/postgresql/cluster/ports/tls"
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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

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

func TestBlueGreenCandidatePreparerCreatesSchemaOnlyTargetWithoutProductionArchiving(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default", UID: types.UID("parent-uid")}}
	walArchiver := true
	source := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default", UID: types.UID("source-uid")},
		Spec: cnpgv1.ClusterSpec{
			ImageName: "postgres:17", Instances: 3, SuperuserSecret: &cnpgv1.LocalObjectReference{Name: "orders-superuser"},
			Backup: &cnpgv1.BackupConfiguration{}, Plugins: []cnpgv1.PluginConfiguration{{Name: "barman", IsWALArchiver: &walArchiver}},
		},
		Status: cnpgv1.ClusterStatus{Certificates: cnpgv1.CertificatesStatus{CertificatesConfiguration: cnpgv1.CertificatesConfiguration{ServerCASecret: "orders-ca", ClientCASecret: "orders-client-ca"}}},
	}
	setLogicalSlotSynchronization(&source.Spec)
	database := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-db", Namespace: "default", UID: types.UID("database-uid")},
		Spec:       platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: parent.Name}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "orders"}}},
	}
	attemptID := "attempt-1"
	targetStorage := resource.MustParse("20Gi")
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: replicationSecretName(parent.Name, attemptID), Namespace: parent.Namespace, Labels: map[string]string{blueGreenAttemptLabel: attemptID}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}},
		Data:       map[string][]byte{replicationSecretUsernameKey: []byte(replicationRoleName(attemptID)), replicationSecretPasswordKey: []byte("password")},
	}
	superuserSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "orders-superuser", Namespace: parent.Namespace}, Data: map[string][]byte{replicationSecretPasswordKey: []byte("source-password")}}
	serverCA := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "orders-ca", Namespace: parent.Namespace}, Data: map[string][]byte{"ca.crt": []byte("source-ca"), "ca.key": []byte("source-key")}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent, source, database, secret, superuserSecret, serverCA).Build()
	key := types.NamespacedName{Name: parent.Name, Namespace: parent.Namespace}
	declarations, err := listManagedDatabases(t.Context(), client, key)
	require.NoError(t, err)
	baseline := &BlueGreenPreparer{client: client, key: key, cluster: parent, plan: BlueGreenPreflightPlan{TargetImage: "postgres:18"}}
	fingerprint, err := baseline.configurationFingerprint(source, declarations)
	require.NoError(t, err)
	credentials, err := baseline.credentialFingerprints(t.Context(), source, declarations, secret.Name)
	require.NoError(t, err)
	completedAt := metav1.Now()
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	phase := string(mvutypes.Preflight)
	sourceVersion := "17"
	targetVersion := "18"
	intent := mvutypes.Intent{AttemptID: attemptID, Strategy: strategy, SourcePgVersion: sourceVersion, TargetPgVersion: targetVersion, State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
		Phase: &phase, Strategy: &strategy, SourcePgVersion: &sourceVersion, TargetPgVersion: &targetVersion,
		BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: attemptID,
			Blue:                           &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: source.Name, Namespace: source.Namespace, UID: source.UID}},
			SourcePreparation:              &platformv1alpha1.BlueGreenSourcePreparationStatus{CompletedAt: &completedAt},
			SourceConfigurationFingerprint: &fingerprint, CredentialFingerprints: credentials},
	}}}
	preparer := NewBlueGreenCandidatePreparer(client, key, parent, BlueGreenCandidatePlan{TargetImage: "postgres:18", TargetStorage: &targetStorage, TargetInstances: 3})
	preparer.verifier = candidateSourceVerifiedStub{}

	status, err := preparer.PrepareCandidate(t.Context(), intent)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowPending)
	require.NotNil(t, status.Green)

	green := &cnpgv1.Cluster{}
	require.NoError(t, client.Get(t.Context(), types.NamespacedName{Name: candidateClusterName(parent.Name, attemptID), Namespace: parent.Namespace}, green))
	assert.Equal(t, "postgres:18", green.Spec.ImageName)
	assert.Equal(t, 1, green.Spec.Instances)
	assert.Equal(t, "20Gi", green.Spec.StorageConfiguration.Size)
	require.NotNil(t, green.Spec.Bootstrap)
	require.NotNil(t, green.Spec.Bootstrap.InitDB)
	require.NotNil(t, green.Spec.Bootstrap.InitDB.Import)
	assert.Equal(t, cnpgv1.MonolithSnapshotType, green.Spec.Bootstrap.InitDB.Import.Type)
	assert.True(t, green.Spec.Bootstrap.InitDB.Import.SchemaOnly)
	assert.Equal(t, candidateImportExternalClusterName, green.Spec.Bootstrap.InitDB.Import.Source.ExternalCluster)
	assert.Equal(t, []string{"orders"}, green.Spec.Bootstrap.InitDB.Import.Databases)
	assert.Equal(t, []string{"orders_admin", "orders_rw"}, green.Spec.Bootstrap.InitDB.Import.Roles,
		"the source-only replication role must not be imported without its REPLICATION attribute")
	assert.Nil(t, green.Spec.Backup)
	assert.Empty(t, green.Spec.Plugins)
	require.NotNil(t, green.Spec.Certificates)
	assert.Equal(t, "orders-ca", green.Spec.Certificates.ServerCASecret)
	assert.Empty(t, green.Spec.Certificates.ServerTLSSecret)
	require.Len(t, green.Spec.ExternalClusters, 2)
	importSource, replicationSource := green.Spec.ExternalClusters[0], green.Spec.ExternalClusters[1]
	assert.Equal(t, candidateImportExternalClusterName, importSource.Name)
	assert.Equal(t, "postgres", importSource.ConnectionParameters["user"])
	assert.Equal(t, superuserSecret.Name, importSource.Password.Name)
	assert.Equal(t, candidateSourceExternalClusterName, replicationSource.Name)
	assert.Equal(t, replicationRoleName(attemptID), replicationSource.ConnectionParameters["user"])
	assert.Equal(t, secret.Name, replicationSource.Password.Name)
	for _, external := range green.Spec.ExternalClusters {
		assert.Equal(t, "orders-rw.default.svc.cluster.local", external.ConnectionParameters["host"])
		assert.Equal(t, "verify-full", external.ConnectionParameters["sslmode"])
		assert.Equal(t, "orders-ca", external.SSLRootCert.Name)
	}

	intent.State[0].BlueGreen = status
	_, err = preparer.PrepareCandidate(t.Context(), intent)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowPending, "reconcile restart reuses the immutable candidate")
	green.Spec.Instances = 3
	require.NoError(t, client.Update(t.Context(), green))
	_, err = preparer.PrepareCandidate(t.Context(), intent)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowPending, "a scaled candidate remains valid on reconcile restart")

	updatedSource := &cnpgv1.Cluster{}
	require.NoError(t, client.Get(t.Context(), types.NamespacedName{Name: source.Name, Namespace: source.Namespace}, updatedSource))
	require.NotNil(t, updatedSource.Spec.Backup, "green creation must not modify blue backup policy")
	require.Len(t, updatedSource.Spec.Plugins, 1, "green creation must not modify blue WAL archiving")

	updatedSource.Spec.Instances++
	require.NoError(t, client.Update(t.Context(), updatedSource))
	driftIntent := intent
	driftIntent.AttemptID = "attempt-2"
	driftIntent.State[0].BlueGreen = status.DeepCopy()
	driftIntent.State[0].BlueGreen.AttemptID = driftIntent.AttemptID
	driftIntent.State[0].BlueGreen.Green = nil
	_, err = preparer.PrepareCandidate(t.Context(), driftIntent)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.ErrorContains(t, err, "frozen blue/green configuration changed")
	err = client.Get(t.Context(), types.NamespacedName{Name: candidateClusterName(parent.Name, driftIntent.AttemptID), Namespace: parent.Namespace}, &cnpgv1.Cluster{})
	require.True(t, apierrors.IsNotFound(err), "source drift must be rejected before a second green is created")
}

type candidateSourceVerifiedStub struct{}

func (candidateSourceVerifiedStub) VerifySourceBaseline(context.Context, *cnpgv1.Cluster, []declaredDatabase, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	return nil
}

func (candidateSourceVerifiedStub) VerifyAndGuard(context.Context, *cnpgv1.Cluster, []declaredDatabase, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	return nil
}

func (candidateSourceVerifiedStub) VerifyReplication(context.Context, *cnpgv1.Cluster, *cnpgv1.Cluster, []declaredDatabase, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	return false, nil
}

func TestBlueGreenCandidatePreparerCreatesInitialCopySubscriptions(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default", UID: types.UID("parent-uid")}}
	database := declaredDatabase{name: "orders"}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).Build()
	preparer := NewBlueGreenCandidatePreparer(client, types.NamespacedName{Name: parent.Name, Namespace: parent.Namespace}, parent, BlueGreenCandidatePlan{})
	imported := true
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{DatabaseReplication: []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{DatabaseName: database.name, SchemaImported: &imported}}}
	candidate := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "orders-bg-12345", Namespace: parent.Namespace}}

	ready, err := preparer.reconcileSubscriptions(t.Context(), candidate, []declaredDatabase{database}, "attempt-1", status)
	require.NoError(t, err)
	assert.False(t, ready)

	subscription := &cnpgv1.Subscription{}
	require.NoError(t, client.Get(t.Context(), types.NamespacedName{Name: subscriptionName(parent.Name, database.name), Namespace: parent.Namespace}, subscription))
	assert.Equal(t, candidate.Name, subscription.Spec.ClusterRef.Name)
	assert.Equal(t, publicationName(parent.Name, database.name), subscription.Spec.PublicationName)
	assert.Equal(t, candidateSourceExternalClusterName, subscription.Spec.ExternalClusterName)
	assert.Equal(t, "true", subscription.Spec.Parameters["copy_data"])
	assert.Equal(t, "true", subscription.Spec.Parameters["failover"])
	assert.Equal(t, subscription.Name, subscription.Spec.Parameters["slot_name"])
	assert.Equal(t, "attempt-1", subscription.Labels[blueGreenAttemptLabel])
	subscription.Spec.Parameters["failover"] = "false"
	desired := preparer.buildCandidateSubscription(candidate.Name, database.name, subscription.Name, "attempt-1")
	require.ErrorIs(t, preparer.validateSubscription(subscription, desired, "attempt-1"), mvutypes.ErrUpgradeFlowFailed)
	subscription.Spec.Parameters["failover"] = "true"
	subscription.Spec.ReclaimPolicy = cnpgv1.SubscriptionReclaimRetain
	require.ErrorIs(t, preparer.validateSubscription(subscription, desired, "attempt-1"), mvutypes.ErrUpgradeFlowFailed)
}

func TestCandidateValidationRejectsManagedSpecDrift(t *testing.T) {
	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default", UID: "parent-uid"}}
	preparer := &BlueGreenCandidatePreparer{cluster: parent, plan: BlueGreenCandidatePlan{TargetInstances: 3}}
	source := &cnpgv1.Cluster{Spec: cnpgv1.ClusterSpec{Instances: 3, MinSyncReplicas: 1, MaxSyncReplicas: 2}}
	desired := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "green", Namespace: parent.Namespace}, Spec: cnpgv1.ClusterSpec{
		ImageName: "postgres:18", Instances: 1, StorageConfiguration: cnpgv1.StorageConfiguration{Size: "20Gi"},
		Bootstrap:        &cnpgv1.BootstrapConfiguration{InitDB: &cnpgv1.BootstrapInitDB{Import: &cnpgv1.Import{Databases: []string{"orders"}, SchemaOnly: true}}},
		ExternalClusters: []cnpgv1.ExternalCluster{{Name: candidateSourceExternalClusterName, ConnectionParameters: map[string]string{"host": "blue-rw"}}},
		Certificates:     &cnpgv1.CertificatesConfiguration{ServerCASecret: "orders-ca"},
	}}
	base := desired.DeepCopy()
	base.Labels = map[string]string{blueGreenAttemptLabel: "attempt-1"}
	base.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}
	require.NoError(t, preparer.validateCandidate(base, desired, source, "attempt-1"))
	scaled := base.DeepCopy()
	scaled.Spec.Instances = source.Spec.Instances
	scaled.Spec.MinSyncReplicas = source.Spec.MinSyncReplicas
	scaled.Spec.MaxSyncReplicas = source.Spec.MaxSyncReplicas
	require.NoError(t, preparer.validateCandidate(scaled, desired, source, "attempt-1"))
	tests := []struct {
		name   string
		mutate func(*cnpgv1.Cluster)
	}{
		{"storage", func(c *cnpgv1.Cluster) { c.Spec.StorageConfiguration.Size = "10Gi" }},
		{"resources", func(c *cnpgv1.Cluster) {
			c.Spec.Resources.Requests = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}
		}},
		{"postgresql configuration", func(c *cnpgv1.Cluster) {
			c.Spec.PostgresConfiguration.Parameters = map[string]string{"shared_buffers": "256MB"}
		}},
		{"HBA", func(c *cnpgv1.Cluster) { c.Spec.PostgresConfiguration.PgHBA = []string{"host all all 0.0.0.0/0 trust"} }},
		{"schema import", func(c *cnpgv1.Cluster) { c.Spec.Bootstrap.InitDB.Import.Databases = []string{"other"} }},
		{"external cluster", func(c *cnpgv1.Cluster) { c.Spec.ExternalClusters[0].ConnectionParameters["host"] = "other-rw" }},
		{"CA", func(c *cnpgv1.Cluster) { c.Spec.Certificates.ServerCASecret = "other-ca" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := base.DeepCopy()
			test.mutate(candidate)
			require.ErrorIs(t, preparer.validateCandidate(candidate, desired, source, "attempt-1"), mvutypes.ErrUpgradeFlowFailed)
		})
	}
}

func TestCandidatePoolerValidationComparesFullDesiredSpec(t *testing.T) {
	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default", UID: "parent-uid"}}
	preparer := &BlueGreenCandidatePreparer{cluster: parent}
	source := &cnpgv1.Pooler{ObjectMeta: metav1.ObjectMeta{Name: "blue-pooler-rw", Namespace: parent.Namespace}, Spec: cnpgv1.PoolerSpec{
		Cluster: cnpgv1.LocalObjectReference{Name: "blue"}, Type: cnpgv1.PoolerTypeRW,
		PgBouncer: &cnpgv1.PgBouncerSpec{Parameters: map[string]string{"max_client_conn": "100"}},
	}}
	desired := preparer.buildCandidatePooler(source, "green", "green-pooler-rw", "attempt-1")
	require.NoError(t, preparer.validateCandidatePooler(desired.DeepCopy(), desired, "attempt-1"))
	changed := desired.DeepCopy()
	changed.Spec.PgBouncer.Parameters["max_client_conn"] = "200"
	require.ErrorIs(t, preparer.validateCandidatePooler(changed, desired, "attempt-1"), mvutypes.ErrUpgradeFlowFailed)
}

func TestCandidatePoolerReadyRequiresServingPods(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	pooler := &cnpgv1.Pooler{ObjectMeta: metav1.ObjectMeta{Name: "green-pooler-rw", Namespace: "default", UID: "pooler-uid"},
		Spec:   cnpgv1.PoolerSpec{Instances: ptr.To(int32(2))},
		Status: cnpgv1.PoolerStatus{Phase: cnpgv1.PoolerPhaseActive, Instances: 2}}
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: pooler.Name, Namespace: pooler.Namespace,
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(pooler, cnpgv1.SchemeGroupVersion.WithKind("Pooler"))}},
		Spec: corev1.ServiceSpec{Selector: map[string]string{cnpgutils.PgbouncerNameLabel: pooler.Name}}}
	readyPod := func(name string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pooler.Namespace,
			Labels: map[string]string{cnpgutils.PgbouncerNameLabel: pooler.Name}},
			Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
	}
	for _, test := range []struct {
		name  string
		phase cnpgv1.PoolerPhase
		pods  []*corev1.Pod
		want  bool
	}{
		{name: "both serving", phase: cnpgv1.PoolerPhaseActive, pods: []*corev1.Pod{readyPod("one"), readyPod("two")}, want: true},
		{name: "paused", phase: cnpgv1.PoolerPhasePaused, pods: []*corev1.Pod{readyPod("one"), readyPod("two")}},
		{name: "one missing", phase: cnpgv1.PoolerPhaseActive, pods: []*corev1.Pod{readyPod("one")}},
		{name: "one unready", phase: cnpgv1.PoolerPhaseActive, pods: []*corev1.Pod{readyPod("one"), {ObjectMeta: metav1.ObjectMeta{Name: "two", Namespace: pooler.Namespace, Labels: map[string]string{cnpgutils.PgbouncerNameLabel: pooler.Name}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := pooler.DeepCopy()
			current.Status.Phase = test.phase
			objects := make([]client.Object, 0, len(test.pods))
			for _, pod := range test.pods {
				objects = append(objects, pod)
			}
			preparer := &BlueGreenCandidatePreparer{client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()}
			ready, err := preparer.candidatePoolerReady(t.Context(), current, service)
			require.NoError(t, err)
			assert.Equal(t, test.want, ready)
		})
	}
}

type recordingCandidateTLSManager struct {
	request          tlsport.Request
	existingDNSNames []string
	err              error
}

func (m *recordingCandidateTLSManager) ExistingCertificateDNSNames(context.Context, tlsport.Identity) ([]string, error) {
	return m.existingDNSNames, nil
}
func (m *recordingCandidateTLSManager) Reconcile(_ context.Context, request tlsport.Request) error {
	m.request = request
	return m.err
}
func (*recordingCandidateTLSManager) Finalize(context.Context, tlsport.FinalizeRequest) error {
	return nil
}

func TestCandidateCertManagerLeafReusesFrozenBlueCA(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	mode := platformv1alpha1.PostgresCertificateModeCertManager
	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default", UID: "parent-uid"}, Spec: platformv1alpha1.PostgresClusterSpec{Class: "gold"}}
	class := &platformv1alpha1.PostgresClusterClass{ObjectMeta: metav1.ObjectMeta{Name: "gold"}, Spec: platformv1alpha1.PostgresClusterClassSpec{TLS: &platformv1alpha1.PostgresClusterClassTLS{Certificates: &platformv1alpha1.PostgresCertificateConfig{
		Mode: &mode, IssuerRef: &platformv1alpha1.CertificateIssuerReference{Name: "platform-ca", Kind: "Issuer"},
	}}}}
	source := &cnpgv1.Cluster{Spec: cnpgv1.ClusterSpec{Certificates: &cnpgv1.CertificatesConfiguration{ServerTLSSecret: "orders-server-tls"}}, Status: cnpgv1.ClusterStatus{Certificates: cnpgv1.CertificatesStatus{CertificatesConfiguration: cnpgv1.CertificatesConfiguration{ServerCASecret: "orders-ca"}}}}
	blueCA := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "orders-ca", Namespace: parent.Namespace}, Data: map[string][]byte{"ca.crt": []byte("same-ca")}}
	greenCA := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "orders-bg-server-ca", Namespace: parent.Namespace}, Data: map[string][]byte{"ca.crt": []byte("same-ca")}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(class, blueCA, greenCA).Build()
	retainedPoolerName := "orders-bg-pooler-rw.default.svc.cluster.local"
	manager := &recordingCandidateTLSManager{existingDNSNames: []string{retainedPoolerName, "old.example.com"}}
	preparer := &BlueGreenCandidatePreparer{client: client, cluster: parent, key: types.NamespacedName{Name: parent.Name, Namespace: parent.Namespace}, plan: BlueGreenCandidatePlan{TargetConfiguration: &platformv1alpha1.PostgresClusterSpec{}}, tls: manager}
	leaf, err := preparer.reconcileCandidateServerTLS(t.Context(), source, "orders-bg", "attempt-1")
	require.NoError(t, err)
	assert.Equal(t, "orders-bg-server-tls", leaf)
	assert.Equal(t, "orders-bg-server-tls", manager.request.Identity.CertificateName)
	assert.Equal(t, "attempt-1", manager.request.Identity.Labels()[blueGreenAttemptLabel])
	assert.Contains(t, manager.request.DNSNames, "orders-bg-rw.default.svc.cluster.local")
	assert.Contains(t, manager.request.DNSNames, retainedPoolerName)
	assert.Equal(t, []string{retainedPoolerName}, manager.request.RetainedPoolerDNSNames)
	assert.NotContains(t, manager.request.DNSNames, "old.example.com")
	assert.Equal(t, "platform-ca", manager.request.IssuerRef.Name)

	greenCA.Data["ca.crt"] = []byte("different-ca")
	require.NoError(t, client.Update(t.Context(), greenCA))
	_, err = preparer.reconcileCandidateServerTLS(t.Context(), source, "orders-bg", "attempt-1")
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}

func TestCNPGCandidateLeafRequiresSourceCASigningKey(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	source := &cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{Certificates: cnpgv1.CertificatesStatus{CertificatesConfiguration: cnpgv1.CertificatesConfiguration{ServerCASecret: "orders-ca"}}}}
	ca := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "orders-ca", Namespace: "default"}, Data: map[string][]byte{"ca.crt": []byte("certificate")}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ca).Build()
	preparer := &BlueGreenCandidatePreparer{client: client, key: types.NamespacedName{Name: "orders", Namespace: "default"}}
	_, err := preparer.reconcileCandidateServerTLS(t.Context(), source, "orders-bg", "attempt-1")
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	ca.Data["ca.key"] = []byte("signing-key")
	require.NoError(t, client.Update(t.Context(), ca))
	leaf, err := preparer.reconcileCandidateServerTLS(t.Context(), source, "orders-bg", "attempt-1")
	require.NoError(t, err)
	assert.Empty(t, leaf, "CNPG must issue the candidate leaf under the shared CA")
}

func TestCandidateTopologyWaitsForObservedReplicasAndRestoresSyncPolicy(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	source := &cnpgv1.Cluster{Spec: cnpgv1.ClusterSpec{Instances: 3, MinSyncReplicas: 1, MaxSyncReplicas: 2}}
	candidate := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "green", Namespace: "default"},
		Spec:       cnpgv1.ClusterSpec{Instances: 1},
		Status: cnpgv1.ClusterStatus{
			Phase: cnpgv1.PhaseHealthy, Instances: 1, ReadyInstances: 1, CurrentPrimary: "green-1",
			InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"green-1"}},
		},
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(candidate).Build()
	preparer := &BlueGreenCandidatePreparer{client: client, plan: BlueGreenCandidatePlan{TargetInstances: 3}}

	ready, err := preparer.reconcileCandidateTopology(t.Context(), candidate, source)
	require.NoError(t, err)
	assert.False(t, ready, "a spec patch is not proof of healthy replicas")
	assert.Equal(t, 3, candidate.Spec.Instances)
	assert.Equal(t, 1, candidate.Spec.MinSyncReplicas)
	assert.Equal(t, 2, candidate.Spec.MaxSyncReplicas)

	ready, err = preparer.reconcileCandidateTopology(t.Context(), candidate, source)
	require.NoError(t, err)
	assert.False(t, ready, "stale healthy 1/1 status must not certify a 3-instance target")

	candidate.Status.Instances = 3
	candidate.Status.ReadyInstances = 3
	candidate.Status.InstancesStatus[cnpgv1.PodHealthy] = []string{"green-1", "green-2", "green-3"}
	ready, err = preparer.reconcileCandidateTopology(t.Context(), candidate, source)
	require.NoError(t, err)
	assert.True(t, ready)
}

func TestCandidateClusterNameLeavesRoomForPoolerResources(t *testing.T) {
	name := candidateClusterName(strings.Repeat("a", postgresIdentifierMaxLength), "attempt-1")
	assert.LessOrEqual(t, len(name), candidateClusterNameMaxLength)
	assert.LessOrEqual(t, len(candidatePoolerName(name, "rw")), postgresIdentifierMaxLength)
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

func TestSourceBaselineDetectsSlotSynchronizationDrift(t *testing.T) {
	parent := &platformv1alpha1.PostgresCluster{}
	source := &cnpgv1.Cluster{}
	preparer := &BlueGreenPreparer{cluster: parent}
	frozen, err := preparer.configurationFingerprint(source, nil)
	require.NoError(t, err)
	live, err := preparer.baseline().configurationFingerprint(source, nil)
	require.NoError(t, err)
	assert.NotEqual(t, frozen, live, "disabling source slot synchronization must change the frozen fingerprint")
	setLogicalSlotSynchronization(&source.Spec)
	live, err = preparer.baseline().configurationFingerprint(source, nil)
	require.NoError(t, err)
	assert.Equal(t, frozen, live)
}

func TestSourceBaselineDetectsServerCARotation(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	ca := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "server-ca", Namespace: "default", UID: "ca-uid"}, Data: map[string][]byte{"ca.crt": []byte("original")}}
	superuser := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "source-superuser", Namespace: "default"}, Data: map[string][]byte{"password": []byte("unchanged")}}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ca, superuser).Build()
	source := &cnpgv1.Cluster{Spec: cnpgv1.ClusterSpec{SuperuserSecret: &cnpgv1.LocalObjectReference{Name: superuser.Name}}, Status: cnpgv1.ClusterStatus{Certificates: cnpgv1.CertificatesStatus{CertificatesConfiguration: cnpgv1.CertificatesConfiguration{ServerCASecret: ca.Name}}}}
	baseline := sourceBaseline{client: reader, key: types.NamespacedName{Namespace: ca.Namespace}}
	frozen, err := baseline.credentialFingerprints(t.Context(), source, nil, "")
	require.NoError(t, err)
	ca.Data["ca.crt"] = []byte("rotated")
	require.NoError(t, reader.Update(t.Context(), ca))
	current, err := baseline.credentialFingerprints(t.Context(), source, nil, "")
	require.NoError(t, err)
	require.ErrorIs(t, verifyCredentialBaseline(frozen, current), mvutypes.ErrUpgradeFlowFailed)
}

func TestValidateReplicationNetworkPolicyRejectsImplicitIngressPolicySelectingSource(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, networkingv1.AddToScheme(scheme))
	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "source-only", Namespace: "default"},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"cnpg.io/cluster": "source", "role": "primary"}},
		},
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "source-1", Namespace: "default", Labels: map[string]string{"cnpg.io/cluster": "source", "role": "primary"}}}
	preparer := &BlueGreenPreparer{client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(policy, pod).Build()}
	err := preparer.validateReplicationNetworkPolicy(t.Context(), &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: "default"}})
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}

func TestValidateReplicationNetworkPolicyRejectsFutureGreenEgressIsolation(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, networkingv1.AddToScheme(scheme))
	source := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: "default"}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "source-1", Namespace: "default", Labels: map[string]string{"cnpg.io/cluster": source.Name}}}
	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "deny-egress", Namespace: "default"},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
		},
	}
	preparer := &BlueGreenPreparer{client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(policy, pod).Build()}
	require.ErrorIs(t, preparer.validateReplicationNetworkPolicy(t.Context(), source), mvutypes.ErrUpgradeFlowFailed)

	policy.Spec.PodSelector.MatchLabels = map[string]string{"cnpg.io/cluster": source.Name}
	preparer.client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(policy, pod).Build()
	require.NoError(t, preparer.validateReplicationNetworkPolicy(t.Context(), source), "a source-only egress policy cannot isolate future green")
}

type successfulSourcePreflight struct{}

type failingSourcePreflight struct{ successfulSourcePreflight }

func (failingSourcePreflight) Inspect(context.Context, *cnpgv1.Cluster, []declaredDatabase, string) ([]databasePreflight, error) {
	return nil, mvutypes.ErrUpgradeFlowFailed
}

func (successfulSourcePreflight) Inspect(_ context.Context, _ *cnpgv1.Cluster, databases []declaredDatabase, _ string) ([]databasePreflight, error) {
	result := make([]databasePreflight, 0, len(databases))
	for _, database := range databases {
		result = append(result, databasePreflight{name: database.name, schemaFingerprint: "schema-" + database.name, accessFingerprint: "access-" + database.name})
	}
	return result, nil
}

func (successfulSourcePreflight) EnsureReplicationRole(context.Context, *cnpgv1.Cluster, []declaredDatabase, string, string) error {
	return nil
}

func TestBlueGreenPreparerRejectsSourceWithoutNativeLogicalSlotFailover(t *testing.T) {
	preparer := NewBlueGreenPreparer(fake.NewClientBuilder().Build(), types.NamespacedName{}, nil, identityadapter.NewIdentityResolver(), BlueGreenPreflightPlan{})
	_, err := preparer.Prepare(t.Context(), mvutypes.Intent{AttemptID: "attempt-1", SourcePgVersion: "16"})
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}

func TestBlueGreenPreparerRejectsInsufficientTargetStorage(t *testing.T) {
	targetStorage := resource.MustParse("9Gi")
	preparer := NewBlueGreenPreparer(nil, types.NamespacedName{}, nil, nil, BlueGreenPreflightPlan{TargetConfiguration: &platformv1alpha1.PostgresClusterSpec{Storage: &targetStorage}})
	err := preparer.validateTargetPlan(&cnpgv1.Cluster{Spec: cnpgv1.ClusterSpec{StorageConfiguration: cnpgv1.StorageConfiguration{Size: "10Gi"}}})
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}

func TestVerifyBlueGreenBaselineRejectsConfigurationDrift(t *testing.T) {
	oldFingerprint, newFingerprint := "old", "new"
	intent := mvutypes.Intent{AttemptID: "attempt-1", State: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt-1", Blue: &platformv1alpha1.BlueGreenEnvironmentStatus{}, SourceConfigurationFingerprint: &oldFingerprint}}}}
	err := verifyBlueGreenBaseline(intent, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt-1", SourceConfigurationFingerprint: &newFingerprint})
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}

func TestVerifyBlueGreenBaselineStartsAfterSourceClaim(t *testing.T) {
	intent := mvutypes.Intent{AttemptID: "attempt-1", State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
		BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt-1"},
	}}}
	current := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt-1", DatabaseReplication: []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{DatabaseName: "orders"}}}
	require.NoError(t, verifyBlueGreenBaseline(intent, current), "the Preflight placeholder is not a baseline")

	intent.State[0].BlueGreen.Blue = &platformv1alpha1.BlueGreenEnvironmentStatus{}
	require.ErrorIs(t, verifyBlueGreenBaseline(intent, current), mvutypes.ErrUpgradeFlowFailed,
		"a claimed empty database inventory must reject a later addition")
}

func TestVerifyDatabaseBaselineRejectsAddedDeclaration(t *testing.T) {
	previous := []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{DatabaseName: "orders"}}
	current := append(append([]platformv1alpha1.BlueGreenDatabaseReplicationStatus{}, previous...), platformv1alpha1.BlueGreenDatabaseReplicationStatus{DatabaseName: "extra"})
	require.ErrorIs(t, verifyDatabaseBaseline(previous, current), mvutypes.ErrUpgradeFlowFailed)
}

func TestConfigurationFingerprintCoversFrozenDeclarationsAndLiveBlue(t *testing.T) {
	raw := &platformv1alpha1.PostgresCluster{Spec: platformv1alpha1.PostgresClusterSpec{PostgreSQLConfig: map[string]string{"shared_buffers": "128MB"}}}
	effective := &platformv1alpha1.PostgresClusterSpec{PostgreSQLConfig: map[string]string{"shared_buffers": "128MB"}}
	preparer := &BlueGreenPreparer{cluster: raw, plan: BlueGreenPreflightPlan{TargetConfiguration: effective, TargetImage: "postgres:18"}}
	source := &cnpgv1.Cluster{Spec: cnpgv1.ClusterSpec{ImageName: "postgres:17"}}
	databases := []declaredDatabase{{parent: platformv1alpha1.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "orders-db", UID: types.UID("orders-uid")}}, definition: platformv1alpha1.DatabaseDefinition{Name: "orders"}}}

	fingerprint, err := preparer.configurationFingerprint(source, databases)
	require.NoError(t, err)
	effective.PostgreSQLConfig["shared_buffers"] = "256MB"
	changedEffective, err := preparer.configurationFingerprint(source, databases)
	require.NoError(t, err)
	assert.NotEqual(t, fingerprint, changedEffective, "the effective target plan is frozen even when blue has not changed")
	effective.PostgreSQLConfig["shared_buffers"] = "128MB"
	preparer.plan.TargetImage = "postgres:18-alternate"
	changedImage, err := preparer.configurationFingerprint(source, databases)
	require.NoError(t, err)
	assert.NotEqual(t, fingerprint, changedImage, "the resolved target image is frozen")
	preparer.plan.TargetImage = "postgres:18"
	databases[0].definition.AdminRoleName = "different_admin"
	changedDatabase, err := preparer.configurationFingerprint(source, databases)
	require.NoError(t, err)
	assert.NotEqual(t, fingerprint, changedDatabase, "a managed database definition must change the baseline")
	databases[0].definition.AdminRoleName = ""

	source.Spec.ReplicationSlots = &cnpgv1.ReplicationSlotsConfiguration{HighAvailability: &cnpgv1.ReplicationSlotsHAConfiguration{SynchronizeLogicalDecoding: true}}
	afterOwnPatch, err := preparer.configurationFingerprint(source, databases)
	require.NoError(t, err)
	assert.Equal(t, fingerprint, afterOwnPatch, "the preparer's own slot-sync patch is not external drift")
	source.Spec.ReplicationSlots.UpdateInterval = cnpgv1.DefaultReplicationSlotsUpdateInterval
	source.Spec.ReplicationSlots.HighAvailability.SlotPrefix = cnpgv1.DefaultReplicationSlotsHASlotPrefix
	withCNPGDefaults, err := preparer.configurationFingerprint(source, databases)
	require.NoError(t, err)
	assert.Equal(t, fingerprint, withCNPGDefaults, "CNPG defaults are not configuration drift")
	accessDisabled := false
	source.Spec.EnableSuperuserAccess = &accessDisabled
	withAccessDefault, err := preparer.configurationFingerprint(source, databases)
	require.NoError(t, err)
	assert.Equal(t, fingerprint, withAccessDefault, "an injected false default is not configuration drift")
	source.Spec.PostgresConfiguration.Parameters = map[string]string{}
	withEmptyParameters, err := preparer.configurationFingerprint(source, databases)
	require.NoError(t, err)
	assert.Equal(t, fingerprint, withEmptyParameters, "an injected empty map is not configuration drift")
	source.Spec.ReplicationSlots.UpdateInterval++
	changedSlots, err := preparer.configurationFingerprint(source, databases)
	require.NoError(t, err)
	assert.NotEqual(t, fingerprint, changedSlots, "live replication-slot changes must be frozen")
	source.Spec.ReplicationSlots.UpdateInterval--
	source.Spec.PostgresConfiguration.Parameters = map[string]string{"shared_buffers": "256MB"}
	changedParameters, err := preparer.configurationFingerprint(source, databases)
	require.NoError(t, err)
	assert.NotEqual(t, fingerprint, changedParameters, "live PostgreSQL parameters must be frozen")
	source.Spec.PostgresConfiguration.Parameters = nil

	raw.Spec.PostgreSQLConfig["shared_buffers"] = "64MB"
	changedCluster, err := preparer.configurationFingerprint(source, databases)
	require.NoError(t, err)
	assert.NotEqual(t, fingerprint, changedCluster)

	source.Spec.ImageName = "postgres:18"
	changedCNPG, err := preparer.configurationFingerprint(source, databases)
	require.NoError(t, err)
	assert.NotEqual(t, changedCluster, changedCNPG)
}

func TestPreserveCredentialCaptureTimesRetainsExistingBaselineTime(t *testing.T) {
	capturedAt := metav1.Now()
	previous := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{CredentialFingerprints: []platformv1alpha1.BlueGreenCredentialFingerprintStatus{{
		SecretName: "credentials", SecretUID: "uid", Fingerprint: "stable", CapturedAt: &capturedAt,
	}}}
	current := []platformv1alpha1.BlueGreenCredentialFingerprintStatus{
		{SecretName: "credentials", SecretUID: "uid", Fingerprint: "stable"},
		{SecretName: "rotated", SecretUID: "new", Fingerprint: "replacement"},
	}

	result := preserveCredentialCaptureTimes(previous, current)
	require.Equal(t, capturedAt, *result[0].CapturedAt)
	require.Nil(t, result[1].CapturedAt)
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
