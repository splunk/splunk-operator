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
	"fmt"
	"maps"
	"reflect"
	"strconv"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgcnpg "github.com/splunk/splunk-operator/pkg/postgresql/shared/cnpg"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	pgendpointprotocol "github.com/splunk/splunk-operator/pkg/postgresql/shared/endpointprotocol"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// reconcileEndpointProtocol is the single entry point for database endpoint
// writes during a blue/green attempt. A handled attempt deliberately does not
// fall through to the ordinary role ConfigMap writer.
func reconcileEndpointProtocol(ctx context.Context, rc *ReconcileContext, db *platformv1alpha1.PostgresDatabase) (bool, ctrl.Result, error) {
	if rc.APIReader == nil {
		return reconcileEndpointWithoutAPIReader(ctx, rc, db)
	}
	parent, err := readEndpointParent(ctx, rc.APIReader, db)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return waitForMissingEndpointParent(ctx, rc, db)
		}
		return true, ctrl.Result{}, err
	}
	view := inspectEndpointAttempt(parent)
	bg := currentEndpointAttempt(parent)
	if bg == nil {
		return reconcileInactiveEndpointAttempt(ctx, rc, db, parent, view)
	}
	return reconcileActiveEndpointAttempt(ctx, rc, db, parent, bg)
}

// A missing parent must still produce the existing ClusterNotFound condition.
// Keep this path handled and use the uncached result as authority: a stale
// cached Ready observation must neither mark the child Ready nor reach a writer.
func waitForMissingEndpointParent(ctx context.Context, rc *ReconcileContext, db *platformv1alpha1.PostgresDatabase) (bool, ctrl.Result, error) {
	wasReady := db.Status.Phase != nil && *db.Status.Phase == string(readyDBPhase)
	rc.emitWarnOnceBeforeWait(db, db.Status.Conditions, clusterReady,
		EventClusterNotFound, fmt.Sprintf("PostgresCluster %s not found", db.Spec.ClusterRef.Name))
	if err := persistStatus(ctx, rc.Client, rc.Metrics, db, wasReady,
		clusterReady, metav1.ConditionFalse, reasonClusterNotFound,
		"Cluster CR not found", pendingDBPhase); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictClusterStatus, "persisting missing cluster status"); ok {
			return true, result, conflictErr
		}
		return true, ctrl.Result{}, err
	}
	return true, ctrl.Result{RequeueAfter: clusterNotFoundRetryDelay}, nil
}

func endpointPublicationSettled(ctx context.Context, rc *ReconcileContext, db *platformv1alpha1.PostgresDatabase, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	latest := inspectEndpointAttempt(parent).hasPublicationProof(bg)
	if !latest || bg.AttemptID == "" || bg.Switchover == nil || bg.Switchover.TargetAvailableAt == nil || bg.Green == nil || bg.EndpointPreparation == nil || bg.EndpointPublication == nil || bg.CommitReceipt == nil || bg.EndpointPublication.Generation <= 0 || bg.EndpointPublication.Generation != bg.EndpointIntentRevision || bg.EndpointPublication.Generation != bg.EndpointPreparation.Generation || bg.EndpointPublication.EndpointFingerprint == nil || bg.EndpointPreparation.EndpointFingerprint == nil || *bg.EndpointPublication.EndpointFingerprint == "" || *bg.EndpointPublication.EndpointFingerprint != *bg.EndpointPreparation.EndpointFingerprint {
		return false, nil
	}
	generation := bg.EndpointPublication.Generation
	fingerprint := *bg.EndpointPublication.EndpointFingerprint
	current, err := readCurrentEndpointChild(ctx, rc.APIReader, db)
	if err != nil {
		return false, err
	}
	// Once production has completed, retained-blue cleanup is independent of
	// ordinary database management. Original children still need their
	// acknowledgements; later children can be released after green authority
	// converges.
	cleaned := bg.Cleanup != nil && bg.Cleanup.State == platformv1alpha1.BlueGreenCleanupStateCleaned
	completed := endpointAttemptProductionCompleted(parent, bg)
	outsideRoster := !endpointChildInFrozenRoster(bg, current)
	if completed {
		// The ordinary cluster controller may have recreated the canonical
		// ConfigMap since the commit. Its old UID is no longer a release gate,
		// including for children in the original cutover roster.
		if err := verifyDurableGreenCommitStatus(parent, bg); err != nil {
			return false, err
		}
	} else if err := verifyDurableCommitIdentity(ctx, rc.APIReader, parent, bg); err != nil {
		return false, err
	}
	if outsideRoster && (completed || cleaned) {
		return ordinaryReaderObservesGreen(ctx, rc, current, parent, bg)
	}
	if err := verifyIntentInventory(bg.EndpointPreparation.Acknowledgements, current); err != nil {
		return false, err
	}
	if err := verifyPublicationIntentInventory(bg.EndpointPublication.Acknowledgements, current); err != nil {
		return false, err
	}
	if !preparationAcknowledged(current.Status.EndpointPreparation, bg, current, generation, fingerprint) || !publicationAcknowledged(current.Status.EndpointPublication, bg, current, generation, fingerprint) {
		return false, nil
	}
	return ordinaryReaderObservesGreen(ctx, rc, current, parent, bg)
}

// The cluster state store removes cutover proof after Cleanup=Cleaned. Only
// AttemptID, RearmedAt, and Cleanup survive in that durable history summary.
func cleanedHistoryAuthoritySettled(ctx context.Context, rc *ReconcileContext, db *platformv1alpha1.PostgresDatabase, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	if bg.AttemptID == "" || bg.Cleanup == nil || bg.Cleanup.State != platformv1alpha1.BlueGreenCleanupStateCleaned || parent.Status.ProvisionerRef == nil || parent.Status.ProvisionerRef.Name == "" || parent.Status.ProvisionerRef.UID == "" {
		return false, nil
	}
	current, err := readCurrentEndpointChild(ctx, rc.APIReader, db)
	if err != nil {
		return false, err
	}
	cached := &platformv1alpha1.PostgresCluster{}
	if err := rc.Client.Get(ctx, types.NamespacedName{Namespace: parent.Namespace, Name: parent.Name}, cached); err != nil {
		return false, err
	}
	cachedBG := mostRecentBlueGreenStatus(cached)
	if cached.UID != parent.UID || cached.ResourceVersion != parent.ResourceVersion ||
		cachedBG == nil || cachedBG.AttemptID != bg.AttemptID || cachedBG.Cleanup == nil || cachedBG.Cleanup.State != platformv1alpha1.BlueGreenCleanupStateCleaned ||
		cached.Status.ProvisionerRef == nil || cached.Status.ProvisionerRef.Name != parent.Status.ProvisionerRef.Name || cached.Status.ProvisionerRef.UID != parent.Status.ProvisionerRef.UID {
		return false, nil
	}
	if rc.ClusterReader == nil {
		return false, fmt.Errorf("cluster reader is required before releasing endpoint writer")
	}
	facts, err := rc.ClusterReader.Read(ctx, current.Namespace, current.Spec.ClusterRef.Name)
	if err != nil {
		return false, err
	}
	if facts.Cluster == nil {
		return false, fmt.Errorf("cluster reader returned no cluster identity before endpoint writer release")
	}
	identity := facts.Cluster.Authoritative.Identity
	return identity.Name == parent.Status.ProvisionerRef.Name && identity.UID == parent.Status.ProvisionerRef.UID && reflect.DeepEqual(facts.ConnectionPoolerStatus, parent.Status.ConnectionPoolerStatus), nil
}

func endpointChildInFrozenRoster(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, db *platformv1alpha1.PostgresDatabase) bool {
	for _, entry := range bg.EndpointPreparation.Acknowledgements {
		if entry.PostgresDatabaseName == db.Name && entry.PostgresDatabaseUID == string(db.UID) && entry.PostgresDatabaseGeneration == db.Generation {
			return true
		}
	}
	for _, entry := range bg.EndpointPublication.Acknowledgements {
		if entry.PostgresDatabaseName == db.Name && entry.PostgresDatabaseUID == string(db.UID) && entry.PostgresDatabaseGeneration == db.Generation {
			return true
		}
	}
	// A current child ACK also identifies an original participant if the parent
	// roster was damaged. It must not bypass the stricter historical checks.
	for _, ack := range []*platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{db.Status.EndpointPreparation, db.Status.EndpointPublication} {
		if ack != nil && ack.AttemptID == bg.AttemptID && ack.IntentGeneration == bg.EndpointIntentRevision && ack.ObservedDatabaseGeneration == db.Generation {
			return true
		}
	}
	return false
}

func ordinaryReaderObservesGreen(ctx context.Context, rc *ReconcileContext, db *platformv1alpha1.PostgresDatabase, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	cached := &platformv1alpha1.PostgresCluster{}
	if err := rc.Client.Get(ctx, types.NamespacedName{Namespace: parent.Namespace, Name: parent.Name}, cached); err != nil {
		return false, err
	}
	if cached.UID != parent.UID || cached.ResourceVersion != parent.ResourceVersion || cached.Status.ProvisionerRef == nil || cached.Status.ProvisionerRef.Name != bg.Green.Ref.Name || cached.Status.ProvisionerRef.UID != bg.Green.Ref.UID {
		return false, nil
	}
	cachedBG := mostRecentBlueGreenStatus(cached)
	if cachedBG == nil || cachedBG.AttemptID != bg.AttemptID || cachedBG.EndpointIntentRevision != bg.EndpointIntentRevision {
		return false, nil
	}
	if rc.ClusterReader == nil {
		return false, fmt.Errorf("cluster reader is required before releasing endpoint writer")
	}
	facts, err := rc.ClusterReader.Read(ctx, db.Namespace, db.Spec.ClusterRef.Name)
	if err != nil {
		return false, err
	}
	if facts.Cluster == nil {
		return false, fmt.Errorf("cluster reader returned no cluster identity before endpoint writer release")
	}
	identity := facts.Cluster.Authoritative.Identity
	return identity.Name == bg.Green.Ref.Name && identity.UID == bg.Green.Ref.UID && reflect.DeepEqual(facts.ConnectionPoolerStatus, parent.Status.ConnectionPoolerStatus), nil
}

func currentEndpointAttempt(parent *platformv1alpha1.PostgresCluster) *platformv1alpha1.PostgresBlueGreenUpgradeStatus {
	view := inspectEndpointAttempt(parent)
	if view.state == endpointAttemptActive {
		return view.status
	}
	return nil
}

func mostRecentBlueGreenStatus(parent *platformv1alpha1.PostgresCluster) *platformv1alpha1.PostgresBlueGreenUpgradeStatus {
	return inspectEndpointAttempt(parent).status
}

// A historical blue/green entry may still carry an intent. Keep it fenced,
// but never acknowledge or mutate its ConfigMaps after a newer upgrade entry
// has become the parent's current record.
func endpointAttemptIsLatest(parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
	return inspectEndpointAttempt(parent).isLatest(bg)
}

func endpointAttemptProductionCompleted(parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
	return inspectEndpointAttempt(parent).productionCompleted(bg)
}

func endpointCancellationRequested(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
	return bg.Gates != nil && bg.Gates.Cancellation != nil && bg.Gates.Cancellation.RequestedAt != nil
}

func readEndpointParent(ctx context.Context, reader client.Reader, db *platformv1alpha1.PostgresDatabase) (*platformv1alpha1.PostgresCluster, error) {
	parent := &platformv1alpha1.PostgresCluster{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: db.Namespace, Name: db.Spec.ClusterRef.Name}, parent); err != nil {
		return nil, fmt.Errorf("reading endpoint intent for PostgresDatabase %s: %w", db.Name, err)
	}
	return parent, nil
}

func readCurrentEndpointChild(ctx context.Context, reader client.Reader, original *platformv1alpha1.PostgresDatabase) (*platformv1alpha1.PostgresDatabase, error) {
	current := &platformv1alpha1.PostgresDatabase{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: original.Namespace, Name: original.Name}, current); err != nil {
		return nil, err
	}
	if current.UID != original.UID || current.Generation != original.Generation || current.Spec.ClusterRef.Name != original.Spec.ClusterRef.Name || !reflect.DeepEqual(current.Spec.Databases, original.Spec.Databases) || current.DeletionTimestamp != nil {
		return nil, fmt.Errorf("PostgresDatabase %s changed while processing endpoint intent", original.Name)
	}
	return current, nil
}

func endpointIntentMatches(current, original *platformv1alpha1.PostgresBlueGreenUpgradeStatus, generation int64, publication bool) bool {
	if current == nil || original == nil || current.AttemptID != original.AttemptID || current.EndpointIntentRevision != generation || current.Green == nil || original.Green == nil || current.Green.Ref.UID == "" || current.Green.Ref.UID != original.Green.Ref.UID {
		return false
	}
	if publication {
		return current.EndpointPublication != nil && current.EndpointPublication.Generation == generation && current.CommitReceipt != nil
	}
	return current.EndpointPreparation != nil && current.EndpointPreparation.Generation == generation && current.CommitReceipt == nil && current.EndpointPublication == nil && !endpointCancellationRequested(current)
}

func preparationIntent(parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, db *platformv1alpha1.PostgresDatabase) (int64, string, error) {
	if !endpointAttemptIsLatest(parent, bg) {
		return 0, "", fmt.Errorf("endpoint preparation attempt is no longer current")
	}
	if bg.Green == nil || bg.Blue == nil || bg.Green.Ref.Name == "" || bg.Green.Ref.UID == "" || bg.Blue.Ref.Name == "" || bg.Blue.Ref.UID == "" || bg.EndpointPreparation == nil || bg.EndpointPreparation.Generation <= 0 || bg.EndpointPreparation.Generation != bg.EndpointIntentRevision || bg.EndpointPreparation.EndpointFingerprint == nil || *bg.EndpointPreparation.EndpointFingerprint == "" || bg.CommitReceipt != nil || bg.EndpointPublication != nil || endpointCancellationRequested(bg) {
		return 0, "", fmt.Errorf("incomplete or stale endpoint preparation intent for %s", db.Name)
	}
	if err := verifyIntentInventory(bg.EndpointPreparation.Acknowledgements, db); err != nil {
		return 0, "", err
	}
	if parent.Status.ProvisionerRef != nil && parent.Status.ProvisionerRef.UID == bg.Green.Ref.UID {
		return 0, "", fmt.Errorf("green provider selected before canonical commit")
	}
	return bg.EndpointPreparation.Generation, *bg.EndpointPreparation.EndpointFingerprint, nil
}

func verifyIntentInventory(entries []platformv1alpha1.BlueGreenEndpointPreparationAcknowledgementStatus, db *platformv1alpha1.PostgresDatabase) error {
	for _, spec := range db.Spec.Databases {
		found := false
		for _, entry := range entries {
			if entry.PostgresDatabaseName == db.Name && entry.PostgresDatabaseUID == string(db.UID) && entry.PostgresDatabaseGeneration == db.Generation && entry.DatabaseName == spec.Name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("database %s is absent from endpoint preparation inventory", spec.Name)
		}
	}
	return nil
}

func prepareDatabaseEndpoints(ctx context.Context, rc *ReconcileContext, db *platformv1alpha1.PostgresDatabase, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	generation, fingerprint, err := preparationIntent(parent, bg, db)
	if err != nil {
		return err
	}
	if _, err := readCurrentEndpointChild(ctx, rc.APIReader, db); err != nil {
		return err
	}
	green, expected, err := candidateRoutingData(ctx, rc.APIReader, db.Namespace, parent, bg)
	if err != nil {
		return err
	}
	if pgconninfo.FingerprintEndpointData(expected) != fingerprint {
		return fmt.Errorf("green endpoint fingerprint differs from preparation intent")
	}
	blue := &cnpgv1.Cluster{}
	if err := rc.APIReader.Get(ctx, types.NamespacedName{Namespace: db.Namespace, Name: bg.Blue.Ref.Name}, blue); err != nil {
		return fmt.Errorf("reading blue CNPG Cluster: %w", err)
	}
	if blue.UID != bg.Blue.Ref.UID {
		return fmt.Errorf("blue CNPG Cluster UID changed")
	}
	blueEndpoints, err := resolveClusterEndpoints(parent.Status.ConnectionPoolerStatus, blue, db.Namespace)
	if err != nil {
		return fmt.Errorf("resolving blue endpoint data: %w", err)
	}
	blueRouting, err := pgconninfo.RoutingData(blueEndpoints)
	if err != nil {
		return err
	}
	op := preparationOperation{endpointOperationIntent: endpointOperationIntent{routing: blueRouting, attemptID: bg.AttemptID, generation: generation, fingerprint: fingerprint}}
	for _, spec := range db.Spec.Databases {
		if err := checkCurrentPreparation(ctx, rc.APIReader, db, bg, generation, fingerprint); err != nil {
			return err
		}
		if err := writeEndpointConfigMap(ctx, rc, db, spec, op); err != nil {
			return err
		}
	}
	if err := checkCurrentPreparation(ctx, rc.APIReader, db, bg, generation, fingerprint); err != nil {
		return err
	}
	proofs, err := verifyEndpointConfigMaps(ctx, rc.APIReader, db, op)
	if err != nil {
		return err
	}
	ack := &platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{
		AttemptID: bg.AttemptID, IntentGeneration: generation, ObservedDatabaseGeneration: db.Generation,
		GreenClusterUID: green.UID, Databases: proofs,
	}
	return persistEndpointAcknowledgement(ctx, rc, db, bg, op, ack)
}

func checkCurrentPreparation(ctx context.Context, reader client.Reader, db *platformv1alpha1.PostgresDatabase, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, generation int64, fingerprint string) error {
	parent, err := readEndpointParent(ctx, reader, db)
	if err != nil {
		return err
	}
	current := currentEndpointAttempt(parent)
	if !endpointAttemptIsLatest(parent, current) || !endpointIntentMatches(current, bg, generation, false) || current.EndpointPreparation.EndpointFingerprint == nil || *current.EndpointPreparation.EndpointFingerprint != fingerprint {
		return fmt.Errorf("endpoint preparation intent changed")
	}
	if _, err := readCurrentEndpointChild(ctx, reader, db); err != nil {
		return err
	}
	if parent.Status.Resources != nil && parent.Status.Resources.ConfigMapRef != nil {
		cm := &corev1.ConfigMap{}
		err := reader.Get(ctx, types.NamespacedName{Namespace: db.Namespace, Name: parent.Status.Resources.ConfigMapRef.Name}, cm)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		if err == nil && (cm.Annotations[pgconninfo.AnnotationActiveEnvironment] == bg.Green.Ref.Name || pgconninfo.FingerprintEndpointData(cm.Data) == fingerprint) {
			return fmt.Errorf("canonical endpoint commit was observed during preparation")
		}
	}
	return nil
}

func candidateRoutingData(ctx context.Context, reader client.Reader, namespace string, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (*cnpgv1.Cluster, map[string]string, error) {
	green := &cnpgv1.Cluster{}
	if bg.Green == nil || bg.Green.Ref.Name == "" || bg.Green.Ref.UID == "" {
		return nil, nil, fmt.Errorf("green CNPG Cluster identity is missing")
	}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: bg.Green.Ref.Name}, green); err != nil {
		return nil, nil, fmt.Errorf("reading green CNPG Cluster: %w", err)
	}
	if green.UID != bg.Green.Ref.UID || green.Status.Phase != cnpgv1.PhaseHealthy || green.Status.ReadyInstances < green.Spec.Instances || green.Status.ReadyInstances == 0 {
		return nil, nil, fmt.Errorf("green CNPG Cluster is not the expected healthy candidate")
	}
	var pooler pgcnpg.CandidatePoolerExpectation
	if parent.Status.ConnectionPoolerStatus != nil && parent.Status.ConnectionPoolerStatus.Enabled {
		pooler.Enabled = true
		pooler.RWEnabled = parent.Status.ConnectionPoolerStatus.ReadWriteEnabled
		pooler.ROEnabled = parent.Status.ConnectionPoolerStatus.ReadOnlyEnabled
	}
	data, err := pgcnpg.ResolveCandidateConnectionData(ctx, reader, green, bg.CandidateEndpoints, pooler)
	if err != nil {
		return nil, nil, fmt.Errorf("validating green endpoint Services: %w", err)
	}
	return green, data, nil
}

func publishDatabaseEndpoints(ctx context.Context, rc *ReconcileContext, db *platformv1alpha1.PostgresDatabase, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	if !endpointAttemptIsLatest(parent, bg) {
		return fmt.Errorf("endpoint publication attempt is no longer current")
	}
	if bg.EndpointPublication == nil || bg.EndpointPreparation == nil || bg.EndpointPublication.Generation <= 0 || bg.EndpointPublication.Generation != bg.EndpointIntentRevision || bg.EndpointPublication.Generation != bg.EndpointPreparation.Generation || bg.EndpointPublication.EndpointFingerprint == nil || bg.EndpointPreparation.EndpointFingerprint == nil || *bg.EndpointPublication.EndpointFingerprint == "" || *bg.EndpointPublication.EndpointFingerprint != *bg.EndpointPreparation.EndpointFingerprint {
		return fmt.Errorf("incomplete endpoint publication intent")
	}
	generation := bg.EndpointPublication.Generation
	fingerprint := *bg.EndpointPublication.EndpointFingerprint
	if err := verifyPreparedPublicationInventory(bg, db); err != nil {
		return err
	}
	if _, err := readCurrentEndpointChild(ctx, rc.APIReader, db); err != nil {
		return err
	}
	expected, err := committedCanonicalRouting(ctx, rc.APIReader, parent, bg, generation, fingerprint)
	if err != nil {
		return err
	}
	op := publicationOperation{endpointOperationIntent: endpointOperationIntent{routing: expected, attemptID: bg.AttemptID, generation: generation, fingerprint: fingerprint}, greenEnvironment: bg.Green.Ref.Name}
	if err := checkCurrentPublication(ctx, rc.APIReader, db, bg, generation, fingerprint, expected); err != nil {
		return err
	}
	if !preparationAcknowledged(db.Status.EndpointPreparation, bg, db, generation, fingerprint) {
		return fmt.Errorf("matching endpoint preparation acknowledgement is missing")
	}
	for _, spec := range db.Spec.Databases {
		if err := checkCurrentPublication(ctx, rc.APIReader, db, bg, generation, fingerprint, expected); err != nil {
			return err
		}
		if err := writeEndpointConfigMap(ctx, rc, db, spec, op); err != nil {
			return err
		}
	}
	if err := checkCurrentPublication(ctx, rc.APIReader, db, bg, generation, fingerprint, expected); err != nil {
		return err
	}
	proofs, err := verifyEndpointConfigMaps(ctx, rc.APIReader, db, op)
	if err != nil {
		return err
	}
	ack := &platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{
		AttemptID: bg.AttemptID, IntentGeneration: generation, ObservedDatabaseGeneration: db.Generation,
		GreenClusterUID: bg.Green.Ref.UID, Databases: proofs,
	}
	return persistEndpointAcknowledgement(ctx, rc, db, bg, op, ack)
}

func preparationAcknowledged(ack *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, db *platformv1alpha1.PostgresDatabase, generation int64, fingerprint string) bool {
	if ack == nil || ack.AttemptID != bg.AttemptID || ack.IntentGeneration != generation || ack.ObservedDatabaseGeneration != db.Generation || ack.GreenClusterUID != bg.Green.Ref.UID || len(ack.Databases) != len(db.Spec.Databases) {
		return false
	}
	for _, spec := range db.Spec.Databases {
		found := false
		for _, proof := range ack.Databases {
			if proof.DatabaseName == spec.Name && proof.EndpointFingerprint == fingerprint && proof.AcknowledgedAt != nil {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func publicationAcknowledged(ack *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, db *platformv1alpha1.PostgresDatabase, generation int64, fingerprint string) bool {
	if !preparationAcknowledged(ack, bg, db, generation, fingerprint) {
		return false
	}
	seen := make(map[string]struct{}, len(ack.Databases))
	for _, proof := range ack.Databases {
		if proof.ConfigMapUID == "" {
			return false
		}
		if _, exists := seen[proof.DatabaseName]; exists {
			return false
		}
		seen[proof.DatabaseName] = struct{}{}
	}
	return true
}

func checkCurrentPublication(ctx context.Context, reader client.Reader, db *platformv1alpha1.PostgresDatabase, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, generation int64, fingerprint string, expected map[string]string) error {
	parent, err := readEndpointParent(ctx, reader, db)
	if err != nil {
		return err
	}
	current := currentEndpointAttempt(parent)
	if !endpointAttemptIsLatest(parent, current) || !endpointIntentMatches(current, bg, generation, true) || current.EndpointPublication.EndpointFingerprint == nil || *current.EndpointPublication.EndpointFingerprint != fingerprint || current.EndpointPreparation == nil || current.EndpointPreparation.Generation != generation || current.EndpointPreparation.EndpointFingerprint == nil || *current.EndpointPreparation.EndpointFingerprint != fingerprint {
		return fmt.Errorf("endpoint publication intent changed")
	}
	child, err := readCurrentEndpointChild(ctx, reader, db)
	if err != nil {
		return err
	}
	if !preparationAcknowledged(child.Status.EndpointPreparation, current, child, generation, fingerprint) {
		return fmt.Errorf("matching endpoint preparation acknowledgement disappeared")
	}
	if err := verifyPublicationIntentInventory(current.EndpointPublication.Acknowledgements, child); err != nil {
		return err
	}
	committed, err := committedCanonicalRouting(ctx, reader, parent, current, generation, fingerprint)
	if err != nil {
		return err
	}
	if !pgconninfo.EndpointDataMatches(committed, expected) {
		return fmt.Errorf("canonical endpoint ConfigMap routing changed during publication")
	}
	return nil
}

func verifyPublicationIntentInventory(entries []platformv1alpha1.BlueGreenEndpointPublicationAcknowledgementStatus, db *platformv1alpha1.PostgresDatabase) error {
	for _, spec := range db.Spec.Databases {
		found := false
		for _, entry := range entries {
			if entry.PostgresDatabaseName == db.Name && entry.PostgresDatabaseUID == string(db.UID) && entry.PostgresDatabaseGeneration == db.Generation && entry.DatabaseName == spec.Name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("database %s is absent from endpoint publication inventory", spec.Name)
		}
	}
	return nil
}

func verifyDurableCommitIdentity(ctx context.Context, reader client.Reader, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	if err := verifyDurableGreenCommitStatus(parent, bg); err != nil {
		return err
	}
	receipt := bg.CommitReceipt
	cm := &corev1.ConfigMap{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: parent.Namespace, Name: receipt.CanonicalConfigMap.Name}, cm); err != nil {
		return fmt.Errorf("reading canonical endpoint ConfigMap: %w", err)
	}
	if cm.UID != receipt.CanonicalConfigMap.UID {
		return fmt.Errorf("canonical endpoint ConfigMap UID changed after commit")
	}
	return nil
}

func verifyDurableGreenCommitStatus(parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	if !pgendpointprotocol.DurableCommitStatusMatches(parent, bg) {
		return fmt.Errorf("canonical endpoint commit status is incomplete")
	}
	return nil
}

func committedCanonicalRouting(ctx context.Context, reader client.Reader, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, generation int64, fingerprint string) (map[string]string, error) {
	if err := verifyDurableCommitIdentity(ctx, reader, parent, bg); err != nil {
		return nil, err
	}
	cm := &corev1.ConfigMap{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: parent.Namespace, Name: bg.CommitReceipt.CanonicalConfigMap.Name}, cm); err != nil {
		return nil, fmt.Errorf("reading canonical endpoint ConfigMap: %w", err)
	}
	routing, valid := pgendpointprotocol.CommittedCanonicalRouting(parent, bg, cm, generation, fingerprint)
	if !valid {
		return nil, fmt.Errorf("canonical endpoint ConfigMap does not prove the current green commit")
	}
	return routing, nil
}

func completeCommittedRouting(data map[string]string) bool {
	return pgendpointprotocol.CompleteRouting(data)
}

func writeEndpointConfigMap(ctx context.Context, rc *ReconcileContext, db *platformv1alpha1.PostgresDatabase, spec platformv1alpha1.DatabaseDefinition, op endpointOperation) error {
	intent := op.intent()
	key := types.NamespacedName{Namespace: db.Namespace, Name: configMapName(db.Name, spec.Name)}
	cm := &corev1.ConfigMap{}
	err := rc.APIReader.Get(ctx, key, cm)
	if apierrors.IsNotFound(err) {
		return createEndpointConfigMap(ctx, rc, db, spec, op, key)
	}
	if err != nil {
		return err
	}
	if !metav1.IsControlledBy(cm, db) {
		return fmt.Errorf("endpoint ConfigMap %s is not controlled by PostgresDatabase UID %s", key.Name, db.UID)
	}
	if err := rejectNewerEndpointPublication(ctx, rc, db, cm, op); err != nil {
		return err
	}
	if pgconninfo.EndpointDataMatches(cm.Data, intent.routing) && op.stampMatches(cm) {
		return nil
	}
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	for _, endpointKey := range pgconninfo.EndpointKeys() {
		delete(cm.Data, endpointKey)
		if value, ok := intent.routing[endpointKey]; ok {
			cm.Data[endpointKey] = value
		}
	}
	op.stamp(cm)
	if err := validateEndpointWriterIntent(ctx, rc, db, op); err != nil {
		return err
	}
	if err := rc.Client.Update(ctx, cm); err != nil {
		return fmt.Errorf("updating endpoint ConfigMap %s: %w", key.Name, err)
	}
	return nil
}

func validateEndpointWriterIntent(ctx context.Context, rc *ReconcileContext, db *platformv1alpha1.PostgresDatabase, op endpointOperation) error {
	if rc.APIReader == nil {
		return fmt.Errorf("uncached API reader is required for endpoint ConfigMap mutation")
	}
	parent, err := readEndpointParent(ctx, rc.APIReader, db)
	if err != nil {
		return err
	}
	current := currentEndpointAttempt(parent)
	if current == nil || current.AttemptID != op.intent().attemptID || !endpointAttemptIsLatest(parent, current) {
		return fmt.Errorf("endpoint intent changed before ConfigMap mutation")
	}
	return op.validateCurrent(ctx, rc.APIReader, db, current)
}

func rejectNewerEndpointPublication(ctx context.Context, rc *ReconcileContext, db *platformv1alpha1.PostgresDatabase, cm *corev1.ConfigMap, op endpointOperation) error {
	intent := op.intent()
	if cm.Annotations == nil || cm.Annotations[pgconninfo.AnnotationEndpointAttemptID] == "" {
		return nil
	}
	if cm.Annotations[pgconninfo.AnnotationEndpointAttemptID] != intent.attemptID {
		// A previous attempt's projection may legitimately be the blue side of
		// the new attempt. A delayed old writer may not overwrite a newer
		// projection: it must prove that its intent is still the current one.
		parent, err := readEndpointParent(ctx, rc.APIReader, db)
		if err != nil {
			return err
		}
		current := currentEndpointAttempt(parent)
		if current == nil || current.AttemptID != intent.attemptID {
			return fmt.Errorf("endpoint ConfigMap %s belongs to a newer attempt", cm.Name)
		}
		return op.validateCurrent(ctx, rc.APIReader, db, current)
	}
	previous, err := strconv.ParseInt(cm.Annotations[pgconninfo.AnnotationEndpointIntentGeneration], 10, 64)
	if err != nil {
		return fmt.Errorf("endpoint ConfigMap %s has invalid intent generation annotation: %w", cm.Name, err)
	}
	if previous > intent.generation {
		return fmt.Errorf("endpoint ConfigMap %s has newer publication generation %d", cm.Name, previous)
	}
	return nil
}

func stampEndpointPublication(cm *corev1.ConfigMap, environment, attempt string, generation int64, fingerprint string) {
	if cm.Annotations == nil {
		cm.Annotations = map[string]string{}
	}
	cm.Annotations[pgconninfo.AnnotationActiveEnvironment] = environment
	cm.Annotations[pgconninfo.AnnotationEndpointAttemptID] = attempt
	cm.Annotations[pgconninfo.AnnotationEndpointIntentGeneration] = strconv.FormatInt(generation, 10)
	cm.Annotations[pgconninfo.AnnotationEndpointFingerprint] = fingerprint
}

func endpointStampMatches(cm *corev1.ConfigMap, environment, attempt string, generation int64, fingerprint string) bool {
	return cm != nil && pgendpointprotocol.PublicationStampMatchesValues(cm.Annotations, environment, attempt, generation, fingerprint)
}

func verifyEndpointConfigMaps(ctx context.Context, reader client.Reader, db *platformv1alpha1.PostgresDatabase, op endpointOperation) ([]platformv1alpha1.DatabaseEndpointAcknowledgementStatus, error) {
	intent := op.intent()
	proofs := make([]platformv1alpha1.DatabaseEndpointAcknowledgementStatus, 0, len(db.Spec.Databases))
	for _, spec := range db.Spec.Databases {
		cm := &corev1.ConfigMap{}
		if err := reader.Get(ctx, types.NamespacedName{Namespace: db.Namespace, Name: configMapName(db.Name, spec.Name)}, cm); err != nil {
			return nil, err
		}
		if !metav1.IsControlledBy(cm, db) || !pgconninfo.EndpointDataMatches(cm.Data, intent.routing) || !op.stampMatches(cm) {
			return nil, fmt.Errorf("endpoint ConfigMap %s failed read-back verification", cm.Name)
		}
		now := metav1.Now()
		proofs = append(proofs, platformv1alpha1.DatabaseEndpointAcknowledgementStatus{DatabaseName: spec.Name, EndpointFingerprint: intent.fingerprint, ConfigMapUID: cm.UID, ConfigMapResourceVersion: cm.ResourceVersion, AcknowledgedAt: &now})
	}
	return proofs, nil
}

func persistEndpointAcknowledgement(ctx context.Context, rc *ReconcileContext, db *platformv1alpha1.PostgresDatabase, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, op endpointOperation, ack *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus) error {
	if err := op.validateCurrent(ctx, rc.APIReader, db, bg); err != nil {
		return err
	}
	if err := op.verifyBeforeAck(ctx, rc.APIReader, db); err != nil {
		return err
	}
	current, err := readCurrentEndpointChild(ctx, rc.APIReader, db)
	if err != nil {
		return err
	}
	if endpointAcknowledgementMatches(op.existingAck(current), ack) {
		return nil
	}
	op.setAck(current, ack)
	// Narrow the parent/child status race window immediately before the write.
	// The parent still verifies attempt and generation again at its barrier.
	if err := op.validateCurrent(ctx, rc.APIReader, db, bg); err != nil {
		return err
	}
	return rc.Client.Status().Update(ctx, current)
}

func endpointAcknowledgementMatches(existing, desired *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus) bool {
	if existing == nil || existing.AttemptID != desired.AttemptID || existing.IntentGeneration != desired.IntentGeneration || existing.ObservedDatabaseGeneration != desired.ObservedDatabaseGeneration || existing.GreenClusterUID != desired.GreenClusterUID || len(existing.Databases) != len(desired.Databases) {
		return false
	}
	for _, wanted := range desired.Databases {
		found := false
		for _, have := range existing.Databases {
			if have.DatabaseName == wanted.DatabaseName && have.EndpointFingerprint == wanted.EndpointFingerprint && have.ConfigMapUID == wanted.ConfigMapUID && have.AcknowledgedAt != nil {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func reconcileEndpointWithoutAPIReader(ctx context.Context, rc *ReconcileContext, db *platformv1alpha1.PostgresDatabase) (bool, ctrl.Result, error) {
	// The uncached reader is mandatory once an endpoint intent exists. Use the
	// cached client only to detect that fail-closed condition in old callers.
	parent := &platformv1alpha1.PostgresCluster{}
	if err := rc.Client.Get(ctx, types.NamespacedName{Namespace: db.Namespace, Name: db.Spec.ClusterRef.Name}, parent); err != nil {
		if apierrors.IsNotFound(err) {
			return waitForMissingEndpointParent(ctx, rc, db)
		}
		return true, ctrl.Result{}, fmt.Errorf("checking endpoint intent without uncached reader: %w", err)
	}
	view := inspectEndpointAttempt(parent)
	if latest := view.status; latest != nil && (latest.EndpointPreparation != nil || latest.EndpointPublication != nil || latest.CommitReceipt != nil || latest.HistoryCompacted || latest.Cleanup != nil && latest.Cleanup.State == platformv1alpha1.BlueGreenCleanupStateCleaned) {
		return true, ctrl.Result{}, fmt.Errorf("uncached API reader is required for endpoint intent %s", latest.AttemptID)
	}
	return false, ctrl.Result{}, nil
}

func reconcileActiveEndpointAttempt(ctx context.Context, rc *ReconcileContext, db *platformv1alpha1.PostgresDatabase, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, ctrl.Result, error) {
	// TargetAvailable is only a release hint. Require the durable commit,
	// child acknowledgement, and green authority in the ordinary reader before
	// returning to its writer. Routing may have changed since cutover.
	if bg.Switchover != nil && bg.Switchover.TargetAvailableAt != nil {
		return waitForEndpointPublication(ctx, rc, db, parent, bg)
	}
	if bg.EndpointPublication != nil {
		if err := publishDatabaseEndpoints(ctx, rc, db, parent, bg); err != nil {
			return true, ctrl.Result{}, err
		}
		return true, ctrl.Result{RequeueAfter: retryDelay}, nil
	}
	if bg.CommitReceipt != nil {
		// There is an externally observable commit, but the parent has not yet
		// published the child intent. Never let ordinary reconciliation run here.
		return true, ctrl.Result{RequeueAfter: retryDelay}, nil
	}
	if bg.EndpointPreparation != nil && !endpointCancellationRequested(bg) {
		if err := prepareDatabaseEndpoints(ctx, rc, db, parent, bg); err != nil {
			return true, ctrl.Result{}, err
		}
		return true, ctrl.Result{RequeueAfter: retryDelay}, nil
	}
	// Before an intent, or after pre-commit cancellation, ordinary blue
	// reconciliation may continue. A switched provider without a commit is
	// inconsistent, so it remains fenced from the ordinary writer.
	if bg.Green != nil && parent.Status.ProvisionerRef != nil && parent.Status.ProvisionerRef.UID == bg.Green.Ref.UID {
		return true, ctrl.Result{RequeueAfter: retryDelay}, nil
	}
	return false, ctrl.Result{}, nil
}

func waitForEndpointPublication(ctx context.Context, rc *ReconcileContext, db *platformv1alpha1.PostgresDatabase, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, ctrl.Result, error) {
	settled, err := endpointPublicationSettled(ctx, rc, db, parent, bg)
	if settled {
		return false, ctrl.Result{}, nil
	}
	if err != nil {
		return true, ctrl.Result{}, err
	}
	// A historical cutover payload must never be republished after
	// availability; ordinary reconciliation owns subsequent routing changes.
	return true, ctrl.Result{RequeueAfter: retryDelay}, nil
}

func reconcileInactiveEndpointAttempt(ctx context.Context, rc *ReconcileContext, db *platformv1alpha1.PostgresDatabase, parent *platformv1alpha1.PostgresCluster, view endpointAttemptView) (bool, ctrl.Result, error) {
	if view.state == endpointAttemptHistoricalNonOwning {
		return false, ctrl.Result{}, nil
	}
	if view.state == endpointAttemptInvalid {
		if view.status.AttemptID == "" {
			return true, ctrl.Result{}, fmt.Errorf("endpoint intent is missing attempt ID")
		}
		return true, ctrl.Result{}, fmt.Errorf("endpoint attempt has invalid history state")
	}
	if view.state == endpointAttemptHistorical && (view.status.Cleanup == nil || view.status.Cleanup.State != platformv1alpha1.BlueGreenCleanupStateCleaned) {
		return true, ctrl.Result{}, fmt.Errorf("endpoint attempt %s is no longer current", view.status.AttemptID)
	}
	if view.status == nil {
		return false, ctrl.Result{}, nil
	}
	switch view.state {
	case endpointAttemptCleanedDetailed, endpointAttemptCleanedCompacted, endpointAttemptHistorical:
		return waitForCleanedEndpointAuthority(ctx, rc, db, parent, view)
	default:
		return false, ctrl.Result{}, nil
	}
}

// Cleanup can finish before the ordinary reader observes the current provider
// (green after commit, blue after pre-commit cancellation). Keep the fence until
// that reader catches up.
func waitForCleanedEndpointAuthority(ctx context.Context, rc *ReconcileContext, db *platformv1alpha1.PostgresDatabase, parent *platformv1alpha1.PostgresCluster, view endpointAttemptView) (bool, ctrl.Result, error) {
	latest := view.status
	if view.state == endpointAttemptHistorical && !view.compacted && latest.CommitReceipt == nil && !view.precommitCancellation(parent) {
		return true, ctrl.Result{RequeueAfter: retryDelay}, nil
	}
	var settled bool
	var err error
	if view.state == endpointAttemptCleanedCompacted || view.state == endpointAttemptHistorical && (view.compacted || latest.CommitReceipt == nil) {
		settled, err = cleanedHistoryAuthoritySettled(ctx, rc, db, parent, latest)
	} else {
		settled, err = endpointPublicationSettled(ctx, rc, db, parent, latest)
	}
	if err != nil {
		return true, ctrl.Result{}, err
	}
	if !settled {
		return true, ctrl.Result{RequeueAfter: retryDelay}, nil
	}
	return false, ctrl.Result{}, nil
}

func createEndpointConfigMap(ctx context.Context, rc *ReconcileContext, db *platformv1alpha1.PostgresDatabase, spec platformv1alpha1.DatabaseDefinition, op endpointOperation, key types.NamespacedName) error {
	intent := op.intent()
	data := make(map[string]string, len(intent.routing)+3)
	maps.Copy(data, intent.routing)
	roles := EffectiveRoleNames(spec)
	data[ConfigMapKeyDatabaseName] = spec.Name
	data[ConfigMapKeyAdminUser] = roles.Admin
	data[ConfigMapKeyRWUser] = roles.RW
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace, Labels: map[string]string{labelManagedBy: "splunk-operator"}}, Data: data}
	op.stamp(cm)
	if err := controllerutil.SetControllerReference(db, cm, rc.Scheme); err != nil {
		return err
	}
	if err := validateEndpointWriterIntent(ctx, rc, db, op); err != nil {
		return err
	}
	if err := rc.Client.Create(ctx, cm); err != nil {
		return fmt.Errorf("creating endpoint ConfigMap %s: %w", key.Name, err)
	}
	return nil
}

func verifyPreparedPublicationInventory(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, db *platformv1alpha1.PostgresDatabase) error {
	if err := verifyIntentInventory(bg.EndpointPreparation.Acknowledgements, db); err != nil {
		return err
	}
	if err := verifyPublicationIntentInventory(bg.EndpointPublication.Acknowledgements, db); err != nil {
		return err
	}
	return nil
}
