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

// Package endpointprotocol holds the pure proof rules shared by the parent
// cluster and child database endpoint controllers. Each controller reads its
// own current Kubernetes objects before applying these rules.
package endpointprotocol

import (
	"strconv"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CommitReceiptValidation names the one permitted recovery relaxation: the
// authoritative provider reference may still need repair from the receipt.
type CommitReceiptValidation uint8

const (
	RequireAuthoritativeProvider CommitReceiptValidation = iota
	AllowProviderRepair
)

// DurableCommitStatusMatches validates the persisted identity of the green
// commit. It does not inspect the live canonical ConfigMap, whose UID can
// change after the production cutover has completed.
func DurableCommitStatusMatches(parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
	return CommitReceiptIdentityMatches(parent, bg, RequireAuthoritativeProvider)
}

// CommitReceiptTargetsGreen checks the immutable receipt payload independently
// of repairable parent projections. Callers without a parent still use this
// same identity check, then verify their own additional live evidence.
func CommitReceiptTargetsGreen(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
	if bg == nil || bg.Green == nil || bg.Green.Ref.Name == "" || bg.Green.Ref.UID == "" || bg.CommitReceipt == nil {
		return false
	}
	r := bg.CommitReceipt
	return r.Sequence > 0 && r.CanonicalConfigMap != nil && r.CanonicalConfigMap.Name != "" && r.CanonicalConfigMap.UID != "" &&
		r.Target != nil && r.Target.Name == bg.Green.Ref.Name && r.Target.UID == bg.Green.Ref.UID
}

// CommitReceiptIdentityMatches always binds the canonical name and green UID.
// Only recovery may omit the provider projection it is about to repair.
func CommitReceiptIdentityMatches(parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, validation CommitReceiptValidation) bool {
	if parent == nil || !CommitReceiptTargetsGreen(bg) || parent.Status.Resources == nil || parent.Status.Resources.ConfigMapRef == nil ||
		parent.Status.Resources.ConfigMapRef.Name != bg.CommitReceipt.CanonicalConfigMap.Name {
		return false
	}
	switch validation {
	case AllowProviderRepair:
		return true
	case RequireAuthoritativeProvider:
		return parent.Status.ProvisionerRef != nil && parent.Status.ProvisionerRef.Name == bg.Green.Ref.Name && parent.Status.ProvisionerRef.UID == bg.Green.Ref.UID
	default:
		return false
	}
}

// CompleteRouting requires the same endpoint shape in both controllers.
// The RO endpoint may be empty during scale-out, but its key must be present.
func CompleteRouting(data map[string]string) bool {
	for _, key := range pgconninfo.RequiredKeys() {
		value, present := data[key]
		if !present || (key != pgconninfo.KeyClusterROEndpoint && value == "") {
			return false
		}
	}
	if data[pgconninfo.KeyDefaultClusterPort] != pgconninfo.DefaultPort {
		return false
	}
	_, rwPooler := data[pgconninfo.KeyPoolerRWEndpoint]
	_, roPooler := data[pgconninfo.KeyPoolerROEndpoint]
	return rwPooler == roPooler
}

// PublicationStampMatches binds a ConfigMap to the green attempt, intent
// generation, and endpoint fingerprint that passed preparation.
func PublicationStampMatches(cm *corev1.ConfigMap, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, generation int64, fingerprint string) bool {
	if cm == nil || bg == nil || bg.Green == nil || bg.Green.Ref.Name == "" || bg.AttemptID == "" || generation <= 0 || fingerprint == "" {
		return false
	}
	return PublicationStampMatchesValues(cm.Annotations, bg.Green.Ref.Name, bg.AttemptID, generation, fingerprint)
}

// PublicationStampMatchesValues also checks child-owned ConfigMaps, where the
// writer has the already validated intent values instead of the parent status.
func PublicationStampMatchesValues(annotations map[string]string, environment, attempt string, generation int64, fingerprint string) bool {
	return environment != "" && attempt != "" && generation > 0 && fingerprint != "" &&
		annotations[pgconninfo.AnnotationActiveEnvironment] == environment &&
		annotations[pgconninfo.AnnotationEndpointAttemptID] == attempt &&
		annotations[pgconninfo.AnnotationEndpointIntentGeneration] == strconv.FormatInt(generation, 10) &&
		annotations[pgconninfo.AnnotationEndpointFingerprint] == fingerprint
}

// CommittedCanonicalRouting validates a fresh canonical ConfigMap against the
// durable receipt and returns only shared endpoint keys. Active publication
// accepts the original ConfigMap UID or an explicit controller-owned recovery
// projection bound to the same pre-existing commit and payload.
func CommittedCanonicalRouting(parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, cm *corev1.ConfigMap, generation int64, fingerprint string) (map[string]string, bool) {
	if !DurableCommitStatusMatches(parent, bg) || cm == nil ||
		!CanonicalOwnedByParent(parent, cm) ||
		cm.Name != bg.CommitReceipt.CanonicalConfigMap.Name ||
		!canonicalUIDMatches(parent, bg, cm, fingerprint) ||
		!CompleteRouting(cm.Data) ||
		pgconninfo.FingerprintEndpointData(cm.Data) != fingerprint ||
		!PublicationStampMatches(cm, bg, generation, fingerprint) {
		return nil, false
	}
	routing := make(map[string]string, len(pgconninfo.EndpointKeys()))
	for _, key := range pgconninfo.EndpointKeys() {
		if value, present := cm.Data[key]; present {
			routing[key] = value
		}
	}
	return routing, true
}

func canonicalUIDMatches(parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, cm *corev1.ConfigMap, fingerprint string) bool {
	if cm.UID == bg.CommitReceipt.CanonicalConfigMap.UID {
		return true
	}
	r := bg.CanonicalRecovery
	return r != nil && r.CommitSequence == bg.CommitReceipt.Sequence && r.EndpointFingerprint == fingerprint &&
		r.ConfigMap.APIVersion == "v1" && r.ConfigMap.Kind == "ConfigMap" &&
		r.ConfigMap.Name == cm.Name && r.ConfigMap.Namespace == parent.Namespace && r.ConfigMap.UID == cm.UID &&
		r.ConfigMap.UID != "" && r.RecoveredAt != nil && metav1.IsControlledBy(cm, parent)
}

// CanonicalOwnedByParent binds a canonical projection to the exact controller
// identity and namespace shared by the parent and child endpoint controllers.
func CanonicalOwnedByParent(parent *platformv1alpha1.PostgresCluster, cm *corev1.ConfigMap) bool {
	if parent == nil || cm == nil || parent.DeletionTimestamp != nil || cm.DeletionTimestamp != nil || cm.Namespace != parent.Namespace {
		return false
	}
	controller := metav1.GetControllerOf(cm)
	return controller != nil && parent.UID != "" && controller.UID == parent.UID && controller.Name == parent.Name &&
		controller.Kind == "PostgresCluster" && controller.APIVersion == platformv1alpha1.GroupVersion.String()
}
