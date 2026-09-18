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
	"fmt"

	identitytypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/identity"
)

type finalizationAction string

const (
	finalizationActionDelete finalizationAction = "Delete"
	finalizationActionRetain finalizationAction = "Retain"
)

// finalizationEnvironmentAction states the deletion-policy operation for one
// managed provider environment. The executor validates ownership and observed
// UID before it mutates the provider Cluster.
type finalizationEnvironmentAction struct {
	Environment identitytypes.Environment
	Action      finalizationAction
}

// finalizationPlan turns a PostgresCluster deletion policy and resolved card
// into one deterministic action for each managed environment. Every managed
// lifecycle role remains in scope on logical-cluster deletion; a missing,
// foreign, or UID-rebound object is handled by the executor rather than by
// inferring permission from its name.
func finalizationPlan(policy string, card identitytypes.ClusterCard) ([]finalizationEnvironmentAction, error) {
	var action finalizationAction
	switch policy {
	case clusterDeletionPolicyDelete:
		action = finalizationActionDelete
	case clusterDeletionPolicyRetain:
		action = finalizationActionRetain
	default:
		return nil, fmt.Errorf("unknown ClusterDeletionPolicy %q: must be %q or %q", policy, clusterDeletionPolicyDelete, clusterDeletionPolicyRetain)
	}

	planned := make([]finalizationEnvironmentAction, 0, len(card.Managed))
	seen := make(map[string]struct{}, len(card.Managed))
	for _, environment := range card.Managed {
		identity := environment.Identity
		if identity.Name == "" || identity.Namespace == "" {
			continue
		}
		key := identity.APIVersion + "/" + identity.Kind + "/" + identity.Namespace + "/" + identity.Name
		if _, found := seen[key]; found {
			continue
		}
		seen[key] = struct{}{}
		planned = append(planned, finalizationEnvironmentAction{
			Environment: environment,
			Action:      action,
		})
	}
	return planned, nil
}
