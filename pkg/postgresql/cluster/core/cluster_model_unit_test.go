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
package core

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"log/slog"
	"maps"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/google/go-cmp/cmp"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	structuraldefaulting "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/yaml"
)

func fakeClientWithPostgreSQLParameterApply(t *testing.T, scheme *runtime.Scheme, initiallyOwned map[client.ObjectKey][]string, objects ...client.Object) client.WithWatch {
	t.Helper()

	managedFields := map[client.ObjectKey][]metav1.ManagedFieldsEntry{}
	for _, obj := range objects {
		if _, ok := obj.(*cnpgv1.Cluster); ok && len(obj.GetManagedFields()) > 0 {
			managedFields[client.ObjectKeyFromObject(obj)] = obj.GetManagedFields()
		}
	}

	owned := make(map[client.ObjectKey]map[string]struct{}, len(initiallyOwned))
	for key, params := range initiallyOwned {
		owned[key] = make(map[string]struct{}, len(params))
		for _, param := range params {
			owned[key][param] = struct{}{}
		}
	}

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if err := c.Get(ctx, key, obj, opts...); err != nil {
					return err
				}
				if fields, ok := managedFields[key]; ok {
					if _, isCNPG := obj.(*cnpgv1.Cluster); isCNPG {
						obj.SetManagedFields(fields)
					}
				}
				return nil
			},
			Apply: func(ctx context.Context, c client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
				applyOptions := &client.ApplyOptions{}
				applyOptions.ApplyOptions(opts)
				require.Equal(t, postgresqlParametersFieldManager, applyOptions.FieldManager)
				require.Nil(t, applyOptions.Force, "postgresql parameter SSA must not force ownership")

				key, desiredParams := postgreSQLParametersFromApplyConfiguration(t, obj)
				current := &cnpgv1.Cluster{}
				if err := c.Get(ctx, key, current); err != nil {
					return err
				}

				ownedParams := owned[key]
				if ownedParams == nil {
					ownedParams = map[string]struct{}{}
				}

				nextParams := maps.Clone(current.Spec.PostgresConfiguration.Parameters)
				if nextParams == nil {
					nextParams = map[string]string{}
				}
				for param := range ownedParams {
					if _, stillDesired := desiredParams[param]; !stillDesired {
						delete(nextParams, param)
					}
				}
				for param, value := range desiredParams {
					nextParams[param] = value
				}

				nextOwnedParams := make(map[string]struct{}, len(desiredParams))
				for param := range desiredParams {
					nextOwnedParams[param] = struct{}{}
				}
				owned[key] = nextOwnedParams

				if maps.Equal(current.Spec.PostgresConfiguration.Parameters, nextParams) {
					return nil
				}

				current.Spec.PostgresConfiguration.Parameters = nextParams
				current.Generation++
				return c.Update(ctx, current)
			},
		}).
		Build()
}

func postgreSQLParametersFromApplyConfiguration(t *testing.T, obj runtime.ApplyConfiguration) (client.ObjectKey, map[string]string) {
	t.Helper()

	applyObject, ok := obj.(interface {
		GetName() string
		GetNamespace() string
		UnstructuredContent() map[string]any
	})
	require.True(t, ok, "postgresql parameter apply payload must be unstructured-backed")

	rawParams, found, err := unstructured.NestedFieldNoCopy(applyObject.UnstructuredContent(), "spec", "postgresql", "parameters")
	require.NoError(t, err)
	if !found {
		return client.ObjectKey{Name: applyObject.GetName(), Namespace: applyObject.GetNamespace()}, map[string]string{}
	}

	params := map[string]string{}
	switch typedParams := rawParams.(type) {
	case map[string]string:
		maps.Copy(params, typedParams)
	case map[string]any:
		for key, value := range typedParams {
			stringValue, ok := value.(string)
			require.True(t, ok, "postgresql parameter %q value must be a string", key)
			params[key] = stringValue
		}
	default:
		require.Failf(t, "unexpected postgresql parameters payload", "got %T", rawParams)
	}

	return client.ObjectKey{Name: applyObject.GetName(), Namespace: applyObject.GetNamespace()}, params
}

func postgreSQLParametersFieldsRaw(t *testing.T, params ...string) []byte {
	t.Helper()

	parameterFields := map[string]any{}
	for _, param := range params {
		parameterFields["f:"+param] = map[string]any{}
	}
	raw, err := json.Marshal(map[string]any{
		"f:spec": map[string]any{
			"f:postgresql": map[string]any{
				"f:parameters": parameterFields,
			},
		},
	})
	require.NoError(t, err)
	return raw
}

func TestClusterModelActuatePatchesPrimaryUpdateMethodDrift(t *testing.T) {
	t.Parallel()

	// Arrange
	scheme := newTestScheme()

	instances := int32(3)
	version := "15.13"
	storageSize := resource.MustParse("10Gi")
	restart := "restart"
	switchover := "switchover"

	baseSpec := &platformv1alpha1.PostgresClusterSpec{
		Instances:        &instances,
		PostgresVersion:  &version,
		Storage:          &storageSize,
		Resources:        &corev1.ResourceRequirements{},
		PostgreSQLConfig: map[string]string{},
		PgHBA:            []string{},
	}
	currentConfig := &MergedConfig{
		Spec: baseSpec.DeepCopy(),
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: &restart},
	}
	desiredConfig := &MergedConfig{
		Spec: baseSpec.DeepCopy(),
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: &switchover},
	}

	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
	}
	clusterClass := &platformv1alpha1.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class"},
	}
	existingCNPG := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace},
		Spec:       buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, currentConfig, "pg1", "pg1-secret", false),
	}
	events := &captureEventEmitter{}
	c := fakeClientWithPostgreSQLParameterApply(t, scheme, nil, existingCNPG)
	contracts := &reconcileContracts{Secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret", Namespace: "default"}}}
	model := newClusterModel(c, scheme, events, nil, cluster, clusterClass, desiredConfig, contracts)

	// Act
	require.NoError(t, model.Reconcile(context.Background()))

	// Assert
	require.True(t, model.cnpgPatch.requiresPhaseGate())
	assert.False(t, model.cnpgCreated)
	updated := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(existingCNPG), updated))
	assert.Equal(t, cnpgv1.PrimaryUpdateMethodSwitchover, updated.Spec.PrimaryUpdateMethod)
	assert.Contains(t, events.normals, EventClusterUpdateStarted+":CNPG cluster spec updated for PostgresCluster pg1, waiting for healthy state")
}

func TestClusterModelBlocksMajorVersionDriftWithoutUpgradeConfig(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	currentVersion := "15.10"
	requestedVersion := "18"
	instances := int32(1)
	storageSize := resource.MustParse("10Gi")

	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec: platformv1alpha1.PostgresClusterSpec{
			PostgresVersion: &requestedVersion,
		},
	}
	clusterClass := &platformv1alpha1.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class"},
	}
	currentConfig := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &currentVersion,
			Storage:          &storageSize,
			Resources:        &corev1.ResourceRequirements{},
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
		},
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}
	desiredConfig := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &requestedVersion,
			Storage:          &storageSize,
			Resources:        &corev1.ResourceRequirements{},
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
		},
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}
	existingCNPG := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace},
		Spec:       buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, currentConfig, cluster.Name, "pg1-secret", false),
		Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy, Instances: int(instances), ReadyInstances: int(instances)},
	}
	require.NoError(t, ctrl.SetControllerReference(cluster, existingCNPG, scheme))

	c := fakeClientWithPostgreSQLParameterApply(t, scheme, nil, existingCNPG)
	model := newClusterModel(
		c,
		scheme,
		noopEventEmitter{},
		nil,
		cluster,
		clusterClass,
		desiredConfig,
		&reconcileContracts{Secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret", Namespace: "default"}}},
	)

	require.NoError(t, model.Reconcile(context.Background()))

	updated := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(existingCNPG), updated))
	assert.Equal(t, "ghcr.io/cloudnative-pg/postgresql:15.10", updated.Spec.ImageName)

	health, err := model.Observe(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Pending, health.State)
	assert.Equal(t, pendingClusterPhase, health.Phase)
	assert.Equal(t, clusterReady, health.Condition)
	assert.Equal(t, reasonMajorUpgradeConfigRequired, health.Reason)
	assert.Equal(t, "Detected requested PostgreSQL major version change from 15.10 to 18. Set spec.postgresMajorUpgradeConfig.allow=true to start the major upgrade workflow.", health.Message)
}

func TestClusterModelBlocksMajorVersionDowngrade(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	currentVersion := "18"
	requestedVersion := "15.10"
	instances := int32(1)
	storageSize := resource.MustParse("10Gi")

	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec: platformv1alpha1.PostgresClusterSpec{
			PostgresVersion: &requestedVersion,
			PostgresMajorUpgradeConfig: &platformv1alpha1.PostgresMajorUpgradeConfig{
				Allow: ptr.To(true),
			},
		},
	}
	clusterClass := &platformv1alpha1.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class"},
	}
	currentConfig := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &currentVersion,
			Storage:          &storageSize,
			Resources:        &corev1.ResourceRequirements{},
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
		},
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}
	desiredConfig := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &requestedVersion,
			Storage:          &storageSize,
			Resources:        &corev1.ResourceRequirements{},
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
		},
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}
	existingCNPG := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace},
		Spec:       buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, currentConfig, cluster.Name, "pg1-secret", false),
		Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy, Instances: int(instances), ReadyInstances: int(instances)},
	}
	require.NoError(t, ctrl.SetControllerReference(cluster, existingCNPG, scheme))

	c := fakeClientWithPostgreSQLParameterApply(t, scheme, nil, existingCNPG)
	model := newClusterModel(
		c,
		scheme,
		noopEventEmitter{},
		nil,
		cluster,
		clusterClass,
		desiredConfig,
		&reconcileContracts{Secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret", Namespace: "default"}}},
	)

	require.NoError(t, model.Reconcile(context.Background()))

	updated := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(existingCNPG), updated))
	assert.Equal(t, "ghcr.io/cloudnative-pg/postgresql:18", updated.Spec.ImageName)

	health, err := model.Observe(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Pending, health.State)
	assert.Equal(t, pendingClusterPhase, health.Phase)
	assert.Equal(t, clusterReady, health.Condition)
	assert.Equal(t, reasonMajorDowngradeUnsupported, health.Reason)
	assert.Equal(t, "Detected requested PostgreSQL major version downgrade from 18 to 15.10. Downgrades are not supported by reconciliation; restore from backup or create a new cluster.", health.Message)
}

// TestClusterModelHoldsMajorVersionUpgradeWhenAllowed verifies that allow=true does
// NOT license the provisioner to bump the CNPG image itself. The provisioner must
// hold (Pending) and leave the image untouched so the major-upgrade use case owns
// the backup -> preflight -> patch -> verify -> finalize transition. Patching here
// would skip the orchestrated workflow and could jump multiple majors at once.
//
// It also asserts that even while held, Observe still publishes
// status.CurrentPgVersion from the live CNPG version. The major-upgrade use case
// now reads its source version straight from CNPG (PGDataImageInfo.MajorVersion),
// so it no longer depends on this projection to activate; the projection remains a
// user-facing observability field, and this guards that the held provisioner keeps
// it current.
func TestClusterModelHoldsMajorVersionUpgradeWhenAllowed(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	currentVersion := "15.10"
	requestedVersion := "18"
	instances := int32(1)
	storageSize := resource.MustParse("10Gi")

	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec: platformv1alpha1.PostgresClusterSpec{
			PostgresVersion: &requestedVersion,
			PostgresMajorUpgradeConfig: &platformv1alpha1.PostgresMajorUpgradeConfig{
				Allow: ptr.To(true),
			},
		},
	}
	clusterClass := &platformv1alpha1.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class"},
	}
	currentConfig := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &currentVersion,
			Storage:          &storageSize,
			Resources:        &corev1.ResourceRequirements{},
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
		},
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}
	desiredConfig := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &requestedVersion,
			Storage:          &storageSize,
			Resources:        &corev1.ResourceRequirements{},
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
		},
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}
	existingCNPG := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace},
		Spec:       buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, currentConfig, cluster.Name, "pg1-secret", false),
		Status: cnpgv1.ClusterStatus{
			Phase:           cnpgv1.PhaseHealthy,
			Instances:       int(instances),
			ReadyInstances:  int(instances),
			PGDataImageInfo: &cnpgv1.ImageInfo{Image: "ghcr.io/cloudnative-pg/postgresql:15.10", MajorVersion: 15},
		},
	}
	require.NoError(t, ctrl.SetControllerReference(cluster, existingCNPG, scheme))

	c := fakeClientWithPostgreSQLParameterApply(t, scheme, nil, existingCNPG)
	model := newClusterModel(
		c,
		scheme,
		noopEventEmitter{},
		nil,
		cluster,
		clusterClass,
		desiredConfig,
		&reconcileContracts{Secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret", Namespace: "default"}}},
	)

	require.NoError(t, model.Reconcile(context.Background()))

	updated := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(existingCNPG), updated))
	assert.Equal(t, "ghcr.io/cloudnative-pg/postgresql:15.10", updated.Spec.ImageName,
		"allow=true must not let the provisioner patch the major bump itself")

	health, err := model.Observe(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Pending, health.State)
	assert.Equal(t, pendingClusterPhase, health.Phase)
	assert.Equal(t, clusterReady, health.Condition)
	assert.Equal(t, reasonMajorUpgradePending, health.Reason)
	assert.Equal(t, "Major version upgrade from 15.10 to 18 is allowed; holding the CNPG image until the major upgrade workflow takes ownership.", health.Message)

	// The held pass still publishes the observable current version projection.
	assert.Equal(t, "15", cluster.Status.CurrentPgVersion,
		"held provisioner must still publish CurrentPgVersion as an observability projection")
}

func TestClusterModelAppliesPostgreSQLParametersWithSSAOwnership(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	instances := int32(1)
	version := "16"
	storageSize := resource.MustParse("10Gi")

	currentSpec := &platformv1alpha1.PostgresClusterSpec{
		Instances:       &instances,
		PostgresVersion: &version,
		Storage:         &storageSize,
		Resources:       &corev1.ResourceRequirements{},
		PostgreSQLConfig: map[string]string{
			"shared_buffers":  "256MB",
			"max_connections": "200",
		},
		PgHBA: []string{},
	}
	desiredSpec := currentSpec.DeepCopy()
	desiredSpec.PostgreSQLConfig = map[string]string{
		"shared_buffers": "256MB",
	}

	currentConfig := &MergedConfig{
		Spec: currentSpec,
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}
	desiredConfig := &MergedConfig{
		Spec: desiredSpec,
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}

	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
	}
	clusterClass := &platformv1alpha1.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class"},
	}
	existingCNPG := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cluster.Name,
			Namespace:   cluster.Namespace,
			Annotations: map[string]string{},
		},
		Spec: buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, currentConfig, cluster.Name, "pg1-secret", false),
	}
	require.NoError(t, ctrl.SetControllerReference(cluster, existingCNPG, scheme))
	existingCNPG.Spec.PostgresConfiguration.Parameters["cnpg_injected"] = "keep-me"

	c := fakeClientWithPostgreSQLParameterApply(t, scheme, map[client.ObjectKey][]string{
		client.ObjectKeyFromObject(existingCNPG): {"shared_buffers", "max_connections"},
	}, existingCNPG)
	contracts := &reconcileContracts{Secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret", Namespace: "default"}}}
	model := newClusterModel(c, scheme, &captureEventEmitter{}, nil, cluster, clusterClass, desiredConfig, contracts)

	require.NoError(t, model.Reconcile(context.Background()))

	updated := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(existingCNPG), updated))
	assert.Equal(t, map[string]string{
		"shared_buffers": "256MB",
		"cnpg_injected":  "keep-me",
	}, updated.Spec.PostgresConfiguration.Parameters)
}

func TestClusterModelAdoptsAndPrunesLegacyPostgreSQLParameters(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	instances := int32(1)
	version := "16"
	storageSize := resource.MustParse("10Gi")

	currentSpec := &platformv1alpha1.PostgresClusterSpec{
		Instances:       &instances,
		PostgresVersion: &version,
		Storage:         &storageSize,
		Resources:       &corev1.ResourceRequirements{},
		PostgreSQLConfig: map[string]string{
			"shared_buffers":  "256MB",
			"max_connections": "200",
		},
		PgHBA: []string{},
	}
	desiredSpec := currentSpec.DeepCopy()
	desiredSpec.PostgreSQLConfig = map[string]string{
		"shared_buffers": "256MB",
	}

	currentConfig := &MergedConfig{
		Spec: currentSpec,
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}
	desiredConfig := &MergedConfig{
		Spec: desiredSpec,
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}

	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
	}
	clusterClass := &platformv1alpha1.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class"},
	}
	existingCNPG := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
			ManagedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:    "manager",
					Operation:  metav1.ManagedFieldsOperationUpdate,
					APIVersion: cnpgv1.SchemeGroupVersion.String(),
					FieldsType: "FieldsV1",
					FieldsV1: &metav1.FieldsV1{Raw: postgreSQLParametersFieldsRaw(t,
						"shared_buffers",
						"max_connections",
						"application_name",
						"archive_mode",
					)},
				},
				{
					Manager:    "external-postgresql-parameters",
					Operation:  metav1.ManagedFieldsOperationApply,
					APIVersion: cnpgv1.SchemeGroupVersion.String(),
					FieldsType: "FieldsV1",
					FieldsV1:   &metav1.FieldsV1{Raw: postgreSQLParametersFieldsRaw(t, "application_name")},
				},
			},
		},
		Spec: buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, currentConfig, cluster.Name, "pg1-secret", false),
	}
	require.NoError(t, ctrl.SetControllerReference(cluster, existingCNPG, scheme))
	existingCNPG.Spec.PostgresConfiguration.Parameters["application_name"] = "keep-me"
	existingCNPG.Spec.PostgresConfiguration.Parameters["archive_mode"] = "on"

	events := &captureEventEmitter{}
	c := fakeClientWithPostgreSQLParameterApply(t, scheme, nil, existingCNPG)
	contracts := &reconcileContracts{Secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret", Namespace: "default"}}}
	model := newClusterModel(c, scheme, events, nil, cluster, clusterClass, desiredConfig, contracts)

	require.NoError(t, model.Reconcile(context.Background()))

	updated := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(existingCNPG), updated))
	assert.Equal(t, map[string]string{
		"shared_buffers":   "256MB",
		"application_name": "keep-me",
		"archive_mode":     "on",
	}, updated.Spec.PostgresConfiguration.Parameters)
	assert.True(t, model.cnpgPatch.requiresPhaseGate())
	assert.Contains(t, events.normals, EventClusterUpdateStarted+":CNPG cluster spec updated for PostgresCluster pg1, waiting for healthy state")
}

func TestGetMergedConfig(t *testing.T) {
	classInstances := int32(1)
	classVersion := "17"
	classStorage := resource.MustParse("50Gi")
	baseClass := &platformv1alpha1.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "standard"},
		Spec: platformv1alpha1.PostgresClusterClassSpec{
			Config: &platformv1alpha1.PostgresClusterClassConfig{
				Instances:        &classInstances,
				PostgresVersion:  &classVersion,
				Storage:          &classStorage,
				Resources:        &corev1.ResourceRequirements{},
				PostgreSQLConfig: map[string]string{"shared_buffers": "128MB"},
				PgHBA:            []string{"host all all 0.0.0.0/0 md5"},
			},
			CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("switchover")},
		},
	}

	t.Run("cluster spec overrides class defaults", func(t *testing.T) {
		overrideInstances := int32(5)
		overrideVersion := "18"
		overrideStorage := resource.MustParse("100Gi")
		cluster := &platformv1alpha1.PostgresCluster{
			Spec: platformv1alpha1.PostgresClusterSpec{
				Instances:        &overrideInstances,
				PostgresVersion:  &overrideVersion,
				Storage:          &overrideStorage,
				PostgreSQLConfig: map[string]string{"max_connections": "200"},
				PgHBA:            []string{"hostssl all all 0.0.0.0/0 scram-sha-256"},
			},
		}

		cfg := GetMergedConfig(baseClass, cluster)

		require.Empty(t, ValidateMergedConfig(cfg, baseClass.Name))
		assert.Equal(t, int32(5), *cfg.Spec.Instances)
		assert.Equal(t, "18", *cfg.Spec.PostgresVersion)
		assert.Equal(t, "100Gi", cfg.Spec.Storage.String())
		assert.Equal(t, "200", cfg.Spec.PostgreSQLConfig["max_connections"])
		assert.Equal(t, "hostssl all all 0.0.0.0/0 scram-sha-256", cfg.Spec.PgHBA[0])
	})

	t.Run("class defaults fill in nil cluster fields", func(t *testing.T) {
		cluster := &platformv1alpha1.PostgresCluster{
			Spec: platformv1alpha1.PostgresClusterSpec{},
		}

		cfg := GetMergedConfig(baseClass, cluster)

		require.Empty(t, ValidateMergedConfig(cfg, baseClass.Name))
		assert.Equal(t, int32(1), *cfg.Spec.Instances)
		assert.Equal(t, "17", *cfg.Spec.PostgresVersion)
		assert.Equal(t, "50Gi", cfg.Spec.Storage.String())
		assert.Equal(t, "128MB", cfg.Spec.PostgreSQLConfig["shared_buffers"])
	})

	t.Run("returns error when required fields missing from both", func(t *testing.T) {
		emptyClass := &platformv1alpha1.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "empty"},
			Spec:       platformv1alpha1.PostgresClusterClassSpec{},
		}
		cluster := &platformv1alpha1.PostgresCluster{
			Spec: platformv1alpha1.PostgresClusterSpec{},
		}

		cfg := GetMergedConfig(emptyClass, cluster)

		require.NotEmpty(t, ValidateMergedConfig(cfg, emptyClass.Name))
	})

	t.Run("CNPG config comes from class not cluster", func(t *testing.T) {
		cluster := &platformv1alpha1.PostgresCluster{
			Spec: platformv1alpha1.PostgresClusterSpec{},
		}

		cfg := GetMergedConfig(baseClass, cluster)

		require.Empty(t, ValidateMergedConfig(cfg, baseClass.Name))
		require.NotNil(t, cfg.CNPG)
		assert.Equal(t, "switchover", *cfg.CNPG.PrimaryUpdateMethod)
	})

	t.Run("rejects postgresqlConfig containing CNPG fixed parameters", func(t *testing.T) {
		badClass := &platformv1alpha1.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "bad"},
			Spec: platformv1alpha1.PostgresClusterClassSpec{
				Config: &platformv1alpha1.PostgresClusterClassConfig{
					Instances:        &classInstances,
					PostgresVersion:  &classVersion,
					Storage:          &classStorage,
					Resources:        &corev1.ResourceRequirements{},
					PostgreSQLConfig: map[string]string{"ssl": "on"},
				},
			},
		}
		cluster := &platformv1alpha1.PostgresCluster{Spec: platformv1alpha1.PostgresClusterSpec{}}

		cfg := GetMergedConfig(badClass, cluster)
		errs := ValidateMergedConfig(cfg, badClass.Name)
		require.NotEmpty(t, errs)
		assert.Contains(t, errs[0].Error(), "postgresqlConfig must not set CNPG-managed parameters")
		assert.Contains(t, errs[0].Error(), "ssl")

		clusterOverride := &platformv1alpha1.PostgresCluster{
			Spec: platformv1alpha1.PostgresClusterSpec{
				Instances:       &classInstances,
				PostgresVersion: &classVersion,
				Storage:         &classStorage,
				PostgreSQLConfig: map[string]string{
					"shared_buffers": "128MB",
					"ssl_cert_file":  "/tmp/x.crt",
				},
				Resources: &corev1.ResourceRequirements{},
			},
		}
		cfg2 := GetMergedConfig(baseClass, clusterOverride)
		errs2 := ValidateMergedConfig(cfg2, baseClass.Name)
		require.NotEmpty(t, errs2)
		assert.Contains(t, errs2[0].Error(), "ssl_cert_file")
	})

	t.Run("nil maps and slices initialized to safe zero values", func(t *testing.T) {
		classWithNoMaps := &platformv1alpha1.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "minimal"},
			Spec: platformv1alpha1.PostgresClusterClassSpec{
				Config: &platformv1alpha1.PostgresClusterClassConfig{
					Instances:       &classInstances,
					PostgresVersion: &classVersion,
					Storage:         &classStorage,
				},
			},
		}
		cluster := &platformv1alpha1.PostgresCluster{
			Spec: platformv1alpha1.PostgresClusterSpec{},
		}

		cfg := GetMergedConfig(classWithNoMaps, cluster)

		require.Empty(t, ValidateMergedConfig(cfg, classWithNoMaps.Name))
		assert.NotNil(t, cfg.Spec.PostgreSQLConfig)
		assert.NotNil(t, cfg.Spec.PgHBA)
		assert.NotNil(t, cfg.Spec.Resources)
	})

	t.Run("rejects 6-field cron schedule", func(t *testing.T) {
		enabled := true
		sixField := "0 */5 * * * *"
		cluster := &platformv1alpha1.PostgresCluster{
			Spec: platformv1alpha1.PostgresClusterSpec{
				Backup: &platformv1alpha1.BackupConfig{
					Enabled:  &enabled,
					Schedule: &sixField,
				},
			},
		}

		cfg := GetMergedConfig(baseClass, cluster)
		errs := ValidateMergedConfig(cfg, baseClass.Name)

		require.NotEmpty(t, errs)
		assert.Contains(t, errs[0].Field, "schedule")
		assert.Contains(t, errs[0].Error(), "5-field")
	})

	t.Run("accepts valid 5-field cron schedule", func(t *testing.T) {
		enabled := true
		fiveField := "*/5 * * * *"
		cluster := &platformv1alpha1.PostgresCluster{
			Spec: platformv1alpha1.PostgresClusterSpec{
				Backup: &platformv1alpha1.BackupConfig{
					Enabled:  &enabled,
					Schedule: &fiveField,
				},
			},
		}

		cfg := GetMergedConfig(baseClass, cluster)
		errs := ValidateMergedConfig(cfg, baseClass.Name)

		require.Empty(t, errs)
	})
}

func TestBuildCNPGClusterSpec(t *testing.T) {
	version := "18"
	instances := int32(3)
	storage := resource.MustParse("50Gi")
	primaryUpdateMethod := "switchover"
	cfg := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{
			PostgresVersion: &version,
			Instances:       &instances,
			Storage:         &storage,
			PostgreSQLConfig: map[string]string{
				"shared_buffers":  "256MB",
				"max_connections": "200",
			},
			PgHBA: []string{
				"hostssl all all 0.0.0.0/0 scram-sha-256",
				"host replication all 10.0.0.0/8 md5",
			},
			Resources: &corev1.ResourceRequirements{},
		},
		CNPG: &platformv1alpha1.CNPGConfig{
			PrimaryUpdateMethod: &primaryUpdateMethod,
		},
	}

	spec := buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "c1", "my-secret", false)

	assert.Equal(t, "ghcr.io/cloudnative-pg/postgresql:18", spec.ImageName)
	assert.Equal(t, 3, spec.Instances)
	require.NotNil(t, spec.SuperuserSecret)
	assert.Equal(t, "my-secret", spec.SuperuserSecret.Name)
	assert.Equal(t, "my-secret", spec.Bootstrap.InitDB.Secret.Name)
	require.NotNil(t, spec.EnableSuperuserAccess)
	assert.True(t, *spec.EnableSuperuserAccess)
	assert.Equal(t, "postgres", spec.Bootstrap.InitDB.Database)
	assert.Equal(t, "postgres", spec.Bootstrap.InitDB.Owner)
	assert.Equal(t, "50Gi", spec.StorageConfiguration.Size)
	assert.Equal(t, "256MB", spec.PostgresConfiguration.Parameters["shared_buffers"])
	assert.Equal(t, "200", spec.PostgresConfiguration.Parameters["max_connections"])
	assert.Equal(t, cnpgv1.PrimaryUpdateMethodSwitchover, spec.PrimaryUpdateMethod)
	require.Len(t, spec.PostgresConfiguration.PgHBA, 2)
	assert.Equal(t, "hostssl all all 0.0.0.0/0 scram-sha-256", spec.PostgresConfiguration.PgHBA[0])
	assert.Equal(t, "host replication all 10.0.0.0/8 md5", spec.PostgresConfiguration.PgHBA[1])
	require.NotNil(t, spec.InheritedMetadata)
	assert.Empty(t, spec.InheritedMetadata.Annotations)

	t.Run("adds postgres scrape annotations when enabled", func(t *testing.T) {
		spec := buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "c1", "my-secret", true)

		require.NotNil(t, spec.InheritedMetadata)
		assert.Equal(t, "true", spec.InheritedMetadata.Annotations[prometheusScrapeAnnotation])
		assert.Equal(t, metricsPath, spec.InheritedMetadata.Annotations[prometheusPathAnnotation])
		assert.Equal(t, postgresMetricsPortString, spec.InheritedMetadata.Annotations[prometheusPortAnnotation])
	})

	t.Run("preserves unowned fields from live spec", func(t *testing.T) {
		managedRoles := []cnpgv1.RoleConfiguration{
			{Name: "app_user", Ensure: cnpgv1.EnsurePresent},
			{Name: "app_admin", Ensure: cnpgv1.EnsurePresent},
		}

		liveCluster := cnpgv1.ClusterSpec{Managed: &cnpgv1.ManagedConfiguration{Roles: managedRoles}}
		spec := buildCNPGClusterSpec(liveCluster, cfg, "c1", "my-secret", true)

		require.NotNil(t, spec.Managed)
		assert.Equal(t, managedRoles, spec.Managed.Roles)
		assert.Equal(t, "ghcr.io/cloudnative-pg/postgresql:18", spec.ImageName)
		assert.Equal(t, 3, spec.Instances)
		require.NotNil(t, spec.SuperuserSecret)
		assert.Equal(t, "my-secret", spec.SuperuserSecret.Name)
	})

	t.Run("sets backup when enabled and volume snapshot configured", func(t *testing.T) {
		t.Parallel()
		enabled := true
		className := "csi-snapclass"
		specCopy := *cfg.Spec
		specCopy.Backup = &platformv1alpha1.BackupConfig{Enabled: &enabled}
		cnpgCopy := *cfg.CNPG
		cnpgCopy.Backup = &platformv1alpha1.CNPGBackupConfig{
			VolumeSnapshot: &platformv1alpha1.CNPGVolumeSnapshotConfig{
				ClassName: &className,
			},
		}
		backupCfg := MergedConfig{Spec: &specCopy, CNPG: &cnpgCopy}

		spec := buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, &backupCfg, "c1", "my-secret", false)

		require.NotNil(t, spec.Backup)
		require.NotNil(t, spec.Backup.VolumeSnapshot)
		assert.Equal(t, className, spec.Backup.VolumeSnapshot.ClassName)
	})

	t.Run("clears stale backup from live spec when backup is disabled", func(t *testing.T) {
		t.Parallel()
		staleBackup := &cnpgv1.BackupConfiguration{
			VolumeSnapshot: &cnpgv1.VolumeSnapshotConfiguration{ClassName: "old-snapclass"},
		}
		liveSpec := cnpgv1.ClusterSpec{Backup: staleBackup}

		disabled := false
		specCopy := *cfg.Spec
		specCopy.Backup = &platformv1alpha1.BackupConfig{Enabled: &disabled}
		cnpgCopy := *cfg.CNPG
		disabledCfg := MergedConfig{Spec: &specCopy, CNPG: &cnpgCopy}

		spec := buildCNPGClusterSpec(liveSpec, &disabledCfg, "c1", "my-secret", false)

		assert.Nil(t, spec.Backup, "stale backup config must be cleared when backup is disabled")
	})
}

func TestBuildCNPGCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	platformv1alpha1.AddToScheme(scheme)
	cnpgv1.AddToScheme(scheme)

	instances := int32(3)
	version := "18"
	storage := resource.MustParse("50Gi")
	postgresCluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "db-ns",
			UID:       "pg-uid",
		},
	}
	cfg := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &version,
			Storage:          &storage,
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
			Resources:        &corev1.ResourceRequirements{},
		},
		CNPG: &platformv1alpha1.CNPGConfig{
			PrimaryUpdateMethod: ptr.To("restart"),
		},
	}

	cluster, err := buildCNPGCluster(scheme, postgresCluster, cfg, "my-secret", true)

	require.NoError(t, err)
	assert.Equal(t, "my-cluster", cluster.Name)
	assert.Equal(t, "db-ns", cluster.Namespace)
	require.Len(t, cluster.OwnerReferences, 1)
	assert.Equal(t, "pg-uid", string(cluster.OwnerReferences[0].UID))
	assert.Equal(t, 3, cluster.Spec.Instances)
	require.NotNil(t, cluster.Spec.InheritedMetadata)
	assert.Equal(t, postgresMetricsPortString, cluster.Spec.InheritedMetadata.Annotations[prometheusPortAnnotation])
}

func TestBuildPostgreSQLParametersPatchUsesEmptyMapForNilParameters(t *testing.T) {
	t.Parallel()

	cluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
	}

	patch := buildPostgreSQLParametersPatch(cluster, nil)
	content := patch.(*unstructured.Unstructured).UnstructuredContent()

	rawParams, found, err := unstructured.NestedFieldNoCopy(content, "spec", "postgresql", "parameters")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, map[string]string{}, rawParams)
}

func TestPostgreSQLParametersWithLegacyAdoption(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		cnpgParams    map[string]string
		managedFields []metav1.ManagedFieldsEntry
		desired       map[string]string
		want          map[string]string
	}{
		{
			name: "adopts stale legacy update-owned parameter before pruning",
			cnpgParams: map[string]string{
				"shared_buffers":  "256MB",
				"max_connections": "200",
			},
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   "manager",
					Operation: metav1.ManagedFieldsOperationUpdate,
					FieldsV1:  &metav1.FieldsV1{Raw: postgreSQLParametersFieldsRaw(t, "shared_buffers", "max_connections")},
				},
			},
			desired: map[string]string{
				"shared_buffers": "256MB",
			},
			want: map[string]string{
				"shared_buffers":  "256MB",
				"max_connections": "200",
			},
		},
		{
			name: "does not adopt externally apply-owned parameter",
			cnpgParams: map[string]string{
				"shared_buffers":   "256MB",
				"application_name": "keep-me",
			},
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   "manager",
					Operation: metav1.ManagedFieldsOperationUpdate,
					FieldsV1:  &metav1.FieldsV1{Raw: postgreSQLParametersFieldsRaw(t, "shared_buffers", "application_name")},
				},
				{
					Manager:   "external-postgresql-parameters",
					Operation: metav1.ManagedFieldsOperationApply,
					FieldsV1:  &metav1.FieldsV1{Raw: postgreSQLParametersFieldsRaw(t, "application_name")},
				},
			},
			desired: map[string]string{
				"shared_buffers": "256MB",
			},
			want: nil,
		},
		{
			name: "does not adopt CNPG managed default parameter",
			cnpgParams: map[string]string{
				"shared_buffers": "256MB",
				"archive_mode":   "on",
			},
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   "manager",
					Operation: metav1.ManagedFieldsOperationUpdate,
					FieldsV1:  &metav1.FieldsV1{Raw: postgreSQLParametersFieldsRaw(t, "shared_buffers", "archive_mode")},
				},
			},
			desired: map[string]string{
				"shared_buffers": "256MB",
			},
			want: nil,
		},
		{
			name: "does not adopt when no legacy update ownership exists",
			cnpgParams: map[string]string{
				"shared_buffers":  "256MB",
				"max_connections": "200",
			},
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   postgresqlParametersFieldManager,
					Operation: metav1.ManagedFieldsOperationApply,
					FieldsV1:  &metav1.FieldsV1{Raw: postgreSQLParametersFieldsRaw(t, "shared_buffers")},
				},
			},
			desired: map[string]string{
				"shared_buffers": "256MB",
			},
			want: nil,
		},
		{
			name: "does not adopt parameter updated by non-legacy manager",
			cnpgParams: map[string]string{
				"shared_buffers":  "256MB",
				"max_connections": "200",
			},
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   "kubectl-patch",
					Operation: metav1.ManagedFieldsOperationUpdate,
					FieldsV1:  &metav1.FieldsV1{Raw: postgreSQLParametersFieldsRaw(t, "max_connections")},
				},
			},
			desired: map[string]string{
				"shared_buffers": "256MB",
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{ManagedFields: tt.managedFields},
				Spec: cnpgv1.ClusterSpec{
					PostgresConfiguration: cnpgv1.PostgresConfiguration{Parameters: tt.cnpgParams},
				},
			}

			got := postgreSQLParametersWithLegacyAdoption(cluster, tt.desired)

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeCNPGClusterSpec(t *testing.T) {
	tests := []struct {
		name     string
		spec     cnpgv1.ClusterSpec
		expected normalizedCNPGClusterSpec
	}{
		{
			name: "basic fields are copied",
			spec: cnpgv1.ClusterSpec{
				ImageName:            "ghcr.io/cloudnative-pg/postgresql:18",
				Instances:            3,
				StorageConfiguration: cnpgv1.StorageConfiguration{Size: "10Gi"},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "ghcr.io/cloudnative-pg/postgresql:18",
				Instances:           3,
				PrimaryUpdateMethod: "",
				StorageSize:         "10Gi",
			},
		},
		{
			name: "image digest suffix stripped for drift detection",
			spec: cnpgv1.ClusterSpec{
				ImageName:            "ghcr.io/cloudnative-pg/postgresql:18@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Instances:            1,
				StorageConfiguration: cnpgv1.StorageConfiguration{Size: "10Gi"},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "ghcr.io/cloudnative-pg/postgresql:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
				StorageSize:         "10Gi",
			},
		},
		{
			name: "primary update method is included in drift detection",
			spec: cnpgv1.ClusterSpec{
				ImageName:           "img:18",
				Instances:           3,
				PrimaryUpdateMethod: cnpgv1.PrimaryUpdateMethodSwitchover,
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "img:18",
				Instances:           3,
				PrimaryUpdateMethod: string(cnpgv1.PrimaryUpdateMethodSwitchover),
			},
		},
		{
			name: "postgresql parameters are excluded from normal drift comparison",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				PostgresConfiguration: cnpgv1.PostgresConfiguration{
					Parameters: map[string]string{
						"shared_buffers":  "256MB",
						"max_connections": "200",
						"cnpg_injected":   "should-not-appear",
					},
				},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
			},
		},
		{
			name: "unowned postgresql parameters are excluded from normal drift comparison",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				PostgresConfiguration: cnpgv1.PostgresConfiguration{
					Parameters: map[string]string{"cnpg_injected": "val"},
				},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
			},
		},
		{
			name: "missing postgresql parameter is excluded from normal drift comparison",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				PostgresConfiguration: cnpgv1.PostgresConfiguration{
					Parameters: map[string]string{
						"shared_buffers": "256MB",
					},
				},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
			},
		},
		{
			name: "empty parameter value remains present",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				PostgresConfiguration: cnpgv1.PostgresConfiguration{
					Parameters: map[string]string{
						"application_name": "",
					},
				},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
			},
		},
		{
			name: "PgHBA included when non-empty",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				PostgresConfiguration: cnpgv1.PostgresConfiguration{
					PgHBA: []string{"hostssl all all 0.0.0.0/0 scram-sha-256"},
				},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
				PgHBA:               []string{"hostssl all all 0.0.0.0/0 scram-sha-256"},
			},
		},
		{
			name: "empty PgHBA is excluded",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				PostgresConfiguration: cnpgv1.PostgresConfiguration{
					PgHBA: []string{},
				},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
			},
		},
		{
			name: "bootstrap populates database and owner",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				Bootstrap: &cnpgv1.BootstrapConfiguration{
					InitDB: &cnpgv1.BootstrapInitDB{
						Database: "mydb",
						Owner:    "admin",
					},
				},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
				DefaultDatabase:     "mydb",
				Owner:               "admin",
				BootstrapType:       "initdb",
			},
		},
		{
			name: "inherited annotations included when non-empty",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				InheritedMetadata: &cnpgv1.EmbeddedObjectMetadata{
					Annotations: map[string]string{
						prometheusScrapeAnnotation: "true",
						prometheusPortAnnotation:   postgresMetricsPortString,
					},
				},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
				InheritedAnnotations: map[string]string{
					prometheusScrapeAnnotation: "true",
					prometheusPortAnnotation:   postgresMetricsPortString,
				},
			},
		},
		{
			name: "recovery bootstrap sets BootstrapType recovery",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				Bootstrap: &cnpgv1.BootstrapConfiguration{
					Recovery: &cnpgv1.BootstrapRecovery{},
				},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
				BootstrapType:       "recovery",
				// A bare recovery stanza (no source/externalCluster/target) still captures an empty
				// recovery spec so the wiring participates in drift detection once populated.
				Recovery: &normalizedRecoverySpec{},
			},
		},
		{
			name: "recovery bootstrap captures source, origin externalCluster and target",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				Bootstrap: &cnpgv1.BootstrapConfiguration{
					Recovery: &cnpgv1.BootstrapRecovery{
						Source:         recoveryExternalClusterName,
						RecoveryTarget: &cnpgv1.RecoveryTarget{TargetTime: "2026-05-01T13:30:00Z", Exclusive: ptr.To(true)},
					},
				},
				ExternalClusters: []cnpgv1.ExternalCluster{
					{Name: "foreign"},
					{
						Name: recoveryExternalClusterName,
						PluginConfiguration: &cnpgv1.PluginConfiguration{
							Name:       barmanCloudPluginName,
							Parameters: map[string]string{"serverName": "src"},
						},
					},
				},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
				BootstrapType:       "recovery",
				Recovery: &normalizedRecoverySpec{
					Source: recoveryExternalClusterName,
					ExternalCluster: &normalizedRecoveryExternalCluster{
						Name:       recoveryExternalClusterName,
						PluginName: barmanCloudPluginName,
						Parameters: map[string]string{"serverName": "src"},
					},
					Target: &normalizedRecoveryTarget{
						TargetTime: "2026-05-01T13:30:00Z",
						Exclusive:  ptr.To(true),
					},
				},
			},
		},
		{
			name: "nil bootstrap leaves database and owner empty",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
			},
		},
		{
			name: "certificates ServerAltDNSNames included in normalization for drift detection",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				Certificates: &cnpgv1.CertificatesConfiguration{
					ServerAltDNSNames: []string{"z.example", "a.example"},
				},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:         "img:18",
				Instances:         1,
				ServerAltDNSNames: []string{"z.example", "a.example"},
			},
		},
		{
			name: "plugin with nil Enabled normalizes to enabled (CNPG default)",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				Plugins: []cnpgv1.PluginConfiguration{
					{Name: barmanCloudPluginName, IsWALArchiver: ptr.To(true)},
				},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
				Plugins: []normalizedPluginSpec{
					{Name: barmanCloudPluginName, Enabled: true, IsWALArchiver: true},
				},
			},
		},
		{
			name: "plugin explicitly disabled is detected as disabled for drift",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				Plugins: []cnpgv1.PluginConfiguration{
					{Name: barmanCloudPluginName, Enabled: ptr.To(false), IsWALArchiver: ptr.To(true)},
				},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
				Plugins: []normalizedPluginSpec{
					{Name: barmanCloudPluginName, Enabled: false, IsWALArchiver: true},
				},
			},
		},
		{
			name: "plugin explicitly enabled is preserved",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				Plugins: []cnpgv1.PluginConfiguration{
					{Name: barmanCloudPluginName, Enabled: ptr.To(true), IsWALArchiver: ptr.To(true)},
				},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
				Plugins: []normalizedPluginSpec{
					{Name: barmanCloudPluginName, Enabled: true, IsWALArchiver: true},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeCNPGClusterSpec(tt.spec)

			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestClusterModelAdoptsOrphanedCNPGCluster(t *testing.T) {
	t.Parallel()

	// Arrange
	scheme := newTestScheme()

	instances := int32(1)
	version := "16"
	storageSize := resource.MustParse("10Gi")
	cfg := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &version,
			Storage:          &storageSize,
			Resources:        &corev1.ResourceRequirements{},
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
		},
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}
	cluster := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"}}
	clusterClass := &platformv1alpha1.PostgresClusterClass{ObjectMeta: metav1.ObjectMeta{Name: "pg1-class"}}
	orphanedCNPG := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace},
		Spec:       buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "c1", "pg1-secret", false),
	}
	events := &captureEventEmitter{}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(orphanedCNPG).Build()
	contracts := &reconcileContracts{Secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret", Namespace: "default"}}}
	model := newClusterModel(c, scheme, events, nil, cluster, clusterClass, cfg, contracts)

	// Act
	err := model.Reconcile(context.Background())

	// Assert
	require.NoError(t, err)
	assert.Equal(t, cnpgPatchMetadata, model.cnpgPatch, "adoption patches only owner reference — metadata, no phase gate needed")
	assert.False(t, model.cnpgCreated)
	adopted := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(orphanedCNPG), adopted))
	require.Len(t, adopted.OwnerReferences, 1, "owner reference must be set after adoption")
	assert.Equal(t, cluster.Name, adopted.OwnerReferences[0].Name)
	assert.Contains(t, events.normals, EventClusterAdopted+":Adopted existing CNPG cluster for PostgresCluster pg1")
}

func TestClusterModelContractsNotReadyIsUpstreamPending(t *testing.T) {
	t.Parallel()

	// Arrange: contracts has no Secret — clusterModel is the root and checks only for Secret.
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
	}
	clusterClass := &platformv1alpha1.PostgresClusterClass{ObjectMeta: metav1.ObjectMeta{Name: "pg1-class"}}
	contracts := &reconcileContracts{} // Secret is nil
	model := newClusterModel(
		fake.NewClientBuilder().Build(), nil, noopEventEmitter{}, nil, cluster, clusterClass, &MergedConfig{}, contracts,
	)

	// Act
	reconcileErr := model.CheckContracts()
	health, err := model.Observe(context.Background(), reconcileErr)

	// Assert
	require.ErrorIs(t, reconcileErr, errContractsNotReady)
	require.NoError(t, err)
	assert.Equal(t, clusterReady, health.Condition)
	assert.Equal(t, pgcConstants.Pending, health.State)
	assert.Equal(t, reasonUpstreamNotReady, health.Reason)
	assert.True(t, health.Result.RequeueAfter > 0)
}

func TestComponentStateTriggerConditions(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	exampleClusterClass := &platformv1alpha1.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class", Namespace: "default"},
		Spec: platformv1alpha1.PostgresClusterClassSpec{
			Config: &platformv1alpha1.PostgresClusterClassConfig{
				ConnectionPooler: &platformv1alpha1.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)},
			},
		},
	}
	exampleCm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-config", Namespace: "default"},
		Data: map[string]string{
			pgconninfo.KeyClusterRWEndpoint:  "pg1-rw.default.svc.cluster.local",
			pgconninfo.KeyClusterROEndpoint:  "pg1-ro.default.svc.cluster.local",
			pgconninfo.KeyClusterREndpoint:   "pg1-r.default.svc.cluster.local",
			pgconninfo.KeyDefaultClusterPort: pgconninfo.DefaultPort,
			configMapKeySuperUserSecretRef:   "pg1-secret",
		},
	}
	examplePgCluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status: platformv1alpha1.PostgresClusterStatus{
			Resources: &platformv1alpha1.PostgresClusterResources{
				ConfigMapRef: &corev1.LocalObjectReference{Name: "pg1-config"},
				SuperUserSecretRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "pg1-secret"},
					Key:                  "password",
				},
			},
		},
	}
	exampleSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret", Namespace: "default"},
		Data:       map[string][]byte{"password": []byte("s3cr3t")},
	}
	exampleCASecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-server-ca", Namespace: "default"},
		Data:       map[string][]byte{defaultServerCACertKey: []byte("-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----\n")},
	}

	instances := int32(1)
	version := "16"
	storageSize := resource.MustParse("10Gi")
	mergedConfig := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &version,
			Storage:          &storageSize,
			Resources:        &corev1.ResourceRequirements{},
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
		},
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}

	// makeContracts builds a contracts object with a healthy CNPG cluster pre-populated,
	// simulating a prior successful clusterModel reconcile (owner reference already set).
	makeContracts := func(cluster *platformv1alpha1.PostgresCluster, withCA bool) *reconcileContracts {
		cnpgStatus := cnpgv1.ClusterStatus{
			Phase:        cnpgv1.PhaseHealthy,
			WriteService: cluster.Name + "-rw",
			ReadService:  cluster.Name + "-ro",
			// Settled instance count matching mergedConfig so the scale gate stays
			// closed — these cases exercise downstream component gating, not scaling.
			Instances:      int(instances),
			ReadyInstances: int(instances),
		}
		if withCA {
			cnpgStatus.Certificates = cnpgv1.CertificatesStatus{
				CertificatesConfiguration: cnpgv1.CertificatesConfiguration{
					ServerCASecret: exampleCASecret.Name,
				},
			}
		}
		cnpg := &cnpgv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace},
			Spec:       buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, mergedConfig, cluster.Name, "pg1-secret", false),
			Status:     cnpgStatus,
		}
		require.NoError(t, ctrl.SetControllerReference(cluster, cnpg, scheme))
		return &reconcileContracts{CNPGCluster: cnpg}
	}

	combinations := []struct {
		name       string
		components func() []component
		conditions []conditionTypes
		requeue    []bool
		expectAll  bool
		message    string
	}{
		{
			name: "Provisioner ready, pooler blocked by contracts (no CNPG cluster yet)",
			components: func() []component {
				cluster := examplePgCluster.DeepCopy()
				cnpg := &cnpgv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace},
					Spec:       buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, mergedConfig, cluster.Name, "pg1-secret", false),
					Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy, Instances: int(instances), ReadyInstances: int(instances)},
				}
				require.NoError(t, ctrl.SetControllerReference(cluster, cnpg, scheme))
				// provisioner gets full contracts; pooler gets empty contracts (no CNPGCluster).
				provisionerContracts := &reconcileContracts{
					Secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret", Namespace: "default"}},
				}
				poolerContracts := &reconcileContracts{} // simulates pooler running before provisioner publishes
				provisioner := newClusterModel(
					fakeClientWithPostgreSQLParameterApply(t, scheme, nil, cnpg),
					scheme, noopEventEmitter{}, nil, cluster, exampleClusterClass, mergedConfig, provisionerContracts,
				)
				pooler := newPoolerModel(
					fake.NewClientBuilder().WithScheme(scheme).Build(),
					scheme, noopEventEmitter{}, nil, cluster, exampleClusterClass, mergedConfig, poolerContracts,
				)
				return []component{provisioner, pooler}
			},
			conditions: []conditionTypes{clusterReady, poolerReady},
			requeue:    []bool{false, true},
			expectAll:  false,
			message:    "Provisioner ready but pooler is blocked until CNPGCluster contract is populated",
		},
		{
			name: "Provisioner ready, pooler ready, configMap pending from NotFound",
			components: func() []component {
				cluster := examplePgCluster.DeepCopy()
				contracts := makeContracts(cluster, false)
				contracts.Secret = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret", Namespace: "default"}}
				provisioner := newClusterModel(
					fakeClientWithPostgreSQLParameterApply(t, scheme, nil, contracts.CNPGCluster),
					scheme, noopEventEmitter{}, nil, cluster, exampleClusterClass, mergedConfig, contracts,
				)
				pooler := newPoolerModel(
					fake.NewClientBuilder().WithScheme(scheme).Build(),
					scheme, noopEventEmitter{}, nil, cluster, exampleClusterClass, mergedConfig, contracts,
				)
				configMap := newConfigMapModel(
					configMapNotFoundClient{Client: fake.NewClientBuilder().WithScheme(scheme).Build()},
					scheme, noopEventEmitter{}, nil, cluster, contracts,
				)
				return []component{provisioner, pooler, configMap}
			},
			conditions: []conditionTypes{clusterReady, poolerReady, configMapsReady},
			requeue:    []bool{false, false, true},
			expectAll:  false,
			message:    "ConfigMap NotFound must stay pending even when provisioner and pooler are ready",
		},
		{
			name: "Flow successful, all components ready",
			components: func() []component {
				cluster := examplePgCluster.DeepCopy()
				contracts := makeContracts(cluster, true)
				contracts.Secret = exampleSecret
				provisioner := newClusterModel(
					fakeClientWithPostgreSQLParameterApply(t, scheme, nil, contracts.CNPGCluster),
					scheme, noopEventEmitter{}, nil, cluster, exampleClusterClass, mergedConfig, contracts,
				)
				pooler := newPoolerModel(
					fake.NewClientBuilder().WithScheme(scheme).Build(),
					scheme, noopEventEmitter{}, nil, cluster, exampleClusterClass, mergedConfig, contracts,
				)
				configMap := newConfigMapModel(
					fake.NewClientBuilder().WithScheme(scheme).WithObjects(exampleCm, exampleCASecret).Build(),
					scheme, noopEventEmitter{}, nil, cluster, contracts,
				)
				secret := newSecretModel(
					fake.NewClientBuilder().WithScheme(scheme).WithObjects(exampleSecret).Build(),
					scheme, noopEventEmitter{}, nil, cluster, "pg1-secret", contracts,
				)
				return []component{provisioner, pooler, configMap, secret}
			},
			conditions: []conditionTypes{clusterReady, poolerReady, configMapsReady, secretsReady},
			requeue:    []bool{false, false, false, false},
			expectAll:  true,
			message:    "",
		},
	}

	for _, tt := range combinations {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Arrange
			components := tt.components()

			// Act + Assert per-component
			state := pgcConstants.Empty
			for i, c := range components {
				var reconcileErr error
				if reconcileErr = c.CheckContracts(); reconcileErr == nil {
					reconcileErr = c.Reconcile(t.Context())
				}
				health, err := c.Observe(t.Context(), reconcileErr)
				require.NoError(t, err)
				state = health.State
				assert.Equal(t, tt.conditions[i], health.Condition)
				assert.Equal(t, tt.requeue[i], health.Result.RequeueAfter > 0)
				if isIntermediateState(health.State) {
					break
				}
			}
			assert.Equal(t, tt.expectAll, state&pgcConstants.Ready == pgcConstants.Ready, tt.message)
		})
	}
}

func TestRunComponentsStopsOnObserveError(t *testing.T) {
	t.Parallel()

	// Arrange: two components — first returns an error from Observe, second must not run.
	scheme := newTestScheme()
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status: platformv1alpha1.PostgresClusterStatus{
			Resources: &platformv1alpha1.PostgresClusterResources{
				SuperUserSecretRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "pg1-secret"},
					Key:                  "password",
				},
			},
		},
	}
	// First component: secret model with a Get error — will fail in Reconcile → Observe returns error.
	errClient := getErrorClient{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		err:    assert.AnError,
		matcher: func(obj client.Object) bool {
			_, ok := obj.(*corev1.Secret)
			return ok
		},
	}
	var secondRan bool
	firstComponent := newSecretModel(errClient, scheme, noopEventEmitter{}, nil, cluster, "pg1-secret", &reconcileContracts{})

	// Second component: a managed roles model — tracked via a side effect on a local flag.
	// We use a capture on the contracts to detect if Reconcile is called.
	cnpg := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"}}
	contracts := &reconcileContracts{CNPGCluster: cnpg, Secret: &corev1.Secret{}}
	secondComponent := newManagedRolesModel(
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build(),
		scheme, &captureEventEmitter{}, nil, cluster, contracts, nil,
	)
	_ = secondRan

	components := []component{firstComponent, secondComponent}

	// Act
	result, err := runComponents(context.Background(), slog.Default(), components, nil)

	// Assert: runComponents returns the error from the first component and does not proceed.
	require.Error(t, err)
	require.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, ctrl.Result{}, result)
	// Second component's contracts.Secret is only set by its own Reconcile.
	// If it ran, CNPGCluster would be unchanged; but the cluster.Status.ManagedRolesStatus
	// would be populated. Since runComponents stopped early, it must be nil.
	assert.Nil(t, cluster.Status.ManagedRolesStatus)
}

func TestRunComponentsSkipsReconcileWhenCheckContractsFails(t *testing.T) {
	t.Parallel()

	// Arrange: configmap model with missing contracts — CheckContracts returns errContractsNotReady,
	// so Reconcile must never be called (which would panic on nil CNPGCluster).
	scheme := newTestScheme()
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status:     platformv1alpha1.PostgresClusterStatus{Resources: &platformv1alpha1.PostgresClusterResources{}},
	}
	contracts := &reconcileContracts{} // CNPGCluster and Secret both nil
	model := newConfigMapModel(fake.NewClientBuilder().WithScheme(scheme).Build(), scheme, noopEventEmitter{}, nil, cluster, contracts)

	// Act — runComponents must not panic, and must return an intermediate result (Pending)
	result, err := runComponents(context.Background(), slog.Default(), []component{model}, nil)

	// Assert
	require.NoError(t, err)
	assert.True(t, result.RequeueAfter > 0, "contracts not ready should requeue")
}

// Only cnpgPatchBody may force Observe to hold ClusterReady=Provisioning;
// cnpgPatchNone / cnpgPatchMetadata must not gate.
func TestCNPGPatchKind_RequiresPhaseGate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		kind     cnpgPatchKind
		wantGate bool
	}{
		{"none does not gate", cnpgPatchNone, false},
		{"annotation-only does not gate", cnpgPatchMetadata, false},
		{"material drift gates", cnpgPatchBody, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.wantGate, tc.kind.requiresPhaseGate())
		})
	}
}

func TestIsClusterDrift(t *testing.T) {
	t.Parallel()

	clone := func(s normalizedCNPGClusterSpec) normalizedCNPGClusterSpec {
		out := s
		if s.InheritedAnnotations != nil {
			out.InheritedAnnotations = make(map[string]string, len(s.InheritedAnnotations))
			maps.Copy(out.InheritedAnnotations, s.InheritedAnnotations)
		}
		if s.PgHBA != nil {
			out.PgHBA = append([]string(nil), s.PgHBA...)
		}
		return out
	}

	base := normalizedCNPGClusterSpec{
		ImageName:           "ghcr.io/cloudnative-pg/postgresql:17",
		Instances:           3,
		PrimaryUpdateMethod: "restart",
		StorageSize:         "10Gi",
		Resources:           corev1.ResourceRequirements{},
		DefaultDatabase:     "app",
		Owner:               "app",
		InheritedAnnotations: map[string]string{
			prometheusScrapeAnnotation: "true",
			prometheusPortAnnotation:   postgresMetricsPortString,
		},
	}

	tests := []struct {
		name   string
		mutate func(s *normalizedCNPGClusterSpec)
		want   bool
	}{
		{
			name:   "identical specs are not material drift",
			mutate: func(s *normalizedCNPGClusterSpec) {},
			want:   false,
		},
		{
			name: "annotation-only drift is NOT material",
			mutate: func(s *normalizedCNPGClusterSpec) {
				s.InheritedAnnotations = map[string]string{prometheusScrapeAnnotation: "false"}
			},
			want: false,
		},
		{
			name: "annotation cleared (non-nil → nil) is NOT material",
			mutate: func(s *normalizedCNPGClusterSpec) {
				s.InheritedAnnotations = nil
			},
			want: false,
		},
		{
			name: "image rollout IS material",
			mutate: func(s *normalizedCNPGClusterSpec) {
				s.ImageName = "ghcr.io/cloudnative-pg/postgresql:18"
			},
			want: true,
		},
		{
			name: "instance scale IS material",
			mutate: func(s *normalizedCNPGClusterSpec) {
				s.Instances = 5
			},
			want: true,
		},
		{
			name: "storage resize IS material",
			mutate: func(s *normalizedCNPGClusterSpec) {
				s.StorageSize = "20Gi"
			},
			want: true,
		},
		{
			name: "PrimaryUpdateMethod drift IS material",
			mutate: func(s *normalizedCNPGClusterSpec) {
				s.PrimaryUpdateMethod = ""
			},
			want: true,
		},
		{
			name: "pg_hba change IS material",
			mutate: func(s *normalizedCNPGClusterSpec) {
				s.PgHBA = []string{"host all all all md5"}
			},
			want: true,
		},
		{
			name: "image change + annotation change together IS material",
			mutate: func(s *normalizedCNPGClusterSpec) {
				s.ImageName = "ghcr.io/cloudnative-pg/postgresql:18"
				s.InheritedAnnotations = map[string]string{prometheusScrapeAnnotation: "false"}
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := clone(base)
			b := clone(base)
			tt.mutate(&b)
			assert.Equal(t, tt.want, isClusterDrift(a, b),
				"isClusterDrift result mismatch; every normalized field is material EXCEPT InheritedAnnotations")
		})
	}

	t.Run("does not mutate caller's specs", func(t *testing.T) {
		a := clone(base)
		b := clone(base)
		b.ImageName = "different"

		_ = isClusterDrift(a, b)

		require.NotNil(t, a.InheritedAnnotations, "isClusterDrift must not mutate caller's a.InheritedAnnotations")
		require.NotNil(t, b.InheritedAnnotations, "isClusterDrift must not mutate caller's b.InheritedAnnotations")
		assert.Equal(t, "true", a.InheritedAnnotations[prometheusScrapeAnnotation])
		assert.Equal(t, "true", b.InheritedAnnotations[prometheusScrapeAnnotation])
	})
}

func TestClusterModelActuatePreservesManagedRoles(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	instances := int32(3)
	version := "15.13"
	storageSize := resource.MustParse("10Gi")

	cfg := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &version,
			Storage:          &storageSize,
			Resources:        &corev1.ResourceRequirements{},
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
		},
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}

	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
	}
	clusterClass := &platformv1alpha1.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class"},
	}

	existingCNPG := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace},
		Spec:       buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "c1", "pg1-secret", false),
	}
	roleConfig := cnpgv1.ManagedConfiguration{
		Roles: []cnpgv1.RoleConfiguration{
			{Name: "app-user", Ensure: cnpgv1.EnsurePresent, Login: true},
		},
	}
	existingCNPG.Spec.Managed = roleConfig.DeepCopy()

	updatedInstances := int32(5)
	driftedCfg := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{
			Instances:        &updatedInstances,
			PostgresVersion:  &version,
			Storage:          &storageSize,
			Resources:        &corev1.ResourceRequirements{},
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
		},
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}

	c := fakeClientWithPostgreSQLParameterApply(t, scheme, nil, existingCNPG)
	contracts := &reconcileContracts{Secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret"}}}
	model := newClusterModel(c, scheme, &captureEventEmitter{}, nil, cluster, clusterClass, driftedCfg, contracts)
	require.NoError(t, model.Reconcile(context.Background()))

	require.True(t, model.cnpgPatch.requiresPhaseGate())

	updated := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(existingCNPG), updated))
	assert.NotNil(t, updated.Spec.Managed)
	assert.Equal(t, &roleConfig, updated.Spec.Managed)
}

func TestClusterModelReconcilePatchesPoolerSANDrift(t *testing.T) {
	t.Parallel()

	// Arrange: existing CNPG cluster has no pooler SANs; pooler is enabled.
	// Reconcile must detect SAN drift and issue a single patch.
	scheme := newTestScheme()
	instances := int32(1)
	version := "16"
	storageSize := resource.MustParse("10Gi")
	mergedConfig := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &version,
			Storage:          &storageSize,
			Resources:        &corev1.ResourceRequirements{},
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
			ConnectionPooler: &platformv1alpha1.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)},
		},
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
	}
	existingCNPG := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec:       cnpgv1.ClusterSpec{},
	}
	contracts := &reconcileContracts{
		Secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret"}},
	}
	c := fakeClientWithPostgreSQLParameterApply(t, scheme, nil, existingCNPG)
	clusterClass := &platformv1alpha1.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class"},
		Spec: platformv1alpha1.PostgresClusterClassSpec{
			Config: &platformv1alpha1.PostgresClusterClassConfig{ConnectionPooler: &platformv1alpha1.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)}},
		},
	}
	model := newClusterModel(c, scheme, noopEventEmitter{}, nil, cluster, clusterClass, mergedConfig, contracts)

	// Act
	require.NoError(t, model.Reconcile(context.Background()))

	// Assert: CNPG cluster in store now has pooler SANs
	var got cnpgv1.Cluster
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "pg1", Namespace: "default"}, &got))
	expectedSANs := computeDesiredPoolerSANSet(true, nil, "pg1", "default")
	assert.ElementsMatch(t, expectedSANs, got.Spec.Certificates.ServerAltDNSNames,
		"Reconcile must patch pooler SANs into CNPG cluster spec when drift is detected")
}

func TestClusterModelEnsureSANPolicy(t *testing.T) {
	t.Parallel()

	spec := cnpgv1.ClusterSpec{
		Certificates: &cnpgv1.CertificatesConfiguration{
			ServerAltDNSNames: []string{"existing.example"},
		},
	}
	applyPoolerSANs(&spec, true, "pg1", "default")

	assert.Contains(t, spec.Certificates.ServerAltDNSNames, "existing.example")
	assert.Contains(t, spec.Certificates.ServerAltDNSNames, "pg1-pooler-rw.default"+poolerSANSuffix)
	assert.Contains(t, spec.Certificates.ServerAltDNSNames, "pg1-pooler-ro.default"+poolerSANSuffix)

	sansBefore := append([]string(nil), spec.Certificates.ServerAltDNSNames...)
	applyPoolerSANs(&spec, true, "pg1", "default")
	assert.Equal(t, sansBefore, spec.Certificates.ServerAltDNSNames, "second applyPoolerSANs call must be a no-op")
}

func TestClusterModelSANPolicyPoolerDisabledRetainsPoolerSANs(t *testing.T) {
	t.Parallel()

	rwShort := "pg1-pooler-rw.default"
	roShort := "pg1-pooler-ro.default"
	initial := []string{
		"existing.example",
		rwShort, rwShort + poolerSANSuffix,
		roShort, roShort + poolerSANSuffix,
	}
	spec := cnpgv1.ClusterSpec{
		Certificates: &cnpgv1.CertificatesConfiguration{
			ServerAltDNSNames: append([]string(nil), initial...),
		},
	}
	applyPoolerSANs(&spec, false, "pg1", "default")

	assert.ElementsMatch(t, initial, spec.Certificates.ServerAltDNSNames, "pooler disabled must not patch SANs away")
}

func TestClusterModelSANPolicyPoolerDisabledDoesNotInjectPoolerSANsWhenAbsent(t *testing.T) {
	t.Parallel()

	spec := cnpgv1.ClusterSpec{
		Certificates: &cnpgv1.CertificatesConfiguration{
			ServerAltDNSNames: []string{"only-custom.example"},
		},
	}
	applyPoolerSANs(&spec, false, "pg1", "default")

	assert.Equal(t, []string{"only-custom.example"}, spec.Certificates.ServerAltDNSNames,
		"pooler disabled must not add pooler DNS names when they are absent from spec")
	for _, s := range spec.Certificates.ServerAltDNSNames {
		assert.NotContains(t, s, "pooler-rw")
		assert.NotContains(t, s, "pooler-ro")
	}
}

func TestClusterModelIsSANPolicyConvergedNilVsEmptyServerAltDNSNames(t *testing.T) {
	t.Parallel()

	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec:       cnpgv1.ClusterSpec{Certificates: &cnpgv1.CertificatesConfiguration{ServerAltDNSNames: nil}},
	}
	applyPoolerSANs(&cnpg.Spec, false, "pg1", "default")
	assert.True(t, isSANPolicyConverged(cnpg, false))
}

func TestClusterModelSANPolicyPoolerEnabledAddsShortAndFQDNPoolerSANs(t *testing.T) {
	t.Parallel()

	spec := cnpgv1.ClusterSpec{
		Certificates: &cnpgv1.CertificatesConfiguration{
			ServerAltDNSNames: []string{"static.example"},
		},
	}
	applyPoolerSANs(&spec, true, "pg1", "default")

	ns := "default"
	rwShort := "pg1-pooler-rw." + ns
	roShort := "pg1-pooler-ro." + ns
	for _, want := range []string{rwShort, rwShort + poolerSANSuffix, roShort, roShort + poolerSANSuffix} {
		assert.Contains(t, spec.Certificates.ServerAltDNSNames, want)
	}
}

func TestClusterModelIsSANPolicyConvergedPoolerEnabledDetectsDrift(t *testing.T) {
	t.Parallel()

	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec: cnpgv1.ClusterSpec{
			Certificates: &cnpgv1.CertificatesConfiguration{
				ServerAltDNSNames: []string{"static.example", "pg1-pooler-rw.default"},
			},
		},
	}

	assert.False(t, isSANPolicyConverged(cnpg, true), "missing RO / fqdn pooler SANs must not converge")

	applyPoolerSANs(&cnpg.Spec, true, "pg1", "default")
	assert.True(t, isSANPolicyConverged(cnpg, true), "applyPoolerSANs must have added the missing pooler SANs")
}

func TestClusterModelSANPolicyPoolerDisabledIsStrictNoOp(t *testing.T) {
	t.Parallel()

	unsorted := []string{"zebra.internal", "alpha.internal"}
	spec := cnpgv1.ClusterSpec{
		Certificates: &cnpgv1.CertificatesConfiguration{
			ServerAltDNSNames: append([]string(nil), unsorted...),
		},
	}

	cnpg := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"}, Spec: spec}
	assert.True(t, isSANPolicyConverged(cnpg, false), "isSANPolicyConverged must return true when pooler is disabled")

	applyPoolerSANs(&spec, false, "pg1", "default")
	assert.Equal(t, unsorted, spec.Certificates.ServerAltDNSNames, "applyPoolerSANs must be a strict no-op when pooler is disabled")
}

func TestClusterModelIsServerTLSLeafAlignedWithSpec(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	wantSANs := []string{"pg1-rw.default.svc.cluster.local", "pg1-pooler-rw.default.svc.cluster.local"}
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec: cnpgv1.ClusterSpec{
			Certificates: &cnpgv1.CertificatesConfiguration{
				ServerAltDNSNames: wantSANs,
			},
		},
		Status: cnpgv1.ClusterStatus{
			Certificates: cnpgv1.CertificatesStatus{
				CertificatesConfiguration: cnpgv1.CertificatesConfiguration{
					ServerTLSSecret: "pg1-server-tls",
				},
			},
		},
	}

	t.Run("nil_cnpg_short_circuits_true", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		ok, err := isServerTLSLeafAlignedWithSpec(context.Background(), c, "default", nil)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("missing_secret", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		ok, err := isServerTLSLeafAlignedWithSpec(context.Background(), c, "default", cnpg)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("leaf_missing_dns", func(t *testing.T) {
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1-server-tls", Namespace: "default"},
			Data: map[string][]byte{
				corev1.TLSCertKey: testSelfSignedLeafCertPEM(t, []string{"pg1-rw.default.svc.cluster.local"}),
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sec).Build()
		ok, err := isServerTLSLeafAlignedWithSpec(context.Background(), c, "default", cnpg)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("leaf_aligned", func(t *testing.T) {
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1-server-tls", Namespace: "default"},
			Data: map[string][]byte{
				corev1.TLSCertKey: testSelfSignedLeafCertPEM(t, wantSANs),
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sec).Build()
		ok, err := isServerTLSLeafAlignedWithSpec(context.Background(), c, "default", cnpg)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("empty_spec_sans_skips_secret", func(t *testing.T) {
		emptyCNPG := &cnpgv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg2", Namespace: "default"},
			Spec:       cnpgv1.ClusterSpec{},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		ok, err := isServerTLSLeafAlignedWithSpec(context.Background(), c, "default", emptyCNPG)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("malformed_pem_returns_sentinel", func(t *testing.T) {
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1-server-tls", Namespace: "default"},
			Data:       map[string][]byte{corev1.TLSCertKey: []byte("this is not a PEM block")},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sec).Build()
		ok, err := isServerTLSLeafAlignedWithSpec(context.Background(), c, "default", cnpg)
		require.Error(t, err, "malformed PEM must escalate via the sentinel so callers can route to Failed")
		assert.True(t, errors.Is(err, errServerTLSLeafInvalid))
		assert.Contains(t, err.Error(), "PEM decode failed")
		assert.Contains(t, err.Error(), "default/pg1-server-tls")
		assert.False(t, ok)
	})

	t.Run("invalid_certificate_bytes_returns_sentinel", func(t *testing.T) {
		badDER := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("garbage-not-asn1")})
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1-server-tls", Namespace: "default"},
			Data:       map[string][]byte{corev1.TLSCertKey: badDER},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sec).Build()
		ok, err := isServerTLSLeafAlignedWithSpec(context.Background(), c, "default", cnpg)
		require.Error(t, err, "x509.ParseCertificate failure must escalate via the sentinel")
		assert.True(t, errors.Is(err, errServerTLSLeafInvalid))
		assert.Contains(t, err.Error(), "x509 parse failed")
		assert.Contains(t, err.Error(), "default/pg1-server-tls")
		assert.False(t, ok)
	})
}

// cnpgClusterDefaultsContractYAML is a minimal hand-authored CRD schema that models
// only the spec defaults buildCNPGClusterSpec must mirror.
const cnpgClusterDefaultsContractYAML = `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: clusters.postgresql.cnpg.io
spec:
  group: postgresql.cnpg.io
  scope: Namespaced
  names:
    kind: Cluster
    listKind: ClusterList
    plural: clusters
    singular: cluster
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                primaryUpdateMethod:
                  type: string
                  default: restart
`

func loadCNPGClusterStructuralSchema(t *testing.T) *structuralschema.Structural {
	t.Helper()

	var crd apiextv1.CustomResourceDefinition
	require.NoError(t,
		yaml.Unmarshal([]byte(cnpgClusterDefaultsContractYAML), &crd),
		"parse cnpgClusterDefaultsContractYAML",
	)

	var v1Schema *apiextv1.JSONSchemaProps
	for i := range crd.Spec.Versions {
		v := &crd.Spec.Versions[i]
		if v.Name == "v1" && v.Schema != nil {
			v1Schema = v.Schema.OpenAPIV3Schema
			break
		}
	}
	require.NotNil(t, v1Schema, "defaults contract YAML has no v1 schema")

	var internalSchema apiext.JSONSchemaProps
	require.NoError(t,
		apiextv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(v1Schema, &internalSchema, nil),
		"convert v1 -> internal JSONSchemaProps",
	)

	ss, err := structuralschema.NewStructural(&internalSchema)
	require.NoError(t, err, "build structural schema from defaults contract")
	return ss
}

func applyCRDDefaulting(t *testing.T, ss *structuralschema.Structural, obj *cnpgv1.Cluster) *cnpgv1.Cluster {
	t.Helper()

	raw, err := json.Marshal(obj)
	require.NoError(t, err, "marshal Cluster -> JSON")

	asMap := map[string]any{}
	require.NoError(t, json.Unmarshal(raw, &asMap), "unmarshal JSON -> map[string]any")

	structuraldefaulting.Default(asMap, ss)

	raw2, err := json.Marshal(asMap)
	require.NoError(t, err, "marshal defaulted map -> JSON")

	out := &cnpgv1.Cluster{}
	require.NoError(t, json.Unmarshal(raw2, out), "unmarshal defaulted JSON -> Cluster")
	return out
}

type roundTripFixture struct {
	instances           int32
	postgresVersion     string
	storage             resource.Quantity
	resources           corev1.ResourceRequirements
	postgresqlConfig    map[string]string
	pgHBA               []string
	primaryUpdateMethod *string
	metricsEnabled      bool
}

func defaultRoundTripFixture() roundTripFixture {
	return roundTripFixture{
		instances:        3,
		postgresVersion:  "17",
		storage:          resource.MustParse("10Gi"),
		resources:        corev1.ResourceRequirements{},
		postgresqlConfig: map[string]string{},
		pgHBA:            []string{},
	}
}

func (f roundTripFixture) mergedConfig() *MergedConfig {
	cfg := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{
			Instances:        ptr.To(f.instances),
			PostgresVersion:  ptr.To(f.postgresVersion),
			Storage:          ptr.To(f.storage),
			Resources:        f.resources.DeepCopy(),
			PostgreSQLConfig: f.postgresqlConfig,
			PgHBA:            f.pgHBA,
		},
	}
	if f.primaryUpdateMethod != nil {
		cfg.CNPG = &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: f.primaryUpdateMethod}
	}
	return cfg
}

func TestBuildCNPGClusterSpec_RoundTripUnderCRDDefaulting(t *testing.T) {
	t.Parallel()

	ss := loadCNPGClusterStructuralSchema(t)

	cases := []struct {
		name    string
		fixture roundTripFixture
	}{
		{name: "default_no_overrides", fixture: defaultRoundTripFixture()},
		{
			name: "primaryUpdateMethod_explicit_restart",
			fixture: func() roundTripFixture {
				f := defaultRoundTripFixture()
				f.primaryUpdateMethod = ptr.To("restart")
				return f
			}(),
		},
		{
			name: "primaryUpdateMethod_explicit_switchover",
			fixture: func() roundTripFixture {
				f := defaultRoundTripFixture()
				f.primaryUpdateMethod = ptr.To("switchover")
				return f
			}(),
		},
		{
			name: "with_custom_postgres_parameters",
			fixture: func() roundTripFixture {
				f := defaultRoundTripFixture()
				f.postgresqlConfig = map[string]string{"shared_buffers": "256MB", "max_connections": "200"}
				return f
			}(),
		},
		{
			name: "with_pg_hba_rules",
			fixture: func() roundTripFixture {
				f := defaultRoundTripFixture()
				f.pgHBA = []string{"hostnossl all all 0.0.0.0/0 reject", "hostssl all all 0.0.0.0/0 scram-sha-256"}
				return f
			}(),
		},
		{
			name: "with_resource_overrides",
			fixture: func() roundTripFixture {
				f := defaultRoundTripFixture()
				f.resources = corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
				}
				return f
			}(),
		},
		{
			name: "with_postgres_metrics_enabled",
			fixture: func() roundTripFixture {
				f := defaultRoundTripFixture()
				f.metricsEnabled = true
				return f
			}(),
		},
		{
			name: "everything_set_together",
			fixture: roundTripFixture{
				instances:           5,
				postgresVersion:     "17",
				storage:             resource.MustParse("50Gi"),
				postgresqlConfig:    map[string]string{"shared_buffers": "256MB"},
				pgHBA:               []string{"hostssl all all 0.0.0.0/0 scram-sha-256"},
				resources:           corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m")}},
				primaryUpdateMethod: ptr.To("switchover"),
				metricsEnabled:      true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := tc.fixture.mergedConfig()
			desiredSpec := buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "c1", "test-secret", tc.fixture.metricsEnabled)

			beforeRT := &cnpgv1.Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: cnpgv1.SchemeGroupVersion.String(),
					Kind:       cnpgv1.ClusterKind,
				},
				ObjectMeta: metav1.ObjectMeta{Name: "round-trip", Namespace: "default"},
				Spec:       *desiredSpec.DeepCopy(),
			}
			afterRT := applyCRDDefaulting(t, ss, beforeRT)

			left := normalizeCNPGClusterSpec(desiredSpec)
			right := normalizeCNPGClusterSpec(afterRT.Spec)

			if !equality.Semantic.DeepEqual(left, right) {
				t.Fatalf(
					"phantom drift: normalized spec diverges across CRD-defaulting round-trip\n"+
						"--- LEFT  (build output, normalized)\n"+
						"+++ RIGHT (after CRD defaulting, normalized)\n"+
						"%s\n"+
						"This usually means buildCNPGClusterSpec left a field empty that the\n"+
						"CNPG CRD schema fills in via `default:` (kube-apiserver applies that\n"+
						"on Create). Mirror the default in buildCNPGClusterSpec.",
					cmp.Diff(left, right),
				)
			}
		})
	}
}

func TestBuildCNPGClusterSpec_RoundTrip_NegativeControl(t *testing.T) {
	t.Parallel()

	ss := loadCNPGClusterStructuralSchema(t)

	cfg := defaultRoundTripFixture().mergedConfig()
	desiredSpec := buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "c1", "test-secret", false)
	desiredSpec.PrimaryUpdateMethod = "" // simulate pre-fix builder

	beforeRT := &cnpgv1.Cluster{
		TypeMeta:   metav1.TypeMeta{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: cnpgv1.ClusterKind},
		ObjectMeta: metav1.ObjectMeta{Name: "round-trip-neg", Namespace: "default"},
		Spec:       *desiredSpec.DeepCopy(),
	}
	afterRT := applyCRDDefaulting(t, ss, beforeRT)

	left := normalizeCNPGClusterSpec(desiredSpec)
	right := normalizeCNPGClusterSpec(afterRT.Spec)

	require.Equal(t, "", left.PrimaryUpdateMethod, "precondition: desired-side primaryUpdateMethod must be empty")
	require.Equal(t, "restart", right.PrimaryUpdateMethod,
		"CRD-schema defaulting must materialize spec.primaryUpdateMethod=\"restart\"")
	require.False(t,
		equality.Semantic.DeepEqual(left, right),
		"negative control: empty desired PrimaryUpdateMethod must round-trip to a different value",
	)
}

func TestClusterModelObserve_PhaseGate(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()
	instances := int32(1)
	version := "16"
	storageSize := resource.MustParse("10Gi")
	cfg := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &version,
			Storage:          &storageSize,
			Resources:        &corev1.ResourceRequirements{},
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
		},
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}

	// makeModel builds a clusterModel with cnpgPatch and cnpgCluster already set,
	// simulating the post-Reconcile state seen by Observe.
	makeModel := func(patchKind cnpgPatchKind, cnpgPhase, specImage, statusImage, pgDataImage string) *clusterModel {
		cluster := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"}}
		cnpg := &cnpgv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
			Spec:       cnpgv1.ClusterSpec{ImageName: specImage},
			// Settled instance count matching cfg so the scale gate does not fire —
			// this test isolates the patch-kind phase gate.
			Status: cnpgv1.ClusterStatus{
				Phase:           cnpgPhase,
				Image:           statusImage,
				PGDataImageInfo: &cnpgv1.ImageInfo{Image: pgDataImage},
				Instances:       int(instances),
				ReadyInstances:  int(instances),
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build()
		contracts := &reconcileContracts{CNPGCluster: cnpg}
		model := newClusterModel(c, scheme, noopEventEmitter{}, nil, cluster, &platformv1alpha1.PostgresClusterClass{}, cfg, contracts)
		model.cnpgCluster = cnpg
		model.cnpgPatch = patchKind
		return model
	}

	tests := []struct {
		name          string
		patchKind     cnpgPatchKind
		cnpgPhase     string
		specImage     string
		statusImage   string
		pgDataImage   string
		expectedState pgcConstants.State
		expectRequeue bool
	}{
		{
			name:          "body patch + CNPG still Healthy holds at Provisioning",
			patchKind:     cnpgPatchBody,
			cnpgPhase:     cnpgv1.PhaseHealthy,
			expectedState: pgcConstants.Provisioning,
			expectRequeue: true,
		},
		{
			name:          "metadata patch + CNPG Healthy reaches Ready immediately",
			patchKind:     cnpgPatchMetadata,
			cnpgPhase:     cnpgv1.PhaseHealthy,
			expectedState: pgcConstants.Ready,
			expectRequeue: false,
		},
		{
			name:          "no patch + CNPG Healthy reaches Ready",
			patchKind:     cnpgPatchNone,
			cnpgPhase:     cnpgv1.PhaseHealthy,
			expectedState: pgcConstants.Ready,
			expectRequeue: false,
		},
		{
			name:          "stale CNPG pod image + Healthy holds at Provisioning",
			patchKind:     cnpgPatchNone,
			cnpgPhase:     cnpgv1.PhaseHealthy,
			specImage:     "ghcr.io/cloudnative-pg/postgresql:18.0",
			statusImage:   "ghcr.io/cloudnative-pg/postgresql:17.6",
			expectedState: pgcConstants.Provisioning,
			expectRequeue: true,
		},
		{
			name:          "stale CNPG data image + Healthy holds at Provisioning",
			patchKind:     cnpgPatchNone,
			cnpgPhase:     cnpgv1.PhaseHealthy,
			specImage:     "ghcr.io/cloudnative-pg/postgresql:18.0",
			statusImage:   "ghcr.io/cloudnative-pg/postgresql:18.0",
			pgDataImage:   "ghcr.io/cloudnative-pg/postgresql:17.6",
			expectedState: pgcConstants.Provisioning,
			expectRequeue: true,
		},
		{
			name:          "digest-qualified CNPG images + Healthy reaches Ready",
			patchKind:     cnpgPatchNone,
			cnpgPhase:     cnpgv1.PhaseHealthy,
			specImage:     "ghcr.io/cloudnative-pg/postgresql:18.0",
			statusImage:   "ghcr.io/cloudnative-pg/postgresql:18.0@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			pgDataImage:   "ghcr.io/cloudnative-pg/postgresql:18.0@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			expectedState: pgcConstants.Ready,
			expectRequeue: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			model := makeModel(tt.patchKind, tt.cnpgPhase, tt.specImage, tt.statusImage, tt.pgDataImage)

			health, err := model.Observe(context.Background(), nil)

			require.NoError(t, err)
			assert.Equal(t, tt.expectedState, health.State)
			assert.Equal(t, tt.expectRequeue, health.Result != ctrl.Result{})
		})
	}
}

func TestClusterModelObserve_AdoptedClusterDoesNotStallAtProvisioning(t *testing.T) {
	t.Parallel()

	// Arrange: simulate Reconcile having adopted an orphaned CNPG cluster —
	// patchKind is Metadata (only owner reference changed), CNPG is already Healthy.
	scheme := newTestScheme()
	cluster := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"}}
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build()
	contracts := &reconcileContracts{CNPGCluster: cnpg}
	model := newClusterModel(c, scheme, noopEventEmitter{}, nil, cluster, &platformv1alpha1.PostgresClusterClass{}, &MergedConfig{}, contracts)
	model.cnpgCluster = cnpg
	model.cnpgPatch = cnpgPatchMetadata

	// Act
	health, err := model.Observe(context.Background(), nil)

	// Assert: metadata-only patch must not trigger phase gate — adopted healthy cluster reaches Ready.
	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Ready, health.State)
	assert.Equal(t, ctrl.Result{}, health.Result)
}

func TestCNPGClusterDefaultsContract_HasExpectedDefaults(t *testing.T) {
	t.Parallel()

	ss := loadCNPGClusterStructuralSchema(t)

	specSchema, ok := ss.Properties["spec"]
	require.True(t, ok, "defaults contract: top-level spec property missing from cnpgClusterDefaultsContractYAML")

	updateMethodSchema, ok := specSchema.Properties["primaryUpdateMethod"]
	require.True(t, ok, "defaults contract: spec.primaryUpdateMethod missing from cnpgClusterDefaultsContractYAML")

	require.NotNil(t, updateMethodSchema.Default, "defaults contract: spec.primaryUpdateMethod has no default in cnpgClusterDefaultsContractYAML")
	assert.Equal(t, "restart", updateMethodSchema.Default.Object,
		"defaults contract: spec.primaryUpdateMethod default must be \"restart\"")
}

func TestClusterModelScaleInProgress(t *testing.T) {
	t.Parallel()

	desired := int32(3)
	cfg := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{Instances: &desired},
	}

	tests := []struct {
		name            string
		mergedConfig    *MergedConfig
		cnpgCluster     *cnpgv1.Cluster
		wantDesired     int
		wantReady       int
		wantScalingFlag bool
	}{
		{
			name:         "nil merged config falls through",
			mergedConfig: nil,
			cnpgCluster:  &cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{Instances: 3, ReadyInstances: 3}},
		},
		{
			name:         "nil cnpg cluster falls through",
			mergedConfig: cfg,
			cnpgCluster:  nil,
		},
		{
			name:         "fully ready: no scaling",
			mergedConfig: cfg,
			cnpgCluster:  &cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{Instances: 3, ReadyInstances: 3}},
		},
		{
			name:            "scaling down: ready trails desired",
			mergedConfig:    cfg,
			cnpgCluster:     &cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{Instances: 3, ReadyInstances: 2}},
			wantDesired:     3,
			wantReady:       2,
			wantScalingFlag: true,
		},
		{
			name:            "scaling out: instances trail desired",
			mergedConfig:    cfg,
			cnpgCluster:     &cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{Instances: 2, ReadyInstances: 2}},
			wantDesired:     3,
			wantReady:       2,
			wantScalingFlag: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := &clusterModel{mergedConfig: tt.mergedConfig, cnpgCluster: tt.cnpgCluster}
			desired, ready, scaling := model.scaleInProgress()
			assert.Equal(t, tt.wantScalingFlag, scaling)
			assert.Equal(t, tt.wantDesired, desired)
			assert.Equal(t, tt.wantReady, ready)
		})
	}
}

func TestClusterModelComputeHealthMirrorsCNPGStatus(t *testing.T) {
	t.Parallel()

	desired := int32(3)
	cfg := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{Instances: &desired, PostgreSQLConfig: map[string]string{}},
	}
	cluster := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"}}
	model := &clusterModel{
		cluster:      cluster,
		mergedConfig: cfg,
		cnpgCluster: &cnpgv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
			Status: cnpgv1.ClusterStatus{
				Phase:          cnpgv1.PhaseHealthy,
				Instances:      3,
				ReadyInstances: 2,
				CurrentPrimary: "pg1-1",
			},
		},
	}

	_, err := model.computeHealth(nil)
	require.NoError(t, err)
	require.NotNil(t, cluster.Status.Instances)
	require.NotNil(t, cluster.Status.ReadyInstances)
	require.NotNil(t, cluster.Status.CurrentPrimary)
	assert.Equal(t, int32(3), *cluster.Status.Instances)
	assert.Equal(t, int32(2), *cluster.Status.ReadyInstances)
	assert.Equal(t, "pg1-1", *cluster.Status.CurrentPrimary)
}

// TestClusterModelComputeHealthGatesScale asserts the cluster component reports
// Provisioning while CNPG holds Phase=Healthy during a scale (ready != desired),
// so runComponents short-circuits here and downstream components stay gated.
// Once the count settles it reports Ready.
func TestClusterModelComputeHealthGatesScale(t *testing.T) {
	t.Parallel()

	desired := int32(3)
	cfg := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{Instances: &desired, PostgreSQLConfig: map[string]string{}},
	}
	newModel := func(instances, ready int) *clusterModel {
		return &clusterModel{
			cluster:      &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"}},
			mergedConfig: cfg,
			cnpgCluster: &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
				Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy, Instances: instances, ReadyInstances: ready},
			},
		}
	}

	t.Run("scale-out tail: ready trails desired holds Provisioning", func(t *testing.T) {
		t.Parallel()
		health, err := newModel(3, 2).computeHealth(nil)
		require.NoError(t, err)
		assert.Equal(t, pgcConstants.Provisioning, health.State)
	})

	t.Run("scale-down: ready trails desired holds Provisioning", func(t *testing.T) {
		t.Parallel()
		health, err := newModel(2, 2).computeHealth(nil)
		require.NoError(t, err)
		assert.Equal(t, pgcConstants.Provisioning, health.State)
	})

	t.Run("settled: ready equals desired reaches Ready", func(t *testing.T) {
		t.Parallel()
		health, err := newModel(3, 3).computeHealth(nil)
		require.NoError(t, err)
		assert.Equal(t, pgcConstants.Ready, health.State)
	})
}

// TestValidateCrossResourceScalingGuardrails asserts ValidateCrossResource (the
// runtime path) enforces switchover-needs-2 but NOT RO-pooler-needs-2 — the
// latter is admission-only because the reconciler suppresses the RO pooler at
// instances<2 instead of failing. Hence the readOnly cases expect no error.
func TestValidateCrossResourceScalingGuardrails(t *testing.T) {
	t.Parallel()

	switchoverClass := func(classInstances *int32) *platformv1alpha1.PostgresClusterClass {
		return &platformv1alpha1.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "switchover-class"},
			Spec: platformv1alpha1.PostgresClusterClassSpec{
				Config: &platformv1alpha1.PostgresClusterClassConfig{Instances: classInstances},
				CNPG:   &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("switchover")},
			},
		}
	}
	poolerClass := func(classInstances *int32) *platformv1alpha1.PostgresClusterClass {
		return &platformv1alpha1.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "pooler-class"},
			Spec: platformv1alpha1.PostgresClusterClassSpec{
				Config: &platformv1alpha1.PostgresClusterClassConfig{Instances: classInstances},
				CNPG:   &platformv1alpha1.CNPGConfig{ConnectionPooler: &platformv1alpha1.ConnectionPoolerConfig{}},
			},
		}
	}

	tests := []struct {
		name      string
		class     *platformv1alpha1.PostgresClusterClass
		cluster   *platformv1alpha1.PostgresCluster
		wantField string // "" means expect no scaling/pooler guardrail error
	}{
		{
			name:      "switchover with cluster instances=1 rejected",
			class:     switchoverClass(ptr.To(int32(3))),
			cluster:   &platformv1alpha1.PostgresCluster{Spec: platformv1alpha1.PostgresClusterSpec{Instances: ptr.To(int32(1))}},
			wantField: "spec.instances",
		},
		{
			name:      "switchover inheriting class default 1 rejected (cluster silent)",
			class:     switchoverClass(ptr.To(int32(1))),
			cluster:   &platformv1alpha1.PostgresCluster{},
			wantField: "spec.instances",
		},
		{
			name:    "switchover with cluster instances=2 accepted",
			class:   switchoverClass(ptr.To(int32(1))),
			cluster: &platformv1alpha1.PostgresCluster{Spec: platformv1alpha1.PostgresClusterSpec{Instances: ptr.To(int32(2))}},
		},
		{
			name:  "readOnly pooler at instances=1 NOT rejected (runtime suppresses RO)",
			class: poolerClass(ptr.To(int32(1))),
			cluster: &platformv1alpha1.PostgresCluster{Spec: platformv1alpha1.PostgresClusterSpec{
				ConnectionPooler: &platformv1alpha1.ConnectionPoolerEnableConfig{Enabled: ptr.To(true), ReadOnly: ptr.To(true)},
			}},
		},
		{
			name:  "readOnly pooler with cluster instances=2 accepted",
			class: poolerClass(ptr.To(int32(1))),
			cluster: &platformv1alpha1.PostgresCluster{Spec: platformv1alpha1.PostgresClusterSpec{
				Instances:        ptr.To(int32(2)),
				ConnectionPooler: &platformv1alpha1.ConnectionPoolerEnableConfig{Enabled: ptr.To(true), ReadOnly: ptr.To(true)},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			errs := ValidateCrossResource(tt.class, tt.cluster)
			var found bool
			for _, e := range errs {
				if e.Field == tt.wantField {
					found = true
				}
			}
			if tt.wantField == "" {
				for _, e := range errs {
					assert.NotEqual(t, "spec.instances", e.Field, "unexpected scaling error: %s", e.Message)
					assert.NotEqual(t, "spec.connectionPooler.readOnly", e.Field, "unexpected pooler error: %s", e.Message)
				}
				return
			}
			assert.True(t, found, "expected guardrail error on %s, got %v", tt.wantField, errs)
		})
	}
}
