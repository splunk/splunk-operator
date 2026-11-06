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
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type closingCutoverConn struct {
	cutoverSQLTestConn
	closeCalls int
	closeErr   error
}

func (c *closingCutoverConn) Close(context.Context) error { c.closeCalls++; return c.closeErr }

func TestCutoverOperationsPreserveOperationAndCloseErrors(t *testing.T) {
	for _, operation := range []string{"fence", "drain", "open"} {
		t.Run(operation, func(t *testing.T) {
			sql, blue, _, id, _ := retainedSourceSQLFixture(t)
			blue.Status.InstancesStatus[cnpgv1.PodHealthy] = []string{"orders-blue-1", "orders-blue-2", "orders-blue-3"}
			blue.Status.ReadyInstances = 3
			operationErr := errors.New("SQL proof failed")
			closeErr := errors.New("connection close failed")
			conn := &closingCutoverConn{cutoverSQLTestConn: cutoverSQLTestConn{query: func(string, []any) ([][]any, error) { return nil, operationErr }}, closeErr: closeErr}
			sql.connect = func(context.Context, string, string, string, string, string, []byte) (pginfra.Conn, error) {
				return conn, nil
			}
			var ready bool
			var err error
			switch operation {
			case "fence":
				ready, err = sql.VerifyFence(t.Context(), blue, id, nil)
			case "drain":
				ready, err = sql.Drain(t.Context(), blue, id, true)
			case "open":
				ready, err = sql.VerifyOpen(t.Context(), blue, id, nil)
			}
			require.False(t, ready)
			require.ErrorIs(t, err, operationErr)
			require.ErrorIs(t, err, closeErr)
			require.Equal(t, 1, conn.closeCalls)
		})
	}
}

func TestCutoverConnectionLifecyclePreservesCloseOnlyFailure(t *testing.T) {
	closeErr := errors.New("close failed")
	conn := &closingCutoverConn{closeErr: closeErr}
	err := withCutoverConn(t.Context(), conn, func(pginfra.Conn) error { return nil })
	require.ErrorIs(t, err, closeErr)
	require.Equal(t, 1, conn.closeCalls)
}

func TestCutoverDialFailureDoesNotRunOperation(t *testing.T) {
	sql, blue, _, id, _ := retainedSourceSQLFixture(t)
	dialErr := errors.New("connection unavailable")
	sql.connect = func(context.Context, string, string, string, string, string, []byte) (pginfra.Conn, error) {
		return nil, dialErr
	}
	called := false
	err := sql.withConn(t.Context(), blue, "10.0.0.1", "postgres", id, func(pginfra.Conn) error { called = true; return nil })
	require.ErrorIs(t, err, dialErr)
	require.False(t, called)
}

func TestAccessConstructorsRejectStaticWiringDefects(t *testing.T) {
	access, _, _, _, _, sql := accessFixture(t)
	for _, test := range []struct {
		name   string
		writer client.Client
		reader client.Reader
		owner  *platformv1alpha1.PostgresCluster
		sql    accessSQL
	}{
		{"writer", nil, access.reader, access.owner, sql},
		{"reader", access.client, nil, access.owner, sql},
		{"owner", access.client, access.reader, nil, sql},
		{"SQL", access.client, access.reader, access.owner, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			component, err := newBlueGreenAccess(test.writer, test.reader, access.key, test.owner, test.sql)
			require.Nil(t, component)
			require.Error(t, err)
			require.NotErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed, "composition errors are not observed upgrade failures")
		})
	}
	mismatch := *access.owner
	mismatch.Name = "another-parent"
	component, err := newBlueGreenAccess(access.client, access.reader, access.key, &mismatch, sql)
	require.Nil(t, component)
	require.Error(t, err)
	for _, test := range []struct {
		name   string
		client client.Client
		owner  *platformv1alpha1.PostgresCluster
	}{
		{"client", nil, access.owner}, {"owner", access.client, nil}, {"identity", access.client, &mismatch},
	} {
		t.Run("SQL "+test.name, func(t *testing.T) {
			component, err := newPostgresSwitchoverSQL(test.client, access.key.Name, test.owner)
			require.Nil(t, component)
			require.Error(t, err)
			require.NotErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
		})
	}
}
