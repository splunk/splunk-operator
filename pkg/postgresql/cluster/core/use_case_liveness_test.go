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

package core

import (
	"context"
	"testing"

	pgtestfixture "github.com/splunk/splunk-operator/internal/testutil/postgres"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	custommetrics "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/custom_metrics"
	pgcconstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationtypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	usecases "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases"
	clusterk8s "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/k8s"
	identityadapter "github.com/splunk/splunk-operator/pkg/postgresql/shared/adapter/identity"
	monitoring "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestDeferredOrdinaryWorkflowRequeuesAfterComponentsObserveRequestedGeneration(t *testing.T) {
	parent, _, c := convergedClusterFixture(t)
	store, err := clusterk8s.NewClusterStateStoreWithReader(c, c, client.ObjectKeyFromObject(parent))
	require.NoError(t, err)
	uc := &generationReadyUseCase{store: store}
	metrics := &availableMetricsFixture{ready: true}
	rc := &ReconcileContext{Client: c, APIReader: c, Scheme: c.Scheme(), Recorder: record.NewFakeRecorder(30), ClusterCardResolver: identityadapter.NewIdentityResolver(), ClusterInputFactory: identityadapter.ClusterInputFromPostgresCluster, EnvironmentNamer: identityadapter.NewIdentityResolver(), UseCaseRegistryProvider: func(types.NamespacedName, *platformv1alpha1.PostgresCluster, *MergedConfig) map[string]usecases.Factory {
		return map[string]usecases.Factory{mvutypes.UseCaseName: func() (usecases.UseCase, error) { return uc, nil }}
	}}
	reconcile := func() ctrl.Result {
		t.Helper()
		result, err := PostgresClusterService(t.Context(), rc, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(parent)}, nil, noopBackupBackend{}, func(string, monitoring.Target) (*custommetrics.Model, error) {
			return custommetrics.NewModel(metrics, metrics, metrics, metrics, metrics), nil
		}, nil)
		require.NoError(t, err)
		return result
	}
	result := reconcile()
	assert.Equal(t, retryDelay, result.RequeueAfter, "status-only convergence must retain a bounded scheduling retry")
	assert.Zero(t, uc.acts, "the deferred upgrade must allow components to converge first")
	fresh := new(platformv1alpha1.PostgresCluster)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), fresh))
	require.NotNil(t, fresh.Status.ObservedGeneration)
	assert.Equal(t, parent.Generation, *fresh.Status.ObservedGeneration)
	assert.Equal(t, string(readyClusterPhase), *fresh.Status.Phase)
	assert.True(t, meta.IsStatusConditionTrue(fresh.Status.Conditions, "Ready"))
	// Run only the explicit timer pass: no CNPG or other external event occurs.
	result = reconcile()
	assert.Equal(t, 1, uc.acts, "the next timer pass must schedule and Act after readiness is observed")
	assert.True(t, result.Requeue || result.RequeueAfter > 0)
}

type generationReadyUseCase struct {
	store *clusterk8s.ClusterStateStore
	acts  int
}

func (u *generationReadyUseCase) Prerequisites(ctx context.Context) error {
	ready, err := u.store.GetReadyToStart(ctx)
	if err != nil {
		return err
	}
	if !ready {
		return usecases.ErrPrerequisiteNotReady
	}
	return nil
}
func (*generationReadyUseCase) Schedule(context.Context) (bool, error) { return true, nil }
func (*generationReadyUseCase) BlocksComponents() []string {
	return []string{pgcconstants.ComponentSecret, pgcconstants.ComponentObjectStore, "ServerTLS", pgcconstants.ComponentProvisioner, pgcconstants.ComponentCustomMetrics, pgcconstants.ComponentManagedRoles, pgcconstants.ComponentPooler, pgcconstants.ComponentBackup, pgcconstants.ComponentConfigMap}
}
func (u *generationReadyUseCase) Act(context.Context) (reconciliationtypes.Report, error) {
	u.acts++
	return reconciliationtypes.Report{Name: mvutypes.UseCaseName, Retry: true, Sleep: new(1)}, nil
}

type availableMetricsFixture struct{ ready bool }

func (*availableMetricsFixture) ListDatabaseContributions(context.Context, string, string) (monitoring.DatabaseContributionSnapshot, error) {
	return monitoring.DatabaseContributionSnapshot{}, nil
}
func (*availableMetricsFixture) FetchConfigMap(context.Context, string, string, string) ([]byte, error) {
	return nil, nil
}
func (*availableMetricsFixture) Parse([]byte, monitoring.QuerySource, *string) ([]monitoring.ResolvedQuery, error) {
	return nil, nil
}
func (*availableMetricsFixture) DetectCollisions([]monitoring.ClusterQuerySet, []monitoring.DatabaseQuerySet) ([]monitoring.ResolvedQuery, []monitoring.DatabaseQuerySet, []monitoring.CollisionError) {
	return nil, nil, nil
}
func (*availableMetricsFixture) Aggregate([]monitoring.ResolvedQuery, []monitoring.DatabaseQuerySet) monitoring.AggregatedConfig {
	return monitoring.AggregatedConfig{}
}
func (*availableMetricsFixture) Apply(context.Context, monitoring.AggregatedConfig) (monitoring.ExpectedState, error) {
	return monitoring.ExpectedState{Revision: "test"}, nil
}
func (f *availableMetricsFixture) Observe(context.Context, monitoring.ExpectedState) (monitoring.Observation, error) {
	if !f.ready {
		return monitoring.Observation{State: monitoring.ObservationPending, Message: "waiting for metrics"}, nil
	}
	return monitoring.Observation{State: monitoring.ObservationReady, Confirmed: &monitoring.ConfirmedState{Revision: "test"}}, nil
}
func (*availableMetricsFixture) Save(context.Context, monitoring.ConfirmedState) (monitoring.SaveResult, error) {
	return monitoring.SaveResult{}, nil
}
func (*availableMetricsFixture) Rollback(context.Context) (monitoring.RollbackResult, error) {
	return monitoring.RollbackResult{}, nil
}

// convergedClusterFixture supplies ordinary component health independently of
// a workflow's scheduling decision.
func convergedClusterFixture(t *testing.T) (*platformv1alpha1.PostgresCluster, *cnpgv1.Cluster, client.Client) {
	t.Helper()
	scheme := newTestScheme()
	parent := newTestCluster("orders", "default")
	parent.Generation = 2
	parent.Finalizers = []string{PostgresClusterFinalizerName}
	parent.Spec = platformv1alpha1.PostgresClusterSpec{Images: pgtestfixture.Images(), Class: "orders-class", Instances: new(int32(3)), Storage: new(resource.MustParse("10Gi")), PostgresVersion: new("17"), PostgresMajorUpgradeConfig: &platformv1alpha1.PostgresMajorUpgradeConfig{Allow: new(true), Strategy: new(mvutypes.MajorUpgradeFlowPgUpgrade)}}
	parent.Status.Phase = new(string(readyClusterPhase))
	parent.Status.ObservedGeneration = new(int64(1))
	meta.SetStatusCondition(&parent.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready", ObservedGeneration: 1})
	class := &platformv1alpha1.PostgresClusterClass{Name: parent.Spec.Class, Spec: platformv1alpha1.PostgresClusterClassSpec{Provisioner: "postgresql.cnpg.io"}}
	sourceConfig := GetMergedConfig(class, parent)
	sourceConfig.Spec.PostgresVersion = new("17")
	secretName := parent.Name + defaultSecretSuffix
	blue, err := buildCNPGCluster(scheme, parent, sourceConfig, secretName, isPostgreSQLMetricsEnabled(parent, class))
	require.NoError(t, err)
	blue.UID = "blue-uid"
	newCNPGDefaultServerTLSPlan(parent, sourceConfig, blue.Name).ApplyToCNPG(&blue.Spec, blue.Name, blue.Namespace)
	blue.Status = cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy, Instances: 3, ReadyInstances: 3, CurrentPrimary: "orders-1", PGDataImageInfo: &cnpgv1.ImageInfo{MajorVersion: 17}, InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"orders-1", "orders-2", "orders-3"}}}
	blue.Status.Certificates.ServerTLSSecret = "orders-server"
	blue.Status.Certificates.ServerCASecret = "orders-ca"
	blue.Status.WriteService, blue.Status.ReadService = "orders-rw", "orders-ro"
	parent.Status.ProvisionerRef = &corev1.ObjectReference{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Cluster", Name: blue.Name, Namespace: blue.Namespace, UID: blue.UID}
	secret := &corev1.Secret{Name: secretName, Namespace: parent.Namespace, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}, Data: map[string][]byte{secretKeyPassword: []byte("test-password")}}
	ca := &corev1.Secret{Name: "orders-ca", Namespace: parent.Namespace, Data: map[string][]byte{defaultServerCACertKey: []byte("test-ca")}}
	c := indexedManagedRolesTestClient(scheme, parent, class, blue, secret, ca).WithStatusSubresource(parent, blue).WithInterceptorFuncs(interceptor.Funcs{Apply: func(ctx context.Context, c client.WithWatch, obj runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
		// Model the parameter-only SSA operation; the fake's CNPG defaulting
		// otherwise claims spec.instances for this unrelated field manager.
		key, params := postgreSQLParametersFromApplyConfiguration(t, obj)
		current := new(cnpgv1.Cluster)
		if err := c.Get(ctx, key, current); err != nil {
			return err
		}
		current.Spec.PostgresConfiguration.Parameters = params
		return c.Update(ctx, current)
	}}).Build()
	return parent, blue, c
}
