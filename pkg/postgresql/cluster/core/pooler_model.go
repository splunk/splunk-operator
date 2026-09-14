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
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/logging"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	pgcnpg "github.com/splunk/splunk-operator/pkg/postgresql/shared/cnpg"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
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
	cluster        *platformv1alpha1.PostgresCluster
	clusterClass   *platformv1alpha1.PostgresClusterClass
	mergedConfig   *MergedConfig
	contracts      *reconcileContracts
	metricsEnabled bool
}

func newPoolerModel(c client.Client, scheme *runtime.Scheme, events poolerEmitter, updateStatus healthStatusUpdater, cluster *platformv1alpha1.PostgresCluster, clusterClass *platformv1alpha1.PostgresClusterClass, mergedConfig *MergedConfig, contracts *reconcileContracts) *poolerModel {
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
		isPoolerEnabled(p.mergedConfig.Spec.ConnectionPooler)
}

func (p *poolerModel) poolerConfigPresent() bool {
	return p.mergedConfig != nil && p.mergedConfig.CNPG != nil &&
		p.mergedConfig.CNPG.ConnectionPooler != nil
}

// roPoolerWanted reports whether the read-only pooler resource should be
// reconciled. Combines the user opt-in (connectionPooler.readOnly, default
// true) with the declared instance count (spec.instances >= the RO threshold).
// Using the declared count rather than ready replicas avoids resource churn
// during transient ready-count dips on scale events.
func (p *poolerModel) roPoolerWanted() bool {
	if p.mergedConfig == nil || p.mergedConfig.Spec == nil || p.mergedConfig.Spec.Instances == nil {
		return false
	}
	if !poolerReadOnlyWanted(p.mergedConfig.Spec.ConnectionPooler) {
		return false
	}
	return *p.mergedConfig.Spec.Instances >= pgcnpg.MinInstancesForReadOnly
}

// rwPoolerWanted reports whether the read-write pooler resource should be
// reconciled. Driven by the user opt-in (connectionPooler.readWrite, default
// true). Validation rejects "enabled with neither RW nor RO" upstream, so
// when poolerEnabled is true at least one of rw/ro will be wanted.
func (p *poolerModel) rwPoolerWanted() bool {
	if p.mergedConfig == nil || p.mergedConfig.Spec == nil {
		return false
	}
	return poolerReadWriteWanted(p.mergedConfig.Spec.ConnectionPooler)
}

// isPoolerEnabled reports whether the connection pooler is enabled by the
// supplied ConnectionPoolerEnableConfig (nil-safe).
func isPoolerEnabled(c *platformv1alpha1.ConnectionPoolerEnableConfig) bool {
	return c != nil && c.Enabled != nil && *c.Enabled
}

// poolerReadWriteWanted reports whether the RW pooler is opted-in. Default is
// true when the parent struct exists; consumers should pair this with
// isPoolerEnabled before acting on it.
func poolerReadWriteWanted(c *platformv1alpha1.ConnectionPoolerEnableConfig) bool {
	if c == nil {
		return false
	}
	return c.ReadWrite == nil || *c.ReadWrite
}

// poolerReadOnlyWanted reports whether the RO pooler is opted-in. Default is
// true when the parent struct exists; consumers should pair this with
// isPoolerEnabled and the instances>=2 check before acting on it.
func poolerReadOnlyWanted(c *platformv1alpha1.ConnectionPoolerEnableConfig) bool {
	if c == nil {
		return false
	}
	return c.ReadOnly == nil || *c.ReadOnly
}

// PoolerReadOnlyRequested reports whether the merged config opts into the RO
// pooler. It does not consider instance count — callers enforce >=2 separately.
func PoolerReadOnlyRequested(merged *MergedConfig) bool {
	if merged == nil {
		return false
	}
	c := merged.Spec.ConnectionPooler
	return isPoolerEnabled(c) && poolerReadOnlyWanted(c)
}

// mergeConnectionPoolerEnable overlays cluster-level ConnectionPoolerEnableConfig
// on top of the class-level defaults at the sub-field granularity, so cluster
// overrides one field (e.g. ReadOnly) without dropping the class-supplied
// values for the rest. Returns nil only when both inputs are nil.
func mergeConnectionPoolerEnable(cluster, class *platformv1alpha1.ConnectionPoolerEnableConfig) *platformv1alpha1.ConnectionPoolerEnableConfig {
	if cluster == nil && class == nil {
		return nil
	}
	out := &platformv1alpha1.ConnectionPoolerEnableConfig{}
	if cluster != nil {
		out.Enabled = cluster.Enabled
		out.ReadWrite = cluster.ReadWrite
		out.ReadOnly = cluster.ReadOnly
	}
	if class != nil {
		if out.Enabled == nil {
			out.Enabled = class.Enabled
		}
		if out.ReadWrite == nil {
			out.ReadWrite = class.ReadWrite
		}
		if out.ReadOnly == nil {
			out.ReadOnly = class.ReadOnly
		}
	}
	return out
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
		rwWanted := p.rwPoolerWanted()
		roWanted := p.roPoolerWanted()
		// Defense-in-depth: ValidateCrossResource rejects this combination at
		// admission and at reconciler entry. The runtime guard catches any
		// future code path that bypasses both validation hooks.
		if !rwWanted && !roWanted {
			return newReconcileFailure(reasonPoolerConfigMissing,
				fmt.Errorf("connection pooler is enabled but no endpoint is opted in (set readWrite and/or readOnly to true)"))
		}
		if err := p.reconcilePoolerEndpoint(ctx, readWriteEndpoint, rwWanted); err != nil {
			return err
		}
		if err := p.reconcilePoolerEndpoint(ctx, readOnlyEndpoint, roWanted); err != nil {
			return err
		}
		return nil
	}
}

// reconcilePoolerEndpoint creates the pooler resource for poolerType when wanted
// is true, or deletes it when wanted is false. Reconcile errors are wrapped as
// *reconcileFailure so classifyReconcileErr surfaces the warning event and
// failed health in Observe.
func (p *poolerModel) reconcilePoolerEndpoint(ctx context.Context, poolerType string, wanted bool) error {
	var err error
	if wanted {
		err = createAndUpdateConnectionPooler(ctx, p.client, p.scheme, p.cluster, p.mergedConfig, p.contracts.CNPGCluster, poolerType, p.metricsEnabled)
	} else {
		err = deleteConnectionPooler(ctx, p.client, p.cluster, poolerType)
	}
	if err == nil {
		return nil
	}
	return newReconcileFailure(reasonPoolerReconciliationFailed, err)
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

	rwWanted := p.rwPoolerWanted()
	roWanted := p.roPoolerWanted()
	var rwExists, roExists bool
	var err error
	if rwWanted {
		rwExists, err = poolerExists(ctx, p.client, p.cluster, readWriteEndpoint)
		if err != nil {
			msg := fmt.Sprintf("failed to sync pooler status for PostgresCluster %s — check operator logs", p.cluster.Name)
			p.events.emitWarning(p.cluster, EventPoolerReconcileFailed, msg)
			return newFailedHealth(poolerReady, reasonPoolerReconciliationFailed, fmt.Sprintf("Failed to check RW pooler existence: %v", err)), err
		}
	}
	if roWanted {
		roExists, err = poolerExists(ctx, p.client, p.cluster, readOnlyEndpoint)
		if err != nil {
			msg := fmt.Sprintf("failed to sync pooler status for PostgresCluster %s — check operator logs", p.cluster.Name)
			p.events.emitWarning(p.cluster, EventPoolerReconcileFailed, msg)
			return newFailedHealth(poolerReady, reasonPoolerReconciliationFailed, fmt.Sprintf("Failed to check RO pooler existence: %v", err)), err
		}
	}
	if (rwWanted && !rwExists) || (roWanted && !roExists) {
		p.events.emitPoolerCreationTransition(p.cluster, p.cluster.Status.Conditions)
		return newProvisioningHealth(poolerReady, reasonPoolerCreating, msgPoolersProvisioning), nil
	}

	var rwPooler *cnpgv1.Pooler
	if rwWanted {
		rwPooler = &cnpgv1.Pooler{}
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
	}
	var roPooler *cnpgv1.Pooler
	if roWanted {
		roPooler = &cnpgv1.Pooler{}
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
	}
	if !arePoolersReady(rwPooler, roPooler) {
		p.events.emitPoolerCreationTransition(p.cluster, p.cluster.Status.Conditions)
		return newPendingHealth(poolerReady, reasonPoolerCreating, msgPoolersNotReady), nil
	}

	p.cluster.Status.ConnectionPoolerStatus = &platformv1alpha1.ConnectionPoolerStatus{
		Enabled:          true,
		ReadWriteEnabled: rwWanted,
		ReadOnlyEnabled:  roWanted,
	}
	h := newReadyHealth(poolerReady, reasonAllInstancesReady, msgPoolersReady)
	p.events.emitPoolerReadyTransition(p.cluster, oldConditions)
	return h, nil
}

func poolerResourceName(clusterName, poolerType string) string {
	return fmt.Sprintf("%s%s%s", clusterName, defaultPoolerSuffix, poolerType)
}

func poolerExists(ctx context.Context, c client.Client, cluster *platformv1alpha1.PostgresCluster, poolerType string) (bool, error) {
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

// arePoolersReady reports whether each supplied pooler is ready. A nil
// pointer for either side means that pooler is not wanted and is skipped.
// At least one of the two is expected to be non-nil; both nil returns true
// (the upstream "no endpoint enabled" check is enforced separately).
func arePoolersReady(rwPooler, roPooler *cnpgv1.Pooler) bool {
	if rwPooler != nil && !isPoolerReady(rwPooler) {
		return false
	}
	if roPooler != nil && !isPoolerReady(roPooler) {
		return false
	}
	return true
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
func createOrUpdateConnectionPoolers(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *platformv1alpha1.PostgresCluster, cfg *MergedConfig, cnpgCluster *cnpgv1.Cluster, poolerMetricsEnabled bool) error {
	if err := createAndUpdateConnectionPooler(ctx, c, scheme, cluster, cfg, cnpgCluster, readWriteEndpoint, poolerMetricsEnabled); err != nil {
		return fmt.Errorf("reconciling RW pooler: %w", err)
	}
	if err := createAndUpdateConnectionPooler(ctx, c, scheme, cluster, cfg, cnpgCluster, readOnlyEndpoint, poolerMetricsEnabled); err != nil {
		return fmt.Errorf("reconciling RO pooler: %w", err)
	}
	return nil
}

func createAndUpdateConnectionPooler(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *platformv1alpha1.PostgresCluster, cfg *MergedConfig, cnpgCluster *cnpgv1.Cluster, poolerType string, poolerMetricsEnabled bool) error {
	logger := logging.FromContext(ctx).With("func", "createAndUpdateConnectionPooler")
	poolerName := poolerResourceName(cluster.Name, poolerType)
	existing := &cnpgv1.Pooler{}
	err := c.Get(ctx, types.NamespacedName{Name: poolerName, Namespace: cluster.Namespace}, existing)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if apierrors.IsNotFound(err) {
		desired, err := buildCNPGPooler(scheme, cluster, cfg, cnpgCluster, poolerType, poolerMetricsEnabled)
		if err != nil {
			return err
		}
		logger.InfoContext(ctx, "CNPG Pooler creation started", "name", poolerName, "type", poolerType)
		return c.Create(ctx, desired)
	}

	desired, err := buildCNPGPooler(scheme, cluster, cfg, cnpgCluster, poolerType, poolerMetricsEnabled)
	if err != nil {
		return err
	}
	original := existing.DeepCopy()
	existing.Spec = desired.Spec
	if err := ctrl.SetControllerReference(cluster, existing, scheme); err != nil {
		return fmt.Errorf("setting controller reference on existing CNPG pooler: %w", err)
	}
	if equality.Semantic.DeepEqual(normalizeCNPGPoolerSpec(original.Spec), normalizeCNPGPoolerSpec(existing.Spec)) &&
		equality.Semantic.DeepEqual(original.OwnerReferences, existing.OwnerReferences) {
		return nil
	}
	logger.InfoContext(ctx, "CNPG Pooler update started", "name", poolerName, "type", poolerType)
	return patchObject(ctx, c, original, existing, "Pooler")
}

func normalizeCNPGPoolerSpec(spec cnpgv1.PoolerSpec) normalizedCNPGPoolerSpec {
	normalized := normalizedCNPGPoolerSpec{
		ClusterName: spec.Cluster.Name,
		Type:        string(spec.Type),
	}
	if normalized.Type == "" {
		normalized.Type = string(cnpgv1.PoolerTypeRW)
	}
	if spec.Instances != nil {
		normalized.Instances = *spec.Instances
	} else {
		normalized.Instances = 1
	}
	if spec.PgBouncer != nil {
		normalized.PoolMode = string(spec.PgBouncer.PoolMode)
		if normalized.PoolMode == "" {
			normalized.PoolMode = string(cnpgv1.PgBouncerPoolModeSession)
		}
		if len(spec.PgBouncer.Parameters) > 0 {
			normalized.Parameters = spec.PgBouncer.Parameters
		}
	}
	if spec.Template != nil {
		if len(spec.Template.ObjectMeta.Annotations) > 0 {
			normalized.TemplateAnnotations = spec.Template.ObjectMeta.Annotations
		}
		for _, container := range spec.Template.Spec.Containers {
			normalized.TemplateContainers = append(normalized.TemplateContainers, container.Name)
		}
	}
	return normalized
}

func buildCNPGPooler(scheme *runtime.Scheme, cluster *platformv1alpha1.PostgresCluster, cfg *MergedConfig, cnpgCluster *cnpgv1.Cluster, poolerType string, poolerMetricsEnabled bool) (*cnpgv1.Pooler, error) {
	if cfg == nil || cfg.CNPG == nil || cfg.CNPG.ConnectionPooler == nil {
		return nil, fmt.Errorf("connection pooler config is required")
	}
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
func deleteConnectionPoolers(ctx context.Context, c client.Client, cluster *platformv1alpha1.PostgresCluster) error {
	for _, poolerType := range []string{readWriteEndpoint, readOnlyEndpoint} {
		if err := deleteConnectionPooler(ctx, c, cluster, poolerType); err != nil {
			return err
		}
	}
	return nil
}

// deleteConnectionPooler removes a single pooler (by type) if it exists.
func deleteConnectionPooler(ctx context.Context, c client.Client, cluster *platformv1alpha1.PostgresCluster, poolerType string) error {
	logger := logging.FromContext(ctx).With("func", "deleteConnectionPooler")
	poolerName := poolerResourceName(cluster.Name, poolerType)
	exist, err := poolerExists(ctx, c, cluster, poolerType)
	if err != nil {
		return fmt.Errorf("checking pooler existence: %w", err)
	}
	if !exist {
		return nil
	}
	pooler := &cnpgv1.Pooler{}
	if err := c.Get(ctx, types.NamespacedName{Name: poolerName, Namespace: cluster.Namespace}, pooler); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("getting pooler %s: %w", poolerName, err)
	}
	logger.InfoContext(ctx, "CNPG Pooler deletion started", "name", poolerName)
	if err := c.Delete(ctx, pooler); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting pooler %s: %w", poolerName, err)
	}
	return nil
}
