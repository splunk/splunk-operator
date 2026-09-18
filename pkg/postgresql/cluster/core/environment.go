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

package core

import (
	"context"
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	identitytypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/identity"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func conventionalClusterCard(cluster *platformv1alpha1.PostgresCluster) identitytypes.ClusterCard {
	logical := identitytypes.ObjectIdentity{
		APIVersion: platformv1alpha1.GroupVersion.String(),
		Kind:       "PostgresCluster",
		Name:       cluster.Name,
		Namespace:  cluster.Namespace,
		UID:        cluster.UID,
	}
	authoritative := identitytypes.Environment{
		Identity: identitytypes.ObjectIdentity{
			APIVersion: cnpgv1.SchemeGroupVersion.String(),
			Kind:       "Cluster",
			Name:       cluster.Name,
			Namespace:  cluster.Namespace,
		},
		Role:  identitytypes.EnvironmentRoleAuthoritative,
		Scope: identitytypes.NamingScopeConventional,
	}
	return identitytypes.ClusterCard{
		Logical:       logical,
		Authoritative: authoritative,
		Managed:       []identitytypes.Environment{authoritative},
	}
}

// resolveAuthoritativeCNPGCluster reads the CNPG Cluster selected by the
// resolved identity card. It permits creating only the conventional
// environment; a missing nonconventional authoritative environment is an
// error so reconciliation cannot recreate the logical-name Cluster.
func resolveAuthoritativeCNPGCluster(
	ctx context.Context,
	c client.Client,
	cluster *platformv1alpha1.PostgresCluster,
	card identitytypes.ClusterCard,
) (*cnpgv1.Cluster, bool, error) {
	if cluster == nil {
		return nil, false, fmt.Errorf("postgres cluster is required")
	}
	authoritative := card.Authoritative
	if authoritative.Identity.Name == "" {
		authoritative = conventionalClusterCard(cluster).Authoritative
	}

	conventional := authoritative.Identity.Name == cluster.Name &&
		authoritative.Identity.Namespace == cluster.Namespace
	environment := &cnpgv1.Cluster{}
	if err := c.Get(ctx, types.NamespacedName{
		Name: authoritative.Identity.Name, Namespace: authoritative.Identity.Namespace,
	}, environment); err != nil {
		return nil, conventional, err
	}
	if expectedUID := authoritative.Identity.UID; expectedUID != "" && environment.UID != expectedUID {
		return nil, false, fmt.Errorf("resolved CNPG Cluster UID %q does not match %q UID %q", expectedUID, authoritative.Identity.Name, environment.UID)
	}
	return environment, conventional, nil
}

// validateAuthoritativeCNPGEnvironment verifies the UID recorded in the
// identity card before ordinary reconciliation mutates dependent resources.
// A missing UID is intentionally allowed because a conventional environment
// may not have been created yet.
func validateAuthoritativeCNPGEnvironment(ctx context.Context, c client.Reader, card identitytypes.ClusterCard) error {
	authoritative := card.Authoritative.Identity
	if authoritative.UID == "" {
		return nil
	}
	environment := &cnpgv1.Cluster{}
	if err := c.Get(ctx, types.NamespacedName{Name: authoritative.Name, Namespace: authoritative.Namespace}, environment); err != nil {
		return fmt.Errorf("getting authoritative CNPG Cluster %q: %w", authoritative.Name, err)
	}
	if environment.UID != authoritative.UID {
		return fmt.Errorf("resolved CNPG Cluster UID %q does not match %q UID %q", authoritative.UID, authoritative.Name, environment.UID)
	}
	return nil
}

// managedCNPGClusters gets card-managed environments that still exist and are
// controlled by the logical PostgresCluster. Missing historical environments
// are expected during finalizer cleanup.
func managedCNPGClusters(ctx context.Context, c client.Client, cluster *platformv1alpha1.PostgresCluster, card identitytypes.ClusterCard) ([]*cnpgv1.Cluster, error) {
	result := make([]*cnpgv1.Cluster, 0, len(card.Managed))
	for _, identity := range card.Managed {
		if identity.Identity.Name == "" {
			continue
		}
		environment := &cnpgv1.Cluster{}
		if err := c.Get(ctx, types.NamespacedName{Name: identity.Identity.Name, Namespace: identity.Identity.Namespace}, environment); err != nil {
			if client.IgnoreNotFound(err) == nil {
				continue
			}
			return nil, fmt.Errorf("getting CNPG cluster %q: %w", identity.Identity.Name, err)
		}
		if !isManagedCNPGCluster(cluster, identity, environment) {
			continue
		}
		result = append(result, environment)
	}
	return result, nil
}

func isManagedCNPGCluster(cluster *platformv1alpha1.PostgresCluster, identity identitytypes.Environment, environment *cnpgv1.Cluster) bool {
	if environment == nil {
		return false
	}
	for _, owner := range environment.GetOwnerReferences() {
		if owner.Controller != nil && *owner.Controller && owner.UID == cluster.UID {
			if expectedUID := identity.Identity.UID; expectedUID == "" || environment.UID == expectedUID {
				return true
			}
			return false
		}
	}
	return false
}
