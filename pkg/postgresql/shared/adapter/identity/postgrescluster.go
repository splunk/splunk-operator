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

package identity

import (
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	identitytypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/identity"
	corev1 "k8s.io/api/core/v1"
)

// ClusterInputFromPostgresCluster translates one PostgresCluster status
// snapshot into provider-neutral identity facts. It is the sole location that
// interprets the current CNPG and blue/green status representation.
func ClusterInputFromPostgresCluster(cluster *platformv1alpha1.PostgresCluster) (identitytypes.ClusterInput, error) {
	if cluster == nil {
		return identitytypes.ClusterInput{}, fmt.Errorf("postgres cluster is required")
	}

	input := identitytypes.ClusterInput{
		Logical: identitytypes.ObjectIdentity{
			APIVersion: platformv1alpha1.GroupVersion.String(),
			Kind:       "PostgresCluster",
			Name:       cluster.Name,
			Namespace:  cluster.Namespace,
			UID:        cluster.UID,
		},
	}

	authoritative, err := authoritativeEnvironment(cluster)
	if err != nil {
		return identitytypes.ClusterInput{}, err
	}
	input.Environments = append(input.Environments, authoritative)

	for _, upgrade := range cluster.Status.PostgresMajorUpgradeStatus {
		if upgrade.BlueGreen == nil {
			continue
		}
		for _, environment := range []struct {
			status *platformv1alpha1.BlueGreenEnvironmentStatus
			role   identitytypes.EnvironmentRole
		}{
			{status: upgrade.BlueGreen.Blue, role: historicalEnvironmentRole(upgrade.BlueGreen.Cleanup)},
			{status: upgrade.BlueGreen.Green, role: identitytypes.EnvironmentRoleCandidate},
		} {
			if environment.status == nil {
				continue
			}
			translated, err := environmentFromReference(cluster, environment.status.Ref, environment.role)
			if err != nil {
				return identitytypes.ClusterInput{}, err
			}
			if sameEnvironment(translated, authoritative) {
				if translated.Identity.UID != "" &&
					authoritative.Identity.UID != "" &&
					translated.Identity.UID != authoritative.Identity.UID {
					return identitytypes.ClusterInput{}, fmt.Errorf("conflicting provider environment UIDs for %s %s/%s", translated.Identity.Kind, translated.Identity.Namespace, translated.Identity.Name)
				}
				continue
			}
			input.Environments = append(input.Environments, translated)
		}
	}

	return input, nil
}

func authoritativeEnvironment(cluster *platformv1alpha1.PostgresCluster) (identitytypes.Environment, error) {
	if cluster.Status.ProvisionerRef == nil {
		return identitytypes.Environment{
			Identity: identitytypes.ObjectIdentity{
				APIVersion: cnpgv1.SchemeGroupVersion.String(),
				Kind:       "Cluster",
				Name:       cluster.Name,
				Namespace:  cluster.Namespace,
			},
			Role:  identitytypes.EnvironmentRoleAuthoritative,
			Scope: identitytypes.NamingScopeConventional,
		}, nil
	}
	return environmentFromReference(cluster, *cluster.Status.ProvisionerRef, identitytypes.EnvironmentRoleAuthoritative)
}

func environmentFromReference(
	cluster *platformv1alpha1.PostgresCluster,
	ref corev1.ObjectReference,
	role identitytypes.EnvironmentRole,
) (identitytypes.Environment, error) {
	if ref.Name == "" {
		return identitytypes.Environment{}, fmt.Errorf("provider environment name is required")
	}
	if ref.APIVersion != "" && ref.APIVersion != cnpgv1.SchemeGroupVersion.String() {
		return identitytypes.Environment{}, fmt.Errorf("unsupported provider environment apiVersion %q", ref.APIVersion)
	}
	if ref.Kind != "" && ref.Kind != "Cluster" {
		return identitytypes.Environment{}, fmt.Errorf("unsupported provider environment kind %q", ref.Kind)
	}
	namespace := ref.Namespace
	if namespace == "" {
		namespace = cluster.Namespace
	}
	if namespace != cluster.Namespace {
		return identitytypes.Environment{}, fmt.Errorf("provider environment namespace %q does not match PostgresCluster namespace %q", namespace, cluster.Namespace)
	}

	// Preserve the established resource-name contract: a provider Cluster with
	// a nonconventional name receives a distinct generated-resource scope. The
	// card makes that decision explicit for consumers instead of requiring them
	// to compare names themselves.
	scope := identitytypes.NamingScopeConventional
	if ref.Name != cluster.Name {
		scope = identitytypes.NamingScopeEnvironment
	}
	return identitytypes.Environment{
		Identity: identitytypes.ObjectIdentity{
			APIVersion: cnpgv1.SchemeGroupVersion.String(),
			Kind:       "Cluster",
			Name:       ref.Name,
			Namespace:  namespace,
			UID:        ref.UID,
		},
		Role:  role,
		Scope: scope,
	}, nil
}

func historicalEnvironmentRole(cleanup *platformv1alpha1.BlueGreenCleanupStatus) identitytypes.EnvironmentRole {
	if cleanup != nil &&
		(cleanup.State == platformv1alpha1.BlueGreenCleanupStateCleaning ||
			cleanup.State == platformv1alpha1.BlueGreenCleanupStateCleaned) {
		return identitytypes.EnvironmentRoleRetirable
	}
	return identitytypes.EnvironmentRoleRetained
}

func sameEnvironment(left, right identitytypes.Environment) bool {
	return left.Identity.APIVersion == right.Identity.APIVersion &&
		left.Identity.Kind == right.Identity.Kind &&
		left.Identity.Namespace == right.Identity.Namespace &&
		left.Identity.Name == right.Identity.Name
}
