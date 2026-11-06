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
	"fmt"
	"strings"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestVerifyOpenRetainsPrimaryWriteProofWhenDialingPodIPs(t *testing.T) {
	for _, primaryWritable := range []bool{true, false} {
		t.Run(fmt.Sprintf("primary writable %t", primaryWritable), func(t *testing.T) {
			sql, blue, status, id, _ := retainedSourceSQLFixture(t)
			database := &platformv1alpha1.PostgresDatabase{Name: "orders-db", Namespace: blue.Namespace, Spec: platformv1alpha1.PostgresDatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{Name: sql.logicalName},
				Databases:  []platformv1alpha1.DatabaseDefinition{{Name: "orders", AdminRoleName: "admin", RWRoleName: "writer"}},
			}}
			require.NoError(t, sql.client.Create(t.Context(), database))
			blue.Status.InstancesStatus[cnpgv1.PodHealthy] = []string{"orders-blue-1", "orders-blue-2", "orders-blue-3"}
			blue.Status.CurrentPrimary = "orders-blue-2"
			blue.Status.ReadyInstances = 3
			connections := make(map[string]*cutoverSQLTestConn)
			sql.connect = func(_ context.Context, host, database, user, _, serverName string, _ []byte) (pginfra.Conn, error) {
				assert.Equal(t, "orders-blue-rw.default.svc.cluster.local", serverName)
				if user == "admin" {
					assert.Equal(t, "orders", database, "custom application roles must reconnect to their declared database")
				} else {
					assert.Equal(t, "postgres", database)
				}
				conn := &cutoverSQLTestConn{query: func(query string, _ []any) ([][]any, error) {
					if strings.Contains(query, "current_setting('transaction_read_only')") {
						return [][]any{{host == "10.0.0.2" && primaryWritable}}, nil
					}
					return [][]any{{true}}, nil
				}}
				connections[host+"/"+user] = conn
				return conn, nil
			}
			ready, err := sql.VerifyOpen(t.Context(), blue, id, status.Switchover.OriginalRoleLogins)
			require.NoError(t, err)
			assert.Equal(t, primaryWritable, ready, "the observed primary must pass the writable check")
			if primaryWritable {
				require.Contains(t, connections, "10.0.0.2/postgres")
				assert.Equal(t, 1, connections["10.0.0.2/postgres"].rollbacks, "the primary must execute and roll back a real write probe")
				assert.Zero(t, connections["10.0.0.1/postgres"].rollbacks, "replicas cannot substitute for the primary write probe")
				assert.Zero(t, connections["10.0.0.3/postgres"].rollbacks)
			}
		})
	}
}

func TestCutoverWriteProbeRollsBackAndVerifiesItsRow(t *testing.T) {
	for _, observed := range []bool{true, false} {
		t.Run(fmt.Sprintf("row visible %t", observed), func(t *testing.T) {
			conn := &cutoverSQLTestConn{query: func(sql string, args []any) ([][]any, error) { return [][]any{{observed}}, nil }}
			err := proveCutoverWritable(t.Context(), conn)
			if observed {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
			}
			assert.Equal(t, 1, conn.rollbacks)
			assert.Zero(t, conn.commits)
			assert.Equal(t, []string{"SET TRANSACTION READ WRITE", "CREATE TEMP TABLE pg_temp.splunk_operator_cutover_probe (value boolean NOT NULL) ON COMMIT DROP", "INSERT INTO pg_temp.splunk_operator_cutover_probe (value) VALUES (true)"}, conn.execs)
		})
	}
}
