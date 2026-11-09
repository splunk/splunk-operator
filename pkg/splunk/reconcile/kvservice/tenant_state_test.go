// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kvservice

import (
	"errors"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/stretchr/testify/require"
)

func TestEvaluateTenantOutcomes(t *testing.T) {
	unavailable := errors.New("dependency unavailable")
	invalid := tenantTerminal("Correct the tenant configuration", unavailable)
	complete := resolvedTenantDependencies{dependencies: &k8sops.KVServicePostgresDependencies{}, info: &platformApi.DatabaseInfo{}}
	for _, tc := range []struct {
		name         string
		db           databaseResult
		auth         error
		resolved     resolvedTenantDependencies
		reason       enterpriseApi.KVServiceTenantReason
		retry, ready bool
	}{
		{"database pending", databaseResult{}, nil, complete, enterpriseApi.KVServiceTenantDatabasePending, true, false},
		{"database API failure", databaseResult{err: unavailable}, nil, complete, enterpriseApi.KVServiceTenantDatabasePending, true, false},
		{"database conflict", databaseResult{err: invalid}, nil, complete, enterpriseApi.KVServiceTenantDatabaseConflict, false, false},
		{"authentication pending", databaseResult{ready: true}, unavailable, complete, enterpriseApi.KVServiceTenantAuthenticationPending, true, false},
		{"authentication invalid", databaseResult{ready: true}, invalid, complete, enterpriseApi.KVServiceTenantAuthenticationInvalid, false, false},
		{"missing connection references", databaseResult{ready: true}, nil, resolvedTenantDependencies{}, enterpriseApi.KVServiceTenantCredentialsPending, true, false},
		{"credential read failure", databaseResult{ready: true}, nil, resolvedTenantDependencies{err: unavailable}, enterpriseApi.KVServiceTenantCredentialsPending, true, false},
		{"ready", databaseResult{ready: true}, nil, complete, enterpriseApi.KVServiceTenantReady, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateTenant("alpha", "tenant1", tc.db, tc.auth, tc.resolved)
			require.Equal(t, tc.reason, got.status.Reason)
			require.Equal(t, tc.retry, got.retry)
			require.Equal(t, tc.ready, got.status.Ready)
			require.Equal(t, tc.ready, got.database != nil)
			require.Equal(t, "alpha-database", got.status.DatabaseName)
			if tc.auth == invalid || tc.db.err == invalid {
				require.Equal(t, "Correct the tenant configuration", got.status.Message)
				_, terminal := splcommon.TerminalMessage(got.diagnostic)
				require.True(t, terminal)
			}
		})
	}
}
