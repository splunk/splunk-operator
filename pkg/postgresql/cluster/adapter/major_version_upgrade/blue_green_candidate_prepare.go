/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package majorupgradeadapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	clustercore "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	certmanagerinfra "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/certmanager"
	clusterCnpg "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/cnpg"
	tlsport "github.com/splunk/splunk-operator/pkg/postgresql/cluster/ports/tls"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BlueGreenCandidatePlan carries the target shape. The frozen receipt is
// checked against source, target plan, and managed database declarations.
// It never authorizes a candidate stage to write source resources.
type BlueGreenCandidatePlan struct {
	TargetImage         string
	TargetStorage       *resource.Quantity
	TargetInstances     int
	TargetConfiguration *platformv1alpha1.PostgresClusterSpec
}

// BlueGreenCandidatePreparer reconciles the green CNPG Cluster. It does not
// reconcile application-facing ConfigMaps or the authoritative environment.
type BlueGreenCandidatePreparer struct {
	client   client.Client
	key      types.NamespacedName
	cluster  *platformv1alpha1.PostgresCluster
	plan     BlueGreenCandidatePlan
	verifier candidateVerifier
	tls      tlsport.Manager
}

type candidateVerifier interface {
	VerifyAndGuard(context.Context, *cnpgv1.Cluster, []declaredDatabase, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error
	VerifyReplication(context.Context, *cnpgv1.Cluster, *cnpgv1.Cluster, []declaredDatabase, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error)
	VerifySourceBaseline(context.Context, *cnpgv1.Cluster, []declaredDatabase, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error
}

func NewBlueGreenCandidatePreparer(c client.Client, key types.NamespacedName, cluster *platformv1alpha1.PostgresCluster, plan BlueGreenCandidatePlan) *BlueGreenCandidatePreparer {
	return &BlueGreenCandidatePreparer{client: c, key: key, cluster: cluster, plan: plan, verifier: newPostgresCandidateVerifier(c, key.Name, cluster), tls: certmanagerinfra.NewManager(c)}
}

// PrepareCandidate creates the target-major CNPG Cluster from the immutable
// source receipt. CNPG owns the schema-only import execution; this adapter
// only declares its contract and records the candidate reference after the
// object exists. Subscriptions, DML guards, Poolers, and readiness checks are
// deliberately separate candidate stages.
func (p *BlueGreenCandidatePreparer) PrepareCandidate(ctx context.Context, intent mvutypes.Intent) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	if p == nil || p.client == nil || p.cluster == nil || intent.AttemptID == "" {
		return nil, fmt.Errorf("%w: blue/green candidate wiring is incomplete", mvutypes.ErrUpgradeFlowFailed)
	}
	status := previousBlueGreenStatus(intent)
	if status == nil || status.AttemptID != intent.AttemptID || status.SourcePreparation == nil ||
		status.SourcePreparation.CompletedAt == nil || status.Blue == nil ||
		status.Blue.Ref.Name == "" || status.SourceConfigurationFingerprint == nil {
		return status, fmt.Errorf("%w: completed blue/green source preparation is required before creating green", mvutypes.ErrUpgradeFlowFailed)
	}
	source := &cnpgv1.Cluster{}
	sourceKey := types.NamespacedName{Namespace: status.Blue.Ref.Namespace, Name: status.Blue.Ref.Name}
	if sourceKey.Namespace == "" {
		sourceKey.Namespace = p.key.Namespace
	}
	if err := p.client.Get(ctx, sourceKey, source); err != nil {
		return status, fmt.Errorf("reading prepared blue CNPG Cluster: %w", err)
	}
	if status.Blue.Ref.UID != "" && source.UID != status.Blue.Ref.UID {
		return status, fmt.Errorf("%w: prepared blue CNPG Cluster UID changed", mvutypes.ErrUpgradeFlowFailed)
	}
	databases, err := listManagedDatabases(ctx, p.client, p.key)
	if err != nil {
		return status, err
	}
	if p.verifier == nil {
		return status, fmt.Errorf("%w: green candidate verification wiring is incomplete", mvutypes.ErrUpgradeFlowFailed)
	}
	// A changed source cannot seed or resume a valid candidate. Check it before
	// creating green so schema import never starts from an unproved baseline.
	if err := p.verifyFrozenSource(ctx, source, databases, status, intent.AttemptID); err != nil {
		return status, err
	}
	desired, err := p.buildCandidate(ctx, source, databases, intent.AttemptID)
	if err != nil {
		return status, err
	}

	candidate := &cnpgv1.Cluster{}
	err = p.client.Get(ctx, client.ObjectKeyFromObject(desired), candidate)
	if apierrors.IsNotFound(err) {
		if err := p.client.Create(ctx, desired); err != nil {
			return status, fmt.Errorf("creating green CNPG Cluster: %w", err)
		}
		candidate = desired
	} else if err != nil {
		return status, fmt.Errorf("reading green CNPG Cluster: %w", err)
	} else if err := p.validateCandidate(candidate, desired, source, intent.AttemptID); err != nil {
		return status, err
	}

	if status.Green != nil && (status.Green.Ref.Name != candidate.Name ||
		status.Green.Ref.Namespace != candidate.Namespace ||
		(status.Green.Ref.UID != "" && status.Green.Ref.UID != candidate.UID)) {
		return status, fmt.Errorf("%w: recorded green CNPG Cluster does not match this attempt", mvutypes.ErrUpgradeFlowFailed)
	}
	status.Green = &platformv1alpha1.BlueGreenEnvironmentStatus{
		Ref: corev1.ObjectReference{
			APIVersion: "postgresql.cnpg.io/v1", Kind: "Cluster", Namespace: candidate.Namespace, Name: candidate.Name, UID: candidate.UID,
		},
	}
	if err := clusterCnpg.ClusterBlockingError(candidate); err != nil {
		return status, fmt.Errorf("%w: green CNPG Cluster cannot bootstrap: %v", mvutypes.ErrUpgradeFlowFailed, err)
	}
	if !clusterCnpg.PrimaryReady(candidate) {
		return status, fmt.Errorf("%w: waiting for green CNPG Cluster schema-only bootstrap", mvutypes.ErrUpgradeFlowPending)
	}
	if err := p.verifier.VerifyAndGuard(ctx, candidate, databases, status); err != nil {
		return status, err
	}
	subscriptionsReady, err := p.reconcileSubscriptions(ctx, candidate, databases, intent.AttemptID, status)
	if err != nil {
		return status, err
	}
	if !subscriptionsReady {
		return status, fmt.Errorf("%w: waiting for green logical subscriptions to be applied", mvutypes.ErrUpgradeFlowPending)
	}
	topologyReady, err := p.reconcileCandidateTopology(ctx, candidate, source)
	if err != nil {
		return status, err
	}
	if !topologyReady {
		return status, fmt.Errorf("%w: waiting for green candidate topology to scale", mvutypes.ErrUpgradeFlowPending)
	}
	endpointsReady, err := p.reconcileCandidateEndpoints(ctx, source, candidate, intent.AttemptID, status)
	if err != nil {
		return status, err
	}
	if !endpointsReady {
		return status, fmt.Errorf("%w: waiting for green candidate endpoints", mvutypes.ErrUpgradeFlowPending)
	}
	replicationReady, err := p.verifier.VerifyReplication(ctx, source, candidate, databases, status)
	if err != nil {
		return status, err
	}
	if !replicationReady {
		return status, fmt.Errorf("%w: waiting for green initial-copy and logical-slot verification", mvutypes.ErrUpgradeFlowPending)
	}
	return status, nil
}

// verifyFrozenSource is read-only. The source remains production while green
// is prepared, so candidate readiness must reject source configuration,
// credential, schema, access-policy, or database-inventory drift rather than
// silently recapturing a new baseline.
func (p *BlueGreenCandidatePreparer) verifyFrozenSource(ctx context.Context, source *cnpgv1.Cluster, databases []declaredDatabase, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, attemptID string) error {
	if source.Status.Certificates.ServerCASecret == "" || !hasCredentialFingerprint(status.CredentialFingerprints, source.Status.Certificates.ServerCASecret) {
		return fmt.Errorf("%w: frozen blue server CA receipt is missing", mvutypes.ErrUpgradeFlowFailed)
	}
	baseline := sourceBaseline{
		client:              p.client,
		key:                 p.key,
		cluster:             p.cluster,
		targetConfiguration: p.plan.TargetConfiguration,
		targetImage:         p.plan.TargetImage,
	}
	configuration, err := baseline.configurationFingerprint(source, databases)
	if err != nil {
		return err
	}
	if status.SourceConfigurationFingerprint == nil || configuration != *status.SourceConfigurationFingerprint {
		return fmt.Errorf("%w: frozen blue/green configuration changed after preflight", mvutypes.ErrUpgradeFlowFailed)
	}
	credentials, err := baseline.credentialFingerprints(ctx, source, databases, replicationSecretName(p.key.Name, attemptID))
	if err != nil {
		return err
	}
	if err := verifyCredentialBaseline(status.CredentialFingerprints, credentials); err != nil {
		return err
	}
	return p.verifier.VerifySourceBaseline(ctx, source, databases, status)
}

func hasCredentialFingerprint(fingerprints []platformv1alpha1.BlueGreenCredentialFingerprintStatus, name string) bool {
	for _, fingerprint := range fingerprints {
		if fingerprint.SecretName == name {
			return true
		}
	}
	return false
}

func (p *BlueGreenCandidatePreparer) buildCandidate(ctx context.Context, source *cnpgv1.Cluster, databases []declaredDatabase, attemptID string) (*cnpgv1.Cluster, error) {
	if source == nil || p.plan.TargetImage == "" || p.plan.TargetStorage == nil || p.plan.TargetStorage.IsZero() {
		return nil, fmt.Errorf("%w: source CNPG Cluster, target image, or target storage is unavailable", mvutypes.ErrUpgradeFlowFailed)
	}
	if p.plan.TargetInstances != source.Spec.Instances {
		return nil, fmt.Errorf("%w: green topology must inherit blue's %d instances", mvutypes.ErrUpgradeFlowFailed, source.Spec.Instances)
	}
	username, err := p.replicationUsername(ctx, attemptID)
	if err != nil {
		return nil, err
	}
	candidateName := candidateClusterName(p.key.Name, attemptID)
	serverLeaf, err := p.reconcileCandidateServerTLS(ctx, source, candidateName, attemptID)
	if err != nil {
		return nil, err
	}
	host, err := pgconninfo.ServiceFQDN(source.GetServiceReadWriteName(), source.Namespace)
	if err != nil {
		return nil, fmt.Errorf("resolving blue write Service for candidate import: %w", err)
	}
	databaseNames := make([]string, 0, len(databases))
	// The attempt replication role is source-only. CNPG's schema import does
	// not preserve its REPLICATION attribute, and the access fingerprint
	// already excludes the expected attempt role on blue.
	roles := declaredRoleNames(databases, "")
	roleNames := make([]string, 0, len(roles))
	for _, database := range databases {
		databaseNames = append(databaseNames, database.name)
	}
	for role := range roles {
		roleNames = append(roleNames, role)
	}
	sort.Strings(databaseNames)
	sort.Strings(roleNames)
	if len(databaseNames) == 0 {
		return nil, fmt.Errorf("%w: no managed databases are available for schema-only import", mvutypes.ErrUpgradeFlowFailed)
	}

	spec := *source.Spec.DeepCopy()
	spec.ImageName = p.plan.TargetImage
	spec.ImageCatalogRef = nil
	spec.Instances = 1
	spec.StorageConfiguration.Size = p.plan.TargetStorage.String()
	spec.MinSyncReplicas = 0
	spec.MaxSyncReplicas = 0
	spec.Backup = nil
	plugins := spec.Plugins[:0]
	for _, plugin := range spec.Plugins {
		if plugin.IsWALArchiver != nil && *plugin.IsWALArchiver {
			continue
		}
		plugins = append(plugins, plugin)
	}
	spec.Plugins = plugins
	spec.Bootstrap = &cnpgv1.BootstrapConfiguration{InitDB: &cnpgv1.BootstrapInitDB{
		Database: "postgres", Owner: "postgres", Secret: source.Spec.SuperuserSecret,
		Import: &cnpgv1.Import{
			Source: cnpgv1.ImportSource{ExternalCluster: candidateImportExternalClusterName},
			Type:   cnpgv1.MonolithSnapshotType, Databases: databaseNames, Roles: roleNames, SchemaOnly: true,
		},
	}}
	spec.ExternalClusters = withoutExternalCluster(spec.ExternalClusters, candidateImportExternalClusterName)
	spec.ExternalClusters = withoutExternalCluster(spec.ExternalClusters, candidateSourceExternalClusterName)
	importSecret := source.GetSuperuserSecretName()
	if importSecret == "" {
		return nil, fmt.Errorf("%w: blue superuser Secret is unavailable for schema import", mvutypes.ErrUpgradeFlowFailed)
	}
	importSource := cnpgv1.ExternalCluster{
		Name: candidateImportExternalClusterName,
		ConnectionParameters: map[string]string{
			"host": host, "user": pginfra.SuperUsername, "dbname": "postgres", "sslmode": "verify-full",
		},
		Password: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: importSecret}, Key: replicationSecretPasswordKey},
	}
	replicationSource := cnpgv1.ExternalCluster{
		Name: candidateSourceExternalClusterName,
		ConnectionParameters: map[string]string{
			"host": host, "user": username, "dbname": "postgres", "sslmode": "verify-full",
		},
		Password: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: replicationSecretName(p.key.Name, attemptID)}, Key: replicationSecretPasswordKey},
	}
	if source.Status.Certificates.ServerCASecret == "" {
		return nil, fmt.Errorf("%w: blue server CA is not available for candidate import", mvutypes.ErrUpgradeFlowFailed)
	}
	importSource.SSLRootCert = &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: source.Status.Certificates.ServerCASecret}, Key: "ca.crt"}
	replicationSource.SSLRootCert = importSource.SSLRootCert.DeepCopy()
	spec.ExternalClusters = append(spec.ExternalClusters, importSource, replicationSource)
	configureCandidateCertificates(&spec, source, serverLeaf)

	return &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{
		Name: candidateName, Namespace: p.key.Namespace,
		Labels: map[string]string{blueGreenAttemptLabel: attemptID}, OwnerReferences: p.ownerReferences(),
	}, Spec: spec}, nil
}

func (p *BlueGreenCandidatePreparer) replicationUsername(ctx context.Context, attemptID string) (string, error) {
	secret := &corev1.Secret{}
	name := replicationSecretName(p.key.Name, attemptID)
	if err := p.client.Get(ctx, types.NamespacedName{Namespace: p.key.Namespace, Name: name}, secret); err != nil {
		return "", fmt.Errorf("reading blue/green replication Secret: %w", err)
	}
	if secret.Labels[blueGreenAttemptLabel] != attemptID || !metav1.IsControlledBy(secret, p.cluster) {
		return "", fmt.Errorf("%w: replication Secret %s does not belong to this attempt", mvutypes.ErrUpgradeFlowFailed, name)
	}
	username := string(secret.Data[replicationSecretUsernameKey])
	if username == "" {
		return "", fmt.Errorf("%w: replication Secret %s has no username", mvutypes.ErrUpgradeFlowFailed, name)
	}
	return username, nil
}

func (p *BlueGreenCandidatePreparer) validateCandidate(candidate, desired, source *cnpgv1.Cluster, attemptID string) error {
	if candidate.Labels[blueGreenAttemptLabel] != attemptID || !metav1.IsControlledBy(candidate, p.cluster) {
		return fmt.Errorf("%w: green CNPG Cluster %s belongs to another attempt", mvutypes.ErrUpgradeFlowFailed, candidate.Name)
	}
	if candidate.Spec.Instances != 1 && candidate.Spec.Instances != p.plan.TargetInstances {
		return fmt.Errorf("%w: green CNPG Cluster %s has unexpected topology", mvutypes.ErrUpgradeFlowFailed, candidate.Name)
	}
	expected := desired.DeepCopy()
	if candidate.Spec.Instances == p.plan.TargetInstances {
		expected.Spec.Instances = p.plan.TargetInstances
		expected.Spec.MinSyncReplicas = source.Spec.MinSyncReplicas
		expected.Spec.MaxSyncReplicas = source.Spec.MaxSyncReplicas
	}
	observed := candidate.DeepCopy()
	// CNPG admission fills defaults, including target-major PostgreSQL settings.
	// Compare the full operator-owned spec after applying the same defaults to
	// the desired and observed objects; do not maintain a partial drift list.
	expected.Default()
	observed.Default()
	if !equality.Semantic.DeepEqual(expected.Spec, observed.Spec) {
		return fmt.Errorf("%w: green CNPG Cluster %s differs from the desired candidate spec", mvutypes.ErrUpgradeFlowFailed, candidate.Name)
	}
	return nil
}

func (p *BlueGreenCandidatePreparer) reconcileCandidateTopology(ctx context.Context, candidate, source *cnpgv1.Cluster) (bool, error) {
	if p.plan.TargetInstances < 1 {
		return false, fmt.Errorf("%w: target candidate topology is unavailable", mvutypes.ErrUpgradeFlowFailed)
	}
	if p.plan.TargetInstances != source.Spec.Instances {
		return false, fmt.Errorf("%w: green topology must inherit blue's %d instances", mvutypes.ErrUpgradeFlowFailed, source.Spec.Instances)
	}
	if candidate.Spec.Instances != p.plan.TargetInstances || candidate.Spec.MinSyncReplicas != source.Spec.MinSyncReplicas || candidate.Spec.MaxSyncReplicas != source.Spec.MaxSyncReplicas {
		original := candidate.DeepCopy()
		candidate.Spec.Instances = p.plan.TargetInstances
		candidate.Spec.MinSyncReplicas = source.Spec.MinSyncReplicas
		candidate.Spec.MaxSyncReplicas = source.Spec.MaxSyncReplicas
		if err := p.client.Patch(ctx, candidate, client.MergeFrom(original)); err != nil {
			return false, fmt.Errorf("restoring green CNPG topology: %w", err)
		}
		return false, nil
	}
	if err := clusterCnpg.ClusterBlockingError(candidate); err != nil {
		return false, fmt.Errorf("%w: green CNPG Cluster cannot scale: %v", mvutypes.ErrUpgradeFlowFailed, err)
	}
	return candidate.Status.Instances == p.plan.TargetInstances &&
		candidate.Status.ReadyInstances == p.plan.TargetInstances &&
		len(candidate.Status.InstancesStatus[cnpgv1.PodHealthy]) == p.plan.TargetInstances &&
		clusterCnpg.ClusterReady(candidate), nil
}

func (p *BlueGreenCandidatePreparer) reconcileCandidateEndpoints(ctx context.Context, source, candidate *cnpgv1.Cluster, attemptID string, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	direct := &corev1.Service{}
	directName := candidate.GetServiceReadWriteName()
	if err := p.client.Get(ctx, types.NamespacedName{Namespace: candidate.Namespace, Name: directName}, direct); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading green direct Service: %w", err)
	}
	endpoints := &platformv1alpha1.BlueGreenCandidateEndpointsStatus{DirectService: serviceReference(direct)}
	poolerReady, poolerService, err := p.reconcileCandidatePoolers(ctx, source, candidate, attemptID)
	if err != nil {
		return false, err
	}
	if !poolerReady {
		return false, nil
	}
	if poolerService != nil {
		endpoints.PoolerService = serviceReference(poolerService)
	}
	status.CandidateEndpoints = endpoints
	if status.Green != nil && status.Green.AvailableAt == nil {
		status.Green.AvailableAt = new(metav1.Now())
	}
	return true, nil
}

// reconcileCandidatePoolers reproduces only the source's already-managed
// Pooler resources under the candidate environment name. It does not touch
// connection ConfigMaps, which remain authoritative-blue until a later
// endpoint-commit ticket.
func (p *BlueGreenCandidatePreparer) reconcileCandidatePoolers(ctx context.Context, source, candidate *cnpgv1.Cluster, attemptID string) (bool, *corev1.Service, error) {
	items := &cnpgv1.PoolerList{}
	if err := p.client.List(ctx, items, client.InNamespace(candidate.Namespace)); err != nil {
		return false, nil, fmt.Errorf("listing source Poolers for green candidate: %w", err)
	}
	var candidateService *corev1.Service
	for _, item := range items.Items {
		if item.Spec.Cluster.Name != source.Name || !metav1.IsControlledBy(&item, p.cluster) {
			continue
		}
		poolerName := candidatePoolerName(candidate.Name, string(item.Spec.Type))
		desired := p.buildCandidatePooler(&item, candidate.Name, poolerName, attemptID)
		pooler := &cnpgv1.Pooler{}
		err := p.client.Get(ctx, types.NamespacedName{Namespace: candidate.Namespace, Name: poolerName}, pooler)
		if apierrors.IsNotFound(err) {
			if err := p.client.Create(ctx, desired); err != nil {
				return false, nil, fmt.Errorf("creating green Pooler %q: %w", poolerName, err)
			}
			return false, nil, nil
		}
		if err != nil {
			return false, nil, fmt.Errorf("reading green Pooler %q: %w", poolerName, err)
		}
		if err := p.validateCandidatePooler(pooler, desired, attemptID); err != nil {
			return false, nil, err
		}
		service := &corev1.Service{}
		if err := p.client.Get(ctx, types.NamespacedName{Namespace: candidate.Namespace, Name: pooler.Name}, service); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil, nil
			}
			return false, nil, fmt.Errorf("reading green Pooler Service %q: %w", pooler.Name, err)
		}
		ready, err := p.candidatePoolerReady(ctx, pooler, service)
		if err != nil {
			return false, nil, err
		}
		if !ready {
			return false, nil, nil
		}
		if pooler.Spec.Type == cnpgv1.PoolerTypeRW {
			candidateService = service
		}
	}
	return true, candidateService, nil
}

func (p *BlueGreenCandidatePreparer) candidatePoolerReady(ctx context.Context, pooler *cnpgv1.Pooler, service *corev1.Service) (bool, error) {
	if !metav1.IsControlledBy(service, pooler) || service.Spec.Selector[cnpgutils.PgbouncerNameLabel] != pooler.Name {
		return false, fmt.Errorf("%w: green Pooler Service %q does not select its owning Pooler", mvutypes.ErrUpgradeFlowFailed, service.Name)
	}
	desired := int32(1)
	if pooler.Spec.Instances != nil {
		desired = *pooler.Spec.Instances
	}
	if desired < 1 || pooler.Status.Phase != cnpgv1.PoolerPhaseActive || pooler.Status.Instances != desired {
		return false, nil
	}
	pods := &corev1.PodList{}
	if err := p.client.List(ctx, pods, client.InNamespace(pooler.Namespace), client.MatchingLabels(service.Spec.Selector)); err != nil {
		return false, fmt.Errorf("listing green Pooler pods for %q: %w", pooler.Name, err)
	}
	if len(pods.Items) != int(desired) {
		return false, nil
	}
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil {
			return false, nil
		}
		ready := false
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		if !ready {
			return false, nil
		}
	}
	return true, nil
}

func (p *BlueGreenCandidatePreparer) validateCandidatePooler(pooler, desired *cnpgv1.Pooler, attemptID string) error {
	if pooler.Labels[blueGreenAttemptLabel] != attemptID || !metav1.IsControlledBy(pooler, p.cluster) ||
		!equality.Semantic.DeepEqual(normalizeCandidatePoolerSpec(pooler.Spec), normalizeCandidatePoolerSpec(desired.Spec)) {
		return fmt.Errorf("%w: green Pooler %q does not match the candidate contract", mvutypes.ErrUpgradeFlowFailed, pooler.Name)
	}
	return nil
}

func (p *BlueGreenCandidatePreparer) buildCandidatePooler(source *cnpgv1.Pooler, candidateName, poolerName, attemptID string) *cnpgv1.Pooler {
	desired := source.DeepCopy()
	desired.ObjectMeta = metav1.ObjectMeta{
		Name: poolerName, Namespace: source.Namespace,
		Labels: map[string]string{blueGreenAttemptLabel: attemptID}, OwnerReferences: p.ownerReferences(),
	}
	desired.Spec.Cluster.Name = candidateName
	desired.Status = cnpgv1.PoolerStatus{}
	return desired
}

func normalizeCandidatePoolerSpec(spec cnpgv1.PoolerSpec) cnpgv1.PoolerSpec {
	copy := *spec.DeepCopy()
	if copy.Type == "" {
		copy.Type = cnpgv1.PoolerTypeRW
	}
	if copy.Instances == nil {
		copy.Instances = new(int32(1))
	}
	if copy.PgBouncer != nil && copy.PgBouncer.PoolMode == "" {
		copy.PgBouncer.PoolMode = cnpgv1.PgBouncerPoolModeSession
	}
	return copy
}

func serviceReference(service *corev1.Service) *corev1.ObjectReference {
	return &corev1.ObjectReference{APIVersion: "v1", Kind: "Service", Namespace: service.Namespace, Name: service.Name, UID: service.UID}
}

func candidatePoolerName(candidateName, poolerType string) string {
	return candidateName + "-pooler-" + poolerType
}

func (p *BlueGreenCandidatePreparer) ownerReferences() []metav1.OwnerReference {
	return []metav1.OwnerReference{*metav1.NewControllerRef(p.cluster, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}
}

func (p *BlueGreenCandidatePreparer) reconcileSubscriptions(ctx context.Context, candidate *cnpgv1.Cluster, databases []declaredDatabase, attemptID string, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	byDatabase := make(map[string]int, len(status.DatabaseReplication))
	for index, replication := range status.DatabaseReplication {
		byDatabase[replication.DatabaseName] = index
	}
	ready := true
	for _, database := range databases {
		index, found := byDatabase[database.name]
		if !found || status.DatabaseReplication[index].SchemaImported == nil || !*status.DatabaseReplication[index].SchemaImported {
			return false, fmt.Errorf("%w: green schema import for database %q is not verified", mvutypes.ErrUpgradeFlowFailed, database.name)
		}
		name := subscriptionName(p.key.Name, database.name)
		desired := p.buildCandidateSubscription(candidate.Name, database.name, name, attemptID)
		subscription := &cnpgv1.Subscription{}
		err := p.client.Get(ctx, types.NamespacedName{Namespace: p.key.Namespace, Name: name}, subscription)
		if apierrors.IsNotFound(err) {
			if err := p.client.Create(ctx, desired); err != nil {
				return false, fmt.Errorf("creating green logical Subscription %q: %w", name, err)
			}
			ready = false
			continue
		}
		if err != nil {
			return false, fmt.Errorf("reading green logical Subscription %q: %w", name, err)
		}
		if err := p.validateSubscription(subscription, desired, attemptID); err != nil {
			return false, err
		}
		applied := subscription.Status.ObservedGeneration == subscription.Generation && subscription.Status.Applied != nil && *subscription.Status.Applied
		status.DatabaseReplication[index].SubscriptionHealthy = new(applied)
		ready = ready && applied
	}
	return ready, nil
}

func candidateRef(name string) corev1.LocalObjectReference {
	return corev1.LocalObjectReference{Name: name}
}

func (p *BlueGreenCandidatePreparer) buildCandidateSubscription(candidateName, databaseName, name, attemptID string) *cnpgv1.Subscription {
	return &cnpgv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: p.key.Namespace, Labels: map[string]string{blueGreenAttemptLabel: attemptID}, OwnerReferences: p.ownerReferences()},
		Spec: cnpgv1.SubscriptionSpec{
			ClusterRef: candidateRef(candidateName), Name: name, DBName: databaseName,
			Parameters:      map[string]string{"copy_data": "true", "create_slot": "true", "enabled": "true", "failover": "true", "slot_name": name},
			PublicationName: publicationName(p.key.Name, databaseName), PublicationDBName: databaseName,
			ExternalClusterName: candidateSourceExternalClusterName, ReclaimPolicy: cnpgv1.SubscriptionReclaimDelete,
		},
	}
}

func (p *BlueGreenCandidatePreparer) validateSubscription(subscription, desired *cnpgv1.Subscription, attemptID string) error {
	if subscription.Labels[blueGreenAttemptLabel] != attemptID || !metav1.IsControlledBy(subscription, p.cluster) {
		return fmt.Errorf("%w: green logical Subscription %s belongs to another attempt", mvutypes.ErrUpgradeFlowFailed, subscription.Name)
	}
	if !equality.Semantic.DeepEqual(subscription.Spec, desired.Spec) {
		return fmt.Errorf("%w: green logical Subscription %s does not match the candidate contract", mvutypes.ErrUpgradeFlowFailed, subscription.Name)
	}
	return nil
}

func configureCandidateCertificates(spec *cnpgv1.ClusterSpec, source *cnpgv1.Cluster, serverLeaf string) {
	if spec.Certificates == nil {
		spec.Certificates = &cnpgv1.CertificatesConfiguration{}
	}
	spec.Certificates.ServerCASecret = source.Status.Certificates.ServerCASecret
	spec.Certificates.ClientCASecret = source.Status.Certificates.ClientCASecret
	// Each environment gets its own leaf/key pair. Retaining a source leaf
	// reference would make green's generated Services fail TLS name checks.
	spec.Certificates.ServerTLSSecret = serverLeaf
	spec.Certificates.ReplicationTLSSecret = ""
	if serverLeaf != "" {
		// cert-manager owns the leaf SANs; CNPG must not generate a second set.
		spec.Certificates.ServerAltDNSNames = nil
	}
}

func (p *BlueGreenCandidatePreparer) reconcileCandidateServerTLS(ctx context.Context, source *cnpgv1.Cluster, candidateName, attemptID string) (string, error) {
	if source.Spec.Certificates == nil || source.Spec.Certificates.ServerTLSSecret == "" {
		// CNPG can issue a distinct green leaf under blue's CA only if it owns
		// the signing key. A cert-manager CA copy contains ca.crt alone.
		ca := &corev1.Secret{}
		if source.Status.Certificates.ServerCASecret == "" {
			return "", fmt.Errorf("%w: blue server CA is unavailable", mvutypes.ErrUpgradeFlowFailed)
		}
		if err := p.client.Get(ctx, types.NamespacedName{Namespace: p.key.Namespace, Name: source.Status.Certificates.ServerCASecret}, ca); err != nil {
			return "", fmt.Errorf("reading blue server CA: %w", err)
		}
		if len(ca.Data["ca.crt"]) == 0 || len(ca.Data["ca.key"]) == 0 {
			return "", fmt.Errorf("%w: CNPG cannot issue a green leaf without blue's CA signing key", mvutypes.ErrUpgradeFlowFailed)
		}
		return "", nil
	}
	if p.plan.TargetConfiguration == nil || p.cluster.Spec.Class == "" {
		return "", fmt.Errorf("%w: green server TLS workflow is not configured", mvutypes.ErrUpgradeFlowFailed)
	}
	class := &platformv1alpha1.PostgresClusterClass{}
	if err := p.client.Get(ctx, types.NamespacedName{Name: p.cluster.Spec.Class}, class); err != nil {
		return "", fmt.Errorf("reading green server TLS class: %w", err)
	}
	if source.Status.Certificates.ServerCASecret == "" {
		return "", fmt.Errorf("%w: blue server CA is unavailable", mvutypes.ErrUpgradeFlowFailed)
	}
	leafName := candidateName + "-server-tls"
	identity := tlsport.Identity{
		ClusterName: p.cluster.Name, Namespace: p.key.Namespace, ClusterUID: p.cluster.UID,
		OwnerReference:   *metav1.NewControllerRef(p.cluster, platformv1alpha1.GroupVersion.WithKind("PostgresCluster")),
		AdditionalLabels: map[string]string{blueGreenAttemptLabel: attemptID},
		CertificateName:  leafName, ServerTLSSecret: leafName, ServerCASecret: candidateName + "-server-ca",
	}
	plan, err := clustercore.ReconcileServerTLSForEnvironment(ctx, p.tls, p.cluster, class,
		&clustercore.MergedConfig{Spec: p.plan.TargetConfiguration}, identity, candidateName)
	if err != nil {
		if errors.Is(err, tlsport.ErrCertificatePending) {
			return "", fmt.Errorf("%w: waiting for green server certificate", mvutypes.ErrUpgradeFlowPending)
		}
		if errors.Is(err, tlsport.ErrConfiguration) || errors.Is(err, tlsport.ErrInvalidMaterial) || errors.Is(err, tlsport.ErrCertManagerNotInstalled) {
			return "", fmt.Errorf("%w: green server certificate is invalid: %v", mvutypes.ErrUpgradeFlowFailed, err)
		}
		return "", fmt.Errorf("reconciling green server certificate: %w", err)
	}
	if plan.ServerTLSSecretName() != leafName {
		return "", fmt.Errorf("%w: blue uses a managed server leaf but its class does not configure cert-manager", mvutypes.ErrUpgradeFlowFailed)
	}
	blueCA := &corev1.Secret{}
	if err := p.client.Get(ctx, types.NamespacedName{Namespace: p.key.Namespace, Name: source.Status.Certificates.ServerCASecret}, blueCA); err != nil {
		return "", fmt.Errorf("reading blue server CA: %w", err)
	}
	greenCA := &corev1.Secret{}
	if err := p.client.Get(ctx, types.NamespacedName{Namespace: p.key.Namespace, Name: identity.ServerCASecret}, greenCA); err != nil {
		return "", fmt.Errorf("reading green server CA: %w", err)
	}
	if len(blueCA.Data["ca.crt"]) == 0 || !bytes.Equal(bytes.TrimSpace(blueCA.Data["ca.crt"]), bytes.TrimSpace(greenCA.Data["ca.crt"])) {
		return "", fmt.Errorf("%w: green server leaf does not chain to the frozen blue CA", mvutypes.ErrUpgradeFlowFailed)
	}
	return leafName, nil
}

func withoutExternalCluster(entries []cnpgv1.ExternalCluster, name string) []cnpgv1.ExternalCluster {
	result := make([]cnpgv1.ExternalCluster, 0, len(entries)+1)
	for _, entry := range entries {
		if entry.Name != name {
			result = append(result, entry)
		}
	}
	return result
}

func candidateClusterName(clusterName, attemptID string) string {
	digest := sha256.Sum256([]byte(attemptID))
	suffix := fmt.Sprintf("-bg-%x", digest[:5])
	if len(clusterName) > candidateClusterNameMaxLength-len(suffix) {
		clusterName = clusterName[:candidateClusterNameMaxLength-len(suffix)]
	}
	return clusterName + suffix
}

func subscriptionName(clusterName, databaseName string) string {
	digest := sha256.Sum256([]byte(clusterName + "/" + databaseName))
	suffix := fmt.Sprintf("-bgs-%x", digest[:5])
	if len(clusterName) > postgresIdentifierMaxLength-len(suffix) {
		clusterName = clusterName[:postgresIdentifierMaxLength-len(suffix)]
	}
	return clusterName + suffix
}
