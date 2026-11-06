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
	"strings"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

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
		"publisher credentials must remain source-only while application roles are imported unchanged")
	assert.Equal(t, []string{candidatePublisherShadowSQL(replicationRoleName(attemptID))}, green.Spec.Bootstrap.InitDB.PostInitSQL,
		"CNPG must precreate the inert ACL recipient before restoring schema grants")
	assert.Contains(t, green.Spec.Bootstrap.InitDB.PostInitSQL[0], "NOLOGIN NOREPLICATION NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS INHERIT CONNECTION LIMIT -1 PASSWORD NULL")
	assert.Empty(t, green.Spec.Bootstrap.InitDB.Import.PgRestoreExtraOptions, "application ownership and ACL restoration must retain native defaults")
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
	// A bootstrap Job that already ran the old contract may have partial data.
	// Changing the Cluster spec cannot safely replay its initdb/import command.
	legacy := green.DeepCopy()
	legacy.UID = "failed-legacy-green"
	legacy.Spec.Bootstrap.InitDB.PostInitSQL = nil
	legacy.Status.Phase = cnpgv1.PhaseWaitingForUser
	legacy.Status.PhaseReason = "schema restore failed because its ACL recipient was absent"
	require.NoError(t, client.Update(t.Context(), legacy))
	_, err = preparer.PrepareCandidate(t.Context(), intent)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.ErrorContains(t, err, "differs from the desired candidate spec")
	unchangedLegacy := new(cnpgv1.Cluster)
	require.NoError(t, client.Get(t.Context(), types.NamespacedName{Name: legacy.Name, Namespace: legacy.Namespace}, unchangedLegacy))
	assert.Equal(t, legacy.UID, unchangedLegacy.UID, "failed bootstrap must not silently replace a recorded environment")
	assert.Empty(t, unchangedLegacy.Spec.Bootstrap.InitDB.PostInitSQL, "immutable bootstrap drift must not be patched onto a partial PVC")

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

func TestCandidatePreparerUsesLiveInstanceReaderAndExplicitWriter(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	parent := &platformv1alpha1.PostgresCluster{Name: "orders", Namespace: "default", UID: "parent-uid"}
	key := client.ObjectKeyFromObject(parent)
	source := &cnpgv1.Cluster{Name: "blue", Namespace: parent.Namespace, UID: "blue-uid", Status: cnpgv1.ClusterStatus{InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"blue-1"}}}}
	cachedPod := observedInstanceTestPod(source, "blue-1", "10.42.0.10")
	cachedPod.UID = "old-pod-uid"
	livePod := observedInstanceTestPod(source, "blue-1", "10.42.0.20")
	livePod.UID = "new-pod-uid"
	writer := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cachedPod).Build()
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(livePod).Build()
	preparer := NewBlueGreenCandidatePreparerWithReader(writer, reader, key, parent, BlueGreenCandidatePlan{})
	verifier := preparer.verifier.(*postgresCandidateVerifier)
	names, pods, err := verifier.healthySourcePods(t.Context(), source)
	require.NoError(t, err)
	assert.Equal(t, []string{livePod.Name}, names)
	assert.Equal(t, livePod.UID, pods[livePod.Name].UID)
	assert.Equal(t, livePod.Status.PodIP, pods[livePod.Name].Status.PodIP, "candidate SQL must use the uncached PodIP")

	subscription := preparer.buildCandidateSubscription("green", "orders", subscriptionName(parent.Name, "orders"), "attempt")
	require.NoError(t, preparer.client.Create(t.Context(), subscription))
	require.NoError(t, writer.Get(t.Context(), client.ObjectKeyFromObject(subscription), &cnpgv1.Subscription{}))
	require.True(t, apierrors.IsNotFound(reader.Get(t.Context(), client.ObjectKeyFromObject(subscription), &cnpgv1.Subscription{})), "the API reader cannot acquire writer operations")

	livePod.OwnerReferences[0].UID = "foreign-cluster"
	require.NoError(t, reader.Update(t.Context(), livePod))
	_, _, err = verifier.healthySourcePods(t.Context(), source)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed, "a stale owned cached Pod cannot override live foreign ownership")
}

func TestCandidatePreparerReaderConstructorRejectsInvalidWiring(t *testing.T) {
	c := fake.NewClientBuilder().Build()
	parent := &platformv1alpha1.PostgresCluster{Name: "orders", Namespace: "default"}
	key := client.ObjectKeyFromObject(parent)
	for _, construct := range []func(){
		func() { NewBlueGreenCandidatePreparerWithReader(nil, c, key, parent, BlueGreenCandidatePlan{}) },
		func() { NewBlueGreenCandidatePreparerWithReader(c, nil, key, parent, BlueGreenCandidatePlan{}) },
		func() { NewBlueGreenCandidatePreparerWithReader(c, c, key, nil, BlueGreenCandidatePlan{}) },
		func() {
			NewBlueGreenCandidatePreparerWithReader(c, c, client.ObjectKey{}, parent, BlueGreenCandidatePlan{})
		},
		func() {
			NewBlueGreenCandidatePreparerWithReader(c, c, client.ObjectKey{Name: "foreign", Namespace: parent.Namespace}, parent, BlueGreenCandidatePlan{})
		},
	} {
		assert.Panics(t, construct)
	}
	assert.NotPanics(t, func() { NewBlueGreenCandidatePreparer(c, key, parent, BlueGreenCandidatePlan{}) })
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

func TestCandidatePoolerReadyRequiresAvailableDeployment(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, appsv1.AddToScheme(scheme))
	pooler := &cnpgv1.Pooler{ObjectMeta: metav1.ObjectMeta{Name: "green-pooler-rw", Namespace: "default", UID: "pooler-uid"},
		Spec: cnpgv1.PoolerSpec{Instances: new(int32(2))}, Status: cnpgv1.PoolerStatus{Phase: cnpgv1.PoolerPhaseActive, Instances: 2}}
	selector := map[string]string{cnpgutils.PgbouncerNameLabel: pooler.Name}
	owner := []metav1.OwnerReference{*metav1.NewControllerRef(pooler, cnpgv1.SchemeGroupVersion.WithKind("Pooler"))}
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: pooler.Name, Namespace: pooler.Namespace, OwnerReferences: owner}, Spec: corev1.ServiceSpec{Selector: selector}}
	for _, tt := range []struct {
		name      string
		phase     cnpgv1.PoolerPhase
		available int32
		want      bool
	}{
		{name: "all available", phase: cnpgv1.PoolerPhaseActive, available: 2, want: true},
		{name: "paused", phase: cnpgv1.PoolerPhasePaused, available: 2},
		{name: "one available", phase: cnpgv1.PoolerPhaseActive, available: 1},
		{name: "none available", phase: cnpgv1.PoolerPhaseActive},
	} {
		t.Run(tt.name, func(t *testing.T) {
			current := pooler.DeepCopy()
			current.Status.Phase = tt.phase
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: pooler.Name, Namespace: pooler.Namespace, UID: "deployment-uid", Generation: 1, OwnerReferences: owner},
				Spec: appsv1.DeploymentSpec{Replicas: new(int32(2)), Selector: &metav1.LabelSelector{MatchLabels: selector},
					Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: selector}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "pgbouncer", Ports: []corev1.ContainerPort{{Name: "pgbouncer", ContainerPort: 5432}}}}}}},
				Status: appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: tt.available, AvailableReplicas: tt.available},
			}
			preparer := &BlueGreenCandidatePreparer{client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(deployment).Build()}
			ready, err := preparer.candidatePoolerReady(t.Context(), current, service)
			require.NoError(t, err)
			assert.Equal(t, tt.want, ready)
		})
	}
}

func TestCandidateClusterNameLeavesRoomForPoolerResources(t *testing.T) {
	name := candidateClusterName(strings.Repeat("a", postgresIdentifierMaxLength), "attempt-1")
	assert.LessOrEqual(t, len(name), candidateClusterNameMaxLength)
	assert.LessOrEqual(t, len(candidatePoolerName(name, "rw")), postgresIdentifierMaxLength)
}
