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
	"context"
	"errors"
	"strings"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestTargetArchiveReadinessPinsSystemAndPreservesConnectionErrors(t *testing.T) {
	for _, mode := range []string{"ready", "pending restart", "unknown identity", "query failure", "close failure"} {
		t.Run(mode, func(t *testing.T) {
			green := &cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{SystemID: "target-system"}}
			if mode == "unknown identity" {
				green.Status.SystemID = ""
			}
			failure := errors.New("test connection failure")
			conn := &closingCutoverConn{cutoverSQLTestConn: cutoverSQLTestConn{query: func(query string, args []any) ([][]any, error) {
				assert.Contains(t, query, "archive_mode")
				assert.Contains(t, query, "archive_command")
				assert.Contains(t, query, "pg_control_system()")
				assert.Equal(t, []any{"target-system"}, args)
				if mode == "query failure" {
					return nil, failure
				}
				return [][]any{{mode != "pending restart"}}, nil
			}}}
			if mode == "close failure" {
				conn.closeErr = failure
			}
			dials := 0
			sql := &postgresCompletionSQL{teardown: &postgresTeardownSQL{connect: func(_ context.Context, cluster *cnpgv1.Cluster, host, database string) (pginfra.Conn, error) {
				dials++
				assert.Same(t, green, cluster)
				assert.Equal(t, "10.42.0.18", host)
				assert.Equal(t, "postgres", database)
				return conn, nil
			}}}
			ready, err := sql.TargetArchivingReady(t.Context(), green, "10.42.0.18")
			if mode == "query failure" || mode == "close failure" {
				require.ErrorIs(t, err, failure)
			} else {
				require.NoError(t, err)
			}
			if mode == "unknown identity" {
				assert.False(t, ready)
				assert.Zero(t, dials)
			} else {
				assert.Equal(t, mode != "pending restart" && mode != "query failure", ready)
				assert.Equal(t, 1, conn.closeCalls)
			}
		})
	}
}

func TestRetirementHelperUsesSharedReplicationNames(t *testing.T) {
	for _, cluster := range []string{"orders", strings.Repeat("long-name", 9)} {
		for _, database := range []string{"orders", `odd'"database`} {
			t.Run(cluster+"/"+database, func(t *testing.T) {
				id := cutoverIdentity{Role: "control", AttemptID: "attempt", LogicalName: cluster}
				helper := retireReplicationHelper(id, database)
				assert.Contains(t, helper.body, "DECLARE pub text := "+quoteSQLStringLiteral(publicationName(cluster, database)))
				assert.Contains(t, helper.body, "; slot text := "+quoteSQLStringLiteral(replicationSlotName(cluster, database)))
				assert.NotContains(t, helper.body, "sha256")
				assert.NotContains(t, helper.body, "substr")
				assert.NotEqual(t, helper.body, retireReplicationHelper(id, database+"-other").body)
			})
		}
	}
}

func TestRecoveryVerifierAcceptsMinorQualifiedTarget(t *testing.T) {
	for _, test := range []struct {
		target   string
		major    int
		verified bool
	}{
		{"18", 18, true}, {"18.1", 18, true}, {"17.9", 17, false},
		{"invalid", 0, false}, {"", 0, false}, {"0", 0, false},
		{"999999999999999999999999", 0, false},
	} {
		t.Run(test.target, func(t *testing.T) {
			var queried bool
			conn := &cutoverSQLTestConn{query: func(_ string, args []any) ([][]any, error) {
				queried = true
				assert.Equal(t, test.major, args[0])
				assert.Equal(t, "green-system", args[1])
				return [][]any{{test.major == 18}}, nil
			}}
			sql := &postgresCompletionSQL{teardown: &postgresTeardownSQL{connect: func(_ context.Context, _ *cnpgv1.Cluster, _, database string) (pginfra.Conn, error) {
				assert.Equal(t, "postgres", database)
				return conn, nil
			}}}
			now := metav1.Now()
			backup := &cnpgv1.Backup{Status: cnpgv1.BackupStatus{StartedAt: &now, StoppedAt: &now, EndWal: "000000010000000000000001"}}
			green := &cnpgv1.Cluster{Name: "green", Namespace: "default", Status: cnpgv1.ClusterStatus{SystemID: "green-system"}}
			verified, err := sql.VerifyRecovery(t.Context(), green, backup, test.target)
			assert.Equal(t, test.verified, verified)
			assert.Equal(t, test.major > 0, queried)
			if test.major > 0 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestSourceReplicationCleanupVisitsPrimaryDespitePendingStandbys(t *testing.T) {
	for _, test := range []struct {
		name, recordedPrimary, primaryIP string
		blocked, firstDone               bool
	}{
		{"primary first", "orders-blue-1", "10.0.0.1", false, true},
		{"primary second", "orders-blue-2", "10.0.0.2", false, false},
		{"primary last", "orders-blue-3", "10.0.0.3", false, false},
		{"stale primary status", "orders-blue-1", "10.0.0.2", false, false},
		{"unknown primary status", "", "10.0.0.3", false, false},
		{"active primary slot", "orders-blue-2", "10.0.0.2", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			sql, blue, status, _, _ := retainedSourceSQLFixture(t)
			blue.Status.CurrentPrimary = test.recordedPrimary
			status.DatabaseReplication = []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{DatabaseName: "orders"}, {DatabaseName: "audit"}}
			slots := map[string]bool{"orders": true, "audit": true}
			var primaryCalls int
			sql.connect = func(_ context.Context, host, database, _, _, _ string, _ []byte) (pginfra.Conn, error) {
				return &cutoverSQLTestConn{query: func(query string, _ []any) ([][]any, error) {
					if query == controlHelperCallSQL(controlRetireStandbySlotFunction) {
						return [][]any{{false}}, nil
					}
					if query == controlHelperCallSQL(controlRetireReplicationFunction) {
						if host == test.primaryIP {
							primaryCalls++
							if !test.blocked {
								slots[database] = false
							}
						}
						return [][]any{{!slots[database]}}, nil
					}
					return [][]any{{true}}, nil
				}}, nil
			}
			access, err := newBlueGreenAccess(sql.client, sql.client, client.ObjectKeyFromObject(sql.owner), sql.owner, sql)
			require.NoError(t, err)
			completion := &postgresCompletionSQL{switchover: sql, access: access}
			for pass, want := range []bool{test.firstDone, !test.blocked, !test.blocked} {
				done, err := completion.RemoveSourceReplication(t.Context(), blue, status)
				require.NoError(t, err)
				assert.Equal(t, want, done)
				assert.Equal(t, (pass+1)*len(slots), primaryCalls, "every database must reach the actual primary")
			}
		})
	}
}
