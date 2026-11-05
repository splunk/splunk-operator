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
// blue/green endpoint protocol. CPI-2122 supplies canonical commit and opening
// green; this package provides live guards for both transitions.
package endpointpublication

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	pgendpointprotocol "github.com/splunk/splunk-operator/pkg/postgresql/shared/endpointprotocol"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// Store reads safety evidence directly from the API server and writes only
// PostgresCluster status with Kubernetes resourceVersion conflict detection.
type Store interface {
	ReadParent(context.Context, types.NamespacedName) (*platformv1alpha1.PostgresCluster, error)
	ListChildren(context.Context, string) ([]platformv1alpha1.PostgresDatabase, error)
	ReadConfigMap(context.Context, types.NamespacedName) (*corev1.ConfigMap, error)
	WriteParentStatus(context.Context, *platformv1alpha1.PostgresCluster) error
}

// Proof is a fresh, provider-validated pair of complete routing payloads.
// The adapter must check green CNPG UID and Service identity and Pooler Deployment readiness.
type Proof struct {
	Blue     map[string]string
	Green    map[string]string
	GreenUID types.UID
}

type ProofResolver interface {
	Resolve(context.Context, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (Proof, error)
	ResolveBlue(context.Context, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (map[string]string, error)
}

type Coordinator struct {
	store Store
	proof ProofResolver
}

func NewCoordinator(store Store, proof ProofResolver) *Coordinator {
	return &Coordinator{store: store, proof: proof}
}

var (
	ErrStaleAttempt        = errors.New("endpoint publication attempt is stale")
	ErrIncompleteInventory = errors.New("endpoint publication inventory is incomplete")
	ErrCanonicalMismatch   = errors.New("canonical access ConfigMap does not prove endpoint commit")
)

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
