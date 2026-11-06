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
	"syscall"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/jackc/pgx/v5/pgconn"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestCutoverSingleRowProofCannotHideATerminalSQLError(t *testing.T) {
	late := errors.New("SQL failed after delivering its row")
	conn := &preflightSQLTestConn{results: [][]any{{true}}, rowErrors: []error{late}}
	var proof bool
	require.ErrorIs(t, queryOne(t.Context(), conn, &proof, "SELECT fence_proof()"), late)
}
