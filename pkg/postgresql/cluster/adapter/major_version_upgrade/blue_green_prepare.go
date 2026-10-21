/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package majorupgradeadapter

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	clusterCnpg "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/cnpg"
	clusteridentity "github.com/splunk/splunk-operator/pkg/postgresql/cluster/ports/identity"
	identityadapter "github.com/splunk/splunk-operator/pkg/postgresql/shared/adapter/identity"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	blueGreenAttemptLabel              = "platform.splunk.com/blue-green-attempt"
	cnpgClusterLabel                   = "cnpg.io/cluster"
	postgresIdentifierMaxLength        = 63
	candidateImportExternalClusterName = "sok-bluegreen-import"
	candidateSourceExternalClusterName = "sok-bluegreen-source"
	candidateGuardSchema               = "splunk_operator_bluegreen"
	candidateGuardFunction             = "reject_ordinary_dml"
	candidateGuardTrigger              = "sok_bluegreen_dml_guard"
	candidateClusterNameMaxLength      = postgresIdentifierMaxLength - len("-pooler-rw")
)

// BlueGreenPreparer reconciles only source-side CNPG resources. It deliberately
// has no target name or target image and therefore cannot create green.
type BlueGreenPreparer struct {
	client    client.Client
	key       types.NamespacedName
	cluster   *platformv1alpha1.PostgresCluster
	resolver  clusteridentity.ClusterCardResolver
	preflight sourcePreflight
	plan      BlueGreenPreflightPlan
}

// BlueGreenPreflightPlan is the effective target shape checked before the
// source claim and frozen in its fingerprint. Later class changes cannot
// silently change green within the same attempt.
type BlueGreenPreflightPlan struct {
	TargetConfiguration *platformv1alpha1.PostgresClusterSpec
	TargetImage         string
}

func NewBlueGreenPreparer(c client.Client, key types.NamespacedName, cluster *platformv1alpha1.PostgresCluster, resolver clusteridentity.ClusterCardResolver, plan BlueGreenPreflightPlan) *BlueGreenPreparer {
	return &BlueGreenPreparer{client: c, key: key, cluster: cluster, resolver: resolver, preflight: newPostgresSourcePreflight(c, key.Name, cluster), plan: plan}
}

func (p *BlueGreenPreparer) Prepare(ctx context.Context, intent mvutypes.Intent) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	if p == nil || p.client == nil || intent.AttemptID == "" {
		return nil, fmt.Errorf("%w: blue/green preparation wiring is incomplete", mvutypes.ErrUpgradeFlowFailed)
	}
	cluster, err := p.readySource(ctx, intent)
	if err != nil {
		return nil, err
	}
	previous := previousBlueGreenStatus(intent)
	databases, checks, err := p.inspectSource(ctx, intent, cluster, previous)
	if err != nil {
		return nil, err
	}
	if previous != nil && previous.Blue != nil &&
		(previous.Blue.Ref.Name != cluster.Name || previous.Blue.Ref.Namespace != cluster.Namespace || previous.Blue.Ref.UID != cluster.UID) {
		return previous, fmt.Errorf("%w: source CNPG Cluster identity changed after blue/green preflight", mvutypes.ErrUpgradeFlowFailed)
	}
	status, err := p.captureSourceBaseline(ctx, intent, cluster, previous, databases, checks)
	if err != nil {
		return previous, err
	}
	// The outer use case persists this source claim before any source mutation.
	// A failed status write therefore leaves no attempt-scoped artifacts behind.
	if previous == nil || previous.Blue == nil {
		return status, fmt.Errorf("%w: recording blue/green source ownership before preparation", mvutypes.ErrUpgradeFlowPending)
	}
	return p.prepareClaimedSource(ctx, intent, cluster, databases, previous, status)
}

func (p *BlueGreenPreparer) readySource(ctx context.Context, intent mvutypes.Intent) (*cnpgv1.Cluster, error) {
	if err := p.validateLogicalSlotSupport(intent); err != nil {
		return nil, err
	}
	sourceKey, expectedUID, err := p.sourceClusterKey()
	if err != nil {
		return nil, err
	}
	cluster := &cnpgv1.Cluster{}
	if err := p.client.Get(ctx, sourceKey, cluster); err != nil {
		return nil, err
	}
	if expectedUID != "" && cluster.UID != expectedUID {
		return nil, fmt.Errorf("%w: source CNPG Cluster %s/%s has UID %q, expected %q", mvutypes.ErrUpgradeFlowFailed, cluster.Namespace, cluster.Name, cluster.UID, expectedUID)
	}
	if !clusterCnpg.PrimaryReady(cluster) {
		return nil, fmt.Errorf("%w: source CNPG Cluster is not ready", mvutypes.ErrUpgradeFlowPending)
	}
	return cluster, nil
}

func (p *BlueGreenPreparer) inspectSource(ctx context.Context, intent mvutypes.Intent, cluster *cnpgv1.Cluster, previous *platformv1alpha1.PostgresBlueGreenUpgradeStatus) ([]declaredDatabase, []databasePreflight, error) {
	// The class-resolved plan is an initial feasibility check, not part of
	// blue's frozen configuration. Retries use the claimed live blue baseline.
	if previous == nil || previous.Blue == nil {
		if err := p.validateTargetPlan(cluster); err != nil {
			return nil, nil, err
		}
	}
	if err := p.validateReplicationNetworkPolicy(ctx, cluster); err != nil {
		return nil, nil, err
	}
	databases, err := p.managedDatabases(ctx)
	if err != nil {
		return nil, nil, err
	}
	if p.preflight == nil {
		return nil, nil, fmt.Errorf("%w: source preflight wiring is incomplete", mvutypes.ErrUpgradeFlowFailed)
	}
	checks, err := p.preflight.Inspect(ctx, cluster, databases, intent.AttemptID)
	if err != nil {
		return nil, nil, err
	}
	return databases, checks, nil
}

func (p *BlueGreenPreparer) captureSourceBaseline(ctx context.Context, intent mvutypes.Intent, cluster *cnpgv1.Cluster, previous *platformv1alpha1.PostgresBlueGreenUpgradeStatus, databases []declaredDatabase, checks []databasePreflight) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	status := p.preparationStatus(intent, cluster)
	configurationFingerprint, err := p.configurationFingerprint(cluster, databases)
	if err != nil {
		return previous, err
	}
	status.SourceConfigurationFingerprint = ptr.To(configurationFingerprint)
	status.DatabaseReplication, err = databaseReplicationStatus(databases, checks)
	if err != nil {
		return previous, err
	}
	replicationSecret := replicationSecretName(p.key.Name, intent.AttemptID)
	baselineSecret := ""
	if previous != nil {
		for _, fingerprint := range previous.CredentialFingerprints {
			if fingerprint.SecretName == replicationSecret {
				baselineSecret = replicationSecret
				break
			}
		}
	}
	fingerprints, err := p.credentialFingerprints(ctx, cluster, databases, baselineSecret)
	if err != nil {
		return previous, err
	}
	status.CredentialFingerprints = preserveCredentialCaptureTimes(previous, fingerprints)
	if err := verifyBlueGreenBaseline(intent, status); err != nil {
		return nil, err
	}
	return status, nil
}

func (p *BlueGreenPreparer) prepareClaimedSource(ctx context.Context, intent mvutypes.Intent, cluster *cnpgv1.Cluster, databases []declaredDatabase, previous, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	replicationSecret := replicationSecretName(p.key.Name, intent.AttemptID)
	role, password, err := p.reconcileReplicationSecret(ctx, intent.AttemptID)
	if err != nil {
		return status, err
	}
	fingerprints, err := p.credentialFingerprints(ctx, cluster, databases, replicationSecret)
	if err != nil {
		return status, err
	}
	status.CredentialFingerprints = preserveCredentialCaptureTimes(previous, fingerprints)
	if err := verifyBlueGreenBaseline(intent, status); err != nil {
		return previous, err
	}
	if err := p.preflight.EnsureReplicationRole(ctx, cluster, databases, role, password); err != nil {
		return status, err
	}
	if err := p.enableLogicalSlotSynchronization(ctx, cluster); err != nil {
		return status, err
	}
	publicationsReady := true
	for _, database := range databases {
		ready, err := p.reconcilePublication(ctx, cluster.Name, database, intent.AttemptID)
		if err != nil {
			return status, err
		}
		publicationsReady = publicationsReady && ready
	}
	if !publicationsReady {
		return status, fmt.Errorf("%w: waiting for source publications to be applied", mvutypes.ErrUpgradeFlowPending)
	}

	if status.SourcePreparation == nil {
		now := metav1.Now()
		status.SourcePreparation = &platformv1alpha1.BlueGreenSourcePreparationStatus{CompletedAt: &now}
	}
	return status, nil
}

func databaseReplicationStatus(databases []declaredDatabase, checks []databasePreflight) ([]platformv1alpha1.BlueGreenDatabaseReplicationStatus, error) {
	byName := make(map[string]databasePreflight, len(checks))
	for _, check := range checks {
		byName[check.name] = check
	}
	result := make([]platformv1alpha1.BlueGreenDatabaseReplicationStatus, 0, len(databases))
	for _, database := range databases {
		check, ok := byName[database.name]
		if !ok {
			return nil, fmt.Errorf("%w: source preflight did not return database %q", mvutypes.ErrUpgradeFlowFailed, database.name)
		}
		result = append(result, platformv1alpha1.BlueGreenDatabaseReplicationStatus{
			PostgresDatabaseName: database.parent.Name, PostgresDatabaseUID: string(database.parent.UID), DatabaseName: database.name,
			SourceSchemaFingerprint: ptr.To(check.schemaFingerprint), SourceAccessPolicyFingerprint: ptr.To(check.accessFingerprint),
		})
	}
	return result, nil
}

func preserveCredentialCaptureTimes(previous *platformv1alpha1.PostgresBlueGreenUpgradeStatus, current []platformv1alpha1.BlueGreenCredentialFingerprintStatus) []platformv1alpha1.BlueGreenCredentialFingerprintStatus {
	if previous == nil {
		return current
	}
	byKey := make(map[string]platformv1alpha1.BlueGreenCredentialFingerprintStatus, len(previous.CredentialFingerprints))
	for _, fingerprint := range previous.CredentialFingerprints {
		byKey[fingerprint.SecretName+"/"+fingerprint.SecretUID] = fingerprint
	}
	for i := range current {
		prior, ok := byKey[current[i].SecretName+"/"+current[i].SecretUID]
		if ok && prior.Fingerprint == current[i].Fingerprint {
			current[i].CapturedAt = prior.CapturedAt
		}
	}
	return current
}

func (p *BlueGreenPreparer) preparationStatus(intent mvutypes.Intent, source *cnpgv1.Cluster) *platformv1alpha1.PostgresBlueGreenUpgradeStatus {
	status := previousBlueGreenStatus(intent)
	if status == nil {
		status = &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: intent.AttemptID}
	}
	if status.Blue == nil {
		status.Blue = &platformv1alpha1.BlueGreenEnvironmentStatus{
			Ref: corev1.ObjectReference{Name: source.Name, Namespace: source.Namespace, UID: source.UID},
		}
	}
	if status.Gates == nil {
		status.Gates = &platformv1alpha1.BlueGreenGateStatus{}
	}
	if status.Gates.Allow == nil {
		status.Gates.Allow = &platformv1alpha1.BlueGreenGateConsumptionStatus{
			Generation: p.generation(),
			ConsumedAt: ptr.To(metav1.Now()),
		}
	}
	return status
}

func previousBlueGreenStatus(intent mvutypes.Intent) *platformv1alpha1.PostgresBlueGreenUpgradeStatus {
	for i := len(intent.State) - 1; i >= 0; i-- {
		entry := intent.State[i]
		if mvutypes.MatchesIntent(entry, intent) && entry.BlueGreen != nil {
			return entry.BlueGreen.DeepCopy()
		}
	}
	return nil
}

// verifyBlueGreenBaseline keeps the first successful preflight authoritative.
// A retry may rediscover resources, but it must not silently replace a schema,
// access, configuration, or credential baseline after blue has been frozen.
func verifyBlueGreenBaseline(intent mvutypes.Intent, current *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	for i := len(intent.State) - 1; i >= 0; i-- {
		entry := intent.State[i]
		previous := entry.BlueGreen
		if previous == nil || previous.AttemptID != intent.AttemptID || previous.Blue == nil {
			continue
		}
		if previous.SourceConfigurationFingerprint != nil &&
			(current.SourceConfigurationFingerprint == nil || *previous.SourceConfigurationFingerprint != *current.SourceConfigurationFingerprint) {
			return fmt.Errorf("%w: source configuration changed after blue/green preflight", mvutypes.ErrUpgradeFlowFailed)
		}
		if err := verifyCredentialBaseline(previous.CredentialFingerprints, current.CredentialFingerprints); err != nil {
			return err
		}
		if err := verifyDatabaseBaseline(previous.DatabaseReplication, current.DatabaseReplication); err != nil {
			return err
		}
		return nil
	}
	return nil
}

func verifyCredentialBaseline(previous, current []platformv1alpha1.BlueGreenCredentialFingerprintStatus) error {
	if len(previous) == 0 {
		return nil
	}
	values := make(map[string]string, len(current))
	for _, fingerprint := range current {
		values[fingerprint.SecretName+"/"+fingerprint.SecretUID] = fingerprint.Fingerprint
	}
	for _, fingerprint := range previous {
		key := fingerprint.SecretName + "/" + fingerprint.SecretUID
		if values[key] != fingerprint.Fingerprint {
			return fmt.Errorf("%w: credential Secret %q changed after blue/green preflight", mvutypes.ErrUpgradeFlowFailed, fingerprint.SecretName)
		}
	}
	return nil
}

func verifyDatabaseBaseline(previous, current []platformv1alpha1.BlueGreenDatabaseReplicationStatus) error {
	if len(previous) != len(current) {
		return fmt.Errorf("%w: managed database inventory changed after blue/green preflight", mvutypes.ErrUpgradeFlowFailed)
	}
	values := make(map[string]platformv1alpha1.BlueGreenDatabaseReplicationStatus, len(current))
	for _, database := range current {
		values[database.PostgresDatabaseName+"/"+database.PostgresDatabaseUID+"/"+database.DatabaseName] = database
	}
	for _, database := range previous {
		key := database.PostgresDatabaseName + "/" + database.PostgresDatabaseUID + "/" + database.DatabaseName
		currentDatabase, ok := values[key]
		if !ok || !sameOptionalString(database.SourceSchemaFingerprint, currentDatabase.SourceSchemaFingerprint) || !sameOptionalString(database.SourceAccessPolicyFingerprint, currentDatabase.SourceAccessPolicyFingerprint) {
			return fmt.Errorf("%w: schema or access policy for database %q changed after blue/green preflight", mvutypes.ErrUpgradeFlowFailed, database.DatabaseName)
		}
	}
	return nil
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (p *BlueGreenPreparer) validateTargetPlan(source *cnpgv1.Cluster) error {
	if p.plan.TargetConfiguration == nil || p.plan.TargetConfiguration.Storage == nil || p.plan.TargetConfiguration.Storage.IsZero() {
		return fmt.Errorf("%w: effective target storage is unavailable", mvutypes.ErrUpgradeFlowFailed)
	}
	if source == nil {
		return fmt.Errorf("%w: source storage is unavailable", mvutypes.ErrUpgradeFlowFailed)
	}
	if p.plan.TargetConfiguration.Instances != nil && int(*p.plan.TargetConfiguration.Instances) != source.Spec.Instances {
		return fmt.Errorf("%w: green topology must inherit blue's %d instances", mvutypes.ErrUpgradeFlowFailed, source.Spec.Instances)
	}
	sourceStorage, err := resource.ParseQuantity(source.Spec.StorageConfiguration.Size)
	if err != nil || sourceStorage.IsZero() {
		return fmt.Errorf("%w: source storage capacity is unavailable", mvutypes.ErrUpgradeFlowFailed)
	}
	if p.plan.TargetConfiguration.Storage.Cmp(sourceStorage) < 0 {
		return fmt.Errorf("%w: planned target storage %s is smaller than source storage %s", mvutypes.ErrUpgradeFlowFailed, p.plan.TargetConfiguration.Storage.String(), sourceStorage.String())
	}
	return nil
}

// validateReplicationNetworkPolicy proves the simple, safe case: blue accepts
// ingress and no policy can isolate future green's egress. A policy that might
// select green needs a concrete future-green peer contract from CPI-2120.
func (p *BlueGreenPreparer) validateReplicationNetworkPolicy(ctx context.Context, source *cnpgv1.Cluster) error {
	policies := &networkingv1.NetworkPolicyList{}
	if err := p.client.List(ctx, policies, client.InNamespace(source.Namespace)); err != nil {
		return fmt.Errorf("listing network policies for source replication: %w", err)
	}
	pods := &corev1.PodList{}
	if err := p.client.List(ctx, pods, client.InNamespace(source.Namespace), client.MatchingLabels{cnpgClusterLabel: source.Name}); err != nil {
		return fmt.Errorf("listing source pods for replication network policy: %w", err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("%w: source CNPG Cluster has no Pods to validate replication network policy", mvutypes.ErrUpgradeFlowFailed)
	}
	for _, policy := range policies.Items {
		selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
		if err != nil {
			return fmt.Errorf("%w: network policy %s has an invalid pod selector", mvutypes.ErrUpgradeFlowFailed, policy.Name)
		}
		if policySelectsIngress(policy) && anyPodMatches(selector, pods.Items) {
			return fmt.Errorf("%w: network policy %q selects blue; green-to-blue replication ingress cannot be proven before green exists", mvutypes.ErrUpgradeFlowFailed, policy.Name)
		}
		if policySelectsEgress(policy) && !policyTargetsOnlySourceCluster(policy.Spec.PodSelector, source.Name) {
			return fmt.Errorf("%w: network policy %q may select future green; replication egress cannot be proven before green exists", mvutypes.ErrUpgradeFlowFailed, policy.Name)
		}
	}
	return nil
}

func policyTargetsOnlySourceCluster(selector metav1.LabelSelector, sourceName string) bool {
	if selector.MatchLabels[cnpgClusterLabel] == sourceName {
		return true
	}
	for _, requirement := range selector.MatchExpressions {
		if requirement.Key == cnpgClusterLabel && requirement.Operator == metav1.LabelSelectorOpIn && len(requirement.Values) == 1 && requirement.Values[0] == sourceName {
			return true
		}
	}
	return false
}

func policySelectsEgress(policy networkingv1.NetworkPolicy) bool {
	if len(policy.Spec.PolicyTypes) == 0 {
		return len(policy.Spec.Egress) != 0
	}
	for _, policyType := range policy.Spec.PolicyTypes {
		if policyType == networkingv1.PolicyTypeEgress {
			return true
		}
	}
	return false
}

func anyPodMatches(selector labels.Selector, pods []corev1.Pod) bool {
	for _, pod := range pods {
		if selector.Matches(labels.Set(pod.Labels)) {
			return true
		}
	}
	return false
}

func policySelectsIngress(policy networkingv1.NetworkPolicy) bool {
	// Kubernetes defaults an omitted policyTypes field to include Ingress.
	// Treat it as ingress-isolating unless the author explicitly selected an
	// egress-only policy.
	if len(policy.Spec.PolicyTypes) == 0 {
		return true
	}
	for _, policyType := range policy.Spec.PolicyTypes {
		if policyType == networkingv1.PolicyTypeIngress {
			return true
		}
	}
	return false
}

func (p *BlueGreenPreparer) sourceClusterKey() (types.NamespacedName, types.UID, error) {
	if p.cluster == nil || p.resolver == nil {
		return types.NamespacedName{}, "", fmt.Errorf("%w: blue/green source identity wiring is incomplete", mvutypes.ErrUpgradeFlowFailed)
	}
	input, err := identityadapter.ClusterInputFromPostgresCluster(p.cluster)
	if err != nil {
		return types.NamespacedName{}, "", fmt.Errorf("resolving blue/green source identity: %w", err)
	}
	card, err := p.resolver.ResolveCluster(input)
	if err != nil {
		return types.NamespacedName{}, "", fmt.Errorf("resolving blue/green source authority: %w", err)
	}
	return types.NamespacedName{Name: card.Authoritative.Identity.Name, Namespace: card.Authoritative.Identity.Namespace}, card.Authoritative.Identity.UID, nil
}

func (p *BlueGreenPreparer) generation() int64 {
	if p.cluster == nil {
		return 0
	}
	return p.cluster.Generation
}

func (p *BlueGreenPreparer) validateLogicalSlotSupport(intent mvutypes.Intent) error {
	major, err := strconv.Atoi(strings.SplitN(intent.SourcePgVersion, ".", 2)[0])
	if err != nil || major < 17 {
		return fmt.Errorf("%w: source PostgreSQL %q does not support native logical-slot failover without an explicitly managed extension", mvutypes.ErrUpgradeFlowFailed, intent.SourcePgVersion)
	}
	return nil
}

type declaredDatabase struct {
	parent     platformv1alpha1.PostgresDatabase
	name       string
	definition platformv1alpha1.DatabaseDefinition
}

func (p *BlueGreenPreparer) managedDatabases(ctx context.Context) ([]declaredDatabase, error) {
	return listManagedDatabases(ctx, p.client, p.key)
}

func listManagedDatabases(ctx context.Context, c client.Client, key types.NamespacedName) ([]declaredDatabase, error) {
	var items platformv1alpha1.PostgresDatabaseList
	if err := c.List(ctx, &items, client.InNamespace(key.Namespace)); err != nil {
		return nil, err
	}
	var result []declaredDatabase
	owners := make(map[string]string)
	for _, parent := range items.Items {
		if parent.Spec.ClusterRef.Name != key.Name {
			continue
		}
		for _, database := range parent.Spec.Databases {
			if owner, exists := owners[database.Name]; exists {
				return nil, fmt.Errorf("%w: database %q is declared by both PostgresDatabase %q and %q", mvutypes.ErrUpgradeFlowFailed, database.Name, owner, parent.Name)
			}
			owners[database.Name] = parent.Name
			result = append(result, declaredDatabase{parent: parent, name: database.Name, definition: database})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].parent.Name == result[j].parent.Name {
			return result[i].name < result[j].name
		}
		return result[i].parent.Name < result[j].parent.Name
	})
	return result, nil
}

func (p *BlueGreenPreparer) enableLogicalSlotSynchronization(ctx context.Context, cluster *cnpgv1.Cluster) error {
	original := cluster.DeepCopy()
	if setLogicalSlotSynchronization(&cluster.Spec) {
		return nil
	}
	if err := p.client.Patch(ctx, cluster, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("configuring source logical-slot synchronization: %w", err)
	}
	return nil
}

func setLogicalSlotSynchronization(spec *cnpgv1.ClusterSpec) bool {
	if spec.ReplicationSlots == nil {
		spec.ReplicationSlots = &cnpgv1.ReplicationSlotsConfiguration{}
	}
	if spec.ReplicationSlots.HighAvailability == nil {
		spec.ReplicationSlots.HighAvailability = &cnpgv1.ReplicationSlotsHAConfiguration{}
	}
	alreadyEnabled := spec.ReplicationSlots.HighAvailability.SynchronizeLogicalDecoding
	spec.ReplicationSlots.HighAvailability.SynchronizeLogicalDecoding = true
	return alreadyEnabled
}

func (p *BlueGreenPreparer) reconcilePublication(ctx context.Context, clusterName string, database declaredDatabase, attemptID string) (bool, error) {
	name := publicationName(p.key.Name, database.name)
	publication := &cnpgv1.Publication{}
	err := p.client.Get(ctx, types.NamespacedName{Namespace: p.key.Namespace, Name: name}, publication)
	if err == nil {
		if !publicationMatchesAttempt(publication, p.cluster, clusterName, database.name, attemptID) {
			return false, fmt.Errorf("%w: publication %s does not match the managed source/database contract", mvutypes.ErrUpgradeFlowFailed, name)
		}
		return publication.Status.ObservedGeneration == publication.Generation && publication.Status.Applied != nil && *publication.Status.Applied, nil
	}
	if !apierrors.IsNotFound(err) {
		return false, err
	}
	publication = &cnpgv1.Publication{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       p.key.Namespace,
			Labels:          map[string]string{blueGreenAttemptLabel: attemptID},
			OwnerReferences: p.ownerReferences(),
		},
		Spec: desiredPublicationSpec(clusterName, database.name, name),
	}
	if err := p.client.Create(ctx, publication); err != nil {
		return false, err
	}
	return false, nil
}

func desiredPublicationSpec(sourceName, databaseName, name string) cnpgv1.PublicationSpec {
	return cnpgv1.PublicationSpec{
		ClusterRef:    corev1.LocalObjectReference{Name: sourceName},
		Name:          name,
		DBName:        databaseName,
		Target:        cnpgv1.PublicationTarget{AllTables: true},
		ReclaimPolicy: cnpgv1.PublicationReclaimDelete,
	}
}

func publicationMatchesAttempt(publication *cnpgv1.Publication, owner *platformv1alpha1.PostgresCluster, sourceName, databaseName, attemptID string) bool {
	return publication != nil && owner != nil &&
		publication.Labels[blueGreenAttemptLabel] == attemptID &&
		metav1.IsControlledBy(publication, owner) &&
		equality.Semantic.DeepEqual(publication.Spec, desiredPublicationSpec(sourceName, databaseName, publication.Name))
}

func (p *BlueGreenPreparer) ownerReferences() []metav1.OwnerReference {
	if p.cluster == nil {
		return nil
	}
	return []metav1.OwnerReference{*metav1.NewControllerRef(p.cluster, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}
}

// reconcileReplicationSecret creates one opaque password per attempt. The
// password never reaches status or logs; only the Secret and its fingerprint
// survive a controller restart.
func (p *BlueGreenPreparer) reconcileReplicationSecret(ctx context.Context, attemptID string) (string, string, error) {
	name := replicationSecretName(p.key.Name, attemptID)
	secret := &corev1.Secret{}
	err := p.client.Get(ctx, types.NamespacedName{Namespace: p.key.Namespace, Name: name}, secret)
	if err == nil {
		if secret.Labels[blueGreenAttemptLabel] != attemptID || !metav1.IsControlledBy(secret, p.cluster) {
			return "", "", fmt.Errorf("%w: replication Secret %s belongs to another blue/green attempt", mvutypes.ErrUpgradeFlowFailed, name)
		}
		username, password := string(secret.Data[replicationSecretUsernameKey]), string(secret.Data[replicationSecretPasswordKey])
		if username == "" || password == "" {
			return "", "", fmt.Errorf("%w: replication Secret %s is incomplete", mvutypes.ErrUpgradeFlowFailed, name)
		}
		return username, password, nil
	}
	if !apierrors.IsNotFound(err) {
		return "", "", err
	}
	passwordBytes := make([]byte, 32)
	if _, err := rand.Read(passwordBytes); err != nil {
		return "", "", fmt.Errorf("generating replication password: %w", err)
	}
	role := replicationRoleName(attemptID)
	secret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: p.key.Namespace, Labels: map[string]string{blueGreenAttemptLabel: attemptID}, OwnerReferences: p.ownerReferences()},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{replicationSecretUsernameKey: []byte(role), replicationSecretPasswordKey: []byte(base64.RawURLEncoding.EncodeToString(passwordBytes))},
	}
	if err := p.client.Create(ctx, secret); err != nil {
		return "", "", err
	}
	return role, string(secret.Data[replicationSecretPasswordKey]), nil
}

func (p *BlueGreenPreparer) configurationFingerprint(source *cnpgv1.Cluster, databases []declaredDatabase) (string, error) {
	if source == nil {
		return p.baseline().configurationFingerprint(nil, databases)
	}
	// Source preparation records the intended post-patch value before its first
	// mutation. Candidate verification instead hashes the actual live spec.
	prepared := source.DeepCopy()
	setLogicalSlotSynchronization(&prepared.Spec)
	return p.baseline().configurationFingerprint(prepared, databases)
}

func (p *BlueGreenPreparer) credentialFingerprints(ctx context.Context, source *cnpgv1.Cluster, databases []declaredDatabase, replicationSecret string) ([]platformv1alpha1.BlueGreenCredentialFingerprintStatus, error) {
	return p.baseline().credentialFingerprints(ctx, source, databases, replicationSecret)
}

func (p *BlueGreenPreparer) baseline() sourceBaseline {
	return sourceBaseline{client: p.client, key: p.key, cluster: p.cluster, targetConfiguration: p.plan.TargetConfiguration, targetImage: p.plan.TargetImage}
}

func replicationSecretName(clusterName, attemptID string) string {
	digest := sha256.Sum256([]byte(attemptID))
	return fmt.Sprintf("%s-bgr-%x", clusterName, digest[:4])
}

func replicationRoleName(attemptID string) string {
	digest := sha256.Sum256([]byte(attemptID))
	return fmt.Sprintf("sok_bg_%x", digest[:8])
}

// publicationName is DNS-label safe even when a 50-character PostgresCluster
// name owns a 30-character database. The suffix binds the resource to the
// database without leaking credentials or making a future green name part of
// the source-side contract.
func publicationName(clusterName, databaseName string) string {
	digest := sha256.Sum256([]byte(clusterName + "/" + databaseName))
	suffix := fmt.Sprintf("-bg-%x", digest[:5])
	maxPrefixLength := postgresIdentifierMaxLength - len(suffix)
	if len(clusterName) > maxPrefixLength {
		clusterName = clusterName[:maxPrefixLength]
	}
	return clusterName + suffix
}
