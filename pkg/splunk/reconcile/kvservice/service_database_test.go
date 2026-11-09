// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kvservice

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/logging"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestServiceDatabaseStartsWithoutTenantsAndIsolatesTenantFailures(t *testing.T) {
	for _, failure := range []string{"no tenants", "database pending", "missing auth Secret", "empty auth key", "missing database password", "invalid tenant", "duplicate tenant", "reserved tenant"} {
		t.Run(failure, func(t *testing.T) {
			cr, _, c := postgresReadinessFixture(t)
			ctx := t.Context()
			if failure == "no tenants" {
				for _, name := range []string{"ns-a", "ns-b"} {
					require.NoError(t, c.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}))
				}
			}
			switch failure {
			case "database pending":
				db := &platformApi.PostgresDatabase{}
				require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: "alpha-database"}, db))
				phase := "Pending"
				db.Status.Phase = &phase
				require.NoError(t, c.Status().Update(ctx, db))
			case "missing auth Secret":
				require.NoError(t, c.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "ns-a", Name: "splunk-ns-a-secret"}}))
			case "empty auth key":
				changeTenantSource(t, ctx, c, "ns-a", "")
			case "missing database password":
				require.NoError(t, c.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: cr.Namespace, Name: "alpha-owner"}}))
			case "invalid tenant", "reserved tenant":
				ns := &corev1.Namespace{}
				require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "ns-a"}, ns))
				ns.Labels[common.LabelTenant] = "bad-name"
				if failure == "reserved tenant" {
					ns.Labels[common.LabelTenant] = common.KVServiceSystemDatabase
				}
				require.NoError(t, c.Update(ctx, ns))
			case "duplicate tenant":
				require.NoError(t, c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-c", Labels: map[string]string{common.LabelKVService: "enabled", common.LabelTenant: "alpha"}, Annotations: map[string]string{common.AnnotationKVServiceAddr: "splunk-kvs2-kvservice-service.shared:8066"}}}))
			}
			postgres, err := ensureTenantPostgresReady(ctx, c, cr)
			dependencies, ready, message := postgres.dependencies, postgres.ready, postgres.message
			require.NoError(t, err)
			require.True(t, ready)
			require.Empty(t, message)
			config := &corev1.ConfigMap{}
			require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: "splunk-kvs2-kvservice-tenants"}, config))
			expected := map[string]string{"bravo": "ns-b"}
			if failure == "no tenants" {
				expected = map[string]string{}
			}
			require.Equal(t, len(expected), len(config.Data))
			for tenant, namespace := range expected {
				require.Equal(t, namespace, config.Data[tenant])
			}
			require.Len(t, dependencies.Volumes, 2+len(expected))
			for _, env := range dependencies.Environment {
				if env.Name == "KVSERVICE_CMPK_SERVICE_DATABASE" {
					require.Equal(t, common.KVServiceSystemDatabase, env.Value)
				}
				if env.Name == "KVSERVICE_DB_SERVICE_PASSWORD" {
					require.Equal(t, "kvservicesystem-owner", env.ValueFrom.SecretKeyRef.Name)
				}
			}
			for _, status := range cr.Status.Tenants {
				require.Equal(t, status.Name == "bravo", status.Ready)
				if !status.Ready {
					require.NotEmpty(t, status.Reason)
					require.NotEmpty(t, status.Message)
				}
			}
			service := &platformApi.PostgresDatabase{}
			require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: common.KVServiceSystemDatabaseResource}, service))
			require.Equal(t, resources.NewKVServiceSystemDatabase(cr).Spec, service.Spec)
			require.Empty(t, service.OwnerReferences)
		})
	}
}

func TestTenantStatusEventsAndRecoveryDoNotStallService(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	cr, _, c := postgresReadinessFixture(t)
	ctx := context.Background()
	recorder := events.NewFakeRecorder(100)
	require.NoError(t, c.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "ns-a", Name: "splunk-ns-a-secret"}}))
	_, err := Apply(ctx, c, client.ObjectKeyFromObject(cr), recorder)
	require.NoError(t, err)
	require.Equal(t, 1, countKVServiceEvents(drainKVServiceEvents(recorder), "Warning "+string(common.EventReasonTenantNotReady)+" "))
	deployment := &appsv1.Deployment{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: "splunk-kvs2-kvservice"}, deployment))
	deployment.Status = appsv1.DeploymentStatus{Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1}
	require.NoError(t, c.Status().Update(ctx, deployment))
	_, err = Apply(ctx, c, client.ObjectKeyFromObject(cr), recorder)
	require.NoError(t, err)
	transitionEvents := drainKVServiceEvents(recorder)
	require.Zero(t, countKVServiceEvents(transitionEvents, "Warning "+string(common.EventReasonTenantNotReady)+" "))
	require.Equal(t, 1, countKVServiceEvents(transitionEvents, "Normal "+string(common.EventReasonReady)+" "))
	result, err := Apply(ctx, c, client.ObjectKeyFromObject(cr), recorder)
	require.NoError(t, err)
	require.Equal(t, 5*time.Second, result.RequeueAfter)
	require.Empty(t, drainKVServiceEvents(recorder), "pending tenant retries must not repeat service Ready events")
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(cr), cr))
	require.Equal(t, enterpriseApi.PhaseReady, cr.Status.Phase)
	require.Equal(t, metav1.ConditionFalse, meta.FindStatusCondition(cr.Status.Conditions, "TenantsReady").Status)
	require.False(t, common.IsStalled(cr.Status.Conditions))
	require.NoError(t, c.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "ns-a", Name: "splunk-ns-a-secret"}, Data: map[string][]byte{"pass4SymmKey": []byte("alpha-key")}}))
	_, err = Apply(ctx, c, client.ObjectKeyFromObject(cr), recorder)
	require.NoError(t, err)
	require.Equal(t, 1, countKVServiceEvents(drainKVServiceEvents(recorder), "Normal "+string(common.EventReasonTenantReady)+" "))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(cr), cr))
	require.Equal(t, metav1.ConditionTrue, meta.FindStatusCondition(cr.Status.Conditions, "TenantsReady").Status)
}

func TestTenantAuthenticationReadFailureIsLoggedAndIsolated(t *testing.T) {
	cr, _, c := postgresReadinessFixture(t)
	failure := errors.New("authentication read unavailable")
	var logs bytes.Buffer
	ctx := logging.WithLogger(t.Context(), slog.New(slog.NewTextHandler(&logs, nil)))
	guarded := interceptor.NewClient(c, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
		if _, secret := obj.(*corev1.Secret); secret && key.Namespace == "ns-a" && key.Name == "splunk-ns-a-secret" {
			return failure
		}
		return c.Get(ctx, key, obj, opts...)
	}})
	postgres, err := ensureTenantPostgresReady(ctx, guarded, cr)
	ready := postgres.ready
	require.NoError(t, err)
	require.True(t, ready)
	require.EqualValues(t, "AuthenticationPending", cr.Status.Tenants[0].Reason)
	require.True(t, cr.Status.Tenants[1].Ready)
	for _, detail := range []string{failure.Error(), "tenant=alpha", "namespace=ns-a", "secret=splunk-ns-a-secret"} {
		require.Contains(t, logs.String(), detail)
	}
}

func TestTenantStatusIsUnknownWhenEvaluationCannotRun(t *testing.T) {
	for _, failure := range []string{"reservation", "list"} {
		t.Run(failure, func(t *testing.T) {
			t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
			cr, _, c := postgresReadinessFixture(t)
			ctx := t.Context()
			previous := []enterpriseApi.KVServiceTenantStatus{{Name: "alpha", Namespace: "ns-a", Ready: true, Reason: enterpriseApi.KVServiceTenantReady}}
			cr.Status.Tenants = previous
			require.NoError(t, c.Status().Update(ctx, cr))
			guarded := c
			if failure == "reservation" {
				ring := &corev1.Secret{}
				require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: common.KVServiceTenantVerificationKeysName}, ring))
				ring.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(&enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "previous", UID: "previous"}}, enterpriseApi.GroupVersion.WithKind("KVService"))}
				require.NoError(t, c.Update(ctx, ring))
			} else {
				guarded = interceptor.NewClient(c, interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, services := list.(*enterpriseApi.KVServiceList); services {
						return errors.New("list unavailable")
					}
					return c.List(ctx, list, opts...)
				}})
			}
			recorder := events.NewFakeRecorder(20)
			_, err := Apply(ctx, guarded, client.ObjectKeyFromObject(cr), recorder)
			require.Equal(t, failure == "list", err != nil)
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(cr), cr))
			require.Equal(t, previous, cr.Status.Tenants)
			condition := meta.FindStatusCondition(cr.Status.Conditions, "TenantsReady")
			require.Equal(t, metav1.ConditionUnknown, condition.Status)
			require.Equal(t, "TenantEvaluationPending", condition.Reason)
			require.Empty(t, drainKVServiceEvents(recorder))
		})
	}
}

func TestTenantTransitionEventsWaitForSuccessfulStatusWrite(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	cr, _, c := postgresReadinessFixture(t)
	ctx := t.Context()
	require.NoError(t, c.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "ns-a", Name: "splunk-ns-a-secret"}}))
	writeErr := errors.New("status unavailable")
	guarded := interceptor.NewClient(c, interceptor.Funcs{SubResourceUpdate: func(ctx context.Context, c client.Client, subresource string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
		if _, service := obj.(*enterpriseApi.KVService); service && subresource == "status" {
			return writeErr
		}
		return c.SubResource(subresource).Update(ctx, obj, opts...)
	}})
	recorder := events.NewFakeRecorder(30)
	_, err := Apply(ctx, guarded, client.ObjectKeyFromObject(cr), recorder)
	require.ErrorIs(t, err, writeErr)
	require.Empty(t, drainKVServiceEvents(recorder))
	_, err = Apply(ctx, c, client.ObjectKeyFromObject(cr), recorder)
	require.NoError(t, err)
	emitted := drainKVServiceEvents(recorder)
	require.Equal(t, 1, countKVServiceEvents(emitted, "Warning "+string(common.EventReasonTenantNotReady)+" "))
	require.Equal(t, 1, countKVServiceEvents(emitted, "Normal "+string(common.EventReasonTenantReady)+" "))
	_, err = Apply(ctx, c, client.ObjectKeyFromObject(cr), recorder)
	require.NoError(t, err)
	require.Empty(t, drainKVServiceEvents(recorder))
}

func TestNoTenantsConditionDoesNotBlockService(t *testing.T) {
	cr := &enterpriseApi.KVService{}
	setTenantCondition(cr, true)
	condition := meta.FindStatusCondition(cr.Status.Conditions, "TenantsReady")
	require.Equal(t, metav1.ConditionTrue, condition.Status)
	require.Equal(t, "NoTenants", condition.Reason)
}
