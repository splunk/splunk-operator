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

package managedroles

// This file publishes credential-ready managed-role intent from the
// PostgresDatabase status surface consumed by the PostgresCluster controller.

import (
	"context"
	"fmt"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IntentPublisher persists the database-owned managed-role projection.
type IntentPublisher struct {
	client   client.Client
	database *platformv1alpha1.PostgresDatabase
}

// NewIntentPublisher returns a publisher bound to the current
// PostgresDatabase object and its resource version.
func NewIntentPublisher(c client.Client, database *platformv1alpha1.PostgresDatabase) *IntentPublisher {
	return &IntentPublisher{client: c, database: database}
}

func (p *IntentPublisher) Publish(ctx context.Context, publication dbtypes.ManagedRolePublication) error {
	if err := validatePublicationTarget(p.database, publication.Target); err != nil {
		return err
	}

	updated := p.database.DeepCopy()
	mergeManagedRoleIntent(&updated.Status, publication.Databases)
	if equality.Semantic.DeepEqual(p.database.Status, updated.Status) {
		return nil
	}

	if err := p.client.Status().Update(ctx, updated); err != nil {
		wrapped := fmt.Errorf("publishing managed-role intent for PostgresDatabase %s/%s: %w", publication.Target.Namespace, publication.Target.Name, err)
		if apierrors.IsConflict(err) {
			return fmt.Errorf("%w: %w", dbtypes.ErrManagedRoleIntentConflict, wrapped)
		}
		return wrapped
	}
	p.database.Status = *updated.Status.DeepCopy()
	p.database.ResourceVersion = updated.ResourceVersion
	return nil
}

func validatePublicationTarget(database *platformv1alpha1.PostgresDatabase, target dbtypes.ManagedRolePublicationTarget) error {
	if database == nil {
		return fmt.Errorf("publishing managed-role intent: PostgresDatabase is nil")
	}
	if database.Name != target.Name || database.Namespace != target.Namespace || string(database.UID) != target.UID {
		return fmt.Errorf(
			"publishing managed-role intent: target %s/%s uid %q does not match PostgresDatabase %s/%s uid %q",
			target.Namespace, target.Name, target.UID, database.Namespace, database.Name, database.UID,
		)
	}
	if database.Generation != target.Generation {
		return fmt.Errorf(
			"publishing managed-role intent: target generation %d does not match PostgresDatabase generation %d",
			target.Generation, database.Generation,
		)
	}
	return nil
}

func mergeManagedRoleIntent(status *platformv1alpha1.PostgresDatabaseStatus, desired []dbtypes.DatabaseRoleIntent) {
	existing := make(map[string]platformv1alpha1.DatabaseInfo, len(status.Databases))
	for _, database := range status.Databases {
		existing[database.Name] = database
	}
	result := make([]platformv1alpha1.DatabaseInfo, 0, len(desired))
	for _, database := range desired {
		roles := roleStatus(database.Roles)
		if isRemovalTombstone(database.Roles) {
			result = append(result, platformv1alpha1.DatabaseInfo{Name: database.Database, Roles: roles})
			continue
		}
		current := existing[database.Database]
		current.Name = database.Database
		current.Ready = false
		current.Roles = roles
		result = append(result, current)
	}
	status.Databases = result
}

func roleStatus(roles []dbtypes.ManagedRoleIntent) []platformv1alpha1.DatabaseRoleInfo {
	result := make([]platformv1alpha1.DatabaseRoleInfo, 0, len(roles))
	for _, role := range roles {
		var secretRef *corev1.LocalObjectReference
		if role.SecretName != "" {
			secretRef = &corev1.LocalObjectReference{Name: role.SecretName}
		}
		result = append(result, platformv1alpha1.DatabaseRoleInfo{
			Name: role.Name, SecretRef: secretRef, Exists: role.Exists,
		})
	}
	return result
}

func isRemovalTombstone(roles []dbtypes.ManagedRoleIntent) bool {
	if len(roles) == 0 {
		return false
	}
	for _, role := range roles {
		if role.Exists {
			return false
		}
	}
	return true
}

// IntentFromStatus translates the previously committed role projection used
// to calculate live-removal tombstones.
func IntentFromStatus(database *platformv1alpha1.PostgresDatabase) []dbtypes.DatabaseRoleIntent {
	if database == nil {
		return nil
	}
	result := make([]dbtypes.DatabaseRoleIntent, 0, len(database.Status.Databases))
	for _, status := range database.Status.Databases {
		if len(status.Roles) == 0 {
			continue
		}
		intent := dbtypes.DatabaseRoleIntent{
			Database: status.Name,
			Roles:    make([]dbtypes.ManagedRoleIntent, 0, len(status.Roles)),
		}
		for _, role := range status.Roles {
			secretName := ""
			if role.SecretRef != nil {
				secretName = role.SecretRef.Name
			}
			intent.Roles = append(intent.Roles, dbtypes.ManagedRoleIntent{
				Name: role.Name, SecretName: secretName, Exists: role.Exists,
			})
		}
		result = append(result, intent)
	}
	return result
}
