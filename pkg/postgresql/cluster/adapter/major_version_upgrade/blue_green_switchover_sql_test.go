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

	"reflect"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCutoverRejectProofRequiresAnIntentionalAccessDenial(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		denied bool
	}{
		{"HBA reject", &pgconn.PgError{Code: "28000", Message: "pg_hba.conf rejects connection for host ..."}, true},
		{"role NOLOGIN", fmt.Errorf("dial: %w", &pgconn.PgError{Code: "28000", Message: `role "orders_rw" is not permitted to log in`}), true},
		{"wrong password", &pgconn.PgError{Code: "28P01", Message: "password authentication failed"}, false},
		{"unknown database", &pgconn.PgError{Code: "3D000", Message: "database does not exist"}, false},
		{"network failure", errors.New("connection refused"), false},
		{"TLS failure", errors.New("x509: certificate signed by unknown authority"), false},
		{"unclassified authentication failure", &pgconn.PgError{Code: "28000", Message: "invalid authorization"}, false},
	} {
		t.Run(test.name, func(t *testing.T) { assert.Equal(t, test.denied, intentionalFenceDenial(test.err)) })
	}
}

func TestCutoverErrorProofRequiresEveryJoinedAttempt(t *testing.T) {
	unavailable := &pgconn.PgError{Code: "57P03", Message: "the database system is starting up"}
	denied := &pgconn.PgError{Code: "28000", Message: "pg_hba.conf rejects connection"}
	for _, proof := range []struct {
		name     string
		classify func(error) bool
		allowed  []error
		blocked  []error
	}{
		{
			name: "retired source unavailable", classify: retainedInstanceUnavailable,
			allowed: []error{
				unavailable,
				&pgconn.PgError{Code: "57P01"}, &pgconn.PgError{Code: "57P02"},
				&net.OpError{Op: "dial", Err: syscall.ECONNREFUSED},
				&net.DNSError{Err: "no such host", Name: "retired-source"},
				context.DeadlineExceeded, io.EOF, io.ErrUnexpectedEOF,
			},
			blocked: []error{denied, &net.DNSError{UnwrapErr: &pgconn.PgError{Code: "28P01"}}},
		},
		{
			name: "intentional login denial", classify: intentionalFenceDenial,
			allowed: []error{
				denied, &pgconn.PgError{Code: "28000", Message: `role "writer" is not permitted to log in`},
			},
			blocked: []error{unavailable, &pgconn.PgError{Code: "28000", Message: "unclassified authorization failure"}, syscall.ECONNREFUSED},
		},
	} {
		t.Run(proof.name, func(t *testing.T) {
			assert.False(t, proof.classify(nil))
			allAllowed := fmt.Errorf("all attempts: %w", errors.Join(proof.allowed...))
			assert.True(t, proof.classify(allAllowed), "every attempt has the expected failure")
			blocked := append(proof.blocked,
				&pgconn.PgError{Code: "28P01", Message: "password authentication failed"},
				context.Canceled, errors.New("unknown connection failure"),
				tls.AlertError(42), &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}},
				&net.OpError{Op: "remote error", Err: syscall.ECONNRESET},
				&net.OpError{Op: "local error", Err: proof.allowed[0]},
			)
			for index, contradiction := range blocked {
				for _, reverse := range []bool{false, true} {
					t.Run(fmt.Sprintf("contradiction %d reversed %t", index, reverse), func(t *testing.T) {
						attempts := []error{allAllowed, fmt.Errorf("failed attempt: %w", contradiction)}
						if reverse {
							attempts[0], attempts[1] = attempts[1], attempts[0]
						}
						joined := fmt.Errorf("connection: %w", errors.Join(fmt.Errorf("multi-host: %w", errors.Join(attempts...)), allAllowed))
						assert.False(t, proof.classify(joined), "one allowed attempt must not conceal a contradictory attempt")
					})
				}
			}
		})
	}
}

func TestFreshFenceLoginProbesInspectEveryAttempt(t *testing.T) {
	denied := &pgconn.PgError{Code: "28000", Message: "pg_hba.conf rejects connection"}
	for _, test := range []struct {
		name    string
		failure error
		ready   bool
	}{
		{"all intended denials", errors.Join(denied, fmt.Errorf("second host: %w", &pgconn.PgError{Code: "28000", Message: "role is not permitted to log in"})), true},
		{"denial then bad password", errors.Join(denied, &pgconn.PgError{Code: "28P01"}), false},
		{"bad password then denial", errors.Join(&pgconn.PgError{Code: "28P01"}, denied), false},
		{"denial then TLS failure", errors.Join(denied, &net.OpError{Op: "remote error", Err: tls.AlertError(42)}), false},
		{"TLS failure then denial", errors.Join(&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, denied), false},
		{"denial then unavailable", errors.Join(denied, &pgconn.PgError{Code: "57P03"}), false},
		{"unavailable then denial", errors.Join(&pgconn.PgError{Code: "57P03"}, denied), false},
		{"denial then unknown", errors.Join(denied, errors.New("unknown failure")), false},
		{"source disappears after control proof", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, false},
	} {
		for _, retired := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s retired %t", test.name, retired), func(t *testing.T) {
				sql, blue, status, id, _ := retainedSourceSQLFixture(t)
				blue.Status.InstancesStatus[cnpgv1.PodHealthy] = []string{"orders-blue-1", "orders-blue-2", "orders-blue-3"}
				blue.Status.ReadyInstances = 3
				connect := sql.connect
				var loginProbes int
				sql.connect = func(ctx context.Context, host, database, user, password, serverName string, ca []byte) (pginfra.Conn, error) {
					if user != id.Role {
						loginProbes++
						return nil, fmt.Errorf("fresh login: %w", test.failure)
					}
					return connect(ctx, host, database, user, password, serverName, ca)
				}
				var ready bool
				var err error
				if retired {
					ready, err = sql.VerifyRetainedSourceFence(t.Context(), blue, id, status)
				} else {
					ready, err = sql.VerifyFence(t.Context(), blue, id, roleNames(status.Switchover.OriginalRoleLogins))
				}
				assert.Positive(t, loginProbes)
				assert.Equal(t, test.ready, ready)
				if test.ready {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
	}
}

func TestCutoverControlBootstrapConstrainsPrivilegedHelpers(t *testing.T) {
	id := cutoverIdentity{Role: "sok_bgc_attempt", Password: "password", AttemptID: "attempt-1"}
	conn := &cutoverSQLTestConn{query: func(sql string, args []any) ([][]any, error) {
		if strings.Contains(sql, "SELECT pg_catalog.to_regprocedure") {
			return [][]any{{false}}, nil
		}
		if strings.Contains(sql, "SELECT ns.nspname,c.relname,c.relkind='S'") {
			return [][]any{{`strange"schema`, `seq';drop role postgres;`, true}}, nil
		}
		return [][]any{{true}}, nil
	}}
	boundary := &postgresSwitchoverSQL{}
	require.NoError(t, boundary.installControlDatabase(t.Context(), conn, `db"name`, id, true))
	assert.Equal(t, 3, conn.commits)
	assert.Zero(t, conn.rollbacks)
	all := strings.Join(conn.execs, "\n")
	assert.Contains(t, all, "SECURITY DEFINER SET search_path = pg_catalog")
	assert.Contains(t, all, "REVOKE ALL ON FUNCTION")
	assert.Contains(t, all, "FROM PUBLIC")
	assert.NotContains(t, all, "GRANT pg_signal_backend")
	assert.NotContains(t, all, "GRANT ALL")
	assert.Contains(t, all, `GRANT CONNECT ON DATABASE "db""name" TO "sok_bgc_attempt"`)
	assert.Contains(t, all, `GRANT SELECT, UPDATE ON SEQUENCE "strange""schema"."seq';drop role postgres;" TO "sok_bgc_attempt"`)
	assert.Contains(t, all, "GRANT SELECT (oid,subname,subenabled,subfailover,subslotname,subdbid)")
	assert.NotContains(t, all, "subconninfo")
	for _, helper := range cutoverHelpers(id) {
		assert.Contains(t, helper.body, "session_user <> E'sok_bgc_attempt'")
		assert.NotContains(t, helper.body, "EXECUTE $1")
	}
	assert.Contains(t, cutoverHelpers(id)[0].body, "client_addr IS NOT NULL OR usename <> 'postgres'")
	assert.Contains(t, cutoverHelpers(id)[0].body, "backend_type = 'client backend'")
	assert.Contains(t, cutoverHelpers(id)[2].body, "LIMIT 4")
}

func TestCutoverControlBootstrapRefusesToReplaceAnAlteredHelper(t *testing.T) {
	conn := &cutoverSQLTestConn{query: func(sql string, args []any) ([][]any, error) {
		if strings.Contains(sql, "SELECT pg_catalog.to_regprocedure") {
			return [][]any{{true}}, nil
		}
		return [][]any{{false}}, nil
	}}
	err := (&postgresSwitchoverSQL{}).installControlDatabase(t.Context(), conn, "orders", cutoverIdentity{Role: "control", AttemptID: "attempt-1"}, true)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.NotContains(t, strings.Join(conn.execs, "\n"), "CREATE FUNCTION")
	assert.NotContains(t, strings.Join(conn.execs, "\n"), "CREATE OR REPLACE")
}

func TestCutoverDrainRequeriesAfterTermination(t *testing.T) {
	id := cutoverIdentity{Role: "control", AttemptID: "attempt-1"}
	for _, remaining := range []bool{true, false} {
		t.Run(fmt.Sprintf("sessions remain %t", remaining), func(t *testing.T) {
			var terminated bool
			conn := &cutoverSQLTestConn{query: func(sql string, args []any) ([][]any, error) {
				if strings.Contains(sql, `SELECT "splunk_operator_bluegreen"."cutover_terminate_sessions"()`) {
					terminated = true
					return [][]any{{true}}, nil
				}
				if strings.Contains(sql, "SELECT NOT EXISTS (SELECT 1 FROM pg_catalog.pg_stat_activity WHERE") {
					assert.True(t, terminated, "termination acknowledgment must not be treated as proof of absence")
					assert.Contains(t, sql, "pg_prepared_xacts")
					return [][]any{{!remaining}}, nil
				}
				return [][]any{{true}}, nil
			}}
			drained, err := drainCutoverInstance(t.Context(), conn, id, true)
			require.NoError(t, err)
			assert.Equal(t, !remaining, drained)
		})
	}
}

func TestCutoverDrainReadOnlyPassDoesNotTerminateSessions(t *testing.T) {
	conn := &cutoverSQLTestConn{query: func(sql string, args []any) ([][]any, error) {
		assert.NotContains(t, sql, `SELECT "splunk_operator_bluegreen"."cutover_terminate_sessions"()`)
		return [][]any{{true}}, nil
	}}
	drained, err := drainCutoverInstance(t.Context(), conn, cutoverIdentity{Role: "control", AttemptID: "attempt-1"}, false)
	require.NoError(t, err)
	assert.True(t, drained)
	assert.Empty(t, conn.execs)
}

func TestCutoverSingleRowProofCannotHideATerminalSQLError(t *testing.T) {
	late := errors.New("SQL failed after delivering its row")
	conn := &preflightSQLTestConn{results: [][]any{{true}}, rowErrors: []error{late}}
	var proof bool
	require.ErrorIs(t, queryOne(t.Context(), conn, &proof, "SELECT fence_proof()"), late)
}

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

func retainedSourceSQLFixture(t *testing.T) (*postgresSwitchoverSQL, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus, cutoverIdentity, *[]string) {
	t.Helper()
	access, blue, green, databases, status, _ := accessFixture(t)
	require.NoError(t, access.Snapshot(t.Context(), blue, green, databases, status))
	require.NoError(t, access.PrepareControl(t.Context(), blue, green, databases, status))
	require.NoError(t, access.ApplyFence(t.Context(), blue, green, status))
	require.NoError(t, access.client.Get(t.Context(), client.ObjectKeyFromObject(blue), blue))
	blue.Spec.Instances = 3
	blue.Spec.SuperuserSecret = &cnpgv1.LocalObjectReference{Name: "blue-superuser"}
	blue.Spec.Managed.Roles[1].PasswordSecret = &cnpgv1.LocalObjectReference{Name: "writer-secret"}
	blue.Status.Certificates.ServerCASecret = "blue-ca"
	blue.Status.CurrentPrimary = "orders-blue-1"
	blue.Status.Phase = cnpgv1.PhaseHealthy
	blue.Status.Instances = 3
	blue.Status.InstancesStatus = map[cnpgv1.PodStatus][]string{}
	for index := 1; index <= 3; index++ {
		name := fmt.Sprintf("orders-blue-%d", index)
		pod := &corev1.Pod{Name: name, Namespace: blue.Namespace, UID: types.UID(name + "-uid"), Labels: map[string]string{"cnpg.io/cluster": blue.Name}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(blue, cnpgv1.SchemeGroupVersion.WithKind("Cluster"))},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: fmt.Sprintf("10.0.0.%d", index), Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
		require.NoError(t, access.client.Create(t.Context(), pod))
	}
	for _, secret := range []*corev1.Secret{{Name: "blue-superuser", Namespace: blue.Namespace, Data: map[string][]byte{"password": []byte("postgres-password")}}, {Name: "blue-ca", Namespace: blue.Namespace, Data: map[string][]byte{"ca.crt": []byte("test-ca")}}, {Name: "admin-secret", Namespace: blue.Namespace, Data: map[string][]byte{"password": []byte("admin-password")}}, {Name: "writer-secret", Namespace: blue.Namespace, Data: map[string][]byte{"password": []byte("writer-password")}}} {
		require.NoError(t, access.client.Create(t.Context(), secret))
	}
	now := metav1.Now()
	status.Switchover.FenceVerifiedAt = &now
	status.Switchover.DrainCompletedAt = &now
	status.Switchover.EndpointCommittedAt = &now
	status.Switchover.FinalBlueLSN = new("0/100")
	status.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CanonicalConfigMap: &corev1.ObjectReference{Name: "canonical", UID: "canonical-uid"}, Target: status.Green.Ref.DeepCopy()}
	id, err := access.Credentials(t.Context(), status)
	require.NoError(t, err)
	var calls []string
	sql, err := newPostgresSwitchoverSQL(access.client, access.key.Name, access.owner)
	require.NoError(t, err)
	sql.connect = func(_ context.Context, host, _ string, user, _, serverName string, _ []byte) (pginfra.Conn, error) {
		calls = append(calls, host)
		assert.Contains(t, []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}, host, "each proof must dial its observed pod, without a Service fallback")
		assert.Equal(t, "orders-blue-rw.default.svc.cluster.local", serverName, "the Service DNS identity remains the verified TLS name")
		if user != id.Role {
			return nil, &pgconn.PgError{Code: "28000", Message: "pg_hba.conf rejects connection"}
		}
		return &cutoverSQLTestConn{query: func(string, []any) ([][]any, error) { return [][]any{{true}}, nil }}, nil
	}
	return sql, blue, status, id, &calls
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

func TestCutoverSourceSequenceGrantsAreReadOnly(t *testing.T) {
	conn := &cutoverSQLTestConn{query: func(sql string, args []any) ([][]any, error) {
		if strings.Contains(sql, "SELECT pg_catalog.to_regprocedure") {
			return [][]any{{false}}, nil
		}
		if strings.Contains(sql, "SELECT ns.nspname,c.relname,c.relkind='S'") {
			return [][]any{{"public", "ids", true}}, nil
		}
		return [][]any{{true}}, nil
	}}
	require.NoError(t, (&postgresSwitchoverSQL{}).installControlDatabase(t.Context(), conn, "orders", cutoverIdentity{Role: "control", AttemptID: "attempt-1"}, false))
	sql := strings.Join(conn.execs, "\n")
	assert.Contains(t, sql, `GRANT SELECT ON SEQUENCE "public"."ids" TO "control"`)
	assert.NotContains(t, sql, "GRANT SELECT, UPDATE ON SEQUENCE")
}

type cutoverSQLTestConn struct {
	preflightSQLTestConn
	query    func(string, []any) ([][]any, error)
	rowError func(string, []any) error
}

func (c *cutoverSQLTestConn) Query(_ context.Context, sql string, args ...any) (pginfra.Rows, error) {
	c.queries = append(c.queries, sql)
	rows, err := c.query(sql, args)
	if err != nil {
		return nil, err
	}
	var terminal error
	if c.rowError != nil {
		terminal = c.rowError(sql, args)
	}
	return &cutoverSQLTestRows{values: rows, terminal: terminal}, nil
}

type cutoverSQLTestRows struct {
	values   [][]any
	index    int
	terminal error
}

func (r *cutoverSQLTestRows) Next() bool {
	if r.index >= len(r.values) {
		return false
	}
	r.index++
	return true
}
func (r *cutoverSQLTestRows) Scan(dest ...any) error {
	values := r.values[r.index-1]
	if len(values) != len(dest) {
		return fmt.Errorf("wrong column count")
	}
	for i, v := range values {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}
func (r *cutoverSQLTestRows) Close()     {}
func (r *cutoverSQLTestRows) Err() error { return r.terminal }
