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
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestTargetDurabilityRequiresEveryReplicaAndStableInstanceIdentities(t *testing.T) {
	for _, scenario := range []string{"durable", "primary not flushed", "replica lag", "replica query error", "primary changed", "pod replaced", "pod address changed", "unhealthy topology"} {
		t.Run(scenario, func(t *testing.T) {
			sql, target, _, id, _ := retainedSourceSQLFixture(t)
			target.Status.ReadyInstances = 3
			target.Status.InstancesStatus[cnpgv1.PodHealthy] = []string{"orders-blue-1", "orders-blue-2", "orders-blue-3"}
			require.NoError(t, sql.client.Update(t.Context(), target))
			var hosts []string
			sql.connect = func(_ context.Context, host, database, role, _, _ string, _ []byte) (pginfra.Conn, error) {
				hosts = append(hosts, host)
				assert.Equal(t, "postgres", database)
				assert.Equal(t, id.Role, role, "the barrier must use the private control identity")
				return &cutoverSQLTestConn{query: func(query string, args []any) ([][]any, error) {
					if strings.Contains(query, "pg_current_wal_insert_lsn") {
						assert.Equal(t, "10.0.0.1", host)
						return [][]any{{"0/ABC"}}, nil
					}
					assert.Equal(t, []any{"0/ABC"}, args, "every instance must prove the same final WAL position")
					if host == "10.0.0.1" {
						assert.Contains(t, query, "pg_current_wal_flush_lsn")
						assert.Contains(t, query, "NOT pg_is_in_recovery()")
					} else {
						assert.Contains(t, query, "pg_last_wal_receive_lsn")
						assert.Contains(t, query, "pg_last_wal_replay_lsn")
						assert.Contains(t, query, "pg_is_in_recovery()")
					}
					if scenario == "primary not flushed" && host == "10.0.0.1" || scenario == "replica lag" && host == "10.0.0.3" {
						return [][]any{{false}}, nil
					}
					if scenario == "replica query error" && host == "10.0.0.3" {
						return nil, errors.New("replica disconnected")
					}
					if host == "10.0.0.3" {
						switch scenario {
						case "primary changed", "unhealthy topology":
							fresh := new(cnpgv1.Cluster)
							require.NoError(t, sql.client.Get(t.Context(), client.ObjectKeyFromObject(target), fresh))
							fresh.Status.CurrentPrimary = "orders-blue-2"
							if scenario == "unhealthy topology" {
								fresh.Status.ReadyInstances = 2
							}
							require.NoError(t, sql.client.Update(t.Context(), fresh))
						case "pod replaced", "pod address changed":
							pod := new(corev1.Pod)
							require.NoError(t, sql.client.Get(t.Context(), client.ObjectKey{Namespace: target.Namespace, Name: "orders-blue-3"}, pod))
							if scenario == "pod replaced" {
								pod.UID = "replacement"
							} else {
								pod.Status.PodIP = "10.0.1.3"
								require.NoError(t, sql.client.Status().Update(t.Context(), pod))
							}
							require.NoError(t, sql.client.Update(t.Context(), pod))
						}
					}
					return [][]any{{true}}, nil
				}}, nil
			}
			durable, err := sql.VerifyTargetDurability(t.Context(), target, id)
			if scenario == "replica query error" {
				require.ErrorContains(t, err, "replica disconnected")
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, scenario == "durable", durable)
			if scenario == "durable" {
				assert.Equal(t, []string{"10.0.0.1", "10.0.0.1", "10.0.0.2", "10.0.0.3"}, hosts)
			}
		})
	}
}
