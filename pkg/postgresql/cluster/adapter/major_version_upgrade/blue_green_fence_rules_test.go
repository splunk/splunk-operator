/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFenceProofUsesTheExactAppliedOverlayInOrder(t *testing.T) {
	_, _, _, _, status, _ := accessFixture(t)
	control := cutoverRoleName(status.AttemptID)
	replication := replicationRoleName(status.AttemptID)
	applied := fencedHBA(status, []string{"local all all peer"})
	require.Equal(t, []string{
		"hostssl all " + control + " all scram-sha-256",
		"hostssl all " + replication + " all scram-sha-256",
		"hostssl replication " + replication + " all scram-sha-256",
		"host all all all reject",
		"local all all peer",
	}, applied)
	expected := make([]string, 0, len(applied)-1)
	for _, line := range applied[:len(applied)-1] {
		expected = append(expected, quoteSQLStringLiteral(strings.Join(strings.Fields(line), ":")))
	}
	helper := cutoverHelpers(cutoverIdentity{Role: control, AttemptID: status.AttemptID})[2]
	require.Contains(t, helper.body, "= ARRAY["+strings.Join(expected, ",")+"];")
}

func TestFenceProofQuotesRenderedRulesWithoutChangingTheirFields(t *testing.T) {
	rule := hbaRule{"hostssl", "orders", "role'quoted", "10.0.0.0/8", "scram-sha-256"}
	require.Equal(t, "hostssl orders role'quoted 10.0.0.0/8 scram-sha-256", rule.configLine())
	require.Equal(t, "hostssl:orders:role'quoted:10.0.0.0/8:scram-sha-256", rule.loadedLine())
	require.Equal(t, "ARRAY["+quoteSQLStringLiteral("hostssl:orders:role'quoted:10.0.0.0/8:scram-sha-256")+"]", fenceRuleProofSQL([]hbaRule{rule}))
}
