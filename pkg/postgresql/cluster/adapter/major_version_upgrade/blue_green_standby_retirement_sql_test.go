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
	"errors"
	"testing"

	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStandbySlotRetirementCapability(t *testing.T) {
	for _, test := range []struct {
		name                           string
		standby, present, local, valid bool
		removed, called                bool
		message                        string
	}{
		{name: "primary pending"},
		{name: "legacy synchronized slot", standby: true},
		{name: "legacy local slot", standby: true, local: true, message: "prepared without the standby slot retirement helper"},
		{name: "helper drift", standby: true, present: true, message: "ownership, body, or privileges changed"},
		{name: "local removal pending", standby: true, present: true, valid: true, called: true},
		{name: "already absent", standby: true, present: true, valid: true, called: true, removed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			id := cutoverIdentity{Role: "control", AttemptID: "attempt", LogicalName: "orders"}
			called := false
			conn := &cutoverSQLTestConn{query: func(query string, args []any) ([][]any, error) {
				value := false
				switch query {
				case `SELECT pg_catalog.pg_is_in_recovery()`:
					value = test.standby
				case controlGuardFunctionExistsSQL:
					value = test.present
				case controlHelperProofSQL:
					assert.Equal(t, retireStandbySlotHelper(id, "db").body, args[2])
					assert.Equal(t, id.Role, args[3])
					value = test.valid
				case controlHelperCallSQL(controlRetireStandbySlotFunction):
					called, value = true, test.removed
				default:
					assert.Equal(t, []any{replicationSlotName(id.LogicalName, "db")}, args)
					value = test.local
				}
				return [][]any{{value}}, nil
			}}
			removed, err := retireStandbySlot(t.Context(), conn, id, "db")
			assert.Equal(t, test.removed, removed)
			assert.Equal(t, test.called, called)
			if test.message != "" {
				require.ErrorContains(t, err, test.message)
				if test.name == "helper drift" {
					assert.ErrorIs(t, err, mvutypes.ErrCandidateInvalid)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestStandbySlotRetirementPreservesQueryErrors(t *testing.T) {
	failure := errors.New("test query error")
	for _, failAt := range []string{`SELECT pg_catalog.pg_is_in_recovery()`, controlGuardFunctionExistsSQL, controlHelperProofSQL, controlHelperCallSQL(controlRetireStandbySlotFunction)} {
		t.Run(failAt, func(t *testing.T) {
			conn := &cutoverSQLTestConn{query: func(query string, _ []any) ([][]any, error) {
				if query == failAt {
					return nil, failure
				}
				return [][]any{{true}}, nil
			}}
			removed, err := retireStandbySlot(t.Context(), conn, cutoverIdentity{Role: "control", AttemptID: "attempt", LogicalName: "orders"}, "db")
			assert.False(t, removed)
			assert.ErrorIs(t, err, failure)
		})
	}
}
