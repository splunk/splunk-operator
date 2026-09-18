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

// This file interprets the PostgresCluster acknowledgement. Only an exact
// owner-and-role acknowledgement produces the success decision that unlocks
// the managed-role gate.

import (
	"fmt"

	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
)

// GateState is the database-side interpretation of the cluster acknowledgement.
type GateState string

const (
	GateProceed  GateState = "Proceed"
	GatePending  GateState = "Pending"
	GateConflict GateState = "Conflict"
	GateFailed   GateState = "Failed"
)

// GateDecision retains the policy result required by later status composition.
type GateDecision struct {
	State            GateState
	Message          string
	Role             string
	DatabaseMessages map[string]string
}

func evaluateAcknowledgement(
	databases []Database,
	owner dbtypes.ManagedRoleParticipant,
	acknowledgement dbtypes.ManagedRoleAcknowledgement,
) GateDecision {
	if !acknowledgement.Published {
		return GateDecision{State: GatePending, Message: "Waiting for cluster to publish managed role status"}
	}

	desiredRoles := orderedRoles(databases)
	desiredSet := make(map[string]struct{}, len(desiredRoles))
	for _, role := range desiredRoles {
		desiredSet[role] = struct{}{}
	}
	for _, conflict := range acknowledgement.Conflicts {
		if _, desired := desiredSet[conflict.Role]; desired && sameParticipant(conflict.AttemptedBy, owner) {
			return GateDecision{
				State: GateConflict, Message: fmt.Sprintf("role %s is already claimed", conflict.Role), Role: conflict.Role,
			}
		}
	}
	for _, role := range desiredRoles {
		if reason, failed := acknowledgement.Failed[role]; failed {
			return GateDecision{
				State: GateFailed, Message: fmt.Sprintf("role %s failed to reconcile: %s", role, reason), Role: role,
			}
		}
	}

	reconciled := make(map[string]struct{}, len(acknowledgement.Reconciled))
	for _, role := range acknowledgement.Reconciled {
		reconciled[role] = struct{}{}
	}
	for _, role := range desiredRoles {
		roleOwner, found := acknowledgement.Owners[role]
		if !found || !sameParticipant(roleOwner, owner) {
			return GateDecision{
				State: GatePending, Message: fmt.Sprintf("Waiting for role %s to be owned by this PostgresDatabase", role), Role: role,
			}
		}
		if _, found := reconciled[role]; !found {
			return GateDecision{
				State: GatePending, Message: fmt.Sprintf("Waiting for role %s to be reconciled", role), Role: role,
			}
		}
	}
	return GateDecision{State: GateProceed, Message: "Roles are reconciled and owned by this PostgresDatabase"}
}

func orderedRoles(databases []Database) []string {
	roles := make([]string, 0, roleCount(databases))
	for _, database := range databases {
		for _, role := range database.Roles {
			roles = append(roles, role.Name)
		}
	}
	return roles
}

func roleCount(databases []Database) int {
	count := 0
	for _, database := range databases {
		count += len(database.Roles)
	}
	return count
}

func sameParticipant(a, b dbtypes.ManagedRoleParticipant) bool {
	return a.Name == b.Name && a.UID == b.UID
}

func databaseMessages(databases []Database, decision GateDecision) map[string]string {
	blamed := databaseForRole(databases, decision.Role)
	messages := make(map[string]string, len(databases))
	for _, database := range databases {
		switch {
		case database.Name == blamed:
			messages[database.Name] = decision.Message
		case blamed != "":
			messages[database.Name] = fmt.Sprintf("blocked by role gate on database %q", blamed)
		default:
			messages[database.Name] = decision.Message
		}
	}
	return messages
}

func databaseForRole(databases []Database, roleName string) string {
	for _, database := range databases {
		for _, role := range database.Roles {
			if role.Name == roleName {
				return database.Name
			}
		}
	}
	return ""
}
