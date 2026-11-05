package endpointprotocol

import (
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func canonicalFixture() (*platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus, *corev1.ConfigMap, string) {
	data := map[string]string{
		pgconninfo.KeyClusterRWEndpoint:  "green-rw",
		pgconninfo.KeyClusterROEndpoint:  "green-ro",
		pgconninfo.KeyClusterREndpoint:   "green-r",
		pgconninfo.KeyDefaultClusterPort: pgconninfo.DefaultPort,
	}
	fingerprint := pgconninfo.FingerprintEndpointData(data)
	bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		AttemptID: "attempt",
		Green:     &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}},
		CommitReceipt: &platformv1alpha1.BlueGreenCommitReceiptStatus{
			Sequence:           1,
			CanonicalConfigMap: &corev1.ObjectReference{Name: "canonical", UID: "canonical-uid"},
			Target:             &corev1.ObjectReference{Name: "green", UID: "green-uid"},
		},
	}
	parent := &platformv1alpha1.PostgresCluster{Status: platformv1alpha1.PostgresClusterStatus{
		ProvisionerRef: &corev1.ObjectReference{Name: "green", UID: "green-uid"},
		Resources:      &platformv1alpha1.PostgresClusterResources{ConfigMapRef: &corev1.LocalObjectReference{Name: "canonical"}},
	}}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "canonical", UID: "canonical-uid", Annotations: map[string]string{
			pgconninfo.AnnotationActiveEnvironment:        "green",
			pgconninfo.AnnotationEndpointAttemptID:        "attempt",
			pgconninfo.AnnotationEndpointIntentGeneration: "3",
			pgconninfo.AnnotationEndpointFingerprint:      fingerprint,
		}},
		Data: data,
	}
	return parent, bg, cm, fingerprint
}

func TestDurableCommitStatusMatches(t *testing.T) {
	parent, bg, _, _ := canonicalFixture()
	if !DurableCommitStatusMatches(parent, bg) {
		t.Fatal("valid durable receipt rejected")
	}
	for _, test := range []struct {
		name   string
		mutate func(*platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus)
	}{
		{"missing receipt", func(_ *platformv1alpha1.PostgresCluster, b *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			b.CommitReceipt = nil
		}},
		{"zero sequence", func(_ *platformv1alpha1.PostgresCluster, b *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			b.CommitReceipt.Sequence = 0
		}},
		{"missing historical UID", func(_ *platformv1alpha1.PostgresCluster, b *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			b.CommitReceipt.CanonicalConfigMap.UID = ""
		}},
		{"wrong target", func(_ *platformv1alpha1.PostgresCluster, b *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			b.CommitReceipt.Target.UID = "other"
		}},
		{"wrong provisioner", func(p *platformv1alpha1.PostgresCluster, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			p.Status.ProvisionerRef.Name = "blue"
		}},
		{"wrong canonical name", func(p *platformv1alpha1.PostgresCluster, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			p.Status.Resources.ConfigMapRef.Name = "other"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			p, b, _, _ := canonicalFixture()
			test.mutate(p, b)
			if DurableCommitStatusMatches(p, b) {
				t.Fatal("invalid durable receipt accepted")
			}
		})
	}
}

func TestCompleteRouting(t *testing.T) {
	_, _, cm, _ := canonicalFixture()
	if !CompleteRouting(cm.Data) {
		t.Fatal("valid routing rejected")
	}
	cm.Data[pgconninfo.KeyClusterROEndpoint] = ""
	if !CompleteRouting(cm.Data) {
		t.Fatal("empty RO endpoint should be permitted during scale-out")
	}
	delete(cm.Data, pgconninfo.KeyClusterROEndpoint)
	if CompleteRouting(cm.Data) {
		t.Fatal("missing RO key accepted")
	}
	cm.Data[pgconninfo.KeyClusterROEndpoint] = "green-ro"
	cm.Data[pgconninfo.KeyDefaultClusterPort] = "5433"
	if CompleteRouting(cm.Data) {
		t.Fatal("wrong default port accepted")
	}
	cm.Data[pgconninfo.KeyDefaultClusterPort] = pgconninfo.DefaultPort
	cm.Data[pgconninfo.KeyPoolerRWEndpoint] = "pooler-rw"
	if CompleteRouting(cm.Data) {
		t.Fatal("unpaired pooler endpoint accepted")
	}
}

func TestCommittedCanonicalRoutingRequiresLiveIdentityAndPublicationStamp(t *testing.T) {
	parent, bg, cm, fingerprint := canonicalFixture()
	cm.Data["DATABASE_NAME"] = "orders"
	routing, valid := CommittedCanonicalRouting(parent, bg, cm, 3, fingerprint)
	if !valid || routing[pgconninfo.KeyClusterRWEndpoint] != "green-rw" {
		t.Fatal("valid committed routing rejected")
	}
	if _, included := routing["DATABASE_NAME"]; included {
		t.Fatal("database identity leaked into routing projection")
	}
	for _, test := range []struct {
		name   string
		mutate func(*corev1.ConfigMap)
	}{
		{"recreated canonical UID", func(cm *corev1.ConfigMap) { cm.UID = "new-uid" }},
		{"wrong canonical name", func(cm *corev1.ConfigMap) { cm.Name = "other" }},
		{"changed routing", func(cm *corev1.ConfigMap) { cm.Data[pgconninfo.KeyClusterRWEndpoint] = "other-rw" }},
		{"missing routing", func(cm *corev1.ConfigMap) { delete(cm.Data, pgconninfo.KeyClusterRWEndpoint) }},
		{"wrong attempt", func(cm *corev1.ConfigMap) { cm.Annotations[pgconninfo.AnnotationEndpointAttemptID] = "other" }},
		{"wrong revision", func(cm *corev1.ConfigMap) { cm.Annotations[pgconninfo.AnnotationEndpointIntentGeneration] = "4" }},
		{"wrong environment", func(cm *corev1.ConfigMap) { cm.Annotations[pgconninfo.AnnotationActiveEnvironment] = "blue" }},
		{"wrong fingerprint annotation", func(cm *corev1.ConfigMap) { cm.Annotations[pgconninfo.AnnotationEndpointFingerprint] = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			copy := cm.DeepCopy()
			test.mutate(copy)
			if _, valid := CommittedCanonicalRouting(parent, bg, copy, 3, fingerprint); valid {
				t.Fatal("invalid canonical ConfigMap accepted")
			}
		})
	}
	if PublicationStampMatches(cm, bg, 0, fingerprint) || PublicationStampMatches(cm, bg, 3, "") {
		t.Fatal("incomplete publication stamp accepted")
	}
}
