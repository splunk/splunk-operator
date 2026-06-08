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
	"errors"
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	pgcnpg "github.com/splunk/splunk-operator/pkg/postgresql/shared/cnpg"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type configMapModel struct {
	client       client.Client
	scheme       *runtime.Scheme
	events       eventEmitter
	updateStatus healthStatusUpdater
	contracts    *reconcileContracts
	cluster      *enterprisev4.PostgresCluster
	cm           *corev1.ConfigMap
}

func newConfigMapModel(c client.Client, scheme *runtime.Scheme, events eventEmitter, updateStatus healthStatusUpdater, cluster *enterprisev4.PostgresCluster, contracts *reconcileContracts) *configMapModel {
	return &configMapModel{client: c, scheme: scheme, events: events, updateStatus: updateStatus, contracts: contracts, cluster: cluster}
}

func (c *configMapModel) Name() string { return pgcConstants.ComponentConfigMap }
func (c *configMapModel) Requires() []contractKey {
	return []contractKey{contractCNPGCluster, contractSecret}
}
func (c *configMapModel) Provides() []contractKey { return nil }

func (c *configMapModel) CheckContracts() error {
	correct := checkContractsFromRequirements(c.Requires(), c.contracts)
	if !correct {
		return errContractsNotReady
	}
	return nil
}

func (c *configMapModel) Reconcile(ctx context.Context) error {
	desiredCM, err := generateConfigMap(ctx, c.client, c.scheme, c.cluster, c.contracts.CNPGCluster, c.contracts.Secret.Name)
	if err != nil {
		return newReconcileFailure(reasonConfigMapFailed, err)
	}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: desiredCM.Name, Namespace: desiredCM.Namespace}}
	op, err := controllerutil.CreateOrUpdate(ctx, c.client, cm, func() error {
		cm.Data = desiredCM.Data
		cm.Annotations = desiredCM.Annotations
		cm.Labels = desiredCM.Labels
		if !metav1.IsControlledBy(cm, c.cluster) {
			if setErr := ctrl.SetControllerReference(c.cluster, cm, c.scheme); setErr != nil {
				return fmt.Errorf("setting controller reference: %w", setErr)
			}
		}
		return nil
	})
	if err != nil {
		return newReconcileFailure(reasonConfigMapFailed, err)
	}
	switch op {
	case controllerutil.OperationResultCreated:
		c.events.emitNormal(c.cluster, EventConfigMapReconciled, fmt.Sprintf("ConfigMap %s created", desiredCM.Name))
	case controllerutil.OperationResultUpdated:
		c.events.emitNormal(c.cluster, EventConfigMapReconciled, fmt.Sprintf("ConfigMap %s updated", desiredCM.Name))
	}
	c.cm = cm
	return nil
}

func (c *configMapModel) Observe(ctx context.Context, reconcileErr error) (componentHealth, error) {
	before := c.cluster.Status.DeepCopy()
	health, err := c.computeHealth(reconcileErr)
	statusErr := writeComponentStatus(c.updateStatus, before, health)
	return health, errors.Join(err, statusErr)
}

func (c *configMapModel) computeHealth(reconcileErr error) (componentHealth, error) {
	if h, err, ok := classifyReconcileErr(reconcileErr, configMapsReady, c.events, c.cluster, EventConfigMapReconcileFailed, "ConfigMap"); ok {
		return h, err
	}

	if c.cm == nil {
		return newProvisioningHealth(configMapsReady, reasonConfigMapFailed, msgConfigMapRefNotPublished), nil
	}

	if c.cluster.Status.Resources == nil {
		c.cluster.Status.Resources = &enterprisev4.PostgresClusterResources{}
	}
	if c.cluster.Status.Resources.ConfigMapRef == nil {
		c.cluster.Status.Resources.ConfigMapRef = &corev1.LocalObjectReference{Name: c.cm.Name}
	}

	for _, requiredKey := range requiredClusterConfigMapKeys() {
		if _, ok := c.cm.Data[requiredKey]; !ok {
			return newFailedHealth(configMapsReady, reasonConfigMapFailed, fmt.Sprintf(msgFmtConfigMapMissingRequiredKey, requiredKey)),
				fmt.Errorf("configmap missing key %s", requiredKey)
		}
	}

	// If CNPG already published a ServerCASecret in status, keep requeueing until
	// the access ConfigMap exposes SERVER_CA_* metadata. This prevents a race
	// where CNPG status is ahead of Secret materialization and the controller
	// otherwise settles Ready without ever publishing CA discovery fields.
	if _, ok := c.cm.Data[configMapKeyServerCASecretRef]; !ok {
		return newProvisioningHealth(configMapsReady, reasonConfigMapFailed, msgConfigMapCAMetadataPending), nil
	}

	h := newReadyHealth(configMapsReady, reasonConfigMapReady, msgAccessConfigMapReady)
	if !meta.IsStatusConditionTrue(c.cluster.Status.Conditions, string(configMapsReady)) {
		c.events.emitNormal(c.cluster, EventConfigMapReady, h.Message)
	}
	return h, nil
}

// caMetadataForConfigMap returns a SecretKeySelector for the CNPG server CA Secret when it is
// published and contains the expected data key (same SecretKeySelector shape as database secret refs).
func caMetadataForConfigMap(
	ctx context.Context,
	c client.Client,
	namespace string,
	cnpgCluster *cnpgv1.Cluster,
) (*corev1.SecretKeySelector, bool, error) {
	name := cnpgCluster.Status.Certificates.ServerCASecret
	if name == "" {
		return nil, false, nil // not ready yet — omit keys
	}
	var sec corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &sec); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil // status ahead of materialization — omit keys, requeue later
		}
		return nil, false, err
	}
	key := defaultServerCACertKey
	if len(sec.Data[key]) == 0 && len(sec.StringData[key]) == 0 {
		// Secret exists but unexpected shape — don't advertise a broken contract
		return nil, false, nil
	}
	return &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: name},
		Key:                  key,
	}, true, nil
}

// generateConfigMap builds a ConfigMap with connection details for the PostgresCluster.
func generateConfigMap(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster, secretName string) (*corev1.ConfigMap, error) {
	cmName := fmt.Sprintf("%s%s", cluster.Name, defaultConfigMapSuffix)
	if cluster.Status.Resources != nil && cluster.Status.Resources.ConfigMapRef != nil {
		cmName = cluster.Status.Resources.ConfigMapRef.Name
	}

	rwExists, err := poolerExists(ctx, c, cluster, readWriteEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to check RW pooler existence: %w", err)
	}
	roExists, err := poolerExists(ctx, c, cluster, readOnlyEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to check RO pooler existence: %w", err)
	}

	caSecretRef, ok, err := caMetadataForConfigMap(ctx, c, cluster.Namespace, cnpgCluster)
	if err != nil {
		return nil, fmt.Errorf("failed to get CA metadata for ConfigMap: %w", err)
	}
	caSecretRefValue := ""
	if ok {
		caSecretRefValue = fmt.Sprintf("%s/%s", caSecretRef.Name, caSecretRef.Key)
	}

	endpoints, err := pgcnpg.ResolveConnectionEndpoints(
		cnpgCluster.Name,
		cnpgCluster.Namespace,
		cnpgCluster.Status.WriteService,
		cnpgCluster.Status.ReadService,
		rwExists && roExists,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve connection endpoints: %w", err)
	}
	data, _, err := buildClusterConfigMapData(endpoints, superUsername, secretName, caSecretRefValue)
	if err != nil {
		return nil, fmt.Errorf("failed to build ConfigMap data: %w", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: cluster.Namespace,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "postgrescluster-controller"},
		},
		Data: data,
	}
	if err := ctrl.SetControllerReference(cluster, cm, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}
	return cm, nil
}
