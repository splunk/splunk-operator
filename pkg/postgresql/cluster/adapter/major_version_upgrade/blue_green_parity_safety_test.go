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
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type parityCatalogModel struct {
	roles, databases, unsupported []string
	schema, access                string
	guardReappeared, removed      bool
	blue, green                   cutoverSequence
	setvals                       int
}

func parityCatalogFixture(t *testing.T) (*postgresSwitchoverSQL, *cnpgv1.Cluster, *cnpgv1.Cluster, declaredDatabase, *platformv1alpha1.PostgresBlueGreenUpgradeStatus, cutoverIdentity, *parityCatalogModel) {
	t.Helper()
	sql, blue, status, id, _ := retainedSourceSQLFixture(t)
	green := new(cnpgv1.Cluster)
	require.NoError(t, sql.client.Get(t.Context(), client.ObjectKey{Namespace: blue.Namespace, Name: status.Green.Ref.Name}, green))
	green.Status.Certificates.ServerCASecret = "blue-ca"
	db := declaredDatabase{name: "orders", definition: platformv1alpha1.DatabaseDefinition{Name: "orders", AdminRoleName: "admin", RWRoleName: "writer"}}
	status.DatabaseReplication = []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{DatabaseName: db.name,
		SourceSchemaFingerprint: new("schema"), SourceAccessPolicyFingerprint: new("access"),
		TargetSchemaFingerprint: new("schema"), TargetAccessPolicyFingerprint: new("access"),
	}}
	status.Switchover.CandidateGuardsRemovedAt = status.Switchover.FenceVerifiedAt.DeepCopy()
	model := &parityCatalogModel{roles: []string{"admin", "writer", id.Role}, databases: []string{db.name}, schema: "schema", access: "access",
		blue: cutoverSequence{schema: "public", name: "ids", value: 77, called: false}, green: cutoverSequence{schema: "public", name: "ids", value: 3, called: true}}
	sql.connect = func(_ context.Context, host, _, role, _, _ string, _ []byte) (pginfra.Conn, error) {
		require.Equal(t, id.Role, role)
		side := sideGreen
		if strings.Contains(host, blue.Name) {
			side = sideBlue
		}
		return &cutoverSQLTestConn{query: func(query string, args []any) ([][]any, error) {
			rows := func(values []string) [][]any {
				var result [][]any
				for _, value := range values {
					result = append(result, []any{value})
				}
				return result
			}
			switch {
			case query == sourceRolesSQL:
				return rows(model.roles), nil
			case query == sourceDatabasesSQL:
				return rows(model.databases), nil
			case strings.Contains(query, "SELECT d.datname||'/'"):
				if side == sideBlue {
					return nil, nil
				}
				return [][]any{{db.name + "/" + subscriptionName(sql.logicalName, db.name)}}, nil
			case strings.Contains(query, "SELECT 'tablespace:'"):
				return rows(model.unsupported), nil
			case query == schemaFingerprintSQL:
				return [][]any{{model.schema}}, nil
			case query == cutoverAccessFingerprintSQL(side):
				return [][]any{{model.access}}, nil
			case query == controlRoleSQL || query == controlHelperProofSQL || query == controlSchemaProofSQL || query == controlGrantInventorySQL,
				strings.Contains(query, "pg_catalog.pg_has_role"), strings.Contains(query, "pg_catalog.aclexplode(c.relacl)"):
				return [][]any{{true}}, nil
			case strings.Contains(query, "to_regprocedure($1) IS NOT NULL"):
				return [][]any{{false}}, nil
			case strings.Contains(query, "SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_trigger"):
				return [][]any{{model.guardReappeared}}, nil
			case strings.Contains(query, pginfra.QuoteIdentifier(controlRemoveGuardsFunction)+"()"):
				model.removed = true
				return [][]any{{true}}, nil
			case query == unsupportedDatabaseObjectsSQL:
				return [][]any{{false}}, nil
			case strings.Contains(query, "SELECT format('%I.%I', n.nspname, c.relname)"):
				return nil, nil
			case strings.Contains(query, "SELECT extname FROM pg_extension"):
				return [][]any{{"plpgsql"}}, nil
			case strings.Contains(query, "SELECT 'foreign-server:'"):
				return nil, nil
			case query == cutoverPublicationInventorySQL, strings.Contains(query, "SELECT count(*)=1 AND bool_and(subname="):
				return [][]any{{true}}, nil
			case strings.Contains(query, "SELECT n.nspname,c.relname"):
				return [][]any{{"public", "ids"}}, nil
			case strings.Contains(query, "SELECT last_value,is_called"):
				sequence := model.green
				if side == sideBlue {
					sequence = model.blue
				}
				return [][]any{{sequence.value, sequence.called}}, nil
			case strings.Contains(query, "pg_catalog.setval"):
				require.Equal(t, sideGreen, side, "sequence synchronization must never write blue")
				model.green.value, model.green.called = args[1].(int64), args[2].(bool)
				model.setvals++
				return [][]any{{model.green.value}}, nil
			default:
				return nil, fmt.Errorf("unexpected parity query: %s", query)
			}
		}}, nil
	}
	return sql, blue, green, db, status, id, model
}

func TestFrozenCandidateCatalogRejectsDriftBeforeCommit(t *testing.T) {
	for _, scenario := range []string{"unchanged", "role", "database", "tablespace", "schema", "access", "missing receipt"} {
		t.Run(scenario, func(t *testing.T) {
			sql, _, target, db, status, id, model := parityCatalogFixture(t)
			switch scenario {
			case "role":
				model.roles = append(model.roles, "unmanaged-login")
			case "database":
				model.databases = append(model.databases, "unmanaged-database")
			case "tablespace":
				model.unsupported = []string{"tablespace:external"}
			case "schema":
				model.schema = "changed-schema"
			case "access":
				model.access = "changed-access"
			case "missing receipt":
				status.DatabaseReplication = nil
			}
			err := sql.verifyFrozenCatalogs(t.Context(), target, []declaredDatabase{db}, status, id, sideGreen)
			if scenario == "unchanged" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
			}
		})
	}
}

func TestSequenceAdaptersSynchronizeGreenAndKeepParityReadOnly(t *testing.T) {
	sql, source, target, db, _, id, model := parityCatalogFixture(t)
	ready, err := sql.VerifySequenceParity(t.Context(), source, target, []declaredDatabase{db}, id)
	require.False(t, ready)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	require.NoError(t, sql.SynchronizeSequences(t.Context(), source, target, []declaredDatabase{db}, id))
	require.Equal(t, model.blue, model.green)
	require.Equal(t, 1, model.setvals)
	ready, err = sql.VerifySequenceParity(t.Context(), source, target, []declaredDatabase{db}, id)
	require.NoError(t, err)
	require.True(t, ready)
	model.green.called = !model.blue.called
	ready, err = sql.VerifySequenceParity(t.Context(), source, target, []declaredDatabase{db}, id)
	require.False(t, ready)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	require.Equal(t, 1, model.setvals, "parity checks must not repair drift or advance a sequence")
}

func TestGuardRemovalRequiresVerifiedAbsence(t *testing.T) {
	for _, reappeared := range []bool{false, true} {
		t.Run(fmt.Sprint(reappeared), func(t *testing.T) {
			sql, _, target, db, _, id, model := parityCatalogFixture(t)
			model.guardReappeared = reappeared
			err := sql.RemoveGuards(t.Context(), target, []declaredDatabase{db}, id)
			require.True(t, model.removed)
			if reappeared {
				require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
