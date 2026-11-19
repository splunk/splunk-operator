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
	"fmt"
	"maps"
	"slices"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const controlRetireSubscriptionFunction = "cutover_retire_subscription"

const greenReplicationProofVersion = 1

// Each false result commits one stage before the caller retries.
func retireSubscriptionHelper(id cutoverIdentity, database string) cutoverHelper {
	return cutoverHelper{controlRetireSubscriptionFunction, `DECLARE sub record; BEGIN
IF session_user <> ` + quoteSQLStringLiteral(id.Role) + ` THEN RAISE EXCEPTION 'invalid cutover identity'; END IF;
SELECT oid,subowner,subenabled,subslotname,subpublications INTO sub FROM pg_catalog.pg_subscription
WHERE subname=` + quoteSQLStringLiteral(subscriptionName(id.LogicalName, database)) + ` AND subdbid=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database());
IF NOT FOUND THEN RETURN true; END IF;
IF sub.subowner<>(SELECT oid FROM pg_catalog.pg_roles WHERE rolname='postgres') OR
 (sub.subslotname IS NOT NULL AND sub.subslotname<>` + quoteSQLStringLiteral(replicationSlotName(id.LogicalName, database)) + `) OR
 sub.subpublications<>ARRAY[` + quoteSQLStringLiteral(publicationName(id.LogicalName, database)) + `]::text[] THEN RAISE EXCEPTION 'subscription identity changed'; END IF;
IF sub.subenabled THEN EXECUTE ` + quoteSQLStringLiteral("ALTER SUBSCRIPTION "+pginfra.QuoteIdentifier(subscriptionName(id.LogicalName, database))+" DISABLE") + `; RETURN false; END IF;
IF EXISTS (SELECT 1 FROM pg_catalog.pg_stat_subscription WHERE subid=sub.oid AND pid IS NOT NULL) THEN RETURN false; END IF;
IF sub.subslotname IS NOT NULL THEN EXECUTE ` + quoteSQLStringLiteral("ALTER SUBSCRIPTION "+pginfra.QuoteIdentifier(subscriptionName(id.LogicalName, database))+" SET (slot_name=NONE)") + `; RETURN false; END IF;
EXECUTE ` + quoteSQLStringLiteral("DROP SUBSCRIPTION "+pginfra.QuoteIdentifier(subscriptionName(id.LogicalName, database))) + `;
RETURN true; END;`}
}

// RemoveReplication proves green SQL absence before source teardown retires credentials.
func (c *BlueGreenCancellation) RemoveReplication(ctx context.Context, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	if done, err := c.ensureGreenReplicationRemoved(ctx, bg); err != nil || !done {
		return false, err
	}
	return c.BlueGreenReplicationTeardown.RemoveReplication(ctx, bg)
}

// Older in-flight cancellations may already have a source-teardown receipt.
func (c *BlueGreenCancellation) RemoveCredentials(ctx context.Context, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	if done, err := c.ensureGreenReplicationRemoved(ctx, bg); err != nil || !done {
		return false, err
	}
	return c.BlueGreenReplicationTeardown.RemoveCredentials(ctx, bg)
}

func (c *BlueGreenCancellation) ensureGreenReplicationRemoved(ctx context.Context, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	if bg.Gates == nil || bg.Gates.Cancellation == nil {
		return false, fmt.Errorf("subscription teardown requires a cancellation receipt")
	}
	green, err := c.AttemptGreen(ctx, bg)
	if err != nil {
		return false, err
	}
	if done, err := c.deleteOwned(ctx, &cnpgv1.SubscriptionList{}, bg, func(object client.Object) error {
		if green == nil {
			return c.releaseMissingGreen(ctx, bg, object.(*cnpgv1.Subscription))
		}
		return quiesceRetainedSubscription(ctx, c.client, object.(*cnpgv1.Subscription))
	}); err != nil || !done {
		return false, err
	}
	if bg.Gates.Cancellation.GreenReplicationRemovedAt != nil && bg.Gates.Cancellation.GreenReplicationProofVersion == greenReplicationProofVersion {
		return true, nil
	}
	if green != nil {
		done, err := c.removeGreenSubscriptions(ctx, green, bg)
		if err != nil || !done {
			return false, err
		}
	} else if bg.Green != nil {
		gone, err := c.greenInstancesGone(ctx, bg)
		if err != nil || !gone {
			return false, err
		}
		var claims corev1.PersistentVolumeClaimList
		if err := c.reader.List(ctx, &claims, client.InNamespace(c.key.Namespace)); err != nil {
			return false, err
		}
		for _, claim := range claims.Items {
			for _, owner := range claim.OwnerReferences {
				if owner.UID == bg.Green.Ref.UID {
					return false, fmt.Errorf("candidate storage remains without verifiable SQL subscription removal")
				}
			}
		}
	}
	if bg.Gates.Cancellation.GreenReplicationRemovedAt == nil {
		bg.Gates.Cancellation.GreenReplicationRemovedAt = new(metav1.Now())
	}
	bg.Gates.Cancellation.GreenReplicationProofVersion = greenReplicationProofVersion
	return false, nil
}

func (c *BlueGreenCancellation) removeGreenSubscriptions(ctx context.Context, green *cnpgv1.Cluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	if len(bg.DatabaseReplication) == 0 || bg.SubscriptionCreationAuthorized != nil && !*bg.SubscriptionCreationAuthorized {
		return true, nil
	}
	if c.subscriberSQL == nil {
		return false, fmt.Errorf("candidate subscription SQL is not configured")
	}
	id := cutoverIdentity{Role: "postgres", AttemptID: bg.AttemptID, LogicalName: c.key.Name}
	if bg.Switchover != nil && bg.Switchover.ControlIdentity != nil {
		var err error
		id, err = c.switchover.access.Credentials(ctx, bg)
		if err != nil {
			return false, err
		}
	}
	return c.subscriberSQL.RemoveSubscriptions(ctx, green, bg, id)
}

// CNPG 1.30 skips deletion when observedGeneration matches. Changing only an
// already deleting spec wakes its finalizer without allowing another apply.
func quiesceRetainedSubscription(ctx context.Context, c client.Client, subscription *cnpgv1.Subscription) error {
	if subscription.Spec.ReclaimPolicy != cnpgv1.SubscriptionReclaimRetain {
		return fmt.Errorf("subscription writer quiescence requires deleting Retain intent")
	}
	return wakeDeletingDeclaration(ctx, c, subscription, &subscription.Spec.Parameters, "enabled", subscription.Status.ObservedGeneration, cnpgutils.SubscriptionFinalizerName)
}

func wakeDeletingDeclaration(ctx context.Context, c client.Client, object client.Object, parameters *map[string]string, parameter string, observed int64, finalizer string) error {
	if object.GetDeletionTimestamp() == nil {
		return fmt.Errorf("writer acknowledgement requires a deleting declaration")
	}
	if !slices.Contains(object.GetFinalizers(), finalizer) || object.GetGeneration() != observed {
		return nil
	}
	original := object.DeepCopyObject().(client.Object)
	*parameters = maps.Clone(*parameters)
	if *parameters == nil {
		*parameters = map[string]string{}
	}
	if (*parameters)[parameter] == "false" {
		(*parameters)[parameter] = "true"
	} else {
		(*parameters)[parameter] = "false"
	}
	return c.Patch(ctx, object, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{}))
}
