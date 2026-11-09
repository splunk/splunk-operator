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

// Package endpointpublication coordinates the parent side of the two-phase
// blue/green endpoint protocol, including its canonical commit boundary and
// recovery proof.
package endpointpublication

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	pgendpointprotocol "github.com/splunk/splunk-operator/pkg/postgresql/shared/endpointprotocol"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Store reads safety evidence directly from the API server and writes the
// canonical endpoint record or PostgresCluster status with resourceVersion
// conflict detection.
type Store interface {
	ReadParent(context.Context, types.NamespacedName) (*platformv1alpha1.PostgresCluster, error)
	ListChildren(context.Context, string) ([]platformv1alpha1.PostgresDatabase, error)
	ReadConfigMap(context.Context, types.NamespacedName) (*corev1.ConfigMap, error)
	CreateConfigMap(context.Context, *corev1.ConfigMap) error
	UpdateConfigMap(context.Context, *corev1.ConfigMap) error
	WriteParentStatus(context.Context, *platformv1alpha1.PostgresCluster) error
}

// Proof is a fresh, provider-validated pair of complete routing payloads.
// The adapter must check green CNPG UID and Service identity and Pooler Deployment readiness.
type Proof struct {
	Blue     map[string]string
	Green    map[string]string
	GreenUID types.UID
	// GreenAccess contains only non-sensitive cluster credential/CA selectors.
	GreenAccess map[string]string
}

type ProofResolver interface {
	Resolve(context.Context, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (Proof, error)
	ResolveBlue(context.Context, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (map[string]string, error)
	ResolveBlueAccess(context.Context, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (map[string]string, error)
}

// CommitAuthorizer is supplied by the workflow that owns the interruption.
// It is invoked on fresh parent state immediately before a first commit.
// Already committed records are repaired without asking permission to commit again.
type CommitAuthorizer func(context.Context, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus, time.Time) error

type Coordinator struct {
	store     Store
	proof     ProofResolver
	authorize CommitAuthorizer
	now       func() time.Time
}

func NewCoordinator(store Store, proof ProofResolver, authorize CommitAuthorizer, now func() time.Time) (*Coordinator, error) {
	if store == nil || proof == nil || authorize == nil || now == nil {
		return nil, fmt.Errorf("endpoint coordinator requires a store, proof resolver, commit authorizer, and clock")
	}
	return &Coordinator{store: store, proof: proof, authorize: authorize, now: now}, nil
}

var (
	ErrStaleAttempt        = errors.New("endpoint publication attempt is stale")
	ErrIncompleteInventory = errors.New("endpoint publication inventory is incomplete")
	ErrCanonicalMismatch   = errors.New("canonical access ConfigMap does not prove endpoint commit")
)

// CommitCanonical publishes green in the canonical access ConfigMap, then
// records the provider switch and monotonic receipt together in parent status.
// The ConfigMap write is the commit point: a retry that observes its exact
// attempt stamp repairs status, while a receipt alone can never initiate it.
func (c *Coordinator) CommitCanonical(ctx context.Context, key types.NamespacedName, attemptID string) error {
	if c == nil || c.store == nil || attemptID == "" {
		return fmt.Errorf("endpoint coordinator, store, and attempt ID are required")
	}
	for range 5 {
		parent, bg, err := c.readCommitAttempt(ctx, key, attemptID)
		if err != nil {
			return err
		}
		if bg.CommitReceipt != nil {
			return c.RecoverCanonical(ctx, key, attemptID)
		}
		if _, err := nextCommitSequence(parent); err != nil {
			return err
		}
		canonicalKey, err := canonicalConfigMapKey(parent)
		if err != nil {
			return err
		}
		canonical, err := c.store.ReadConfigMap(ctx, canonicalKey)
		if err != nil {
			return err
		}
		if !pgendpointprotocol.CanonicalOwnedByParent(parent, canonical) {
			return ErrCanonicalMismatch
		}
		generation, fingerprint := bg.EndpointPreparation.Generation, *bg.EndpointPreparation.EndpointFingerprint
		if canonical.Annotations[pgconninfo.AnnotationActiveEnvironment] == bg.Green.Ref.Name {
			// Lost responses are repaired from the exact durable record, without
			// depending on provider health or a still-open interruption deadline.
			if !canonicalCommittedFor(parent, bg, canonical) {
				return ErrCanonicalMismatch
			}
		} else {
			canonical, err = c.commitPreparedCanonical(ctx, key, attemptID, parent, bg, canonicalKey)
			if apierrors.IsConflict(err) {
				continue
			}
			if err != nil {
				return err
			}
		}
		err = c.recordCommitReceipt(ctx, key, attemptID, canonical, generation, fingerprint)
		if apierrors.IsConflict(err) {
			continue
		}
		return err
	}
	return fmt.Errorf("canonical endpoint commit status conflict retry limit exceeded")
}

func (c *Coordinator) readCommitAttempt(ctx context.Context, key types.NamespacedName, attemptID string) (*platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	parent, err := c.store.ReadParent(ctx, key)
	if err != nil {
		return nil, nil, err
	}
	entry, err := activeAttempt(parent, attemptID)
	if err != nil {
		return nil, nil, err
	}
	if entry.BlueGreen.Green == nil || entry.BlueGreen.Green.Ref.Name == "" || entry.BlueGreen.Green.Ref.UID == "" || !preparedRoutingValid(entry.BlueGreen) {
		return nil, nil, fmt.Errorf("current complete endpoint preparation is required")
	}
	return parent, entry.BlueGreen, nil
}

func canonicalConfigMapKey(parent *platformv1alpha1.PostgresCluster) (types.NamespacedName, error) {
	if parent.Status.Resources == nil || parent.Status.Resources.ConfigMapRef == nil || parent.Status.Resources.ConfigMapRef.Name == "" {
		return types.NamespacedName{}, ErrCanonicalMismatch
	}
	return types.NamespacedName{Namespace: parent.Namespace, Name: parent.Status.Resources.ConfigMapRef.Name}, nil
}

// canonicalCommittedFor is the single acceptance predicate for a first commit,
// lost-response recovery, and the read immediately before receipt persistence.
func canonicalCommittedFor(parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, cm *corev1.ConfigMap) bool {
	if bg == nil || bg.Green == nil || !preparedRoutingValid(bg) || cm == nil || cm.UID == "" ||
		parent == nil || parent.Status.Resources == nil || parent.Status.Resources.ConfigMapRef == nil || cm.Name != parent.Status.Resources.ConfigMapRef.Name {
		return false
	}
	p := bg.EndpointPreparation
	return pgendpointprotocol.CanonicalOwnedByParent(parent, cm) &&
		pgendpointprotocol.PublicationStampMatches(cm, bg, p.Generation, *p.EndpointFingerprint) &&
		endpointDataMatches(cm.Data, p.Routing) && accessMetadataMatches(cm.Data, p.AccessMetadata)
}

// commitPreparedCanonical refreshes the live preparation barrier, then asks the
// caller's workflow policy for permission on the exact state used by the CAS.
func (c *Coordinator) commitPreparedCanonical(ctx context.Context, key types.NamespacedName, attemptID string, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, canonicalKey types.NamespacedName) (*corev1.ConfigMap, error) {
	generation, fingerprint := bg.EndpointPreparation.Generation, *bg.EndpointPreparation.EndpointFingerprint
	if err := c.authorize(ctx, parent, bg, c.now()); err != nil {
		return nil, err
	}
	complete, err := c.PreparationComplete(ctx, key, attemptID)
	if err != nil {
		return nil, err
	}
	if !complete {
		return nil, fmt.Errorf("endpoint preparation barrier is incomplete")
	}
	parent, bg, err = c.readCommitAttempt(ctx, key, attemptID)
	if err != nil {
		return nil, err
	}
	if bg.EndpointPreparation.Generation != generation || *bg.EndpointPreparation.EndpointFingerprint != fingerprint {
		return nil, ErrStaleAttempt
	}
	inventory, err := c.inventory(ctx, parent, bg)
	if err != nil {
		return nil, err
	}
	if !preparationAggregated(bg.EndpointPreparation.Acknowledgements, inventory, fingerprint) {
		return nil, fmt.Errorf("endpoint preparation barrier changed before canonical commit")
	}
	proof, liveFingerprint, err := c.resolve(ctx, parent, bg)
	if err != nil {
		return nil, err
	}
	if liveFingerprint != fingerprint {
		return nil, fmt.Errorf("green endpoint proof changed before canonical commit")
	}
	canonical, err := c.store.ReadConfigMap(ctx, canonicalKey)
	if err != nil {
		return nil, err
	}
	if err := c.requireCanonicalBlue(ctx, parent, bg, proof.Blue); err != nil {
		return nil, err
	}
	// Reauthorize the state re-read after PreparationComplete so its deadline gates the CAS.
	if err := c.authorize(ctx, parent, bg, c.now()); err != nil {
		return nil, err
	}
	return c.writeCanonicalCommit(ctx, parent, bg, canonical)
}

// writeCanonicalCommit contains the irreversible CAS and its lost-response read.
func (c *Coordinator) writeCanonicalCommit(ctx context.Context, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, canonical *corev1.ConfigMap) (*corev1.ConfigMap, error) {
	applyPreparedRouting(canonical, bg)
	key := types.NamespacedName{Namespace: canonical.Namespace, Name: canonical.Name}
	if err := c.store.UpdateConfigMap(ctx, canonical); err != nil {
		if apierrors.IsConflict(err) {
			return nil, err
		}
		latest, readErr := c.store.ReadConfigMap(ctx, key)
		if readErr != nil || latest.UID != canonical.UID || !canonicalCommittedFor(parent, bg, latest) {
			return nil, fmt.Errorf("updating canonical endpoint ConfigMap: %w", err)
		}
	}
	latest, err := c.store.ReadConfigMap(ctx, key)
	if err != nil {
		return nil, err
	}
	if latest.UID != canonical.UID || !canonicalCommittedFor(parent, bg, latest) {
		return nil, ErrCanonicalMismatch
	}
	return latest, nil
}

// recordCommitReceipt repairs status from the canonical record using the latest
// parent resourceVersion. A conflict returns to the outer recovery loop.
func (c *Coordinator) recordCommitReceipt(ctx context.Context, key types.NamespacedName, attemptID string, canonical *corev1.ConfigMap, generation int64, fingerprint string) error {
	parent, bg, err := c.readCommitAttempt(ctx, key, attemptID)
	if err != nil {
		return err
	}
	if bg.CommitReceipt != nil {
		_, err := c.committedRouting(ctx, parent, bg, generation, fingerprint)
		return err
	}
	if bg.EndpointPreparation.Generation != generation || *bg.EndpointPreparation.EndpointFingerprint != fingerprint {
		return ErrStaleAttempt
	}
	latest, err := c.store.ReadConfigMap(ctx, types.NamespacedName{Namespace: canonical.Namespace, Name: canonical.Name})
	if err != nil {
		return err
	}
	if latest.UID != canonical.UID || !canonicalCommittedFor(parent, bg, latest) {
		return ErrCanonicalMismatch
	}
	sequence, err := nextCommitSequence(parent)
	if err != nil {
		return err
	}
	now := metav1.NewTime(c.now())
	bg.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{
		Sequence: sequence, CommittedAt: now.DeepCopy(),
		CanonicalConfigMap: &corev1.ObjectReference{APIVersion: "v1", Kind: "ConfigMap", Namespace: parent.Namespace, Name: latest.Name, UID: latest.UID, ResourceVersion: latest.ResourceVersion},
		Target:             bg.Green.Ref.DeepCopy(),
	}
	parent.Status.ProvisionerRef = bg.Green.Ref.DeepCopy()
	parent.Status.BlueGreenCommitSequence = sequence
	return c.store.WriteParentStatus(ctx, parent)
}

func nextCommitSequence(parent *platformv1alpha1.PostgresCluster) (int64, error) {
	sequence := parent.Status.BlueGreenCommitSequence
	for _, entry := range parent.Status.PostgresMajorUpgradeStatus {
		if bg := entry.BlueGreen; bg != nil && bg.CommitReceipt != nil {
			sequence = max(sequence, bg.CommitReceipt.Sequence)
		}
	}
	// Guard the persisted int64 increment against wraparound before commit.
	if sequence == math.MaxInt64 {
		return 0, fmt.Errorf("endpoint commit sequence is exhausted")
	}
	return sequence + 1, nil
}

type inventoryKey struct {
	childName    string
	childUID     string
	databaseName string
}

type inventoryItem struct {
	key             inventoryKey
	childGeneration int64
}

func (c *Coordinator) resolve(ctx context.Context, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (Proof, string, error) {
	if c.proof == nil || bg.Green == nil || bg.Green.Ref.Name == "" || bg.Green.Ref.UID == "" {
		return Proof{}, "", fmt.Errorf("green endpoint proof resolver and candidate identity are required")
	}
	proof, err := c.proof.Resolve(ctx, parent, bg)
	if err != nil {
		return Proof{}, "", err
	}
	if proof.GreenUID != bg.Green.Ref.UID || !pgendpointprotocol.CompleteRouting(proof.Green) || !pgendpointprotocol.CompleteRouting(proof.Blue) {
		return Proof{}, "", fmt.Errorf("endpoint proof does not match green candidate identity")
	}
	return proof, pgconninfo.FingerprintEndpointData(proof.Green), nil
}

func activeAttempt(parent *platformv1alpha1.PostgresCluster, attemptID string) (*platformv1alpha1.PostgresMajorUpgradeStatus, error) {
	if parent == nil || parent.UID == "" || parent.DeletionTimestamp != nil || attemptID == "" {
		return nil, ErrStaleAttempt
	}
	entries := parent.Status.PostgresMajorUpgradeStatus
	if len(entries) == 0 {
		return nil, ErrStaleAttempt
	}
	entry := &entries[len(entries)-1]
	if entry.BlueGreen == nil || entry.BlueGreen.AttemptID != attemptID {
		return nil, ErrStaleAttempt
	}
	return entry, nil
}

func (c *Coordinator) retryStatus(ctx context.Context, key types.NamespacedName, attemptID string, fn func(*platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresMajorUpgradeStatus) (bool, error)) error {
	if c == nil || c.store == nil || attemptID == "" {
		return fmt.Errorf("endpoint coordinator, store, and attempt ID are required")
	}
	for range 5 {
		parent, err := c.store.ReadParent(ctx, key)
		if err != nil {
			return err
		}
		entry, err := activeAttempt(parent, attemptID)
		if err != nil {
			return err
		}
		changed, err := fn(parent, entry)
		if err != nil || !changed {
			return err
		}
		if err := c.store.WriteParentStatus(ctx, parent); err != nil {
			if apierrors.IsConflict(err) {
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("endpoint publication status conflict retry limit exceeded")
}

func (c *Coordinator) children(ctx context.Context, parent *platformv1alpha1.PostgresCluster) (map[string]platformv1alpha1.PostgresDatabase, error) {
	list, err := c.store.ListChildren(ctx, parent.Namespace)
	if err != nil {
		return nil, err
	}
	result := make(map[string]platformv1alpha1.PostgresDatabase)
	for _, child := range list {
		if child.Spec.ClusterRef.Name == parent.Name {
			result[child.Name] = child
		}
	}
	return result, nil
}

// inventory compares the durable preflight inventory with a fresh full
// namespace list. It never treats a missing preflight inventory as zero work.
func (c *Coordinator) inventory(ctx context.Context, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) ([]inventoryItem, error) {
	if len(bg.DatabaseReplication) == 0 {
		return nil, ErrIncompleteInventory
	}
	children, err := c.children(ctx, parent)
	if err != nil {
		return nil, err
	}
	frozen, err := frozenInventory(bg.DatabaseReplication)
	if err != nil {
		return nil, err
	}
	result, err := currentInventory(children, frozen)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(result, func(a, b inventoryItem) int {
		return cmp.Or(
			cmp.Compare(a.key.childName, b.key.childName),
			cmp.Compare(a.key.childUID, b.key.childUID),
			cmp.Compare(a.key.databaseName, b.key.databaseName),
		)
	})
	return result, nil
}

func configMapName(childName, databaseName string) string {
	return childName + "-" + databaseName + "-config"
}

func frozenInventory(databases []platformv1alpha1.BlueGreenDatabaseReplicationStatus) (map[inventoryKey]struct{}, error) {
	frozen := make(map[inventoryKey]struct{}, len(databases))
	for _, database := range databases {
		key := inventoryKey{database.PostgresDatabaseName, database.PostgresDatabaseUID, database.DatabaseName}
		if key.childName == "" || key.childUID == "" || key.databaseName == "" {
			return nil, ErrIncompleteInventory
		}
		if _, duplicate := frozen[key]; duplicate {
			return nil, ErrIncompleteInventory
		}
		frozen[key] = struct{}{}
	}
	return frozen, nil
}

func currentInventory(children map[string]platformv1alpha1.PostgresDatabase, frozen map[inventoryKey]struct{}) ([]inventoryItem, error) {
	result := make([]inventoryItem, 0, len(frozen))
	for _, child := range children {
		if child.DeletionTimestamp != nil || child.UID == "" || child.Generation <= 0 || len(child.Spec.Databases) == 0 {
			return nil, ErrIncompleteInventory
		}
		for _, database := range child.Spec.Databases {
			key := inventoryKey{child.Name, string(child.UID), database.Name}
			if _, found := frozen[key]; !found {
				return nil, ErrIncompleteInventory
			}
			result = append(result, inventoryItem{key: key, childGeneration: child.Generation})
		}
	}
	if len(result) != len(frozen) {
		return nil, ErrIncompleteInventory
	}
	return result, nil
}
