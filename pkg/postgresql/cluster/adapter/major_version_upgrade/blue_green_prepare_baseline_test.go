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
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	identityadapter "github.com/splunk/splunk-operator/pkg/postgresql/shared/adapter/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

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
