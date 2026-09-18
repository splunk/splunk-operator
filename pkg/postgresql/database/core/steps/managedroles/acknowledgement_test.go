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

import (
	"testing"

	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	"github.com/stretchr/testify/assert"
)

func TestEvaluateAcknowledgementStateTable(t *testing.T) {
	owner := dbtypes.ManagedRoleParticipant{Name: "tenant", UID: "database-uid"}
	databases := managedRoleDatabases()
	exactOwners := map[string]dbtypes.ManagedRoleParticipant{
		"payments_admin": owner,
		"payments_rw":    owner,
		"audit_admin":    owner,
		"audit_rw":       owner,
	}

	tests := []struct {
		name string
		ack  dbtypes.ManagedRoleAcknowledgement
		want GateDecision
	}{
		{
			name: "unpublished",
			want: GateDecision{State: GatePending, Message: "Waiting for cluster to publish managed role status"},
		},
		{
			name: "exact owner and reconciliation",
			ack: dbtypes.ManagedRoleAcknowledgement{
				Published: true, Reconciled: []string{"payments_admin", "payments_rw", "audit_admin", "audit_rw"}, Owners: exactOwners,
			},
			want: GateDecision{State: GateProceed, Message: "Roles are reconciled and owned by this PostgresDatabase"},
		},
		{
			name: "recreated UID remains pending",
			ack: dbtypes.ManagedRoleAcknowledgement{
				Published:  true,
				Reconciled: []string{"payments_admin", "payments_rw", "audit_admin", "audit_rw"},
				Owners: map[string]dbtypes.ManagedRoleParticipant{
					"payments_admin": {Name: owner.Name, UID: "old-uid"},
					"payments_rw":    owner,
					"audit_admin":    owner,
					"audit_rw":       owner,
				},
			},
			want: GateDecision{State: GatePending, Role: "payments_admin", Message: "Waiting for role payments_admin to be owned by this PostgresDatabase"},
		},
		{
			name: "relevant conflict takes precedence",
			ack: dbtypes.ManagedRoleAcknowledgement{
				Published: true,
				Failed:    map[string]string{"payments_admin": "provider rejected role"},
				Conflicts: []dbtypes.ManagedRoleConflict{
					{Role: "unrelated", AttemptedBy: owner},
					{Role: "audit_rw", AttemptedBy: owner},
				},
			},
			want: GateDecision{State: GateConflict, Role: "audit_rw", Message: "role audit_rw is already claimed"},
		},
		{
			name: "cluster conflict order is deterministic",
			ack: dbtypes.ManagedRoleAcknowledgement{
				Published: true,
				Conflicts: []dbtypes.ManagedRoleConflict{
					{Role: "audit_rw", AttemptedBy: owner},
					{Role: "payments_admin", AttemptedBy: owner},
				},
			},
			want: GateDecision{State: GateConflict, Role: "audit_rw", Message: "role audit_rw is already claimed"},
		},
		{
			name: "stale and unrelated conflicts are ignored",
			ack: dbtypes.ManagedRoleAcknowledgement{
				Published:  true,
				Reconciled: []string{"payments_admin", "payments_rw", "audit_admin", "audit_rw"},
				Owners:     exactOwners,
				Conflicts: []dbtypes.ManagedRoleConflict{
					{Role: "payments_admin", AttemptedBy: dbtypes.ManagedRoleParticipant{Name: owner.Name, UID: "old-uid"}},
					{Role: "unrelated", AttemptedBy: owner},
				},
			},
			want: GateDecision{State: GateProceed, Message: "Roles are reconciled and owned by this PostgresDatabase"},
		},
		{
			name: "unrelated failure is ignored",
			ack: dbtypes.ManagedRoleAcknowledgement{
				Published:  true,
				Reconciled: []string{"payments_admin", "payments_rw", "audit_admin", "audit_rw"},
				Owners:     exactOwners,
				Failed:     map[string]string{"unrelated": "provider rejected role"},
			},
			want: GateDecision{State: GateProceed, Message: "Roles are reconciled and owned by this PostgresDatabase"},
		},
		{
			name: "first desired failed role is deterministic",
			ack: dbtypes.ManagedRoleAcknowledgement{
				Published: true,
				Failed: map[string]string{
					"audit_admin":    "audit failure",
					"payments_admin": "payments failure",
				},
			},
			want: GateDecision{State: GateFailed, Role: "payments_admin", Message: "role payments_admin failed to reconcile: payments failure"},
		},
		{
			name: "owned role still applying",
			ack: dbtypes.ManagedRoleAcknowledgement{
				Published: true, Owners: exactOwners,
			},
			want: GateDecision{State: GatePending, Role: "payments_admin", Message: "Waiting for role payments_admin to be reconciled"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, evaluateAcknowledgement(databases, owner, tt.ack))
		})
	}
}
