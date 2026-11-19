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
	"fmt"
	"strings"
	"syscall"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/jackc/pgx/v5/pgconn"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type subscriptionSQLFunc func(context.Context, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error)

func (f subscriptionSQLFunc) RemoveSubscriptions(ctx context.Context, green *cnpgv1.Cluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, _ cutoverIdentity) (bool, error) {
	return f(ctx, green, bg)
}

func subscriptionSQLFixture(t *testing.T) (*teardownFixture, *cnpgv1.Cluster, *corev1.Pod, *postgresSwitchoverSQL) {
	t.Helper()
	f := newTeardownFixture(t)
	green := &cnpgv1.Cluster{Name: "green", Namespace: "default"}
	require.True(t, f.exists(green))
	green.Spec.SuperuserSecret = &cnpgv1.LocalObjectReference{Name: "green-superuser"}
	green.Status.Certificates.ServerCASecret = "green-ca"
	require.NoError(t, f.client.Update(t.Context(), green))
	pod := &corev1.Pod{Name: "green-1", Namespace: "default", UID: "green-1-uid", Labels: map[string]string{"cnpg.io/cluster": green.Name},
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(green, cnpgv1.SchemeGroupVersion.WithKind("Cluster"))},
		Status:          corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.2", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
	for _, object := range []client.Object{pod,
		&corev1.Secret{Name: "green-superuser", Namespace: "default", Data: map[string][]byte{"password": []byte("test")}},
		&corev1.Secret{Name: "green-ca", Namespace: "default", Data: map[string][]byte{"ca.crt": []byte("test")}},
	} {
		require.NoError(t, f.client.Create(t.Context(), object))
	}
	sql, err := newPostgresSwitchoverSQL(f.client, f.parent.Name, f.parent)
	require.NoError(t, err)
	return f, green, pod, sql
}

func TestCancellationSubscriptionMissingDatabase(t *testing.T) {
	missing := &pgconn.PgError{Code: "3D000"}
	for _, test := range []struct {
		name   string
		err    error
		absent bool
	}{
		{"missing", missing, true},
		{"wrapped missing", fmt.Errorf("connect: %w", missing), true},
		{"authentication", &pgconn.PgError{Code: "28P01"}, false},
		{"network", syscall.ECONNREFUSED, false},
		{"mixed errors", errors.Join(missing, syscall.ECONNREFUSED), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, green, pod, _ := subscriptionSQLFixture(t)
			calls := 0
			sql := &postgresSwitchoverSQL{client: f.client, owner: f.parent, logicalName: f.parent.Name, bootstrap: func(_ context.Context, host, database, _, _ string, _ []byte) (pginfra.Conn, error) {
				calls++
				assert.Equal(t, pod.Status.PodIP, host)
				assert.Equal(t, "orders", database)
				return nil, test.err
			}}
			cancellation := &BlueGreenCancellation{BlueGreenReplicationTeardown: f.teardown, client: f.client, subscriberSQL: sql, switchover: &BlueGreenSwitchover{sql: sql}}
			done, err := cancellation.removeGreenSubscriptions(t.Context(), green, f.status)
			assert.Equal(t, test.absent, done)
			if test.absent {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, test.err)
			}
			assert.Equal(t, 1, calls)
		})
	}
}

func TestCancellationWaitsForSubscriptionWriter(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint("legacy-", legacy), func(t *testing.T) {
			f := newTeardownFixture(t)
			f.status.Gates = &platformv1alpha1.BlueGreenGateStatus{Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{}}
			if legacy {
				f.status.Gates.Cancellation.GreenReplicationRemovedAt = new(metav1.Now())
			}
			originalReceipt := f.status.Gates.Cancellation.GreenReplicationRemovedAt
			subscription := &cnpgv1.Subscription{}
			require.NoError(t, f.client.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "orders-sub"}, subscription))
			subscription.Finalizers = []string{cnpgutils.SubscriptionFinalizerName}
			require.NoError(t, f.client.Update(t.Context(), subscription))
			probes := 0
			c := &BlueGreenCancellation{BlueGreenReplicationTeardown: f.teardown, client: f.client, subscriberSQL: subscriptionSQLFunc(func(context.Context, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
				probes++
				return true, nil
			})}
			for range 3 {
				done, err := c.ensureGreenReplicationRemoved(t.Context(), f.status)
				require.NoError(t, err)
				assert.False(t, done)
				assert.Equal(t, originalReceipt, f.status.Gates.Cancellation.GreenReplicationRemovedAt)
			}
			require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(subscription), subscription))
			assert.NotNil(t, subscription.DeletionTimestamp)
			assert.Contains(t, subscription.Finalizers, cnpgutils.SubscriptionFinalizerName)
			assert.Equal(t, 0, probes, "SQL must wait for CNPG acknowledgement of its last apply")
			f.release(subscription)
			done, err := c.ensureGreenReplicationRemoved(t.Context(), f.status)
			require.NoError(t, err)
			assert.False(t, done, "persist SQL proof before source cleanup")
			assert.NotNil(t, f.status.Gates.Cancellation.GreenReplicationRemovedAt)
			assert.Equal(t, 1, probes)
			assert.Equal(t, int32(1), f.status.Gates.Cancellation.GreenReplicationProofVersion)
			if legacy {
				assert.Equal(t, originalReceipt, f.status.Gates.Cancellation.GreenReplicationRemovedAt)
			}
		})
	}
}

func TestCancellationSubscriptionCreationAuthorization(t *testing.T) {
	for _, authorization := range []*bool{nil, new(false), new(true)} {
		f := newTeardownFixture(t)
		f.status.SubscriptionCreationAuthorized = authorization
		green := &cnpgv1.Cluster{Name: "green", Namespace: "default"}
		require.True(t, f.exists(green))
		// No ready primary or SQL credentials: only a write-ahead noncreation
		// receipt can prove SQL absence for this failed bootstrap.
		c := &BlueGreenCancellation{BlueGreenReplicationTeardown: f.teardown, client: f.client, subscriberSQL: &postgresSwitchoverSQL{client: f.client, owner: f.parent, logicalName: f.parent.Name}}
		done, err := c.removeGreenSubscriptions(t.Context(), green, f.status)
		require.NoError(t, err)
		assert.Equal(t, authorization != nil && !*authorization, done)
	}
}

func TestCancellationMissingGreenRequiresAbsentInstancesAndStorage(t *testing.T) {
	for _, remaining := range []string{"none", "pod", "storage", "replacement"} {
		t.Run(remaining, func(t *testing.T) {
			f := newTeardownFixture(t)
			f.status.Gates = &platformv1alpha1.BlueGreenGateStatus{Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: new(metav1.Now())}}
			subscription := &cnpgv1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "orders-sub", Namespace: "default"}}
			require.NoError(t, f.client.Delete(t.Context(), subscription))
			f.release(subscription)
			green := &cnpgv1.Cluster{Name: "green", Namespace: "default"}
			require.True(t, f.exists(green))
			require.NoError(t, f.client.Delete(t.Context(), green))
			owner := *metav1.NewControllerRef(green, cnpgv1.SchemeGroupVersion.WithKind("Cluster"))
			switch remaining {
			case "pod":
				require.NoError(t, f.client.Create(t.Context(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "green-1", Namespace: "default", UID: "pod-uid", OwnerReferences: []metav1.OwnerReference{owner}}}))
			case "storage":
				require.NoError(t, f.client.Create(t.Context(), &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "green-data", Namespace: "default", UID: "pvc-uid", OwnerReferences: []metav1.OwnerReference{owner}}}))
			case "replacement":
				green.UID, green.ResourceVersion = "replacement", ""
				require.NoError(t, f.client.Create(t.Context(), green))
			}
			c := &BlueGreenCancellation{BlueGreenReplicationTeardown: f.teardown, client: f.client}
			done, err := c.ensureGreenReplicationRemoved(t.Context(), f.status)
			assert.False(t, done)
			if remaining == "storage" || remaining == "replacement" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, remaining == "none", f.status.Gates.Cancellation.GreenReplicationRemovedAt != nil)
			assert.Empty(t, f.sql.roleCalls)
		})
	}
}

func TestSubscriptionRetirementSQLUsesSelectedAccessAndBoundHelper(t *testing.T) {
	for _, mode := range []string{"bootstrap", "existing helper", "resume identity", "restricted", "helper drift", "pending workers", "query failure", "close failure"} {
		t.Run(mode, func(t *testing.T) {
			f, green, pod, sql := subscriptionSQLFixture(t)
			id := cutoverIdentity{Role: "postgres", AttemptID: "attempt", LogicalName: f.parent.Name}
			restricted := mode == "restricted" || mode == "helper drift"
			if restricted || mode == "resume identity" {
				id.Role, id.Password = "control", "test-password"
				f.status.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{BlueGreenCutoverAccessStatus: platformv1alpha1.BlueGreenCutoverAccessStatus{
					ControlIdentity: &platformv1alpha1.BlueGreenControlIdentityStatus{RoleName: id.Role}, GreenOriginalHBA: new([]string{}),
				}}
				if restricted {
					f.status.Switchover.ControlIdentity.PreparedAt = new(metav1.Now())
				}
			}
			conn := &closingCutoverConn{cutoverSQLTestConn: cutoverSQLTestConn{query: func(query string, _ []any) ([][]any, error) {
				switch query {
				case controlGuardFunctionExistsSQL:
					return [][]any{{mode != "bootstrap" && mode != "resume identity"}}, nil
				case controlHelperProofSQL:
					return [][]any{{mode != "helper drift"}}, nil
				case controlHelperCallSQL(controlRetireSubscriptionFunction):
					if mode == "query failure" {
						return nil, errors.New("subscription query failed")
					}
					return [][]any{{mode != "pending workers"}}, nil
				}
				if strings.Contains(query, "password_encryption") {
					return [][]any{{"scram-sha-256"}}, nil
				}
				return [][]any{{true}}, nil
			}}}
			if mode == "close failure" {
				conn.closeErr = errors.New("connection close failed")
			}
			bootstrap, control := 0, 0
			sql.bootstrap = func(_ context.Context, host, database, _, _ string, _ []byte) (pginfra.Conn, error) {
				bootstrap++
				assert.Equal(t, pod.Status.PodIP, host)
				assert.Equal(t, "orders", database)
				return conn, nil
			}
			sql.connect = func(_ context.Context, host, _, role, _, _ string, _ []byte) (pginfra.Conn, error) {
				control++
				assert.Equal(t, pod.Status.PodIP, host)
				assert.Equal(t, id.Role, role)
				return conn, nil
			}
			done, err := sql.RemoveSubscriptions(t.Context(), green, f.status, id)
			failed := mode == "helper drift" || mode == "query failure" || mode == "close failure"
			if failed {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, !failed && mode != "pending workers", done)
			assert.Equal(t, 1, conn.closeCalls)
			if restricted {
				assert.Equal(t, 1, control)
				assert.Zero(t, bootstrap)
				assert.Empty(t, conn.execs, "restricted access cannot install or replace helpers")
			} else {
				assert.Equal(t, 1, bootstrap)
				assert.Zero(t, control)
			}
			if mode == "resume identity" {
				assert.Contains(t, strings.Join(conn.execs, "\n"), `SET SESSION AUTHORIZATION "control"`)
			}
		})
	}
}
