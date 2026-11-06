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

package endpointprotocol

import (
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func recoveredCanonicalFixture() (*platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus, *corev1.ConfigMap, string) {
	parent, bg, cm, fingerprint := canonicalFixture()
	cm.UID = "recreated-canonical-uid"
	bg.CanonicalRecovery = &platformv1alpha1.BlueGreenCanonicalRecoveryStatus{
		CommitSequence: bg.CommitReceipt.Sequence, EndpointFingerprint: fingerprint, RecoveredAt: new(metav1.Now()),
		ConfigMap: corev1.ObjectReference{APIVersion: "v1", Kind: "ConfigMap", Namespace: parent.Namespace, Name: cm.Name, UID: cm.UID},
	}
	return parent, bg, cm, fingerprint
}

func TestRecreatedCanonicalRequiresExplicitRecoveryForExistingCommit(t *testing.T) {
	parent, bg, cm, fingerprint := canonicalFixture()
	cm.UID = "recreated-canonical-uid"
	_, valid := CommittedCanonicalRouting(parent, bg, cm, 3, fingerprint)
	assert.False(t, valid, "a matching owner and endpoint payload do not authorize a new UID")
	parent, bg, cm, fingerprint = recoveredCanonicalFixture()
	cm.Data["SUPER_USER_SECRET_REF"] = "application-secret-selector"
	routing, valid := CommittedCanonicalRouting(parent, bg, cm, 3, fingerprint)
	assert.True(t, valid)
	assert.Equal(t, "green-rw", routing[pgconninfo.KeyClusterRWEndpoint])
	assert.NotContains(t, routing, "SUPER_USER_SECRET_REF")
	assert.Equal(t, "canonical-uid", string(bg.CommitReceipt.CanonicalConfigMap.UID), "a projection cannot replace historical commit identity")
	bg.CommitReceipt = nil
	_, valid = CommittedCanonicalRouting(parent, bg, cm, 3, fingerprint)
	assert.False(t, valid, "a recovery projection is never authority to initiate a commit")
}

func TestRecoveredCanonicalRejectsEveryMismatchedRecoveryBinding(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus, *corev1.ConfigMap)
	}{
		{"missing recovery", func(_ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, _ *corev1.ConfigMap) {
			bg.CanonicalRecovery = nil
		}},
		{"wrong commit sequence", func(_ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, _ *corev1.ConfigMap) {
			bg.CanonicalRecovery.CommitSequence++
		}},
		{"wrong fingerprint", func(_ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, _ *corev1.ConfigMap) {
			bg.CanonicalRecovery.EndpointFingerprint = "different"
		}},
		{"wrong UID", func(_ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, _ *corev1.ConfigMap) {
			bg.CanonicalRecovery.ConfigMap.UID = "other-uid"
		}},
		{"missing UID", func(_ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, _ *corev1.ConfigMap) {
			bg.CanonicalRecovery.ConfigMap.UID = ""
		}},
		{"wrong ConfigMap name", func(_ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, _ *corev1.ConfigMap) {
			bg.CanonicalRecovery.ConfigMap.Name = "other"
		}},
		{"wrong namespace", func(_ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, _ *corev1.ConfigMap) {
			bg.CanonicalRecovery.ConfigMap.Namespace = "other"
		}},
		{"wrong kind", func(_ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, _ *corev1.ConfigMap) {
			bg.CanonicalRecovery.ConfigMap.Kind = "Secret"
		}},
		{"wrong API version", func(_ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, _ *corev1.ConfigMap) {
			bg.CanonicalRecovery.ConfigMap.APIVersion = "apps/v1"
		}},
		{"missing recovery time", func(_ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, _ *corev1.ConfigMap) {
			bg.CanonicalRecovery.RecoveredAt = nil
		}},
		{"replacement payload changed", func(_ *platformv1alpha1.PostgresCluster, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus, cm *corev1.ConfigMap) {
			cm.Data[pgconninfo.KeyClusterRWEndpoint] = "different"
		}},
		{"wrong attempt stamp", func(_ *platformv1alpha1.PostgresCluster, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus, cm *corev1.ConfigMap) {
			cm.Annotations[pgconninfo.AnnotationEndpointAttemptID] = "other"
		}},
		{"wrong active provider", func(parent *platformv1alpha1.PostgresCluster, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus, _ *corev1.ConfigMap) {
			parent.Status.ProvisionerRef.UID = "blue-uid"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent, bg, cm, fingerprint := recoveredCanonicalFixture()
			test.mutate(parent, bg, cm)
			_, valid := CommittedCanonicalRouting(parent, bg, cm, 3, fingerprint)
			assert.False(t, valid)
		})
	}
}

func TestOriginalAndRecoveredCanonicalRequireLiveOwnerAndNamespace(t *testing.T) {
	for _, recovered := range []bool{false, true} {
		for _, test := range []struct {
			name   string
			mutate func(*corev1.ConfigMap)
		}{
			{"owner removed", func(cm *corev1.ConfigMap) { cm.OwnerReferences = nil }},
			{"different owner UID", func(cm *corev1.ConfigMap) { cm.OwnerReferences[0].UID = "other-parent" }},
			{"different owner name", func(cm *corev1.ConfigMap) { cm.OwnerReferences[0].Name = "other-parent" }},
			{"different owner kind", func(cm *corev1.ConfigMap) { cm.OwnerReferences[0].Kind = "PostgresDatabase" }},
			{"different owner API", func(cm *corev1.ConfigMap) { cm.OwnerReferences[0].APIVersion = "apps/v1" }},
			{"non-controller owner", func(cm *corev1.ConfigMap) { cm.OwnerReferences[0].Controller = new(false) }},
			{"different namespace", func(cm *corev1.ConfigMap) { cm.Namespace = "other" }},
		} {
			t.Run(map[bool]string{false: "original/", true: "recovered/"}[recovered]+test.name, func(t *testing.T) {
				parent, bg, cm, fingerprint := canonicalFixture()
				if recovered {
					parent, bg, cm, fingerprint = recoveredCanonicalFixture()
				}
				test.mutate(cm)
				_, valid := CommittedCanonicalRouting(parent, bg, cm, 3, fingerprint)
				assert.False(t, valid)
			})
		}
	}
}
