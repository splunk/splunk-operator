/*
Copyright 2026.

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
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/jackc/pgx/v5/pgconn"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type catchUpSQLModel struct {
	*parityCatalogModel
	fenced, behind, copying bool
	greenGuards             bool
	applyErrors             int64
	invalidation, extraSlot string
	failQuery               string
	sampledLSN              bool
}

// Keep the production fence, catalog, subscription, and all-instance slot
// verifiers; only PostgreSQL query results and Kubernetes are simulated.
func candidateCatchUpSQLFixture(t *testing.T) (*postgresSwitchoverSQL, *cnpgv1.Cluster, *cnpgv1.Cluster, declaredDatabase, *platformv1alpha1.PostgresBlueGreenUpgradeStatus, cutoverIdentity, *catchUpSQLModel) {
	t.Helper()
	sql, blue, green, db, status, id, catalog := parityCatalogFixture(t)
	model := &catchUpSQLModel{parityCatalogModel: catalog, fenced: true}
	blue.Status.ReadyInstances = 3
	blue.Status.InstancesStatus[cnpgv1.PodHealthy] = []string{"orders-blue-1", "orders-blue-2", "orders-blue-3"}
	setLogicalSlotSynchronization(&blue.Spec)
	require.NoError(t, sql.client.Update(t.Context(), blue))
	green.Spec.Instances = 1
	green.Spec.Managed.Roles[1].PasswordSecret = &cnpgv1.LocalObjectReference{Name: "writer-secret"}
	green.Status.Phase, green.Status.Instances, green.Status.ReadyInstances = cnpgv1.PhaseHealthy, 1, 1
	green.Status.CurrentPrimary = green.Name + "-1"
	green.Status.InstancesStatus = map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {green.Status.CurrentPrimary}}
	require.NoError(t, sql.client.Update(t.Context(), green))
	pod := &corev1.Pod{Name: green.Status.CurrentPrimary, Namespace: green.Namespace, UID: "green-pod-uid", Labels: map[string]string{"cnpg.io/cluster": green.Name}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(green, cnpgv1.SchemeGroupVersion.WithKind("Cluster"))}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.1.1", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
	require.NoError(t, sql.client.Create(t.Context(), pod))
	require.NoError(t, sql.client.Create(t.Context(), &corev1.Secret{Name: green.GetSuperuserSecretName(), Namespace: green.Namespace, Data: map[string][]byte{"password": []byte("target-password")}}))
	publication := &cnpgv1.Publication{Name: publicationName(sql.logicalName, db.name), Namespace: blue.Namespace, Labels: map[string]string{blueGreenAttemptLabel: status.AttemptID}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(sql.owner, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}, Spec: desiredPublicationSpec(blue.Name, db.name, publicationName(sql.logicalName, db.name))}
	require.NoError(t, sql.client.Create(t.Context(), publication))
	dial := sql.connect
	sql.connect = func(ctx context.Context, host, database, role, password, serverName string, ca []byte) (pginfra.Conn, error) {
		if role != id.Role {
			return nil, &pgconn.PgError{Code: "28000", Message: "pg_hba.conf rejects connection"}
		}
		// The reused catalog fixture selects blue/green query responses by name.
		catalogHost := host
		if strings.HasPrefix(host, "10.0.0.") {
			catalogHost = blue.Name
		}
		conn, err := dial(ctx, catalogHost, database, role, password, serverName, ca)
		if err != nil {
			return nil, err
		}
		result := conn.(*cutoverSQLTestConn)
		queryCatalog := result.query
		result.query = func(query string, args []any) ([][]any, error) {
			if model.failQuery != "" && strings.Contains(query, model.failQuery) {
				return nil, errors.New("temporary query failure")
			}
			switch {
			case query == cutoverGuardPresenceSQL:
				return [][]any{{model.greenGuards && catalogHost != blue.Name && !strings.Contains(catalogHost, blue.Name)}}, nil
			case strings.Contains(query, "SELECT language.lanname = 'plpgsql' AND function.prokind = 'f'"):
				return [][]any{{true}}, nil
			case strings.Contains(query, "SELECT n.nspname, c.relname"):
				return nil, nil
			case query == cutoverSourceFinalWALPositionSQL:
				model.sampledLSN = true
				return [][]any{{"0/100"}}, nil
			case query == controlHelperCallSQL(controlFenceFunction):
				return [][]any{{model.fenced}}, nil
			case query == cutoverRoleFenceSQL:
				return [][]any{{true}}, nil
			case query == subscriptionApplyErrorsSQL:
				return [][]any{{model.applyErrors}}, nil
			case strings.Contains(query, "JOIN pg_stat_subscription worker"):
				require.Equal(t, "0/100", args[2], "catch-up must use this pass's blue sample")
				return [][]any{{!model.behind}}, nil
			case strings.Contains(query, "pg_subscription_rel rel"):
				return [][]any{{!model.copying}}, nil
			case strings.Contains(query, "current_setting('hot_standby_feedback')::boolean"):
				return [][]any{{true}}, nil
			case strings.Contains(query, "SELECT active, confirmed_flush_lsn IS NOT NULL"):
				primary := host == "10.0.0.1"
				return [][]any{{primary, true, model.invalidation, true, !primary, false, true}}, nil
			case strings.Contains(query, "SELECT slot_name || '/' || database"):
				rows := [][]any{{replicationSlotName(sql.logicalName, db.name) + "/" + db.name}}
				if model.extraSlot != "" {
					rows = append(rows, []any{model.extraSlot + "/" + db.name})
				}
				return rows, nil
			default:
				return queryCatalog(query, args)
			}
		}
		return result, nil
	}
	return sql, blue, green, db, status, id, model
}

func TestCandidateCatchUpUsesFrozenCatalogsAndAllSourceSlots(t *testing.T) {
	for _, scenario := range []string{"caught up", "subscription lag", "copy pending", "apply error", "slot invalidated", "extra slot", "copy slot closing", "source catalog drift", "query error", "green fence not loaded", "unhealthy source"} {
		t.Run(scenario, func(t *testing.T) {
			sql, blue, green, db, status, id, model := candidateCatchUpSQLFixture(t)
			switch scenario {
			case "subscription lag":
				model.behind = true
			case "copy pending":
				model.copying = true
			case "apply error":
				model.applyErrors = 1
			case "slot invalidated":
				model.invalidation = "wal_removed"
			case "extra slot":
				model.extraSlot = "unmanaged"
			case "copy slot closing":
				model.extraSlot = "pg_10_sync_11_12"
			case "source catalog drift":
				model.schema = "changed"
			case "query error":
				model.failQuery = "JOIN pg_stat_subscription worker"
			case "green fence not loaded":
				model.fenced = false
			case "unhealthy source":
				blue.Status.ReadyInstances = 1
			}
			ready, err := sql.VerifyCandidateCatchUp(t.Context(), blue, green, []declaredDatabase{db}, status, id)
			switch scenario {
			case "apply error", "slot invalidated", "extra slot", "source catalog drift":
				require.ErrorIs(t, err, mvutypes.ErrCandidateInvalid)
			case "query error":
				require.ErrorContains(t, err, "temporary query failure")
				assert.NotErrorIs(t, err, mvutypes.ErrCandidateInvalid)
			default:
				require.NoError(t, err)
			}
			assert.Equal(t, scenario == "caught up", ready)
			if scenario == "caught up" {
				receipt := status.DatabaseReplication[0]
				assert.True(t, *receipt.SubscriptionHealthy)
				assert.True(t, *receipt.InitialCopyComplete)
				assert.True(t, *receipt.SlotHealthy)
				require.Len(t, receipt.ReplicaSlots, 3)
				for _, slot := range receipt.ReplicaSlots {
					assert.True(t, *slot.Valid)
					assert.NotEmpty(t, slot.ReplicaUID)
				}
			}
			if scenario == "green fence not loaded" || scenario == "unhealthy source" {
				assert.False(t, model.sampledLSN)
			}
		})
	}
}

func TestRevalidationResumesPartialBootstrapWithProductionVerifier(t *testing.T) {
	sql, blue, green, db, status, _, model := candidateCatchUpSQLFixture(t)
	green.Status.WriteService = green.GetServiceReadWriteName()
	status.CandidateEndpoints = &platformv1alpha1.BlueGreenCandidateEndpointsStatus{DirectService: serviceReference(createCandidateDirectServices(t, sql.client, green))}
	status.CommitReceipt = nil
	status.Switchover.StartedAt = nil
	status.Switchover.CandidateGuardsRemovedAt = nil
	model.greenGuards = true
	status.Switchover.SourceFenceRestoredAt = new(metav1.NewTime(time.Now().UTC().Truncate(time.Second)))
	// Blue was restored; green was never reached by the earlier fence call.
	setAccess(blue, status, sideBlue, fenceOpen)
	setAccess(green, status, sideGreen, fenceOpen)
	require.NoError(t, sql.client.Update(t.Context(), blue))
	require.NoError(t, sql.client.Update(t.Context(), green))
	before := blue.DeepCopy()
	require.NoError(t, sql.client.Create(t.Context(), &platformv1alpha1.PostgresDatabase{Name: "orders-db", Namespace: blue.Namespace, UID: "database-uid", Spec: platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: sql.logicalName}, Databases: []platformv1alpha1.DatabaseDefinition{db.definition}}}))
	require.NoError(t, sql.client.Create(t.Context(), &corev1.Secret{Name: replicationSecretName(sql.logicalName, status.AttemptID), Namespace: blue.Namespace, UID: "replication-secret", Data: map[string][]byte{"password": []byte("replication")}}))
	runtime := &BlueGreenSwitchover{client: sql.client, reader: sql.client, key: client.ObjectKeyFromObject(sql.owner), owner: sql.owner, sql: sql}
	_, _, databases, err := runtime.inputs(t.Context(), status)
	require.NoError(t, err)
	baseline := runtime.credentialBaseline()
	fingerprint, err := baseline.configurationFingerprint(blue, databases)
	require.NoError(t, err)
	status.SourceConfigurationFingerprint = &fingerprint
	status.CredentialFingerprints, err = baseline.credentialFingerprints(t.Context(), blue, databases, replicationSecretName(sql.logicalName, status.AttemptID))
	require.NoError(t, err)
	status.Switchover.GreenCredentialFingerprints, err = baseline.credentialFingerprints(t.Context(), green, databases, replicationSecretName(sql.logicalName, status.AttemptID))
	require.NoError(t, err)
	greenFingerprint, err := candidateConfigurationFingerprint(green)
	require.NoError(t, err)
	status.Switchover.GreenConfigurationFingerprint = &greenFingerprint
	control := &accessSQLStub{fenceReady: true}
	access, err := newBlueGreenAccess(sql.client, sql.client, runtime.key, sql.owner, control)
	require.NoError(t, err)
	runtime.access = access
	status.Switchover.ControlIdentity.PreparedAt = nil
	parent := &platformv1alpha1.PostgresCluster{}
	require.NoError(t, sql.client.Get(t.Context(), runtime.key, parent))
	parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: status.DeepCopy()}}
	require.NoError(t, sql.client.Update(t.Context(), parent))
	ready, err := runtime.RevalidateCandidate(t.Context(), mvutypes.Intent{}, status)
	require.NoError(t, err)
	assert.False(t, ready, "save completed bootstrap before SQL verification")
	require.NotNil(t, status.Switchover.ControlIdentity.PreparedAt)
	assert.Equal(t, 2, control.prepareCalls, "resume both sides before validating the owned control overlay")
	require.NoError(t, sql.client.Get(t.Context(), client.ObjectKeyFromObject(green), green))
	assert.True(t, accessMatches(green, status, sideGreen, fenceOpen))
	ready, err = runtime.RevalidateCandidate(t.Context(), mvutypes.Intent{}, status)
	require.NoError(t, err)
	assert.True(t, ready, "the real catalog/replication verifier must accept the owned overlay")
	require.NoError(t, sql.client.Get(t.Context(), client.ObjectKeyFromObject(blue), blue))
	assert.Equal(t, before.Spec, blue.Spec)
	assert.Nil(t, status.Switchover.StartedAt)
	assert.Equal(t, int64(1), status.Switchover.Sequence)
	require.NoError(t, sql.client.Get(t.Context(), client.ObjectKeyFromObject(green), green))
	assert.True(t, accessMatches(green, status, sideGreen, fenceApplied))
}
