package endpointpublication

import (
	"context"
	"fmt"
	"math"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	pgendpointprotocol "github.com/splunk/splunk-operator/pkg/postgresql/shared/endpointprotocol"
	"k8s.io/apimachinery/pkg/types"
)

// PublishPreparationIntent freezes one complete, nonempty child inventory and
// advances the per-attempt revision only for a new intent. Retried calls reuse it.
func (c *Coordinator) PublishPreparationIntent(ctx context.Context, key types.NamespacedName, attemptID string) error {
	return c.retryStatus(ctx, key, attemptID, func(parent *platformv1alpha1.PostgresCluster, entry *platformv1alpha1.PostgresMajorUpgradeStatus) (bool, error) {
		bg := entry.BlueGreen
		if bg.CommitReceipt != nil || bg.EndpointPublication != nil {
			return false, fmt.Errorf("cannot prepare endpoints after canonical commit or publication")
		}
		inventory, err := c.inventory(ctx, parent, bg)
		if err != nil {
			return false, err
		}
		proof, fingerprint, err := c.resolve(ctx, parent, bg)
		if err != nil {
			return false, err
		}
		if err := c.requireCanonicalBlue(ctx, parent, bg, proof.Blue); err != nil {
			return false, err
		}
		if prep := bg.EndpointPreparation; prep != nil {
			return false, validateExistingPreparation(prep, bg.EndpointIntentRevision, fingerprint, inventory)
		}
		if bg.EndpointIntentRevision == math.MaxInt64 {
			return false, fmt.Errorf("endpoint intent revision overflow")
		}
		bg.EndpointIntentRevision++
		acks := newPreparationAcknowledgements(inventory)
		bg.EndpointPreparation = &platformv1alpha1.BlueGreenEndpointPreparationStatus{
			Generation: bg.EndpointIntentRevision, EndpointFingerprint: &fingerprint, Acknowledgements: acks,
		}
		return true, nil
	})
}

// CancelPreparation removes only an uncommitted matching intent. The revision
// remains durable, so a later preparation can never reuse the cancelled ID.
func (c *Coordinator) CancelPreparation(ctx context.Context, key types.NamespacedName, attemptID string, generation int64) error {
	return c.retryStatus(ctx, key, attemptID, func(parent *platformv1alpha1.PostgresCluster, entry *platformv1alpha1.PostgresMajorUpgradeStatus) (bool, error) {
		bg := entry.BlueGreen
		if bg.CommitReceipt != nil || bg.EndpointPublication != nil {
			return false, fmt.Errorf("cannot cancel preparation after canonical commit")
		}
		if bg.EndpointPreparation == nil {
			if generation <= bg.EndpointIntentRevision {
				return false, nil
			}
			return false, ErrStaleAttempt
		}
		if bg.EndpointPreparation.Generation != generation {
			return false, ErrStaleAttempt
		}
		if err := c.requireBlueBeforeCancellation(ctx, parent, bg); err != nil {
			return false, err
		}
		bg.EndpointPreparation = nil
		return true, nil
	})
}

// PublishPublicationIntent exposes the second phase only after a verified
// canonical commit. It carries the exact prepared revision and roster.
func (c *Coordinator) PublishPublicationIntent(ctx context.Context, key types.NamespacedName, attemptID string) error {
	return c.retryStatus(ctx, key, attemptID, func(parent *platformv1alpha1.PostgresCluster, entry *platformv1alpha1.PostgresMajorUpgradeStatus) (bool, error) {
		bg := entry.BlueGreen
		prep := bg.EndpointPreparation
		if prep == nil || prep.Generation <= 0 || prep.Generation != bg.EndpointIntentRevision || prep.EndpointFingerprint == nil {
			return false, fmt.Errorf("current endpoint preparation is required")
		}
		inventory, err := c.inventory(ctx, parent, bg)
		if err != nil {
			return false, err
		}
		if !preparationRosterMatches(prep.Acknowledgements, inventory) {
			return false, ErrIncompleteInventory
		}
		fingerprint := *prep.EndpointFingerprint
		if !preparationAggregated(prep.Acknowledgements, inventory, fingerprint) {
			return false, fmt.Errorf("preparation barrier is incomplete")
		}
		if _, err := c.committedRouting(ctx, parent, bg, prep.Generation, fingerprint); err != nil {
			return false, err
		}
		if pub := bg.EndpointPublication; pub != nil {
			return false, validateExistingPublication(pub, prep.Generation, fingerprint, inventory)
		}
		acks := newPublicationAcknowledgements(inventory)
		bg.EndpointPublication = &platformv1alpha1.BlueGreenEndpointPublicationStatus{
			Generation: prep.Generation, EndpointFingerprint: &fingerprint, Acknowledgements: acks,
		}
		return true, nil
	})
}

func preparationRosterMatches(acks []platformv1alpha1.BlueGreenEndpointPreparationAcknowledgementStatus, inventory []inventoryItem) bool {
	if len(acks) != len(inventory) {
		return false
	}
	for _, item := range inventory {
		found := false
		for _, ack := range acks {
			if ack.PostgresDatabaseName == item.key.childName && ack.PostgresDatabaseUID == item.key.childUID && ack.DatabaseName == item.key.databaseName && ack.PostgresDatabaseGeneration == item.childGeneration {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func publicationRosterMatches(acks []platformv1alpha1.BlueGreenEndpointPublicationAcknowledgementStatus, inventory []inventoryItem) bool {
	if len(acks) != len(inventory) {
		return false
	}
	for _, item := range inventory {
		found := false
		for _, ack := range acks {
			if ack.PostgresDatabaseName == item.key.childName && ack.PostgresDatabaseUID == item.key.childUID && ack.DatabaseName == item.key.databaseName && ack.PostgresDatabaseGeneration == item.childGeneration {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func preparationAggregated(acks []platformv1alpha1.BlueGreenEndpointPreparationAcknowledgementStatus, inventory []inventoryItem, fingerprint string) bool {
	if !preparationRosterMatches(acks, inventory) {
		return false
	}
	for _, ack := range acks {
		if ack.ValidatedAt == nil || ack.EndpointFingerprint == nil || *ack.EndpointFingerprint != fingerprint {
			return false
		}
	}
	return true
}

func endpointDataMatches(actual, expected map[string]string) bool {
	return pgconninfo.EndpointDataMatches(actual, expected)
}

func newPreparationAcknowledgements(inventory []inventoryItem) []platformv1alpha1.BlueGreenEndpointPreparationAcknowledgementStatus {
	acks := make([]platformv1alpha1.BlueGreenEndpointPreparationAcknowledgementStatus, 0, len(inventory))
	for _, item := range inventory {
		acks = append(acks, platformv1alpha1.BlueGreenEndpointPreparationAcknowledgementStatus{
			PostgresDatabaseName:       item.key.childName,
			PostgresDatabaseUID:        item.key.childUID,
			PostgresDatabaseGeneration: item.childGeneration,
			DatabaseName:               item.key.databaseName,
		})
	}
	return acks
}

func newPublicationAcknowledgements(inventory []inventoryItem) []platformv1alpha1.BlueGreenEndpointPublicationAcknowledgementStatus {
	acks := make([]platformv1alpha1.BlueGreenEndpointPublicationAcknowledgementStatus, 0, len(inventory))
	for _, item := range inventory {
		acks = append(acks, platformv1alpha1.BlueGreenEndpointPublicationAcknowledgementStatus{
			PostgresDatabaseName:       item.key.childName,
			PostgresDatabaseUID:        item.key.childUID,
			PostgresDatabaseGeneration: item.childGeneration,
			DatabaseName:               item.key.databaseName,
		})
	}
	return acks
}

func (c *Coordinator) requireBlueBeforeCancellation(ctx context.Context, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	if c.proof == nil {
		return fmt.Errorf("blue endpoint proof resolver is required")
	}
	blue, err := c.proof.ResolveBlue(ctx, parent, bg)
	if err != nil {
		return err
	}
	if !pgendpointprotocol.CompleteRouting(blue) {
		return fmt.Errorf("blue endpoint proof is incomplete")
	}
	if err := c.requireCanonicalBlue(ctx, parent, bg, blue); err != nil {
		return err
	}
	return nil
}

func validateExistingPreparation(prep *platformv1alpha1.BlueGreenEndpointPreparationStatus, generation int64, fingerprint string, inventory []inventoryItem) error {
	if prep.Generation != generation || prep.EndpointFingerprint == nil || *prep.EndpointFingerprint != fingerprint || !preparationRosterMatches(prep.Acknowledgements, inventory) {
		return fmt.Errorf("existing endpoint preparation intent differs from current inventory or proof")
	}
	return nil
}

func validateExistingPublication(pub *platformv1alpha1.BlueGreenEndpointPublicationStatus, generation int64, fingerprint string, inventory []inventoryItem) error {
	if pub.Generation != generation || pub.EndpointFingerprint == nil || *pub.EndpointFingerprint != fingerprint || !publicationRosterMatches(pub.Acknowledgements, inventory) {
		return fmt.Errorf("existing endpoint publication intent differs from preparation")
	}
	return nil
}
