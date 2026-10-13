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
	entry, err := bluegreen.New(mvutypes.PreUpgradeBackup, preparer).Advance(t.Context(), intent)
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
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowPending, "a class-only edit must not invalidate the live blue baseline")
	require.NoError(t, client.Get(t.Context(), publicationKey, &cnpgv1.Publication{}))
	*preparer.plan.TargetConfiguration.Storage = resource.MustParse("10Gi")
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
	source := &cnpgv1.Cluster{Spec: cnpgv1.ClusterSpec{SuperuserSecret: &cnpgv1.LocalObjectReference{Name: "superuser"}}}
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
	assert.Equal(t, []string{"admin", "observed-rw", "replication", "rw", "superuser"}, credentialSecretNames(source, []declaredDatabase{database}, "replication"))
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
	preparer := &BlueGreenPreparer{cluster: raw, plan: BlueGreenPreflightPlan{TargetConfiguration: effective}}
	source := &cnpgv1.Cluster{Spec: cnpgv1.ClusterSpec{ImageName: "postgres:17"}}
	databases := []declaredDatabase{{parent: platformv1alpha1.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "orders-db", UID: types.UID("orders-uid")}}, definition: platformv1alpha1.DatabaseDefinition{Name: "orders"}}}

	fingerprint, err := preparer.configurationFingerprint(source, databases)
	require.NoError(t, err)
	effective.PostgreSQLConfig["shared_buffers"] = "256MB"
	changedEffective, err := preparer.configurationFingerprint(source, databases)
	require.NoError(t, err)
	assert.Equal(t, fingerprint, changedEffective, "a class-only edit does not change actual blue")
	effective.PostgreSQLConfig["shared_buffers"] = "128MB"
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
