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
	"errors"
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/logging"
	cnpginfra "github.com/splunk/splunk-operator/pkg/postgresql/database/infrastructure/cnpg"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var errClosedDatabaseWithManagedExtensions = errors.New("CNPG cannot manage extensions while database connections are disabled")

type DatabaseProvisioner struct {
	client client.Client
	scheme *runtime.Scheme
}

func NewDatabaseProvisioner(c client.Client, scheme *runtime.Scheme) *DatabaseProvisioner {
	return &DatabaseProvisioner{client: c, scheme: scheme}
}

func (p *DatabaseProvisioner) Apply(ctx context.Context, target dbtypes.ProvisionerTarget, desired []dbtypes.DesiredDatabase) (dbtypes.ApplyResult, error) {
	owner := &platformv1alpha1.PostgresDatabase{
		TypeMeta: metav1.TypeMeta{
			APIVersion: platformv1alpha1.GroupVersion.String(),
			Kind:       "PostgresDatabase",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      target.PostgresDatabaseName,
			Namespace: target.Namespace,
			UID:       types.UID(target.PostgresDatabaseUID),
		},
	}

	result := dbtypes.ApplyResult{Expected: make([]dbtypes.ExpectedDatabase, 0, len(desired))}
	for _, database := range desired {
		applied, adopted, err := cnpginfra.ApplyDatabase(
			ctx,
			p.client,
			p.scheme,
			owner,
			database.ResourceName,
			func(existing cnpgv1.Database) (cnpgv1.DatabaseSpec, error) {
				return buildDatabaseSpec(target.ProviderClusterName, database, existing)
			},
		)
		if err != nil {
			return result, fmt.Errorf("reconciling CNPG Database %s: %w", database.ResourceName, err)
		}
		if adopted {
			result.Adopted = append(result.Adopted, database.Name)
			logging.FromContext(ctx).InfoContext(ctx, "CNPG Database re-adopted", "name", database.ResourceName)
		}
		result.Expected = append(result.Expected, dbtypes.ExpectedDatabase{
			Name: database.Name, ResourceName: database.ResourceName, Generation: applied.Generation,
		})
	}
	return result, nil
}

func (p *DatabaseProvisioner) Inspect(ctx context.Context, target dbtypes.ProvisionerTarget, identities []dbtypes.DatabaseIdentity) (dbtypes.Observation, error) {
	expected := make([]dbtypes.ExpectedDatabase, 0, len(identities))
	for _, identity := range identities {
		expected = append(expected, dbtypes.ExpectedDatabase{Name: identity.Name, ResourceName: identity.ResourceName})
	}
	return p.observe(ctx, target, expected)
}

func (p *DatabaseProvisioner) Observe(ctx context.Context, target dbtypes.ProvisionerTarget, expected []dbtypes.ExpectedDatabase) (dbtypes.Observation, error) {
	return p.observe(ctx, target, expected)
}

func (p *DatabaseProvisioner) observe(ctx context.Context, target dbtypes.ProvisionerTarget, expected []dbtypes.ExpectedDatabase) (dbtypes.Observation, error) {
	result := dbtypes.Observation{Databases: make([]dbtypes.ObservedDatabase, 0, len(expected))}
	for _, database := range expected {
		observed := dbtypes.ObservedDatabase{
			Name: database.Name, ResourceName: database.ResourceName,
		}
		actual, err := cnpginfra.GetDatabase(ctx, p.client, types.NamespacedName{
			Name: database.ResourceName, Namespace: target.Namespace,
		})
		if apierrors.IsNotFound(err) {
			result.Databases = append(result.Databases, observed)
			continue
		}
		if err != nil {
			return result, fmt.Errorf("getting CNPG Database %s: %w", database.ResourceName, err)
		}
		observed.Found = true
		observed.UID = string(actual.UID)
		observed.ObservedGeneration = actual.Status.ObservedGeneration
		observed.Applied = actual.Status.Applied != nil && *actual.Status.Applied
		observed.Message = databaseMessage(actual)
		result.Databases = append(result.Databases, observed)
	}
	return result, nil
}

func buildDatabaseSpec(clusterName string, desired dbtypes.DesiredDatabase, existing cnpgv1.Database) (cnpgv1.DatabaseSpec, error) {
	if connectionsDisabled(desired.Mutable.AllowConnections) && len(desired.Extensions) > 0 {
		return cnpgv1.DatabaseSpec{}, fmt.Errorf("%w for database %q", errClosedDatabaseWithManagedExtensions, desired.Name)
	}
	allowConnections := cloneBool(desired.Mutable.AllowConnections)
	extensions := reconcileExtensions(desired.Extensions, existing.Spec.Extensions)
	if connectionsDisabled(allowConnections) {
		allowConnections, extensions = stageExtensionRemovalBeforeClose(existing, extensions)
	}
	reclaim := cnpgv1.DatabaseReclaimDelete
	if desired.Reclaim == dbtypes.ReclaimRetain {
		reclaim = cnpgv1.DatabaseReclaimRetain
	}
	return cnpgv1.DatabaseSpec{
		Name:             desired.Name,
		Owner:            desired.Owner,
		ClusterRef:       corev1.LocalObjectReference{Name: clusterName},
		Template:         desired.Creation.Template,
		Encoding:         desired.Creation.Encoding,
		Locale:           desired.Creation.Locale,
		LocaleProvider:   desired.Creation.LocaleProvider,
		LcCollate:        desired.Creation.LocaleCollate,
		LcCtype:          desired.Creation.LocaleCType,
		IcuLocale:        desired.Creation.ICULocale,
		IcuRules:         desired.Creation.ICURules,
		BuiltinLocale:    desired.Creation.BuiltinLocale,
		CollationVersion: desired.Creation.CollationVersion,
		IsTemplate:       cloneBool(desired.Mutable.IsTemplate),
		AllowConnections: allowConnections,
		ConnectionLimit:  intPointer(desired.Mutable.ConnectionLimit),
		Tablespace:       desired.Mutable.Tablespace,
		ReclaimPolicy:    reclaim,
		Extensions:       extensions,
	}, nil
}

// CNPG must connect to a database while removing managed extensions. Keep the
// database open until CNPG has acknowledged the EnsureAbsent declarations,
// then remove those declarations and apply the requested close together.
func stageExtensionRemovalBeforeClose(existing cnpgv1.Database, extensions []cnpgv1.ExtensionSpec) (*bool, []cnpgv1.ExtensionSpec) {
	if hasPresentExtension(existing.Spec.Extensions) {
		return boolPointer(true), extensions
	}
	if len(existing.Spec.Extensions) > 0 && !providerGenerationApplied(existing) {
		return boolPointer(true), extensions
	}
	return boolPointer(false), nil
}

func hasPresentExtension(extensions []cnpgv1.ExtensionSpec) bool {
	for _, extension := range extensions {
		if extension.Ensure != cnpgv1.EnsureAbsent {
			return true
		}
	}
	return false
}

func providerGenerationApplied(database cnpgv1.Database) bool {
	return database.Status.Applied != nil && *database.Status.Applied &&
		database.Status.ObservedGeneration == database.Generation
}

func connectionsDisabled(value *bool) bool {
	return value != nil && !*value
}

func reconcileExtensions(desired []string, existing []cnpgv1.ExtensionSpec) []cnpgv1.ExtensionSpec {
	if len(desired) == 0 && len(existing) == 0 {
		return nil
	}
	desiredSet := make(map[string]struct{}, len(desired))
	result := make([]cnpgv1.ExtensionSpec, 0, len(desired))
	for _, name := range desired {
		desiredSet[name] = struct{}{}
		result = append(result, cnpgv1.ExtensionSpec{
			DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: name, Ensure: cnpgv1.EnsurePresent},
		})
	}
	for _, previous := range existing {
		if _, found := desiredSet[previous.Name]; found {
			continue
		}
		result = append(result, cnpgv1.ExtensionSpec{
			DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: previous.Name, Ensure: cnpgv1.EnsureAbsent},
		})
	}
	return result
}

func databaseMessage(database cnpgv1.Database) string {
	for _, extension := range database.Status.Extensions {
		if !extension.Applied && extension.Message != "" {
			return fmt.Sprintf("extension %q: %s", extension.Name, extension.Message)
		}
	}
	return database.Status.Message
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func boolPointer(value bool) *bool {
	return &value
}

func intPointer(value *int32) *int {
	if value == nil {
		return nil
	}
	converted := int(*value)
	return &converted
}
