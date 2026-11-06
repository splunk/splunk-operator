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

	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/require"
)

func TestParityOperationsPreserveConnectionErrors(t *testing.T) {
	for _, operation := range []string{"source LSN", "maintenance catalog", "source sequence synchronization", "target sequence synchronization", "source sequence proof", "target sequence proof", "guard removal"} {
		for _, failQuery := range []bool{false, true} {
			name := operation + " close only"
			if failQuery {
				name = operation + " operation and close"
			}
			t.Run(name, func(t *testing.T) {
				sql, blue, status, id, _ := retainedSourceSQLFixture(t)
				queryErr, closeErr := errors.New("operation failed"), errors.New("close failed")
				var connections []*closingCutoverConn
				sql.connect = func(context.Context, string, string, string, string, string, []byte) (pginfra.Conn, error) {
					targeted := !strings.HasPrefix(operation, "target") || len(connections) == 1
					conn := &closingCutoverConn{cutoverSQLTestConn: cutoverSQLTestConn{query: func(query string, _ []any) ([][]any, error) {
						if targeted && failQuery {
							return nil, queryErr
						}
						switch query {
						case cutoverSourceFinalWALPositionSQL:
							return [][]any{{"0/100"}}, nil
						case sourceRolesSQL, sourceDatabasesSQL, cutoverSubscriptionRosterSQL, cutoverUnsupportedMaintenanceObjectsSQL, cutoverSequenceInventorySQL:
							return nil, nil
						case controlGuardFunctionExistsSQL, cutoverGuardsRemainSQL:
							return [][]any{{false}}, nil
						default:
							return [][]any{{true}}, nil
						}
					}}}
					if targeted {
						conn.closeErr = closeErr
					}
					connections = append(connections, conn)
					return conn, nil
				}
				var err error
				switch operation {
				case "source LSN":
					var lsn string
					lsn, err = sql.SourceLSN(t.Context(), blue, id)
					require.Empty(t, lsn, "a failed connection lifecycle must not publish a receipt")
				case "maintenance catalog":
					err = sql.verifyFrozenCatalogs(t.Context(), blue, nil, status, id, sideBlue)
				case "source sequence synchronization", "target sequence synchronization":
					err = sql.SynchronizeSequences(t.Context(), blue, blue, []declaredDatabase{{name: "orders"}}, id)
				case "source sequence proof", "target sequence proof":
					var ready bool
					ready, err = sql.VerifySequenceParity(t.Context(), blue, blue, []declaredDatabase{{name: "orders"}}, id)
					require.False(t, ready)
				case "guard removal":
					err = sql.RemoveGuards(t.Context(), blue, []declaredDatabase{{name: "orders"}}, id)
				}
				require.ErrorIs(t, err, closeErr)
				if failQuery {
					require.ErrorIs(t, err, queryErr)
				}
				for _, conn := range connections {
					require.Equal(t, 1, conn.closeCalls)
				}
				if strings.HasPrefix(operation, "target") {
					require.Len(t, connections, 2)
				} else {
					require.Len(t, connections, 1)
				}
			})
		}
	}
}

func TestFrozenDatabaseCatalogPreservesConnectionErrors(t *testing.T) {
	for _, failQuery := range []bool{false, true} {
		t.Run(map[bool]string{false: "close only", true: "operation and close"}[failQuery], func(t *testing.T) {
			sql, _, green, db, status, id, _ := parityCatalogFixture(t)
			queryErr, closeErr := errors.New("database proof failed"), errors.New("database close failed")
			dial := sql.connect
			var databaseConnection *closingCutoverConn
			sql.connect = func(ctx context.Context, host, database, user, password, serverName string, ca []byte) (pginfra.Conn, error) {
				conn, err := dial(ctx, host, database, user, password, serverName, ca)
				if err != nil || database == "postgres" {
					return conn, err
				}
				databaseConnection = &closingCutoverConn{cutoverSQLTestConn: *conn.(*cutoverSQLTestConn), closeErr: closeErr}
				if failQuery {
					databaseConnection.query = func(string, []any) ([][]any, error) { return nil, queryErr }
				}
				return databaseConnection, nil
			}
			err := sql.verifyFrozenCatalogs(t.Context(), green, []declaredDatabase{db}, status, id, sideGreen)
			require.ErrorIs(t, err, closeErr)
			if failQuery {
				require.ErrorIs(t, err, queryErr)
			}
			require.NotNil(t, databaseConnection)
			require.Equal(t, 1, databaseConnection.closeCalls)
		})
	}
}
