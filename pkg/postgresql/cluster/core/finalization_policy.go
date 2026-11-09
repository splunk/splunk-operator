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
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"

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

// Retain must not orphan live replication or discard the credentials needed to
// restore a fenced source. Full teardown is a later lifecycle; keep ownership
// intact until its durable completion evidence exists.
func requireSafeRetainFinalization(cluster *platformv1alpha1.PostgresCluster) error {
	for _, entry := range cluster.Status.PostgresMajorUpgradeStatus {
		bg := entry.BlueGreen
		if bg == nil || mvutypes.IsBlueGreenAttemptCleaned(entry) {
			continue
		}
		if bg.Blue != nil || bg.Green != nil || bg.SourcePreparation != nil || bg.SourceConfigurationFingerprint != nil || len(bg.DatabaseReplication) > 0 || bg.Switchover != nil || bg.CommitReceipt != nil {
			return fmt.Errorf("blueGreen attempt %q requires verified replication teardown and source restoration before Retain finalization; keep the parent and recovery credentials until cleanup completes", bg.AttemptID)
		}
	}
	return nil
}
