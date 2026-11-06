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
	"fmt"
	"strings"
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCutoverFinalParityRequiresDurableLSNBeforeSQL(t *testing.T) {
	boundary := &postgresSwitchoverSQL{}
	ready, err := boundary.VerifyFinalParity(t.Context(), nil, nil, nil, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{}}, cutoverIdentity{})
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.False(t, ready)
}

func TestCutoverFingerprintNormalizesOnlyRecordedOverlay(t *testing.T) {
	sql := cutoverAccessFingerprintSQL(sideBlue)
	assert.Contains(t, sql, "original_logins")
	assert.Contains(t, sql, `COALESCE((SELECT login FROM original_logins WHERE "roleName"=rolname),rolcanlogin)`)
	assert.Contains(t, sql, "member.rolname<>$2 AND role.rolname<>$2")
	assert.Contains(t, sql, "c.relkind='S' AND acl.privilege_type IN ('SELECT','UPDATE') AND NOT acl.is_grantable")
	assert.Contains(t, sql, "role.rolname, member.rolname, membership.admin_option, membership.inherit_option, membership.set_option")
	assert.Contains(t, sql, "pg_default_acl")
	assert.Contains(t, sql, "attempt-role-drift")
	assert.Contains(t, controlGrantInventorySQL, "c.relpersistence='u'")
	assert.Contains(t, controlGrantInventorySQL, "a.is_grantable")
	assert.Contains(t, sql, sourcePublisherRoleDrift)
	assert.NotContains(t, sql, "attempt-role-missing:")
	green := cutoverAccessFingerprintSQL(sideGreen)
	assert.Contains(t, green, greenPublisherRoleDrift)
	assert.Contains(t, green, "attempt-role-missing:")
	assert.Contains(t, green, `COALESCE((SELECT login FROM original_logins WHERE "roleName"=rolname),rolcanlogin)`)
	assert.Contains(t, green, "role.rolname, member.rolname, membership.admin_option, membership.inherit_option, membership.set_option")
}

func TestCutoverGuardRemovalReceiptRequiresContinuedAbsence(t *testing.T) {
	now := metav1.Now()
	for _, test := range []struct {
		name    string
		status  platformv1alpha1.BlueGreenSwitchoverStatus
		present bool
		fail    bool
	}{
		{"completed removal remains absent", platformv1alpha1.BlueGreenSwitchoverStatus{CandidateGuardsRemovedAt: &now}, false, false},
		{"completed removal was recreated", platformv1alpha1.BlueGreenSwitchoverStatus{CandidateGuardsRemovedAt: &now}, true, true},
		{"intent permits lost completion status", platformv1alpha1.BlueGreenSwitchoverStatus{CandidateGuardsRemovalStartedAt: &now}, false, false},
		{"sequence receipt cannot authorize removal", platformv1alpha1.BlueGreenSwitchoverStatus{SequenceSynchronization: &platformv1alpha1.BlueGreenSequenceSynchronizationStatus{VerifiedAt: &now}}, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := &preflightSQLTestConn{results: [][]any{{test.present}}}
			err := verifyCutoverGuardState(t.Context(), conn, &test.status, sideGreen)
			if test.fail {
				require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCutoverSubscriptionRosterIncludesMaintenanceDatabase(t *testing.T) {
	for _, test := range []struct {
		name   string
		side   environmentSide
		actual []any
		fail   bool
	}{
		{"blue has no subscriptions", sideBlue, nil, false},
		{"blue unexpected subscription", sideBlue, []any{"postgres/rogue"}, true},
		{"green expected subscriptions", sideGreen, []any{"orders/" + subscriptionName("orders", "orders")}, false},
		{"green extra maintenance subscription", sideGreen, []any{"orders/" + subscriptionName("orders", "orders"), "postgres/rogue"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := &preflightSQLTestConn{results: [][]any{test.actual}}
			err := verifyCutoverSubscriptionRoster(t.Context(), conn, []declaredDatabase{{name: "orders"}}, "orders", test.side)
			if test.fail {
				require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCutoverGreenSubscriptionUsesSeparateSQLAndSlotNames(t *testing.T) {
	logicalName, database := "orders-with-hyphens", "orders"
	checked := false
	conn := &cutoverSQLTestConn{query: func(sql string, args []any) ([][]any, error) {
		switch {
		case strings.Contains(sql, "pg_largeobject_metadata"):
			return [][]any{{false}}, nil
		case strings.Contains(sql, "SELECT format('%I.%I', n.nspname, c.relname)"):
			return nil, nil
		case strings.Contains(sql, "SELECT extname FROM pg_extension"):
			return [][]any{{"plpgsql"}}, nil
		case strings.Contains(sql, "SELECT 'foreign-server:'"):
			return nil, nil
		case strings.Contains(sql, "FROM pg_catalog.pg_subscription"):
			checked = true
			assert.Contains(t, sql, "subname=$1")
			assert.Contains(t, sql, "subslotname=$2")
			assert.Equal(t, []any{subscriptionName(logicalName, database), replicationSlotName(logicalName, database)}, args)
			assert.NotEqual(t, args[0], args[1], "quoted subscription names and PostgreSQL slot names have different identifier rules")
			return [][]any{{true}}, nil
		case sql == cutoverPublicationInventorySQL:
			assert.Equal(t, []any{publicationName(logicalName, database)}, args)
			return [][]any{{true}}, nil
		default:
			return nil, fmt.Errorf("unexpected catalog query: %s", sql)
		}
	}}
	boundary := &postgresSwitchoverSQL{logicalName: logicalName}
	require.NoError(t, boundary.verifySupportedObjects(t.Context(), conn, nil, declaredDatabase{name: database}, cutoverIdentity{}, sideGreen))
	assert.True(t, checked)
}

func TestCutoverGreenRequiresExactImportedPublicationContract(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(fmt.Sprintf("catalog_contract_%t", valid), func(t *testing.T) {
			checked := false
			conn := &cutoverSQLTestConn{query: func(sql string, args []any) ([][]any, error) {
				switch {
				case strings.Contains(sql, "pg_largeobject_metadata"):
					return [][]any{{false}}, nil
				case strings.Contains(sql, "SELECT format('%I.%I', n.nspname, c.relname)"):
					return nil, nil
				case strings.Contains(sql, "SELECT extname FROM pg_extension"):
					return [][]any{{"plpgsql"}}, nil
				case strings.Contains(sql, "SELECT 'foreign-server:'"):
					return nil, nil
				case strings.Contains(sql, "FROM pg_catalog.pg_subscription"):
					return [][]any{{true}}, nil
				case sql == cutoverPublicationInventorySQL:
					checked = true
					assert.Equal(t, []any{publicationName("orders", "app")}, args)
					assert.Contains(t, sql, "count(*)=1")
					assert.Contains(t, sql, "pubname=$1")
					assert.Contains(t, sql, "pubowner=(SELECT oid FROM pg_catalog.pg_roles WHERE rolname='postgres')")
					assert.Contains(t, sql, "puballtables AND pubinsert AND pubupdate AND pubdelete AND pubtruncate AND NOT pubviaroot")
					assert.Contains(t, sql, "COALESCE(to_jsonb(publication)->>'pubgencols', 'n')='n'")
					return [][]any{{valid}}, nil
				default:
					return nil, fmt.Errorf("unexpected catalog query: %s", sql)
				}
			}}
			boundary := &postgresSwitchoverSQL{logicalName: "orders"}
			err := boundary.verifySupportedObjects(t.Context(), conn, nil, declaredDatabase{name: "app"}, cutoverIdentity{}, sideGreen)
			assert.True(t, checked)
			if valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
				require.ErrorContains(t, err, "unsupported green publication")
			}
		})
	}
}
