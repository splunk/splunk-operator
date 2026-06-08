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
	"github.com/splunk/splunk-operator/pkg/logging"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type poolerModel struct {
	client         client.Client
	scheme         *runtime.Scheme
	events         poolerEmitter
	updateStatus   healthStatusUpdater
	cluster        *enterprisev4.PostgresCluster
	clusterClass   *enterprisev4.PostgresClusterClass
	mergedConfig   *MergedConfig
	contracts      *reconcileContracts
	metricsEnabled bool
}

func newPoolerModel(c client.Client, scheme *runtime.Scheme, events poolerEmitter, updateStatus healthStatusUpdater, cluster *enterprisev4.PostgresCluster, clusterClass *enterprisev4.PostgresClusterClass, mergedConfig *MergedConfig, contracts *reconcileContracts) *poolerModel {
	model := &poolerModel{
		client:       c,
		scheme:       scheme,
		events:       events,
		updateStatus: updateStatus,
		cluster:      cluster,
		clusterClass: clusterClass,
		mergedConfig: mergedConfig,
		contracts:    contracts,
	}
	model.metricsEnabled = isConnectionPoolerMetricsEnabled(cluster, clusterClass)
	return model
}

func (p *poolerModel) poolerEnabled() bool {
	return p.mergedConfig != nil && p.mergedConfig.Spec != nil &&
		p.mergedConfig.Spec.ConnectionPoolerEnabled != nil &&
		*p.mergedConfig.Spec.ConnectionPoolerEnabled
}

func (p *poolerModel) poolerConfigPresent() bool {
	return p.mergedConfig != nil && p.mergedConfig.CNPG != nil &&
		p.mergedConfig.CNPG.ConnectionPooler != nil
}

func (p *poolerModel) Name() string            { return pgcConstants.ComponentPooler }
func (p *poolerModel) Requires() []contractKey { return []contractKey{contractCNPGCluster} }
func (p *poolerModel) Provides() []contractKey { return nil }

func (p *poolerModel) CheckContracts() error {
	if !checkContractsFromRequirements(p.Requires(), p.contracts) {
		return errContractsNotReady
	}
	return nil
}

func (p *poolerModel) Reconcile(ctx context.Context) error {
	switch {
	case !p.poolerEnabled():
		if err := deleteConnectionPoolers(ctx, p.client, p.cluster); err != nil {
			return newReconcileFailure(reasonPoolerReconciliationFailed, err)
		}
		return nil
	case !p.poolerConfigPresent():
		return nil
	default:
		if err := createOrUpdateConnectionPoolers(ctx, p.client, p.scheme, p.cluster, p.mergedConfig, p.contracts.CNPGCluster, p.metricsEnabled); err != nil {
			return newReconcileFailure(reasonPoolerReconciliationFailed, err)
		}
		return nil
	}
}

func (p *poolerModel) Observe(ctx context.Context, reconcileErr error) (componentHealth, error) {
	before := p.cluster.Status.DeepCopy()
	health, err := p.computeHealth(ctx, reconcileErr)
	statusErr := writeComponentStatus(p.updateStatus, before, health)
	return health, errors.Join(err, statusErr)
}

func (p *poolerModel) computeHealth(ctx context.Context, reconcileErr error) (componentHealth, error) {
	oldConditions := append([]metav1.Condition(nil), p.cluster.Status.Conditions...)

	if h, err, ok := classifyReconcileErr(reconcileErr, poolerReady, p.events, p.cluster, EventPoolerReconcileFailed, "connection pooler"); ok {
		return h, err
	}

	if !p.poolerEnabled() {
		if !isSANPolicyConverged(p.contracts.CNPGCluster, p.poolerEnabled()) {
			return newProvisioningHealth(poolerReady, reasonPoolerSANsPending, msgPoolerSANsPending), nil
		}
		p.cluster.Status.ConnectionPoolerStatus = nil
		meta.RemoveStatusCondition(&p.cluster.Status.Conditions, string(poolerReady))
		return newReadyHealth(poolerReady, reasonPoolerDisabled, msgPoolerDisabled), nil
	}
	if !p.poolerConfigPresent() {
		return newFailedHealth(poolerReady, reasonPoolerConfigMissing, msgPoolerConfigMissing), fmt.Errorf("pooler config missing")
	}
	if p.contracts.CNPGCluster == nil {
		return newPendingHealth(poolerReady, reasonCNPGProvisioning, msgCNPGPendingCreation), nil
	}
	if p.contracts.CNPGCluster.Status.Phase != cnpgv1.PhaseHealthy {
		return newProvisioningHealth(poolerReady, reasonCNPGProvisioning, fmt.Sprintf(msgFmtCNPGClusterPhase, p.contracts.CNPGCluster.Status.Phase)), nil
	}

	if !isSANPolicyConverged(p.contracts.CNPGCluster, p.poolerEnabled()) {
		return newProvisioningHealth(poolerReady, reasonPoolerSANsPending, msgPoolerSANsPending), nil
	}

	leafOK, leafErr := isServerTLSLeafAlignedWithSpec(ctx, p.client, p.cluster.Namespace, p.contracts.CNPGCluster)
	if errors.Is(leafErr, errServerTLSLeafInvalid) {
		logger := logging.FromContext(ctx)
		secretName := serverTLSSecretNameFromCNPG(p.contracts.CNPGCluster)
		logger.Error("server TLS secret cannot be parsed; cluster requires investigation",
			"error", leafErr.Error(), "namespace", p.cluster.Namespace,
			"pgCluster", p.cluster.Name, "secret", secretName)
		msg := fmt.Sprintf(string(msgFmtPoolerTLSLeafInvalidCert), p.cluster.Namespace, secretName)
		p.events.emitWarning(p.cluster, EventPoolerReconcileFailed, msg)
		return newFailedHealth(poolerReady, reasonPoolerTLSLeafInvalidCert, msg), leafErr
	}
	if leafErr != nil {
		msg := fmt.Sprintf("failed to verify server TLS leaf for PostgresCluster %s — check operator logs", p.cluster.Name)
		p.events.emitWarning(p.cluster, EventPoolerReconcileFailed, msg)
		return newFailedHealth(poolerReady, reasonPoolerReconciliationFailed, msg), leafErr
	}
	if !leafOK {
		return newProvisioningHealth(poolerReady, reasonPoolerTLSLeafPending, msgPoolerTLSLeafPending), nil
	}

	// TODO: Port material.
	rwExists, err := poolerExists(ctx, p.client, p.cluster, readWriteEndpoint)
	if err != nil {
		msg := fmt.Sprintf("failed to sync pooler status for PostgresCluster %s — check operator logs", p.cluster.Name)
		p.events.emitWarning(p.cluster, EventPoolerReconcileFailed, msg)
		return newFailedHealth(poolerReady, reasonPoolerReconciliationFailed, fmt.Sprintf("Failed to check RW pooler existence: %v", err)), err
	}
	roExists, err := poolerExists(ctx, p.client, p.cluster, readOnlyEndpoint)
	if err != nil {
		msg := fmt.Sprintf("failed to sync pooler status for PostgresCluster %s — check operator logs", p.cluster.Name)
		p.events.emitWarning(p.cluster, EventPoolerReconcileFailed, msg)
		return newFailedHealth(poolerReady, reasonPoolerReconciliationFailed, fmt.Sprintf("Failed to check RO pooler existence: %v", err)), err
	}
	if !rwExists || !roExists {
		p.events.emitPoolerCreationTransition(p.cluster, p.cluster.Status.Conditions)
		return newProvisioningHealth(poolerReady, reasonPoolerCreating, msgPoolersProvisioning), nil
	}

	rwPooler := &cnpgv1.Pooler{}
	if err := p.client.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(p.cluster.Name, readWriteEndpoint),
		Namespace: p.cluster.Namespace,
	}, rwPooler); err != nil {
		if !apierrors.IsNotFound(err) {
			return newFailedHealth(poolerReady, reasonPoolerReconciliationFailed, err.Error()), fmt.Errorf("getting RW pooler: %w", err)
		}
		p.events.emitPoolerCreationTransition(p.cluster, p.cluster.Status.Conditions)
		return newPendingHealth(poolerReady, reasonPoolerCreating, msgWaitRWPoolerObject), nil
	}
	roPooler := &cnpgv1.Pooler{}
	if err := p.client.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(p.cluster.Name, readOnlyEndpoint),
		Namespace: p.cluster.Namespace,
	}, roPooler); err != nil {
		if !apierrors.IsNotFound(err) {
			return newFailedHealth(poolerReady, reasonPoolerReconciliationFailed, err.Error()), fmt.Errorf("getting RO pooler: %w", err)
		}
		p.events.emitPoolerCreationTransition(p.cluster, p.cluster.Status.Conditions)
		return newPendingHealth(poolerReady, reasonPoolerCreating, msgWaitROPoolerObject), nil
	}
	if !arePoolersReady(rwPooler, roPooler) {
		p.events.emitPoolerCreationTransition(p.cluster, p.cluster.Status.Conditions)
		return newPendingHealth(poolerReady, reasonPoolerCreating, msgPoolersNotReady), nil
	}

	p.cluster.Status.ConnectionPoolerStatus = &enterprisev4.ConnectionPoolerStatus{Enabled: true}
	h := newReadyHealth(poolerReady, reasonAllInstancesReady, msgPoolersReady)
	p.events.emitPoolerReadyTransition(p.cluster, oldConditions)
	return h, nil
}

func poolerResourceName(clusterName, poolerType string) string {
	return fmt.Sprintf("%s%s%s", clusterName, defaultPoolerSuffix, poolerType)
}

func poolerExists(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster, poolerType string) (bool, error) {
	pooler := &cnpgv1.Pooler{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(cluster.Name, poolerType),
		Namespace: cluster.Namespace,
	}, pooler)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func arePoolersReady(rwPooler, roPooler *cnpgv1.Pooler) bool {
	return isPoolerReady(rwPooler) && isPoolerReady(roPooler)
}

// isPoolerReady checks if a pooler has all instances scheduled.
// CNPG PoolerStatus only tracks scheduled instances, not ready pods.
func isPoolerReady(pooler *cnpgv1.Pooler) bool {
	desired := int32(1)
	if pooler.Spec.Instances != nil {
		desired = *pooler.Spec.Instances
	}
	return pooler.Status.Instances >= desired
}

// createOrUpdateConnectionPoolers creates RW and RO poolers if they don't exist.
func createOrUpdateConnectionPoolers(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cfg *MergedConfig, cnpgCluster *cnpgv1.Cluster, poolerMetricsEnabled bool) error {
	if err := createConnectionPooler(ctx, c, scheme, cluster, cfg, cnpgCluster, readWriteEndpoint, poolerMetricsEnabled); err != nil {
		return fmt.Errorf("reconciling RW pooler: %w", err)
	}
	if err := createConnectionPooler(ctx, c, scheme, cluster, cfg, cnpgCluster, readOnlyEndpoint, poolerMetricsEnabled); err != nil {
		return fmt.Errorf("reconciling RO pooler: %w", err)
	}
	return nil
}

func createConnectionPooler(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cfg *MergedConfig, cnpgCluster *cnpgv1.Cluster, poolerType string, poolerMetricsEnabled bool) error {
	logger := logging.FromContext(ctx).With("func", "createConnectionPooler")
	poolerName := poolerResourceName(cluster.Name, poolerType)
	existing := &cnpgv1.Pooler{}
	err := c.Get(ctx, types.NamespacedName{Name: poolerName, Namespace: cluster.Namespace}, existing)
	if err == nil {
		return nil // already exists
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	logger.InfoContext(ctx, "CNPG Pooler creation started", "name", poolerName, "type", poolerType)
	pooler, err := buildCNPGPooler(scheme, cluster, cfg, cnpgCluster, poolerType, poolerMetricsEnabled)
	if err != nil {
		return err
	}
	return c.Create(ctx, pooler)
}

func buildCNPGPooler(scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cfg *MergedConfig, cnpgCluster *cnpgv1.Cluster, poolerType string, poolerMetricsEnabled bool) (*cnpgv1.Pooler, error) {
	pc := cfg.CNPG.ConnectionPooler
	instances := *pc.Instances
	mode := cnpgv1.PgBouncerPoolMode(*pc.Mode)
	pooler := &cnpgv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, poolerType), Namespace: cluster.Namespace},
		Spec: cnpgv1.PoolerSpec{
			Cluster:   cnpgv1.LocalObjectReference{Name: cnpgCluster.Name},
			Instances: &instances,
			Type:      cnpgv1.PoolerType(poolerType),
			PgBouncer: &cnpgv1.PgBouncerSpec{
				PoolMode:   mode,
				Parameters: pc.Config,
			},
		},
	}
	poolerAnnotations := make(map[string]string)
	if poolerMetricsEnabled {
		poolerAnnotations = buildPoolerScrapeAnnotations()
	}
	// Template is always set so that annotation removal is explicit in merge patches.
	// CNPG's Pooler CRD requires template.spec.containers to be present — a minimal
	// named container lets CNPG's podspec builder merge in the real PgBouncer
	// image/command/ports while still carrying our annotations.
	pooler.Spec.Template = &cnpgv1.PodTemplateSpec{
		ObjectMeta: cnpgv1.Metadata{Annotations: poolerAnnotations},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "pgbouncer"}},
		},
	}
	if err := ctrl.SetControllerReference(cluster, pooler, scheme); err != nil {
		return nil, fmt.Errorf("setting controller reference on CNPG pooler: %w", err)
	}
	return pooler, nil
}

// deleteConnectionPoolers removes RW and RO poolers if they exist.
func deleteConnectionPoolers(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster) error {
	logger := logging.FromContext(ctx).With("func", "deleteConnectionPoolers")
	for _, poolerType := range []string{readWriteEndpoint, readOnlyEndpoint} {
		poolerName := poolerResourceName(cluster.Name, poolerType)
		exist, err := poolerExists(ctx, c, cluster, poolerType)
		if err != nil {
			return fmt.Errorf("checking pooler existence: %w", err)
		}
		if !exist {
			continue
		}
		pooler := &cnpgv1.Pooler{}
		if err := c.Get(ctx, types.NamespacedName{Name: poolerName, Namespace: cluster.Namespace}, pooler); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("getting pooler %s: %w", poolerName, err)
		}
		logger.InfoContext(ctx, "CNPG Pooler deletion started", "name", poolerName)
		if err := c.Delete(ctx, pooler); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting pooler %s: %w", poolerName, err)
		}
	}
	return nil
}
