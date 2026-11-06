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
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
	"syscall"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/jackc/pgx/v5/pgconn"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestRetainedSourceDoesNotRequireItsFormerTopology(t *testing.T) {
	for _, test := range []struct {
		name    string
		healthy []string
	}{
		{"lost source replica", []string{"orders-blue-1", "orders-blue-2"}},
		{"lost old primary", []string{"orders-blue-2"}},
		{"source fully offline", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			sql, blue, status, id, calls := retainedSourceSQLFixture(t)
			blue.Status.InstancesStatus[cnpgv1.PodHealthy] = test.healthy
			blue.Status.ReadyInstances = len(test.healthy)
			for index := 1; index <= 3; index++ {
				name := fmt.Sprintf("orders-blue-%d", index)
				if !slices.Contains(test.healthy, name) {
					pod := &corev1.Pod{Name: name, Namespace: blue.Namespace}
					require.NoError(t, sql.client.Delete(t.Context(), pod))
				}
			}
			strict, err := sql.VerifyFence(t.Context(), blue, id, roleNames(status.Switchover.OriginalRoleLogins))
			require.NoError(t, err)
			assert.False(t, strict)
			strict, err = sql.Drain(t.Context(), blue, id, true)
			require.NoError(t, err)
			assert.False(t, strict)
			assert.Empty(t, *calls, "precommit verification must retain the full topology prerequisite")
			retained, err := sql.VerifyRetainedSourceFence(t.Context(), blue, id, status)
			require.NoError(t, err)
			assert.True(t, retained)
			retained, err = sql.DrainRetainedSource(t.Context(), blue, id, status)
			require.NoError(t, err)
			assert.True(t, retained)
			for _, host := range *calls {
				present := false
				for _, name := range test.healthy {
					pod := new(corev1.Pod)
					require.NoError(t, sql.client.Get(t.Context(), client.ObjectKey{Namespace: blue.Namespace, Name: name}, pod))
					if host == pod.Status.PodIP {
						present = true
					}
				}
				assert.True(t, present, "retired checks must not wait for a missing source instance")
			}
			if len(test.healthy) > 0 {
				assert.NotEmpty(t, *calls)
			}
		})
	}
}

func TestRetainedSourceVerifiesObservedUnreadyPodsAtTheirIPs(t *testing.T) {
	sql, blue, status, id, calls := retainedSourceSQLFixture(t)
	blue.Status.InstancesStatus[cnpgv1.PodHealthy] = nil
	pod := new(corev1.Pod)
	require.NoError(t, sql.client.Get(t.Context(), client.ObjectKey{Namespace: blue.Namespace, Name: "orders-blue-1"}, pod))
	pod.Status.Phase = corev1.PodPending
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse}}
	require.NoError(t, sql.client.Status().Update(t.Context(), pod))
	ready, err := sql.VerifyRetainedSourceFence(t.Context(), blue, id, status)
	require.NoError(t, err)
	assert.True(t, ready)
	assert.NotEmpty(t, *calls, "an observed retired source may still accept SQL while its pod is unready")
	assert.Contains(t, *calls, pod.Status.PodIP, "a stale empty healthy list cannot omit an addressable owned instance")
}

func TestRetainedSourceSkipsTransportLossButRejectsContradictoryAccess(t *testing.T) {
	for _, test := range []struct {
		name    string
		failure error
		ready   bool
	}{
		{"transport unavailable", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, true},
		{"old instance still starting", &pgconn.PgError{Code: "57P03", Message: "the database system is starting up"}, true},
		{"source control credential rejected", &pgconn.PgError{Code: "28P01", Message: "password authentication failed"}, false},
		{"source TLS identity changed", &net.OpError{Op: "remote error", Net: "tcp", Err: &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}}, false},
		{"wrapped remote TLS alert", fmt.Errorf("dialing old source: %w", &net.OpError{Op: "dial", Net: "tcp", Err: &net.OpError{Op: "remote error", Net: "tcp", Err: tls.AlertError(42)}}), false},
		{"remote TLS operation wrapping a transport error", &net.OpError{Op: "remote error", Net: "tcp", Err: syscall.ECONNRESET}, false},
		{"all unavailable attempts", fmt.Errorf("connecting: %w", errors.Join(&pgconn.PgError{Code: "57P03"}, &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED})), true},
		{"starting then bad password", errors.Join(&pgconn.PgError{Code: "57P03"}, &pgconn.PgError{Code: "28P01"}), false},
		{"bad password then starting", errors.Join(&pgconn.PgError{Code: "28P01"}, &pgconn.PgError{Code: "57P03"}), false},
		{"starting then TLS failure", errors.Join(&pgconn.PgError{Code: "57P03"}, &net.OpError{Op: "remote error", Err: tls.AlertError(42)}), false},
		{"TLS failure then starting", fmt.Errorf("connecting: %w", errors.Join(&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, &pgconn.PgError{Code: "57P03"})), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			sql, blue, status, id, _ := retainedSourceSQLFixture(t)
			blue.Status.InstancesStatus[cnpgv1.PodHealthy] = []string{"orders-blue-1"}
			sql.connect = func(context.Context, string, string, string, string, string, []byte) (pginfra.Conn, error) {
				return nil, test.failure
			}
			ready, err := sql.VerifyRetainedSourceFence(t.Context(), blue, id, status)
			assert.Equal(t, test.ready, ready)
			if test.ready {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			ready, err = sql.DrainRetainedSource(t.Context(), blue, id, status)
			assert.Equal(t, test.ready, ready)
			if test.ready {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestRetainedSourceRequiresCompleteProofAfterAConnectionSucceeds(t *testing.T) {
	for _, stage := range []struct {
		name  string
		match func(string) bool
		drain bool
	}{
		{"control role", func(sql string) bool { return sql == controlRoleSQL }, false},
		{"control membership", func(sql string) bool { return strings.Contains(sql, "pg_catalog.pg_auth_members m JOIN") }, false},
		{"control helper", func(sql string) bool { return sql == controlHelperProofSQL }, false},
		{"control schema", func(sql string) bool { return sql == controlSchemaProofSQL }, false},
		{"control grants", func(sql string) bool { return sql == controlGrantInventorySQL }, false},
		{"loaded fence", func(sql string) bool {
			return strings.Contains(sql, `SELECT "splunk_operator_bluegreen"."cutover_fence_file_valid"()`)
		}, false},
		{"role login fence", func(sql string) bool { return strings.Contains(sql, "SELECT count(*)=cardinality") }, false},
		{"drain control role", func(sql string) bool { return sql == controlRoleSQL }, true},
		{"drain termination", func(sql string) bool {
			return strings.Contains(sql, `SELECT "splunk_operator_bluegreen"."cutover_terminate_sessions"()`)
		}, true},
		{"drain absence", func(sql string) bool {
			return strings.Contains(sql, "SELECT NOT EXISTS (SELECT 1 FROM pg_catalog.pg_stat_activity WHERE")
		}, true},
	} {
		for _, row := range []struct {
			name   string
			values [][]any
		}{
			{"false then EOF", [][]any{{false}}},
			{"true then EOF", [][]any{{true}}},
			{"no row then EOF", nil},
		} {
			t.Run(stage.name+"/"+row.name, func(t *testing.T) {
				sql, blue, status, id, _ := retainedSourceSQLFixture(t)
				blue.Status.InstancesStatus[cnpgv1.PodHealthy] = []string{"orders-blue-1"}
				var matched bool
				sql.connect = func(context.Context, string, string, string, string, string, []byte) (pginfra.Conn, error) {
					return &cutoverSQLTestConn{
						query: func(query string, _ []any) ([][]any, error) {
							if stage.match(query) {
								matched = true
								return row.values, nil
							}
							return [][]any{{true}}, nil
						},
						rowError: func(query string, _ []any) error {
							if stage.match(query) {
								return io.EOF
							}
							return nil
						},
					}, nil
				}
				var ready bool
				var err error
				if stage.drain {
					ready, err = sql.DrainRetainedSource(t.Context(), blue, id, status)
				} else {
					ready, err = sql.VerifyRetainedSourceFence(t.Context(), blue, id, status)
				}
				require.True(t, matched, "the intended proof stage must be exercised")
				assert.False(t, ready, "a reachable source must finish its entire proof in this pass")
				if row.name == "false then EOF" {
					require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
				} else {
					require.ErrorIs(t, err, io.EOF)
				}
			})
		}
	}
}

func TestOfflineRetainedSourceRequiresExactDurableFenceProof(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus)
	}{
		{"missing commit", func(_ *cnpgv1.Cluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.CommitReceipt = nil }},
		{"missing drain proof", func(_ *cnpgv1.Cluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Switchover.DrainCompletedAt = nil
		}},
		{"source replaced", func(blue *cnpgv1.Cluster, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { blue.UID = "replaced" }},
		{"source role overlay changed", func(blue *cnpgv1.Cluster, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			blue.Spec.Managed.Roles[0].Login = true
		}},
		{"source HBA overlay changed", func(blue *cnpgv1.Cluster, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			blue.Spec.PostgresConfiguration.PgHBA = nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			sql, blue, status, id, calls := retainedSourceSQLFixture(t)
			blue.Status.InstancesStatus[cnpgv1.PodHealthy] = nil
			test.mutate(blue, status)
			ready, err := sql.VerifyRetainedSourceFence(t.Context(), blue, id, status)
			require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
			assert.False(t, ready)
			ready, err = sql.DrainRetainedSource(t.Context(), blue, id, status)
			require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
			assert.False(t, ready)
			assert.Empty(t, *calls)
		})
	}
}
