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
	"reflect"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/jackc/pgx/v5/pgconn"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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
	status.Switchover.FinalBlueLSN = new("0/100")
	status.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CommittedAt: &now, CanonicalConfigMap: &corev1.ObjectReference{Name: "canonical", UID: "canonical-uid"}, Target: status.Green.Ref.DeepCopy()}
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

type cutoverSQLTestConn struct {
	preflightSQLTestConn
	startupPIDs []int32
	query       func(string, []any) ([][]any, error)
	rowError    func(string, []any) error
}

func (c *cutoverSQLTestConn) Query(_ context.Context, sql string, args ...any) (pginfra.Rows, error) {
	c.queries = append(c.queries, sql)
	if sql == cutoverStartupPIDsSQL {
		return &cutoverSQLTestRows{values: [][]any{{c.startupPIDs}}}, nil
	}
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

func (r *cutoverSQLTestRows) Close() {}

func (r *cutoverSQLTestRows) Err() error { return r.terminal }
