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

package endpointpublication

import (
	"context"
	"crypto/rand"
	"fmt"
	"maps"
	"strconv"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	pgendpointprotocol "github.com/splunk/splunk-operator/pkg/postgresql/shared/endpointprotocol"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ObserveCommit reads the external commit record before any rollback decision.
// A receipt proves an earlier commit even if that record has since disappeared.
// Read errors and ambiguous records are deliberately not treated as blue.
func (c *Coordinator) ObserveCommit(ctx context.Context, key types.NamespacedName, attemptID string) (bool, error) {
	parent, err := c.store.ReadParent(ctx, key)
	if err != nil {
		return false, err
	}
	entry, err := activeAttempt(parent, attemptID)
	if err != nil {
		return false, err
	}
	bg := entry.BlueGreen
	if bg.CommitReceipt != nil {
		if !pgendpointprotocol.CommitReceiptIdentityMatches(parent, bg, pgendpointprotocol.AllowProviderRepair) {
			return false, ErrCanonicalMismatch
		}
		return true, nil
	}
	if bg.Green == nil || bg.Blue == nil || parent.Status.Resources == nil || parent.Status.Resources.ConfigMapRef == nil {
		return false, ErrCanonicalMismatch
	}
	cm, err := c.store.ReadConfigMap(ctx, types.NamespacedName{Namespace: parent.Namespace, Name: parent.Status.Resources.ConfigMapRef.Name})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !pgendpointprotocol.CanonicalOwnedByParent(parent, cm) {
		return false, ErrCanonicalMismatch
	}
	if cm.Annotations[pgconninfo.AnnotationActiveEnvironment] == bg.Green.Ref.Name {
		if !canonicalStampedFor(bg, cm) {
			return false, ErrCanonicalMismatch
		}
		return true, nil
	}
	blue, err := c.proof.ResolveBlue(ctx, parent, bg)
	if err != nil {
		return false, err
	}
	if !endpointDataMatches(cm.Data, blue) {
		return false, ErrCanonicalMismatch
	}
	return false, nil
}

// RecoverCanonical repairs projections of an existing commit. Only the receipt
// authorizes recreation; preparation alone can never turn a missing record green.
func (c *Coordinator) RecoverCanonical(ctx context.Context, key types.NamespacedName, attemptID string) error {
	return c.retryStatus(ctx, key, attemptID, func(parent *platformv1alpha1.PostgresCluster, entry *platformv1alpha1.PostgresMajorUpgradeStatus) (bool, error) {
		bg := entry.BlueGreen
		if !pgendpointprotocol.CommitReceiptIdentityMatches(parent, bg, pgendpointprotocol.AllowProviderRepair) || !preparedRoutingValid(bg) {
			return false, ErrCanonicalMismatch
		}
		prep := bg.EndpointPreparation
		cmKey := types.NamespacedName{Namespace: parent.Namespace, Name: bg.CommitReceipt.CanonicalConfigMap.Name}
		cm, err := c.store.ReadConfigMap(ctx, cmKey)
		if apierrors.IsNotFound(err) {
			cm = pgconninfo.NewClusterConfigMap(parent, cmKey.Name, prep.AccessMetadata)
			applyPreparedRouting(cm, bg)
			err = c.store.CreateConfigMap(ctx, cm)
			if err != nil && !apierrors.IsAlreadyExists(err) {
				return false, err
			}
		} else if err != nil {
			return false, err
		} else {
			if !pgendpointprotocol.CanonicalOwnedByParent(parent, cm) {
				return false, ErrCanonicalMismatch
			}
			if !canonicalCommittedFor(parent, bg, cm) {
				applyPreparedRouting(cm, bg)
				if err := c.store.UpdateConfigMap(ctx, cm); err != nil {
					return false, err
				}
			}
		}
		cm, err = c.store.ReadConfigMap(ctx, cmKey)
		if err != nil {
			return false, err
		}
		if !canonicalCommittedFor(parent, bg, cm) {
			return false, ErrCanonicalMismatch
		}
		changed := false
		if parent.Status.BlueGreenCommitSequence < bg.CommitReceipt.Sequence {
			parent.Status.BlueGreenCommitSequence = bg.CommitReceipt.Sequence
			changed = true
		}
		if cm.UID != bg.CommitReceipt.CanonicalConfigMap.UID && (bg.CanonicalRecovery == nil || bg.CanonicalRecovery.ConfigMap.UID != cm.UID) {
			now := metav1.NewTime(c.now())
			bg.CanonicalRecovery = &platformv1alpha1.BlueGreenCanonicalRecoveryStatus{
				CommitSequence: bg.CommitReceipt.Sequence, EndpointFingerprint: *prep.EndpointFingerprint, RecoveredAt: &now,
				ConfigMap: corev1.ObjectReference{APIVersion: "v1", Kind: "ConfigMap", Namespace: parent.Namespace, Name: cm.Name, UID: cm.UID},
			}
			changed = true
		}
		if parent.Status.ProvisionerRef == nil || parent.Status.ProvisionerRef.Name != bg.Green.Ref.Name || parent.Status.ProvisionerRef.UID != bg.Green.Ref.UID {
			parent.Status.ProvisionerRef = bg.Green.Ref.DeepCopy()
			changed = true
		}
		return changed, nil
	})
}

// canonicalStampedFor proves the commit write even if access selectors later
// drift. Callers check ownership separately. Recovery repairs those selectors;
// their drift must never authorize a rollback to blue.
func canonicalStampedFor(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, cm *corev1.ConfigMap) bool {
	if bg == nil || bg.EndpointPreparation == nil || bg.EndpointPreparation.EndpointFingerprint == nil || cm == nil {
		return false
	}
	prep := bg.EndpointPreparation
	return pgendpointprotocol.PublicationStampMatches(cm, bg, prep.Generation, *prep.EndpointFingerprint) &&
		pgendpointprotocol.CompleteRouting(cm.Data) && pgconninfo.FingerprintEndpointData(cm.Data) == *prep.EndpointFingerprint
}

// RestoreUncommittedCanonical recreates a missing blue projection and fences
// delayed canonical writes with a verified CAS before source access can reopen.
func (c *Coordinator) RestoreUncommittedCanonical(ctx context.Context, key types.NamespacedName, attemptID string) error {
	committed, err := c.ObserveCommit(ctx, key, attemptID)
	if err != nil {
		return err
	}
	if committed {
		return fmt.Errorf("cannot restore blue after endpoint commit")
	}
	return c.retryStatus(ctx, key, attemptID, func(parent *platformv1alpha1.PostgresCluster, entry *platformv1alpha1.PostgresMajorUpgradeStatus) (bool, error) {
		bg := entry.BlueGreen
		if bg.CommitReceipt != nil || bg.Blue == nil || (bg.Switchover != nil && bg.Switchover.TargetAvailableAt != nil) {
			return false, ErrCanonicalMismatch
		}
		blue, err := c.proof.ResolveBlue(ctx, parent, bg)
		if err != nil {
			return false, err
		}
		cmKey := types.NamespacedName{Namespace: parent.Namespace, Name: parent.Status.Resources.ConfigMapRef.Name}
		cm, err := c.store.ReadConfigMap(ctx, cmKey)
		if apierrors.IsNotFound(err) {
			metadata, err := c.proof.ResolveBlueAccess(ctx, parent, bg)
			if err != nil {
				return false, err
			}
			cm = pgconninfo.NewClusterConfigMap(parent, cmKey.Name, metadata)
			cm.Annotations = map[string]string{pgconninfo.AnnotationActiveEnvironment: bg.Blue.Ref.Name}
			if cm.Data == nil {
				cm.Data = map[string]string{}
			}
			maps.Copy(cm.Data, blue)
			if err := c.store.CreateConfigMap(ctx, cm); err != nil && !apierrors.IsAlreadyExists(err) {
				return false, err
			}
		} else if err != nil {
			return false, err
		}
		cm, err = c.store.ReadConfigMap(ctx, cmKey)
		if err != nil {
			return false, err
		}
		if !pgendpointprotocol.CanonicalOwnedByParent(parent, cm) || cm.Annotations[pgconninfo.AnnotationActiveEnvironment] == bg.Green.Ref.Name || !endpointDataMatches(cm.Data, blue) {
			return false, ErrCanonicalMismatch
		}
		// A read that still sees blue does not settle an earlier Update whose
		// response was lost: the API server may not have applied it yet. Force
		// an actual resourceVersion advance before reopening the source. A
		// unique annotation prevents API storage from eliding a no-op update.
		previousVersion := cm.ResourceVersion
		fence := bg.AttemptID + "/" + rand.Text()
		if cm.Annotations == nil {
			cm.Annotations = map[string]string{}
		}
		cm.Annotations[pgconninfo.AnnotationSourceRestoreFence] = fence
		if err := c.store.UpdateConfigMap(ctx, cm); err != nil {
			return false, fmt.Errorf("fencing unresolved canonical writes before source restoration: %w", err)
		}
		latest, err := c.store.ReadConfigMap(ctx, cmKey)
		if err != nil {
			return false, err
		}
		if !pgendpointprotocol.CanonicalOwnedByParent(parent, latest) || latest.UID != cm.UID || latest.ResourceVersion == previousVersion ||
			latest.Annotations[pgconninfo.AnnotationSourceRestoreFence] != fence || latest.Annotations[pgconninfo.AnnotationActiveEnvironment] == bg.Green.Ref.Name || !endpointDataMatches(latest.Data, blue) {
			return false, ErrCanonicalMismatch
		}
		return false, nil
	})
}

func preparedRoutingValid(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
	p := bg.EndpointPreparation
	return p != nil && p.Generation > 0 && p.Generation == bg.EndpointIntentRevision && p.EndpointFingerprint != nil &&
		pgendpointprotocol.CompleteRouting(p.Routing) && pgconninfo.FingerprintEndpointData(p.Routing) == *p.EndpointFingerprint
}

func applyPreparedRouting(cm *corev1.ConfigMap, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	for _, key := range pgconninfo.EndpointKeys() {
		delete(cm.Data, key)
	}
	maps.Copy(cm.Data, bg.EndpointPreparation.Routing)
	for _, key := range pgconninfo.ClusterAccessKeys() {
		delete(cm.Data, key)
	}
	maps.Copy(cm.Data, bg.EndpointPreparation.AccessMetadata)
	if cm.Annotations == nil {
		cm.Annotations = map[string]string{}
	}
	cm.Annotations[pgconninfo.AnnotationActiveEnvironment] = bg.Green.Ref.Name
	cm.Annotations[pgconninfo.AnnotationEndpointAttemptID] = bg.AttemptID
	cm.Annotations[pgconninfo.AnnotationEndpointIntentGeneration] = strconv.FormatInt(bg.EndpointPreparation.Generation, 10)
	cm.Annotations[pgconninfo.AnnotationEndpointFingerprint] = *bg.EndpointPreparation.EndpointFingerprint
}

func routingCopy(data map[string]string) map[string]string {
	result := make(map[string]string)
	for _, key := range pgconninfo.EndpointKeys() {
		if value, ok := data[key]; ok {
			result[key] = value
		}
	}
	return result
}

func accessMetadataMatches(actual, expected map[string]string) bool {
	return maps.Equal(accessMetadataCopy(actual), accessMetadataCopy(expected))
}

func accessMetadataCopy(data map[string]string) map[string]string {
	result := make(map[string]string)
	for _, key := range pgconninfo.ClusterAccessKeys() {
		if value, ok := data[key]; ok {
			result[key] = value
		}
	}
	return result
}
