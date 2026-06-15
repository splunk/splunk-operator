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
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgpostgres "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type clusterModel struct {
	client       client.Client
	scheme       *runtime.Scheme
	events       eventEmitter
	updateStatus healthStatusUpdater
	cluster      *enterprisev4.PostgresCluster
	clusterClass *enterprisev4.PostgresClusterClass
	mergedConfig *MergedConfig
	contracts    *reconcileContracts
	cnpgCluster  *cnpgv1.Cluster
	cnpgCreated  bool
	// cnpgPatch classifies this reconcile's CNPG spec change. Observe uses
	// requiresPhaseGate() to decide whether to hold ClusterReady=Provisioning
	// while CNPG.Status.Phase still reflects the pre-patch value.
	cnpgPatch cnpgPatchKind

	metricsEnabled bool
}

func newClusterModel(c client.Client, scheme *runtime.Scheme, events eventEmitter, updateStatus healthStatusUpdater, cluster *enterprisev4.PostgresCluster, clusterClass *enterprisev4.PostgresClusterClass, mergedConfig *MergedConfig, contracts *reconcileContracts) *clusterModel {
	model := &clusterModel{
		client: c, scheme: scheme,
		events: events, updateStatus: updateStatus,
		cluster: cluster, clusterClass: clusterClass, mergedConfig: mergedConfig,
		contracts: contracts,
	}
	model.metricsEnabled = isPostgreSQLMetricsEnabled(cluster, clusterClass)
	return model
}

func (p *clusterModel) Name() string            { return pgcConstants.ComponentProvisioner }
func (p *clusterModel) Requires() []contractKey { return []contractKey{contractSecret} }
func (p *clusterModel) Provides() []contractKey { return []contractKey{contractCNPGCluster} }

func (p *clusterModel) CheckContracts() error {
	if !checkContractsFromRequirements(p.Requires(), p.contracts) {
		return errContractsNotReady
	}
	return nil
}

func (p *clusterModel) Reconcile(ctx context.Context) error {
	p.cnpgCreated = false
	p.cnpgPatch = cnpgPatchNone

	poolerEnabled := p.mergedConfig != nil && p.mergedConfig.Spec != nil &&
		isPoolerEnabled(p.mergedConfig.Spec.ConnectionPooler)

	existingCNPG := &cnpgv1.Cluster{}
	err := p.client.Get(ctx, types.NamespacedName{Name: p.cluster.Name, Namespace: p.cluster.Namespace}, existingCNPG)

	desiredSpec := buildCNPGClusterSpec(*existingCNPG.Spec.DeepCopy(), p.mergedConfig, p.contracts.Secret.Name, p.metricsEnabled)
	desiredSpec.PostgresConfiguration.Parameters = maps.Clone(existingCNPG.Spec.PostgresConfiguration.Parameters)
	applyPoolerSANs(&desiredSpec, poolerEnabled, p.cluster.Name, p.cluster.Namespace)
	if err != nil && !apierrors.IsNotFound(err) {
		return newReconcileFailure(reasonClusterGetFailed, err)
	}

	if apierrors.IsNotFound(err) {
		newCluster, err := buildCNPGCluster(p.scheme, p.cluster, p.mergedConfig, p.contracts.Secret.Name, p.metricsEnabled)
		if err != nil {
			return newReconcileFailure(reasonClusterBuildFailed, err)
		}
		applyPoolerSANs(&newCluster.Spec, poolerEnabled, p.cluster.Name, p.cluster.Namespace)
		desiredParameters := maps.Clone(newCluster.Spec.PostgresConfiguration.Parameters)
		newCluster.Spec.PostgresConfiguration.Parameters = nil
		if err = p.client.Create(ctx, newCluster); err != nil {
			return newReconcileFailure(reasonClusterBuildFailed, err)
		}
		if err := applyPostgreSQLParameters(ctx, p.client, newCluster, desiredParameters); err != nil {
			return newReconcileFailure(reasonClusterBuildFailed, err)
		}
		createdCNPG := &cnpgv1.Cluster{}
		if err := p.client.Get(ctx, client.ObjectKeyFromObject(newCluster), createdCNPG); err != nil {
			return newReconcileFailure(reasonClusterGetFailed, err)
		}
		p.cnpgPatch = cnpgPatchBody
		p.events.emitNormal(p.cluster, EventClusterCreationStarted, fmt.Sprintf("CNPG cluster created for PostgresCluster %s, waiting for healthy state", p.cluster.Name))
		p.cnpgCluster = createdCNPG
		p.cnpgCreated = true
		return nil
	}

	p.cnpgCluster = existingCNPG
	hasOwnerRef, ownerRefErr := controllerutil.HasOwnerReference(p.cnpgCluster.GetOwnerReferences(), p.cluster, p.scheme)
	if ownerRefErr != nil {
		return newReconcileFailure(reasonClusterGetFailed, fmt.Errorf("failed to check owner reference on CNPG cluster: %w", ownerRefErr))
	}
	if !hasOwnerRef {
		originalCNPG := p.cnpgCluster.DeepCopy()
		if err := ctrl.SetControllerReference(p.cluster, p.cnpgCluster, p.scheme); err != nil {
			return newReconcileFailure(reasonClusterPatchFailed, fmt.Errorf("failed to set controller reference on existing CNPG cluster: %w", err))
		}
		if err := patchObject(ctx, p.client, originalCNPG, p.cnpgCluster, "CNPGCluster"); err != nil {
			return newReconcileFailure(reasonClusterPatchFailed, err)
		}
		p.events.emitNormal(p.cluster, EventClusterAdopted, fmt.Sprintf("Adopted existing CNPG cluster for PostgresCluster %s", p.cluster.Name))
		p.cnpgPatch = cnpgPatchMetadata
	}

	currentNormalized := normalizeCNPGClusterSpec(p.cnpgCluster.Spec)
	desiredNormalized := normalizeCNPGClusterSpec(desiredSpec)
	specDrift := !equality.Semantic.DeepEqual(currentNormalized, desiredNormalized)
	updateMessage := fmt.Sprintf("CNPG cluster spec updated for PostgresCluster %s, waiting for healthy state", p.cluster.Name)
	needsUpdateEvent := false

	if specDrift {
		originalCluster := p.cnpgCluster.DeepCopy()
		patchKind := cnpgPatchMetadata
		if isClusterDrift(currentNormalized, desiredNormalized) {
			patchKind = cnpgPatchBody
		}
		p.cnpgCluster.Spec = desiredSpec
		if err := patchObject(ctx, p.client, originalCluster, p.cnpgCluster, "CNPGCluster"); err != nil {
			return newReconcileFailure(reasonClusterPatchFailed, err)
		}
		needsUpdateEvent = true
		if p.cnpgPatch != cnpgPatchBody {
			p.cnpgPatch = patchKind
		}
		if err := p.client.Get(ctx, client.ObjectKeyFromObject(p.cnpgCluster), p.cnpgCluster); err != nil {
			return newReconcileFailure(reasonClusterGetFailed, err)
		}
	}
	beforeGeneration := p.cnpgCluster.Generation
	if err := applyPostgreSQLParameters(ctx, p.client, p.cnpgCluster, p.mergedConfig.Spec.PostgreSQLConfig); err != nil {
		return newReconcileFailure(reasonClusterPatchFailed, err)
	}
	updatedCNPG := &cnpgv1.Cluster{}
	if err := p.client.Get(ctx, client.ObjectKeyFromObject(p.cnpgCluster), updatedCNPG); err != nil {
		return newReconcileFailure(reasonClusterGetFailed, err)
	}
	p.cnpgCluster = updatedCNPG
	if updatedCNPG.Generation != beforeGeneration {
		p.cnpgPatch = cnpgPatchBody
		needsUpdateEvent = true
	}
	if needsUpdateEvent {
		p.events.emitNormal(p.cluster, EventClusterUpdateStarted, updateMessage)
	}

	p.contracts.CNPGCluster = p.cnpgCluster
	return nil
}

func (p *clusterModel) Observe(_ context.Context, reconcileErr error) (componentHealth, error) {
	before := p.cluster.Status.DeepCopy()
	health, err := p.computeHealth(reconcileErr)
	statusErr := writeComponentStatus(p.updateStatus, before, health)
	return health, errors.Join(err, statusErr)
}

func (p *clusterModel) computeHealth(reconcileErr error) (componentHealth, error) {
	if h, err, ok := classifyReconcileErr(reconcileErr, clusterReady, p.events, p.cluster, EventClusterCreateFailed, "CNPG cluster"); ok {
		return h, err
	}

	if p.cnpgCluster == nil || p.cnpgCreated {
		return newPendingHealth(clusterReady, reasonCNPGProvisioning, msgCNPGPendingCreation), nil
	}

	p.cluster.Status.ProvisionerRef = &corev1.ObjectReference{
		APIVersion: "postgresql.cnpg.io/v1",
		Kind:       "Cluster",
		Namespace:  p.cnpgCluster.Namespace,
		Name:       p.cnpgCluster.Name,
		UID:        p.cnpgCluster.UID,
	}
	p.cluster.Status.Instances = ptr.To(int32(p.cnpgCluster.Status.Instances))
	p.cluster.Status.ReadyInstances = ptr.To(int32(p.cnpgCluster.Status.ReadyInstances))
	p.cluster.Status.CurrentPrimary = ptr.To(p.cnpgCluster.Status.CurrentPrimary)

	if p.cnpgPatch.requiresPhaseGate() && (p.cnpgCluster.Status.Phase == cnpgv1.PhaseHealthy || p.cnpgCluster.Status.Phase == "") {
		return newProvisioningHealth(clusterReady, reasonCNPGProvisioning, fmt.Sprintf(msgFmtCNPGClusterPhase, p.cnpgCluster.Status.Phase)), nil
	}

	phase := p.cnpgCluster.Status.Phase
	var convergeErr error
	var health componentHealth

	switch phase {
	case cnpgv1.PhaseHealthy:
		// CNPG holds Phase=Healthy throughout scale-down and the scale-out tail
		// (only Instances/ReadyInstances move). Report Provisioning here so
		// runComponents short-circuits at this component and the downstream
		// pooler + configMap never reconcile against a transient ready count —
		// scaling is owned entirely by the cluster component, and other
		// components react only once it has settled.
		if desired, ready, scaling := p.scaleInProgress(); scaling {
			health = newProvisioningHealth(clusterReady, reasonCNPGProvisioning, fmt.Sprintf(msgFmtCNPGScaling, ready, desired))
		} else {
			health = newReadyHealth(clusterReady, reasonCNPGClusterHealthy, msgProvisionerHealthy)
		}
	case cnpgv1.PhaseFirstPrimary, cnpgv1.PhaseCreatingReplica, cnpgv1.PhaseWaitingForInstancesToBeActive:
		health = newProvisioningHealth(clusterReady, reasonCNPGProvisioning, fmt.Sprintf(msgFmtCNPGProvisioning, phase))
	case cnpgv1.PhaseSwitchover:
		health = newConfiguringHealth(clusterReady, reasonCNPGSwitchover, msgCNPGSwitchover)
	case cnpgv1.PhaseFailOver:
		health = newConfiguringHealth(clusterReady, reasonCNPGFailingOver, msgCNPGFailingOver)
	case cnpgv1.PhaseInplacePrimaryRestart, cnpgv1.PhaseInplaceDeletePrimaryRestart:
		health = newConfiguringHealth(clusterReady, reasonCNPGRestarting, fmt.Sprintf(msgFmtCNPGRestarting, phase))
	case cnpgv1.PhaseUpgrade, cnpgv1.PhaseMajorUpgrade, cnpgv1.PhaseUpgradeDelayed, cnpgv1.PhaseOnlineUpgrading:
		health = newConfiguringHealth(clusterReady, reasonCNPGUpgrading, fmt.Sprintf(msgFmtCNPGUpgrading, phase))
	case cnpgv1.PhaseApplyingConfiguration:
		health = newConfiguringHealth(clusterReady, reasonCNPGApplyingConfig, msgCNPGApplyingConfiguration)
	case cnpgv1.PhaseReplicaClusterPromotion:
		health = newConfiguringHealth(clusterReady, reasonCNPGPromoting, msgCNPGPromoting)
	case cnpgv1.PhaseWaitingForUser:
		health = newFailedHealth(clusterReady, reasonCNPGWaitingForUser, msgCNPGWaitingForUser)
		convergeErr = fmt.Errorf("provisioner requires user action")
	case cnpgv1.PhaseUnrecoverable:
		health = newFailedHealth(clusterReady, reasonCNPGUnrecoverable, msgCNPGUnrecoverable)
		convergeErr = fmt.Errorf("provisioner unrecoverable")
	case cnpgv1.PhaseCannotCreateClusterObjects:
		health = newFailedHealth(clusterReady, reasonCNPGProvisioningFailed, msgCNPGCannotCreateObjects)
		convergeErr = fmt.Errorf("provisioner cannot create cluster objects")
	case cnpgv1.PhaseUnknownPlugin, cnpgv1.PhaseFailurePlugin:
		health = newFailedHealth(clusterReady, reasonCNPGPluginError, fmt.Sprintf(msgFmtCNPGPluginError, phase))
		convergeErr = fmt.Errorf("provisioner plugin error")
	case cnpgv1.PhaseImageCatalogError, cnpgv1.PhaseArchitectureBinaryMissing:
		health = newFailedHealth(clusterReady, reasonCNPGImageError, fmt.Sprintf(msgFmtCNPGImageError, phase))
		convergeErr = fmt.Errorf("provisioner image error")
	case "":
		health = newPendingHealth(clusterReady, reasonCNPGProvisioning, msgCNPGPendingCreation)
	default:
		health = newProvisioningHealth(clusterReady, reasonCNPGProvisioning, fmt.Sprintf(msgFmtCNPGClusterPhase, phase))
	}
	return health, convergeErr
}

// scaleInProgress reports whether desired and observed/ready instance counts
// disagree. Returns (_, _, false) when merged config or CNPG status is not yet
// available. computeHealth uses it to hold ClusterReady=Provisioning while CNPG
// reports Phase=Healthy during a scale, so downstream components are gated until
// the count settles.
func (p *clusterModel) scaleInProgress() (desired, ready int, scaling bool) {
	if p.mergedConfig == nil || p.mergedConfig.Spec == nil || p.mergedConfig.Spec.Instances == nil {
		return 0, 0, false
	}
	if p.cnpgCluster == nil {
		return 0, 0, false
	}
	desired = int(*p.mergedConfig.Spec.Instances)
	observed := p.cnpgCluster.Status.Instances
	ready = p.cnpgCluster.Status.ReadyInstances
	if desired == observed && desired == ready {
		return 0, 0, false
	}
	return desired, ready, true
}

// GetMergedConfig overlays PostgresCluster spec on top of the class defaults.
// Class values are used only where the cluster spec is silent.
// Returns the merged config without validation — call ValidateMergedConfig separately.
func GetMergedConfig(class *enterprisev4.PostgresClusterClass, cluster *enterprisev4.PostgresCluster) *MergedConfig {
	result := cluster.Spec.DeepCopy()

	// Config is optional on the class — apply defaults only when provided.
	if defaults := class.Spec.Config; defaults != nil {
		if result.Instances == nil {
			result.Instances = defaults.Instances
		}
		if result.PostgresVersion == nil {
			result.PostgresVersion = defaults.PostgresVersion
		}
		if result.Resources == nil {
			result.Resources = defaults.Resources
		}
		if result.Storage == nil {
			result.Storage = defaults.Storage
		}
		if len(result.PostgreSQLConfig) == 0 {
			result.PostgreSQLConfig = defaults.PostgreSQLConfig
		}
		if len(result.PgHBA) == 0 {
			result.PgHBA = defaults.PgHBA
		}
		result.ConnectionPooler = mergeConnectionPoolerEnable(result.ConnectionPooler, defaults.ConnectionPooler)
		if defaults.Backup != nil {
			if result.Backup == nil {
				result.Backup = defaults.Backup.DeepCopy()
			} else {
				if result.Backup.Enabled == nil {
					result.Backup.Enabled = defaults.Backup.Enabled
				}
				if result.Backup.Schedule == nil {
					result.Backup.Schedule = defaults.Backup.Schedule
				}
			}
		}
	}

	if result.PostgreSQLConfig == nil {
		result.PostgreSQLConfig = make(map[string]string)
	}
	if result.PgHBA == nil {
		result.PgHBA = make([]string, 0)
	}
	if result.Resources == nil {
		result.Resources = &corev1.ResourceRequirements{}
	}

	return &MergedConfig{Spec: result, CNPG: class.Spec.CNPG}
}

// ValidateCrossResource checks constraints that require both the class and the cluster to be visible.
// It is called from both the webhook (admission) and the reconciler (runtime fallback).
func ValidateCrossResource(class *enterprisev4.PostgresClusterClass, cluster *enterprisev4.PostgresCluster) []ConfigValidationError {
	var errs []ConfigValidationError

	if classConfig := class.Spec.Config; classConfig != nil {
		if cluster.Spec.PostgresVersion != nil && classConfig.PostgresVersion != nil {
			clusterMajor, clusterMinor := parseVersion(*cluster.Spec.PostgresVersion)
			classMajor, classMinor := parseVersion(*classConfig.PostgresVersion)
			if clusterMinor < 0 {
				clusterMinor = 0
			}
			if clusterMajor > 0 && classMajor > 0 {
				versionTooLow := clusterMajor < classMajor ||
					(clusterMajor == classMajor && classMinor >= 0 && clusterMinor < classMinor)
				if versionTooLow {
					errs = append(errs, ConfigValidationError{
						Field:   "spec.postgresVersion",
						Value:   *cluster.Spec.PostgresVersion,
						Message: "postgresVersion cannot be lower than class default (" + *classConfig.PostgresVersion + ")",
					})
				}
			}
		}
	}

	// The RO-pooler-needs-2 rule is deliberately NOT enforced here: the reconciler
	// tolerates instances<2 by suppressing the RO pooler (see roPoolerWanted), so
	// it is an admission-only fail-fast. Switchover has no such graceful path, so
	// it stays here where both admission and the reconciler enforce it.
	effectiveInstances := cluster.Spec.Instances
	if effectiveInstances == nil && class.Spec.Config != nil {
		effectiveInstances = class.Spec.Config.Instances
	}
	switchover := class.Spec.CNPG != nil &&
		class.Spec.CNPG.PrimaryUpdateMethod != nil &&
		*class.Spec.CNPG.PrimaryUpdateMethod == "switchover"
	if switchover && effectiveInstances != nil && *effectiveInstances < minInstancesForSwitchover {
		errs = append(errs, ConfigValidationError{
			Field:   "spec.instances",
			Value:   *effectiveInstances,
			Message: fmt.Sprintf("instances must be >= %d when PostgresClusterClass %q uses primaryUpdateMethod=switchover", minInstancesForSwitchover, class.Name),
		})
	}

	var classPooler *enterprisev4.ConnectionPoolerEnableConfig
	if class.Spec.Config != nil {
		classPooler = class.Spec.Config.ConnectionPooler
	}
	mergedPooler := mergeConnectionPoolerEnable(cluster.Spec.ConnectionPooler, classPooler)
	poolerEnabled := isPoolerEnabled(mergedPooler)
	if poolerEnabled && (class.Spec.CNPG == nil || class.Spec.CNPG.ConnectionPooler == nil) {
		errs = append(errs, ConfigValidationError{
			Field:   "spec.connectionPooler.enabled",
			Value:   true,
			Message: "connection pooler requires cnpg.connectionPooler configuration in PostgresClusterClass",
		})
	}
	if poolerEnabled && !poolerReadWriteWanted(mergedPooler) && !poolerReadOnlyWanted(mergedPooler) {
		errs = append(errs, ConfigValidationError{
			Field:   "spec.connectionPooler",
			Value:   "readWrite=false,readOnly=false",
			Message: "at least one of readWrite or readOnly must be enabled when connectionPooler.enabled is true",
		})
	}

	backupEnabled := (cluster.Spec.Backup != nil && cluster.Spec.Backup.Enabled != nil && *cluster.Spec.Backup.Enabled) ||
		(class.Spec.Config != nil && class.Spec.Config.Backup != nil && class.Spec.Config.Backup.Enabled != nil && *class.Spec.Config.Backup.Enabled)
	if backupEnabled && (class.Spec.CNPG == nil || class.Spec.CNPG.Backup == nil || class.Spec.CNPG.Backup.VolumeSnapshot == nil) {
		errs = append(errs, ConfigValidationError{
			Field:   "spec.backup.enabled",
			Value:   true,
			Message: "backup requires cnpg.backup.volumeSnapshot configuration in PostgresClusterClass",
		})
	}

	return errs
}

func parseVersion(version string) (major, minor int) {
	for i, ch := range version {
		if ch == '.' {
			major, _ = strconv.Atoi(version[:i])
			minor, _ = strconv.Atoi(version[i+1:])
			return major, minor
		}
	}
	major, _ = strconv.Atoi(version)
	return major, -1
}

// ValidateMergedConfig checks the merged configuration for required fields and cross-field constraints.
func ValidateMergedConfig(merged *MergedConfig, className string) []ConfigValidationError {
	var errs []ConfigValidationError

	if merged.Spec.Instances == nil {
		errs = append(errs, ConfigValidationError{Field: "spec.instances", Message: "must be set in PostgresCluster or PostgresClusterClass"})
	}
	if merged.Spec.PostgresVersion == nil {
		errs = append(errs, ConfigValidationError{Field: "spec.postgresVersion", Message: "must be set in PostgresCluster or PostgresClusterClass"})
	}
	if merged.Spec.Storage == nil {
		errs = append(errs, ConfigValidationError{Field: "spec.storage", Message: "must be set in PostgresCluster or PostgresClusterClass"})
	}
	if merged.Spec.Backup != nil && merged.Spec.Backup.Enabled != nil && *merged.Spec.Backup.Enabled {
		if merged.Spec.Backup.Schedule == nil || *merged.Spec.Backup.Schedule == "" {
			errs = append(errs, ConfigValidationError{Field: "spec.backup.schedule", Message: "backup.schedule is required when backup.enabled is true"})
		} else if len(strings.Fields(*merged.Spec.Backup.Schedule)) != 5 {
			errs = append(errs, ConfigValidationError{Field: "spec.backup.schedule", Message: "backup.schedule must be a 5-field cron expression (minute hour day month weekday)"})
		}
	}
	if err := validatePostgreSQLConfigNoCNPGFixedKeys(merged.Spec.PostgreSQLConfig); err != nil {
		errs = append(errs, ConfigValidationError{Field: "spec.postgresqlConfig", Message: err.Error()})
	}

	return errs
}

// validatePostgreSQLConfigNoCNPGFixedKeys rejects postgresqlConfig keys that CloudNativePG
// registers as fixed/blocked (see cnpgpostgres.FixedConfigurationParameters). Users must
// not set these; CNPG and the instance manager own them.
func validatePostgreSQLConfigNoCNPGFixedKeys(params map[string]string) error {
	if len(params) == 0 {
		return nil
	}
	invalid := make([]string, 0)
	for k := range params {
		if _, fixed := cnpgpostgres.FixedConfigurationParameters[k]; fixed {
			invalid = append(invalid, k)
		}
	}
	if len(invalid) == 0 {
		return nil
	}
	sort.Strings(invalid)
	return fmt.Errorf("postgresqlConfig must not set CNPG-managed parameters: %s", strings.Join(invalid, ", "))
}

// buildCNPGClusterSpec builds the desired CNPG ClusterSpec by mutating the live
// spec in-place so unowned fields (e.g. Managed) survive the patch.
// IMPORTANT: any field derived from user-controlled CRD fields must also appear in normalizeCNPGClusterSpec,
// otherwise external changes to those fields on the CNPG cluster will be silently ignored.
// Operator-controlled invariants (e.g. SuperuserSecret, EnableSuperuserAccess) are exempt — they
// are always the same value and are never exposed in the PostgresCluster CRD.
func buildCNPGClusterSpec(live cnpgv1.ClusterSpec, specCfg *MergedConfig, secretName string, postgresMetricsEnabled bool) cnpgv1.ClusterSpec {
	live.ImageName = fmt.Sprintf("ghcr.io/cloudnative-pg/postgresql:%s", *specCfg.Spec.PostgresVersion)
	live.Instances = int(*specCfg.Spec.Instances)
	live.PostgresConfiguration = cnpgv1.PostgresConfiguration{
		Parameters: maps.Clone(specCfg.Spec.PostgreSQLConfig),
		PgHBA:      specCfg.Spec.PgHBA,
	}
	live.SuperuserSecret = &cnpgv1.LocalObjectReference{Name: secretName}
	live.EnableSuperuserAccess = ptr.To(true)
	live.Bootstrap = &cnpgv1.BootstrapConfiguration{
		InitDB: &cnpgv1.BootstrapInitDB{
			Database: defaultDatabaseName,
			Owner:    superUsername,
			Secret:   &cnpgv1.LocalObjectReference{Name: secretName},
		},
	}
	live.StorageConfiguration = cnpgv1.StorageConfiguration{
		Size: specCfg.Spec.Storage.String(),
	}
	live.Resources = *specCfg.Spec.Resources
	if specCfg.CNPG != nil && specCfg.CNPG.PrimaryUpdateMethod != nil {
		live.PrimaryUpdateMethod = cnpgv1.PrimaryUpdateMethod(*specCfg.CNPG.PrimaryUpdateMethod)
	} else {
		live.PrimaryUpdateMethod = cnpgv1.PrimaryUpdateMethodRestart
	}
	annotations := make(map[string]string)
	if postgresMetricsEnabled {
		annotations = buildPostgresScrapeAnnotations()
	}
	live.InheritedMetadata = &cnpgv1.EmbeddedObjectMetadata{Annotations: annotations}
	live.Backup = nil
	if specCfg.Spec.Backup != nil && specCfg.Spec.Backup.Enabled != nil && *specCfg.Spec.Backup.Enabled && specCfg.CNPG != nil && specCfg.CNPG.Backup != nil && specCfg.CNPG.Backup.VolumeSnapshot != nil {
		live.Backup = buildCNPGBackupConfiguration(specCfg)
	}
	return live
}

func buildCNPGBackupConfiguration(cfg *MergedConfig) *cnpgv1.BackupConfiguration {
	backupCfg := &cnpgv1.BackupConfiguration{}
	if cfg.CNPG.Backup.Target != nil {
		backupCfg.Target = cnpgv1.BackupTarget(*cfg.CNPG.Backup.Target)
	}
	if vs := cfg.CNPG.Backup.VolumeSnapshot; vs != nil {
		backupCfg.VolumeSnapshot = buildVolumeSnapshotConfiguration(vs)
	}
	return backupCfg
}

func buildVolumeSnapshotConfiguration(vs *enterprisev4.CNPGVolumeSnapshotConfig) *cnpgv1.VolumeSnapshotConfiguration {
	vsCfg := &cnpgv1.VolumeSnapshotConfiguration{}
	if vs.ClassName != nil {
		vsCfg.ClassName = *vs.ClassName
	}
	if vs.WalClassName != nil {
		vsCfg.WalClassName = *vs.WalClassName
	}
	if vs.SnapshotOwnerReference != nil {
		vsCfg.SnapshotOwnerReference = cnpgv1.SnapshotOwnerReference(*vs.SnapshotOwnerReference)
	}
	vsCfg.Online = vs.Online
	vsCfg.Labels = vs.Labels
	vsCfg.Annotations = vs.Annotations
	return vsCfg
}

func buildCNPGCluster(scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cfg *MergedConfig, secretName string, postgresMetricsEnabled bool) (*cnpgv1.Cluster, error) {
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace},
		Spec:       buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, secretName, postgresMetricsEnabled),
	}
	if err := ctrl.SetControllerReference(cluster, cnpg, scheme); err != nil {
		return nil, fmt.Errorf("setting controller reference on CNPG cluster: %w", err)
	}
	return cnpg, nil
}

func normalizeCNPGClusterSpec(spec cnpgv1.ClusterSpec) normalizedCNPGClusterSpec {
	normalized := normalizedCNPGClusterSpec{
		ImageName:           stripImageRefForDrift(spec.ImageName),
		Instances:           spec.Instances,
		PrimaryUpdateMethod: string(spec.PrimaryUpdateMethod),
		StorageSize:         spec.StorageConfiguration.Size,
		Resources:           spec.Resources,
	}
	if len(spec.PostgresConfiguration.PgHBA) > 0 {
		normalized.PgHBA = spec.PostgresConfiguration.PgHBA
	}
	if spec.InheritedMetadata != nil && len(spec.InheritedMetadata.Annotations) > 0 {
		normalized.InheritedAnnotations = spec.InheritedMetadata.Annotations
	}
	if spec.Bootstrap != nil && spec.Bootstrap.InitDB != nil {
		normalized.DefaultDatabase = spec.Bootstrap.InitDB.Database
		normalized.Owner = spec.Bootstrap.InitDB.Owner
	}
	if spec.Certificates != nil && len(spec.Certificates.ServerAltDNSNames) > 0 {
		normalized.ServerAltDNSNames = spec.Certificates.ServerAltDNSNames
	}
	if spec.Backup != nil {
		normalized.Backup = &normalizedBackupSpec{
			Target: string(spec.Backup.Target),
		}
		if spec.Backup.VolumeSnapshot != nil {
			normalized.Backup.VolumeSnapshotClass = spec.Backup.VolumeSnapshot.ClassName
			normalized.Backup.WalClassName = spec.Backup.VolumeSnapshot.WalClassName
			normalized.Backup.SnapshotOwnerReference = string(spec.Backup.VolumeSnapshot.SnapshotOwnerReference)
			normalized.Backup.Online = spec.Backup.VolumeSnapshot.Online
			normalized.Backup.Labels = spec.Backup.VolumeSnapshot.Labels
			normalized.Backup.Annotations = spec.Backup.VolumeSnapshot.Annotations
		}
	}
	return normalized
}

// stripImageRefForDrift trims whitespace and strips an OCI digest suffix (@sha256:… / @…)
// so drift detection matches tag-only desired images against apiserver materialized refs.
func stripImageRefForDrift(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.Index(name, "@"); i >= 0 {
		return name[:i]
	}
	return name
}

// cnpgPatchKind classifies Reconcile's drift outcome so Observe can gate
// ClusterReady on CNPG.Status.Phase only for material changes — annotation
// drift propagates via metadata PATCH without a phase transition.
type cnpgPatchKind int

const (
	cnpgPatchNone     cnpgPatchKind = iota // no drift detected; nothing patched.
	cnpgPatchMetadata                      // InheritedAnnotations changed only; metadata-only.
	cnpgPatchBody                          // structural change; CNPG must observably reconcile.
)

// requiresPhaseGate reports whether Observe should hold ClusterReady=Provisioning
// while CNPG.Status.Phase still reflects the pre-patch value.
func (k cnpgPatchKind) requiresPhaseGate() bool { return k == cnpgPatchBody }

// isClusterDrift reports whether two normalized specs differ in any field CNPG
// must observably reconcile against. InheritedAnnotations is excluded (metadata-only).
func isClusterDrift(a, b normalizedCNPGClusterSpec) bool {
	a.InheritedAnnotations = nil
	b.InheritedAnnotations = nil
	return !equality.Semantic.DeepEqual(a, b)
}

func getServerAltDNSNames(cnpg *cnpgv1.Cluster) []string {
	if cnpg == nil || cnpg.Spec.Certificates == nil {
		return nil
	}
	return cnpg.Spec.Certificates.ServerAltDNSNames
}

// computeDesiredPoolerSANSet returns the desired serverAltDNSNames sorted
// lexicographically. When poolerEnabled is false, existing SANs are preserved
// so a transient toggle does not trigger CNPG cert rotation.
func computeDesiredPoolerSANSet(poolerEnabled bool, current []string, clusterName, namespace string) []string {
	set := make(map[string]struct{}, len(current))
	for _, s := range current {
		if s == "" {
			continue
		}
		set[s] = struct{}{}
	}
	if poolerEnabled {
		for _, s := range []string{
			fmt.Sprintf("%s.%s", poolerResourceName(clusterName, readWriteEndpoint), namespace),
			fmt.Sprintf("%s.%s%s", poolerResourceName(clusterName, readWriteEndpoint), namespace, poolerSANSuffix),
			fmt.Sprintf("%s.%s", poolerResourceName(clusterName, readOnlyEndpoint), namespace),
			fmt.Sprintf("%s.%s%s", poolerResourceName(clusterName, readOnlyEndpoint), namespace, poolerSANSuffix),
		} {
			set[s] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// applyPoolerSANs merges the desired pooler SAN set into the cluster spec's
// ServerAltDNSNames. It is called on desiredSpec before the drift comparison
// so that SAN changes are included in the single CNPG patch.
func applyPoolerSANs(spec *cnpgv1.ClusterSpec, poolerEnabled bool, clusterName, namespace string) {
	current := []string(nil)
	if spec.Certificates != nil {
		current = spec.Certificates.ServerAltDNSNames
	}
	desired := computeDesiredPoolerSANSet(poolerEnabled, current, clusterName, namespace)
	if sets.New(current...).Equal(sets.New(desired...)) {
		return
	}
	if spec.Certificates == nil {
		spec.Certificates = &cnpgv1.CertificatesConfiguration{}
	}
	spec.Certificates.ServerAltDNSNames = desired
}

// isSANPolicyConverged reports whether the contracts CNPGCluster snapshot has
// the desired pooler SANs. Pure comparison — no client call.
func isSANPolicyConverged(cnpg *cnpgv1.Cluster, poolerEnabled bool) bool {
	if cnpg == nil {
		return true
	}
	current := sets.New(getServerAltDNSNames(cnpg)...)
	desired := sets.New(computeDesiredPoolerSANSet(poolerEnabled, current.UnsortedList(), cnpg.Name, cnpg.Namespace)...)
	return current.Equal(desired)
}

func serverTLSSecretNameFromCNPG(cnpg *cnpgv1.Cluster) string {
	if cnpg == nil {
		return ""
	}
	if cnpg.Status.Certificates.ServerTLSSecret != "" {
		return cnpg.Status.Certificates.ServerTLSSecret
	}
	if cnpg.Spec.Certificates != nil && cnpg.Spec.Certificates.ServerTLSSecret != "" {
		return cnpg.Spec.Certificates.ServerTLSSecret
	}
	return ""
}

// isServerTLSLeafAlignedWithSpec checks whether the materialized TLS leaf cert
// covers all SANs declared in the CNPG cluster spec. Failure modes:
//   - no Cluster / no spec SANs    → (true,  nil)
//   - no Secret / no tls.crt       → (false, nil) — transient race with CNPG cert-controller
//   - SAN mismatch                 → (false, nil) — mid-rotation
//   - PEM/x509 parse failure       → (false, %w errServerTLSLeafInvalid)
func isServerTLSLeafAlignedWithSpec(ctx context.Context, c client.Client, namespace string, cnpg *cnpgv1.Cluster) (bool, error) {
	if cnpg == nil {
		return true, nil
	}
	specSANs := getServerAltDNSNames(cnpg)
	if len(specSANs) == 0 {
		return true, nil
	}
	secretName := serverTLSSecretNameFromCNPG(cnpg)
	if secretName == "" {
		return false, nil
	}
	var sec corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: secretName}, &sec); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	raw := sec.Data[corev1.TLSCertKey]
	if len(raw) == 0 {
		return false, nil
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		return false, fmt.Errorf("%w: PEM decode failed for secret %s/%s",
			errServerTLSLeafInvalid, namespace, secretName)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, fmt.Errorf("%w: x509 parse failed for secret %s/%s: %v",
			errServerTLSLeafInvalid, namespace, secretName, err)
	}
	for _, alt := range specSANs {
		if alt == "" {
			continue
		}
		if !slices.Contains(cert.DNSNames, alt) {
			return false, nil
		}
	}
	return true, nil
}

// buildPostgreSQLParametersPatch builds an SSA payload for CNPG spec.postgresql.parameters.
func buildPostgreSQLParametersPatch(cluster *cnpgv1.Cluster, params map[string]string) client.Object {
	parameters := maps.Clone(params)
	if parameters == nil {
		parameters = map[string]string{}
	}
	paramPatch := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": cnpgv1.SchemeGroupVersion.String(),
			"kind":       cnpgv1.ClusterKind,
			"metadata": map[string]any{
				"name":      cluster.Name,
				"namespace": cluster.Namespace,
			},
			"spec": map[string]any{
				"postgresql": map[string]any{
					"parameters": parameters,
				},
			},
		},
	}
	return paramPatch
}

// applyPostgreSQLParametersPatch sends one SSA payload for CNPG spec.postgresql.parameters.
func applyPostgreSQLParametersPatch(ctx context.Context, c client.Client, cluster *cnpgv1.Cluster, params map[string]string) error {
	patch := buildPostgreSQLParametersPatch(cluster, params)
	if err := c.Apply(
		ctx,
		client.ApplyConfigurationFromUnstructured(patch.(*unstructured.Unstructured)),
		client.FieldOwner(postgresqlParametersFieldManager),
	); err != nil {
		return fmt.Errorf("applying PostgreSQL parameters: %w", err)
	}

	return nil
}

// applyPostgreSQLParameters applies CNPG spec.postgresql.parameters with a dedicated SSA field manager.
func applyPostgreSQLParameters(ctx context.Context, c client.Client, cluster *cnpgv1.Cluster, params map[string]string) error {
	adoptionParams := postgreSQLParametersWithLegacyAdoption(cluster, params)
	if len(adoptionParams) > 0 {
		if err := applyPostgreSQLParametersPatch(ctx, c, cluster, adoptionParams); err != nil {
			return err
		}
	}

	return applyPostgreSQLParametersPatch(ctx, c, cluster, params)
}

// postgreSQLParametersWithLegacyAdoption returns a temporary SSA payload that includes parameters
// previously managed by the operator through MergeFrom patches. The following desired-only apply
// can then prune those keys because this field manager owns their managedFields entries.
func postgreSQLParametersWithLegacyAdoption(cluster *cnpgv1.Cluster, desired map[string]string) map[string]string {
	staleLegacyParameters := staleLegacyPostgreSQLParameters(cluster, desired)
	if len(staleLegacyParameters) == 0 {
		return nil
	}

	adoptionParams := maps.Clone(desired)
	if adoptionParams == nil {
		adoptionParams = map[string]string{}
	}
	maps.Copy(adoptionParams, staleLegacyParameters)
	return adoptionParams
}

// staleLegacyPostgreSQLParameters finds live parameters that were owned by the old merge-patch
// manager but are absent from desired config. These keys need one managedFields adoption apply
// before omission can prune them.
func staleLegacyPostgreSQLParameters(cluster *cnpgv1.Cluster, desired map[string]string) map[string]string {
	if cluster == nil || len(cluster.Spec.PostgresConfiguration.Parameters) == 0 {
		return nil
	}

	legacyOwned := legacyUpdatedPostgreSQLParameterKeys(cluster.ManagedFields)
	if len(legacyOwned) == 0 {
		return nil
	}

	applyOwners := appliedPostgreSQLParameterOwners(cluster.ManagedFields)
	stale := map[string]string{}
	for key, value := range cluster.Spec.PostgresConfiguration.Parameters {
		_, inDesired := desired[key]
		_, isLegacy := legacyOwned[key]

		applyOwner := applyOwners[key]
		externallyApplied := applyOwner != "" && applyOwner != postgresqlParametersFieldManager

		if inDesired || !isLegacy || externallyApplied || isCNPGManagedPostgreSQLParameter(key) {
			continue
		}

		stale[key] = value
	}
	return stale
}

// legacyUpdatedPostgreSQLParameterKeys returns parameter keys owned by the old controller-runtime
// update manager used before parameters moved to SSA.
func legacyUpdatedPostgreSQLParameterKeys(managedFields []metav1.ManagedFieldsEntry) map[string]struct{} {
	keys := map[string]struct{}{}
	for _, field := range managedFields {
		if field.Manager != legacyPostgreSQLParametersUpdateManager ||
			field.Operation != metav1.ManagedFieldsOperationUpdate ||
			field.FieldsV1 == nil {
			continue
		}
		for _, key := range parsePostgreSQLParameterFieldNames(field.FieldsV1.GetRawBytes()) {
			keys[key] = struct{}{}
		}
	}
	return keys
}

// appliedPostgreSQLParameterOwners returns per-key SSA managers for PostgreSQL parameters.
func appliedPostgreSQLParameterOwners(managedFields []metav1.ManagedFieldsEntry) map[string]string {
	owners := map[string]string{}
	for _, field := range managedFields {
		if field.Operation != metav1.ManagedFieldsOperationApply || field.FieldsV1 == nil {
			continue
		}
		for _, key := range parsePostgreSQLParameterFieldNames(field.FieldsV1.GetRawBytes()) {
			owners[key] = field.Manager
		}
	}
	return owners
}

// parsePostgreSQLParameterFieldNames extracts f:<parameter> keys from managedFields fieldsV1.
func parsePostgreSQLParameterFieldNames(raw []byte) []string {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}

	spec, _ := fields["f:spec"].(map[string]any)
	postgresql, _ := spec["f:postgresql"].(map[string]any)
	parameters, _ := postgresql["f:parameters"].(map[string]any)
	names := make([]string, 0, len(parameters))
	for key := range parameters {
		name, found := strings.CutPrefix(key, "f:")
		if found && name != "" {
			names = append(names, name)
		}
	}
	return names
}

// isCNPGManagedPostgreSQLParameter reports whether a parameter is known to be CNPG fixed,
// defaulted, or mandatory and should not be adopted as legacy user intent.
func isCNPGManagedPostgreSQLParameter(key string) bool {
	if _, fixed := cnpgpostgres.FixedConfigurationParameters[key]; fixed {
		return true
	}
	if _, defaulted := cnpgpostgres.CnpgConfigurationSettings.GlobalDefaultSettings[key]; defaulted {
		return true
	}
	if _, mandatory := cnpgpostgres.CnpgConfigurationSettings.MandatorySettings[key]; mandatory {
		return true
	}
	return false
}
