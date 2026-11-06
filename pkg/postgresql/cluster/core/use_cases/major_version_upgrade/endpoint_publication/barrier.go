package endpointpublication

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	pgendpointprotocol "github.com/splunk/splunk-operator/pkg/postgresql/shared/endpointprotocol"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// PreparationComplete is a live pre-commit guard. CPI-2122 must call it again
// immediately before committing the canonical ConfigMap under a child freeze.
func (c *Coordinator) PreparationComplete(ctx context.Context, key types.NamespacedName, attemptID string) (bool, error) {
	complete := false
	err := c.retryStatus(ctx, key, attemptID, func(parent *platformv1alpha1.PostgresCluster, entry *platformv1alpha1.PostgresMajorUpgradeStatus) (bool, error) {
		return c.reconcilePreparationStatus(ctx, parent, entry.BlueGreen, &complete)
	})
	return complete, err
}

// ReconcilePublicationStatus checks child acknowledgements and the actual owned
// ConfigMaps, persisting any updated parent status. Its result is intentionally
// not latched or cached.
func (c *Coordinator) ReconcilePublicationStatus(ctx context.Context, key types.NamespacedName, attemptID string) (bool, error) {
	complete := false
	err := c.retryStatus(ctx, key, attemptID, func(parent *platformv1alpha1.PostgresCluster, entry *platformv1alpha1.PostgresMajorUpgradeStatus) (bool, error) {
		return c.reconcilePublicationStatus(ctx, parent, entry.BlueGreen, &complete)
	})
	return complete, err
}

func clearPreparationAck(ack *platformv1alpha1.BlueGreenEndpointPreparationAcknowledgementStatus) {
	ack.EndpointFingerprint = nil
	ack.ValidatedAt = nil
}

func clearPublicationAck(ack *platformv1alpha1.BlueGreenEndpointPublicationAcknowledgementStatus) {
	ack.EndpointFingerprint = nil
	ack.AcknowledgedAt = nil
}

func childAckMatches(ack *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, child *platformv1alpha1.PostgresDatabase, generation int64, fingerprint string, databaseCount int) bool {
	if ack == nil || ack.AttemptID != bg.AttemptID || ack.IntentGeneration != generation || ack.ObservedDatabaseGeneration != child.Generation || ack.GreenClusterUID != bg.Green.Ref.UID || len(ack.Databases) != databaseCount {
		return false
	}
	seen := make(map[string]struct{}, databaseCount)
	for _, proof := range ack.Databases {
		if proof.DatabaseName == "" || proof.EndpointFingerprint != fingerprint || proof.AcknowledgedAt == nil {
			return false
		}
		if _, duplicate := seen[proof.DatabaseName]; duplicate {
			return false
		}
		seen[proof.DatabaseName] = struct{}{}
	}
	return true
}

func findDatabaseProof(ack *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus, databaseName string) *platformv1alpha1.DatabaseEndpointAcknowledgementStatus {
	for i := range ack.Databases {
		if ack.Databases[i].DatabaseName == databaseName {
			return &ack.Databases[i]
		}
	}
	return nil
}

func (c *Coordinator) readOwnedDatabaseConfigMap(ctx context.Context, child *platformv1alpha1.PostgresDatabase, databaseName string) (*corev1.ConfigMap, error) {
	cm, err := c.store.ReadConfigMap(ctx, types.NamespacedName{Namespace: child.Namespace, Name: configMapName(child.Name, databaseName)})
	if err != nil {
		return nil, err
	}
	if !metav1.IsControlledBy(cm, child) {
		return nil, fmt.Errorf("database endpoint ConfigMap %s is not controlled by PostgresDatabase %s", cm.Name, child.Name)
	}
	return cm, nil
}

func (c *Coordinator) requireCanonicalBlue(ctx context.Context, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, expected map[string]string) error {
	if parent.Status.Resources == nil || parent.Status.Resources.ConfigMapRef == nil || parent.Status.Resources.ConfigMapRef.Name == "" {
		return ErrCanonicalMismatch
	}
	cm, err := c.store.ReadConfigMap(ctx, types.NamespacedName{Namespace: parent.Namespace, Name: parent.Status.Resources.ConfigMapRef.Name})
	if err != nil {
		return err
	}
	if !pgendpointprotocol.CanonicalOwnedByParent(parent, cm) || cm.Annotations[pgconninfo.AnnotationActiveEnvironment] == bg.Green.Ref.Name || !endpointDataMatches(cm.Data, expected) {
		return ErrCanonicalMismatch
	}
	return nil
}

// committedRouting derives the durable post-commit payload from the canonical
// access ConfigMap. Its receipt and fingerprint bind it to the candidate that
// passed preparation, even if candidate Services become unhealthy afterward.
func (c *Coordinator) committedRouting(ctx context.Context, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, generation int64, fingerprint string) (map[string]string, error) {
	if !pgendpointprotocol.DurableCommitStatusMatches(parent, bg) {
		return nil, ErrCanonicalMismatch
	}
	cm, err := c.store.ReadConfigMap(ctx, types.NamespacedName{Namespace: parent.Namespace, Name: bg.CommitReceipt.CanonicalConfigMap.Name})
	if err != nil {
		return nil, err
	}
	routing, valid := pgendpointprotocol.CommittedCanonicalRouting(parent, bg, cm, generation, fingerprint)
	if !valid || bg.EndpointPreparation == nil || !accessMetadataMatches(cm.Data, bg.EndpointPreparation.AccessMetadata) {
		return nil, ErrCanonicalMismatch
	}
	return routing, nil
}

func (c *Coordinator) reconcilePreparationStatus(ctx context.Context, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, complete *bool) (bool, error) {
	*complete = false
	prep := bg.EndpointPreparation
	if prep == nil || prep.Generation <= 0 || prep.Generation != bg.EndpointIntentRevision || prep.EndpointFingerprint == nil || bg.CommitReceipt != nil || bg.EndpointPublication != nil {
		return false, nil
	}
	inventory, err := c.inventory(ctx, parent, bg)
	if errors.Is(err, ErrIncompleteInventory) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !preparationRosterMatches(prep.Acknowledgements, inventory) {
		return false, nil
	}
	proof, fingerprint, err := c.resolve(ctx, parent, bg)
	if err != nil {
		return false, err
	}
	if fingerprint != *prep.EndpointFingerprint {
		return false, nil
	}
	if err := c.requireCanonicalBlue(ctx, parent, bg, proof.Blue); err != nil {
		return false, nil
	}
	children, err := c.children(ctx, parent)
	if err != nil {
		return false, err
	}
	updated := slices.Clone(prep.Acknowledgements)
	*complete, err = c.aggregatePreparationProofs(ctx, children, bg, updated, prep.Generation, fingerprint, proof.Blue)
	if err != nil {
		return false, err
	}
	changed := !reflect.DeepEqual(prep.Acknowledgements, updated)
	prep.Acknowledgements = updated
	return changed, nil
}

func (c *Coordinator) aggregatePreparationProofs(ctx context.Context, children map[string]platformv1alpha1.PostgresDatabase, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, updated []platformv1alpha1.BlueGreenEndpointPreparationAcknowledgementStatus, generation int64, fingerprint string, routing map[string]string) (bool, error) {
	complete := true
	for i := range updated {
		ack := &updated[i]
		proofAck, err := c.validatedPreparationProof(ctx, children, bg, ack, generation, fingerprint, routing)
		if err != nil {
			return complete, err
		}
		if proofAck == nil {
			complete = false
			clearPreparationAck(ack)
			continue
		}
		ack.EndpointFingerprint = &fingerprint
		ack.ValidatedAt = proofAck.AcknowledgedAt.DeepCopy()
	}
	return complete, nil
}

func (c *Coordinator) validatedPreparationProof(ctx context.Context, children map[string]platformv1alpha1.PostgresDatabase, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, ack *platformv1alpha1.BlueGreenEndpointPreparationAcknowledgementStatus, generation int64, fingerprint string, routing map[string]string) (*platformv1alpha1.DatabaseEndpointAcknowledgementStatus, error) {
	child, present := children[ack.PostgresDatabaseName]
	if !present || string(child.UID) != ack.PostgresDatabaseUID || child.Generation != ack.PostgresDatabaseGeneration {
		return nil, nil
	}
	proofAck := child.Status.EndpointPreparation
	if !childAckMatches(proofAck, bg, &child, generation, fingerprint, len(child.Spec.Databases)) {
		return nil, nil
	}
	dbProof := findDatabaseProof(proofAck, ack.DatabaseName)
	if dbProof == nil || dbProof.AcknowledgedAt == nil {
		return nil, nil
	}
	cm, err := c.readOwnedDatabaseConfigMap(ctx, &child, ack.DatabaseName)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	if cm == nil || !endpointDataMatches(cm.Data, routing) {
		return nil, nil
	}
	return dbProof, nil
}

func (c *Coordinator) reconcilePublicationStatus(ctx context.Context, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, complete *bool) (bool, error) {
	*complete = false
	prep, pub := bg.EndpointPreparation, bg.EndpointPublication
	if prep == nil || pub == nil || pub.Generation <= 0 || pub.Generation != prep.Generation || pub.Generation != bg.EndpointIntentRevision || prep.EndpointFingerprint == nil || pub.EndpointFingerprint == nil || *prep.EndpointFingerprint != *pub.EndpointFingerprint {
		return false, nil
	}
	inventory, err := c.inventory(ctx, parent, bg)
	if errors.Is(err, ErrIncompleteInventory) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !preparationRosterMatches(prep.Acknowledgements, inventory) || !publicationRosterMatches(pub.Acknowledgements, inventory) || !preparationAggregated(prep.Acknowledgements, inventory, *prep.EndpointFingerprint) {
		return false, nil
	}
	fingerprint := *pub.EndpointFingerprint
	committed, err := c.committedRouting(ctx, parent, bg, pub.Generation, fingerprint)
	if err != nil {
		if errors.Is(err, ErrCanonicalMismatch) {
			return false, nil
		}
		return false, err
	}
	children, err := c.children(ctx, parent)
	if err != nil {
		return false, err
	}
	updated := slices.Clone(pub.Acknowledgements)
	*complete, err = c.aggregatePublicationProofs(ctx, children, bg, updated, pub.Generation, fingerprint, committed)
	if err != nil {
		return false, err
	}
	changed := !reflect.DeepEqual(pub.Acknowledgements, updated)
	pub.Acknowledgements = updated
	return changed, nil
}

func (c *Coordinator) aggregatePublicationProofs(ctx context.Context, children map[string]platformv1alpha1.PostgresDatabase, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, updated []platformv1alpha1.BlueGreenEndpointPublicationAcknowledgementStatus, generation int64, fingerprint string, routing map[string]string) (bool, error) {
	complete := true
	for i := range updated {
		ack := &updated[i]
		proofAck, err := c.validatedPublicationProof(ctx, children, bg, ack, generation, fingerprint, routing)
		if err != nil {
			return complete, err
		}
		if proofAck == nil {
			complete = false
			clearPublicationAck(ack)
			continue
		}
		ack.EndpointFingerprint = &fingerprint
		ack.AcknowledgedAt = proofAck.AcknowledgedAt.DeepCopy()
	}
	return complete, nil
}

func (c *Coordinator) validatedPublicationProof(ctx context.Context, children map[string]platformv1alpha1.PostgresDatabase, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, ack *platformv1alpha1.BlueGreenEndpointPublicationAcknowledgementStatus, generation int64, fingerprint string, routing map[string]string) (*platformv1alpha1.DatabaseEndpointAcknowledgementStatus, error) {
	child, present := children[ack.PostgresDatabaseName]
	if !present || string(child.UID) != ack.PostgresDatabaseUID || child.Generation != ack.PostgresDatabaseGeneration {
		return nil, nil
	}
	proofAck := child.Status.EndpointPublication
	if !childAckMatches(proofAck, bg, &child, generation, fingerprint, len(child.Spec.Databases)) {
		return nil, nil
	}
	dbProof := findDatabaseProof(proofAck, ack.DatabaseName)
	if dbProof == nil || dbProof.AcknowledgedAt == nil || dbProof.ConfigMapUID == "" {
		return nil, nil
	}
	cm, err := c.readOwnedDatabaseConfigMap(ctx, &child, ack.DatabaseName)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	if cm == nil || cm.UID != dbProof.ConfigMapUID || !endpointDataMatches(cm.Data, routing) || !pgendpointprotocol.PublicationStampMatches(cm, bg, generation, fingerprint) {
		return nil, nil
	}
	return dbProof, nil
}
