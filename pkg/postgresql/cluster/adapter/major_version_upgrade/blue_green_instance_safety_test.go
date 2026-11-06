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
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPrecommitFenceAndDrainIncludeUnlistedOwnedInstance(t *testing.T) {
	for _, operation := range []string{"fence", "drain"} {
		t.Run(operation, func(t *testing.T) {
			sql, blue, status, id, _ := retainedSourceSQLFixture(t)
			blue.Status.InstancesStatus[cnpgv1.PodHealthy] = []string{"orders-blue-1", "orders-blue-2", "orders-blue-3"}
			blue.Status.ReadyInstances = 3
			unlisted := observedInstanceTestPod(blue, "orders-blue-unlisted", "10.0.0.4")
			require.NoError(t, sql.client.Create(t.Context(), unlisted))
			blocked := errors.New("unlisted instance still accepts application sessions")
			connect := sql.connect
			observed := false
			sql.connect = func(ctx context.Context, host, database, role, password, serverName string, ca []byte) (pginfra.Conn, error) {
				if host == unlisted.Status.PodIP {
					observed = true
					return nil, blocked
				}
				return connect(ctx, host, database, role, password, serverName, ca)
			}
			check := func() (bool, error) {
				if operation == "fence" {
					return sql.VerifyFence(t.Context(), blue, id, roleNames(status.Switchover.OriginalRoleLogins))
				}
				return sql.Drain(t.Context(), blue, id, true)
			}
			ready, err := check()
			require.ErrorIs(t, err, blocked)
			require.False(t, ready)
			require.True(t, observed, "a complete healthy-status list must not hide an additional owned instance")
			require.NoError(t, sql.client.Delete(t.Context(), unlisted))
			ready, err = check()
			require.NoError(t, err)
			require.True(t, ready, "verification may proceed after the additional instance is confirmed gone")
		})
	}
}

func TestPrecommitDiscoveryBlocksUnlistedUnreadyOrRetaggedOwnedPods(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*corev1.Pod)
		want   error
	}{
		{"unready", func(p *corev1.Pod) { p.Status.Conditions = nil }, mvutypes.ErrUpgradeFlowPending},
		{"terminating", func(p *corev1.Pod) { p.DeletionTimestamp = new(metav1.Now()); p.Finalizers = []string{"test"} }, mvutypes.ErrUpgradeFlowPending},
		{"retagged", func(p *corev1.Pod) { p.Labels = nil }, mvutypes.ErrUpgradeFlowFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			cluster := &cnpgv1.Cluster{Name: "blue", Namespace: "ns", UID: "blue-uid", Status: cnpgv1.ClusterStatus{InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"blue-1"}}}}
			listed := observedInstanceTestPod(cluster, "blue-1", "10.0.0.1")
			unlisted := observedInstanceTestPod(cluster, "blue-unlisted", "10.0.0.2")
			test.mutate(unlisted)
			reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(listed, unlisted).Build()
			_, _, err := healthyInstancePods(t.Context(), reader, cluster)
			require.ErrorIs(t, err, test.want)
		})
	}
}
