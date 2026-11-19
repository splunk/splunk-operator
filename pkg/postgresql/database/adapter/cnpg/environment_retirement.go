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

package cnpg

import (
	"context"
	"fmt"
	"slices"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	pgcnpg "github.com/splunk/splunk-operator/pkg/postgresql/shared/cnpg"
	pgreconcile "github.com/splunk/splunk-operator/pkg/postgresql/shared/reconcile"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// EnvironmentRetirer removes only the frozen provider Database declarations.
type EnvironmentRetirer struct {
	client client.Client
	reader client.Reader
}

func NewEnvironmentRetirer(c client.Client, reader client.Reader) *EnvironmentRetirer {
	return &EnvironmentRetirer{client: c, reader: reader}
}

// RetireEnvironment changes or deletes at most one exact declaration per pass.
func (r *EnvironmentRetirer) RetireEnvironment(ctx context.Context, request dbtypes.EnvironmentRetirement, verify func(context.Context) error) (bool, error) {
	if err := validateRetirementScope(request); err != nil {
		return false, err
	}
	if r.reader == nil || verify == nil {
		return false, fmt.Errorf("environment retirement requires uncached reads and request verification")
	}
	if err := verify(ctx); err != nil {
		return false, err
	}
	for _, database := range request.Databases {
		for _, ref := range database.Resources {
			actual, err := r.declaration(ctx, request, database.DatabaseName, ref)
			if err != nil {
				return false, err
			}
			if actual == nil {
				continue
			}
			if actual.DeletionTimestamp != nil {
				if actual.Spec.ReclaimPolicy != cnpgv1.DatabaseReclaimRetain {
					return false, fmt.Errorf("deleting retirement Database must retain SQL")
				}
				if slices.Contains(actual.Finalizers, cnpgutils.DatabaseFinalizerName) && actual.Generation == actual.Status.ObservedGeneration {
					if err := verify(ctx); err != nil {
						return false, err
					}
					// Wake CNPG 1.30.2 past its observed-generation shortcut after deletion.
					before := actual.DeepCopy()
					if actual.Spec.Ensure == cnpgv1.EnsureAbsent {
						actual.Spec.Ensure = cnpgv1.EnsurePresent
					} else {
						actual.Spec.Ensure = cnpgv1.EnsureAbsent
					}
					return false, pgreconcile.PatchLocked(ctx, r.client, before, actual)
				}
				return false, nil
			}
			if err := verify(ctx); err != nil {
				return false, err
			}
			before := actual.DeepCopy()
			if pgcnpg.RetainDatabase(actual) {
				return false, pgreconcile.PatchLocked(ctx, r.client, before, actual)
			}
			return false, pgreconcile.DeleteExact(ctx, r.client, actual)
		}
	}
	var list cnpgv1.DatabaseList
	if err := r.reader.List(ctx, &list, client.InNamespace(request.Child.Namespace)); err != nil {
		return false, fmt.Errorf("listing retired database declarations: %w", err)
	}
	for _, database := range list.Items {
		owner := metav1.GetControllerOf(&database)
		if owner != nil && owner.UID == request.Child.UID && database.Spec.ClusterRef.Name == request.Environment.Name {
			return false, fmt.Errorf("retirement inventory changed: provider Database %s remains", database.Name)
		}
	}
	return true, nil
}

// declaration validates ownership and logical identity before exposing a mutation target.
func (r *EnvironmentRetirer) declaration(ctx context.Context, request dbtypes.EnvironmentRetirement, name string, ref corev1.ObjectReference) (*cnpgv1.Database, error) {
	if ref.Namespace != request.Child.Namespace || ref.APIVersion != cnpgv1.SchemeGroupVersion.String() || ref.Kind != "Database" || ref.Name == "" || ref.UID == "" {
		return nil, fmt.Errorf("retirement requires exact namespaced Database identities")
	}
	actual := &cnpgv1.Database{}
	if exists, err := pgreconcile.ReadExactObject(ctx, r.reader, ref, actual); err != nil || !exists {
		return nil, err
	}
	owner := metav1.GetControllerOf(actual)
	if owner == nil || owner.UID != request.Child.UID || owner.Name != request.Child.Name ||
		owner.Kind != "PostgresDatabase" || owner.APIVersion != request.Child.APIVersion || actual.Spec.ClusterRef.Name != request.Environment.Name || actual.Spec.Name != name {
		return nil, fmt.Errorf("retirement Database %s has conflicting ownership or identity", ref.Name)
	}
	return actual, nil
}

// validateRetirementScope rejects incomplete identities before any namespace list.
func validateRetirementScope(request dbtypes.EnvironmentRetirement) error {
	child, environment := request.Child, request.Environment
	if child.APIVersion == "" || child.Kind != "PostgresDatabase" || child.Name == "" || child.UID == "" || child.Namespace == "" ||
		environment.APIVersion != cnpgv1.SchemeGroupVersion.String() || environment.Kind != "Cluster" || environment.Name == "" ||
		environment.UID == "" || environment.Namespace != child.Namespace {
		return fmt.Errorf("environment retirement requires exact child and environment identities")
	}
	return nil
}
