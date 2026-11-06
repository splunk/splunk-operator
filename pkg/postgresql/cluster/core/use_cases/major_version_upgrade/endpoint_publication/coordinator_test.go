package endpointpublication

import (
	"context"
	"errors"
	"maps"
	"strconv"
	"testing"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	pgendpointprotocol "github.com/splunk/splunk-operator/pkg/postgresql/shared/endpointprotocol"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

type memoryStore struct {
	parent           *platformv1alpha1.PostgresCluster
	children         []platformv1alpha1.PostgresDatabase
	configMaps       map[string]*corev1.ConfigMap
	conflicts        int
	configMapReadErr error
}

func (s *memoryStore) ReadParent(_ context.Context, _ types.NamespacedName) (*platformv1alpha1.PostgresCluster, error) {
	return s.parent.DeepCopy(), nil
}
func (s *memoryStore) ListChildren(_ context.Context, _ string) ([]platformv1alpha1.PostgresDatabase, error) {
	result := make([]platformv1alpha1.PostgresDatabase, len(s.children))
	for i := range s.children {
		result[i] = *s.children[i].DeepCopy()
	}
	return result, nil
}
func (s *memoryStore) ReadConfigMap(_ context.Context, key types.NamespacedName) (*corev1.ConfigMap, error) {
	if s.configMapReadErr != nil {
		return nil, s.configMapReadErr
	}
	cm := s.configMaps[key.Name]
	if cm == nil {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, key.Name)
	}
	return cm.DeepCopy(), nil
}
func (s *memoryStore) UpdateConfigMap(_ context.Context, cm *corev1.ConfigMap) error {
	current := s.configMaps[cm.Name]
	if current == nil {
		return apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, cm.Name)
	}
	if current.UID != cm.UID || current.ResourceVersion != cm.ResourceVersion {
		return apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, cm.Name, errors.New("stale ConfigMap"))
	}
	next := cm.DeepCopy()
	version, _ := strconv.Atoi(current.ResourceVersion)
	next.ResourceVersion = strconv.Itoa(version + 1)
	s.configMaps[cm.Name] = next
	return nil
}
func (s *memoryStore) CreateConfigMap(_ context.Context, cm *corev1.ConfigMap) error {
	if s.configMaps[cm.Name] != nil {
		return apierrors.NewAlreadyExists(schema.GroupResource{Resource: "configmaps"}, cm.Name)
	}
	next := cm.DeepCopy()
	next.UID = types.UID("recreated-" + cm.Name)
	next.ResourceVersion = "1"
	s.configMaps[cm.Name] = next
	return nil
}
func (s *memoryStore) WriteParentStatus(_ context.Context, parent *platformv1alpha1.PostgresCluster) error {
	if s.conflicts > 0 {
		s.conflicts--
		return apierrors.NewConflict(schema.GroupResource{Resource: "postgresclusters"}, parent.Name, errors.New("injected conflict"))
	}
	if parent.ResourceVersion != s.parent.ResourceVersion {
		return apierrors.NewConflict(schema.GroupResource{Resource: "postgresclusters"}, parent.Name, errors.New("stale version"))
	}
	s.parent = parent.DeepCopy()
	rev, _ := strconv.Atoi(s.parent.ResourceVersion)
	s.parent.ResourceVersion = strconv.Itoa(rev + 1)
	return nil
}

type fixedProof struct {
	proof     Proof
	failGreen bool
	failBlue  bool
}

func (f fixedProof) Resolve(context.Context, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (Proof, error) {
	if f.failGreen || f.failBlue {
		return Proof{}, errors.New("pre-commit endpoint unavailable")
	}
	return f.proof, nil
}
func (f fixedProof) ResolveBlue(context.Context, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (map[string]string, error) {
	if f.failBlue {
		return nil, errors.New("blue CNPG Cluster unavailable")
	}
	return f.proof.Blue, nil
}
func (f fixedProof) ResolveBlueAccess(context.Context, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (map[string]string, error) {
	if f.failBlue {
		return nil, errors.New("blue CNPG Cluster unavailable")
	}
	return map[string]string{"SUPER_USER_NAME": "postgres", "SUPER_USER_SECRET_REF": "blue-superuser", "SERVER_CA_SECRET_REF": "blue-ca"}, nil
}
func (f fixedProof) ResolveGreen(context.Context, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (map[string]string, types.UID, error) {
	if f.failGreen {
		return nil, "", errors.New("green Service unavailable")
	}
	return f.proof.Green, f.proof.GreenUID, nil
}

func fixture(t *testing.T) (*Coordinator, *memoryStore, types.NamespacedName, Proof) {
	blue := map[string]string{pgconninfo.KeyClusterRWEndpoint: "blue-rw", pgconninfo.KeyClusterROEndpoint: "blue-ro", pgconninfo.KeyClusterREndpoint: "blue-r", pgconninfo.KeyDefaultClusterPort: "5432"}
	green := map[string]string{pgconninfo.KeyClusterRWEndpoint: "green-rw", pgconninfo.KeyClusterROEndpoint: "green-ro", pgconninfo.KeyClusterREndpoint: "green-r", pgconninfo.KeyDefaultClusterPort: "5432"}
	proof := Proof{Blue: blue, Green: green, GreenUID: types.UID("green-uid")}
	child := platformv1alpha1.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "tenant", Namespace: "ns", UID: types.UID("child-uid"), Generation: 1}, Spec: platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "cluster"}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "orders"}}}}
	bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt", Blue: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "blue", UID: "blue-uid"}}, Green: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}}, DatabaseReplication: []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{PostgresDatabaseName: "tenant", PostgresDatabaseUID: "child-uid", DatabaseName: "orders"}}}
	now := metav1.Now()
	deadline := metav1.NewTime(now.Add(time.Hour))
	bg.Gates = &platformv1alpha1.BlueGreenGateStatus{Switchover: &platformv1alpha1.BlueGreenGateConsumptionStatus{Generation: 1, ConsumedAt: &now}}
	bg.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1, StartedAt: &now, DeadlineAt: &deadline,
		LatchedTimeout: &metav1.Duration{Duration: time.Hour}, FenceAppliedAt: &now, FenceVerifiedAt: &now,
		DrainCompletedAt: &now, FinalBlueLSN: new("0/123"), FinalParityVerifiedAt: &now,
		CandidateGuardsRemovalStartedAt: &now, CandidateGuardsRemovedAt: &now,
		SequenceSynchronization: &platformv1alpha1.BlueGreenSequenceSynchronizationStatus{CompletedAt: &now, VerifiedAt: &now}}
	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "ns", UID: "parent-uid", ResourceVersion: "1"}, Status: platformv1alpha1.PostgresClusterStatus{Resources: &platformv1alpha1.PostgresClusterResources{ConfigMapRef: &corev1.LocalObjectReference{Name: "cluster-config"}}, PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: bg}}}}
	owner := metav1.OwnerReference{UID: child.UID, Name: child.Name, Controller: new(true)}
	store := &memoryStore{parent: parent, children: []platformv1alpha1.PostgresDatabase{child}, configMaps: map[string]*corev1.ConfigMap{
		"cluster-config":       {ObjectMeta: metav1.ObjectMeta{Name: "cluster-config", Namespace: "ns", UID: "canonical-uid", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}}, Data: blue},
		"tenant-orders-config": {ObjectMeta: metav1.ObjectMeta{Name: "tenant-orders-config", Namespace: "ns", UID: "db-cm-uid", OwnerReferences: []metav1.OwnerReference{owner}}, Data: blue},
	}}
	coordinator, err := newTestCoordinator(store, fixedProof{proof: proof})
	if err != nil {
		t.Fatal(err)
	}
	return coordinator, store, types.NamespacedName{Namespace: "ns", Name: "cluster"}, proof
}

func currentBG(s *memoryStore) *platformv1alpha1.PostgresBlueGreenUpgradeStatus {
	return s.parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen
}

func TestPreparationBarrierRequiresCurrentChildProofAndBlueMaps(t *testing.T) {
	ctx := t.Context()
	coordinator, store, key, proof := fixture(t)
	store.conflicts = 1
	if err := coordinator.PublishPreparationIntent(ctx, key, "attempt"); err != nil {
		t.Fatal(err)
	}
	bg := currentBG(store)
	if bg.EndpointPreparation.Generation != 1 || bg.EndpointIntentRevision != 1 || len(bg.EndpointPreparation.Acknowledgements) != 1 {
		t.Fatalf("unexpected preparation intent: %+v", bg.EndpointPreparation)
	}
	if err := coordinator.PublishPreparationIntent(ctx, key, "attempt"); err != nil {
		t.Fatal(err)
	}
	if currentBG(store).EndpointIntentRevision != 1 {
		t.Fatal("retry advanced revision")
	}
	if complete, err := coordinator.PreparationComplete(ctx, key, "attempt"); err != nil || complete {
		t.Fatalf("placeholder counted as proof: complete=%v err=%v", complete, err)
	}
	fingerprint := pgconninfo.FingerprintEndpointData(proof.Green)
	now := metav1.NewTime(time.Now())
	store.children[0].Status.EndpointPreparation = &platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{AttemptID: "attempt", IntentGeneration: 1, ObservedDatabaseGeneration: 1, GreenClusterUID: "green-uid", Databases: []platformv1alpha1.DatabaseEndpointAcknowledgementStatus{{DatabaseName: "orders", EndpointFingerprint: fingerprint, AcknowledgedAt: &now}}}
	if complete, err := coordinator.PreparationComplete(ctx, key, "attempt"); err != nil || !complete {
		t.Fatalf("valid preparation pending: complete=%v err=%v", complete, err)
	}
	if currentBG(store).EndpointPreparation.Acknowledgements[0].ValidatedAt == nil {
		t.Fatal("parent did not aggregate proof")
	}
	store.configMaps["tenant-orders-config"].Data = proof.Green
	if complete, err := coordinator.PreparationComplete(ctx, key, "attempt"); err != nil || complete {
		t.Fatalf("green child map passed pre-commit barrier: complete=%v err=%v", complete, err)
	}
	if currentBG(store).EndpointPreparation.Acknowledgements[0].ValidatedAt != nil {
		t.Fatal("stale aggregate was not cleared")
	}
	store.configMaps["tenant-orders-config"].Data = proof.Blue
	store.children[0].Generation = 2
	if complete, err := coordinator.PreparationComplete(ctx, key, "attempt"); err != nil || complete {
		t.Fatalf("child generation drift passed: complete=%v err=%v", complete, err)
	}
}

func TestPublicationBarrierChecksReceiptChildProofAndActualMap(t *testing.T) {
	ctx := t.Context()
	coordinator, store, key, proof := fixture(t)
	fingerprint, now := preparePublicationBarrierTest(t, coordinator, store, key, proof)
	bg := currentBG(store)
	bg.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CanonicalConfigMap: &corev1.ObjectReference{Name: "cluster-config", UID: "canonical-uid"}, Target: &corev1.ObjectReference{Name: "green", UID: "green-uid"}}
	store.parent.Status.ProvisionerRef = &corev1.ObjectReference{Name: "green", UID: "green-uid"}
	canonical := store.configMaps["cluster-config"]
	canonical.Data = proof.Green
	canonical.Annotations = map[string]string{pgconninfo.AnnotationActiveEnvironment: "green", pgconninfo.AnnotationEndpointAttemptID: "attempt", pgconninfo.AnnotationEndpointIntentGeneration: "1", pgconninfo.AnnotationEndpointFingerprint: fingerprint}
	coordinator.proof = fixedProof{proof: proof, failBlue: true, failGreen: true}
	if err := coordinator.PublishPublicationIntent(ctx, key, "attempt"); err != nil {
		t.Fatal(err)
	}
	if currentBG(store).EndpointPublication.Generation != 1 {
		t.Fatal("publication did not reuse preparation revision")
	}
	if complete, err := coordinator.ReconcilePublicationStatus(ctx, key, "attempt"); err != nil || complete {
		t.Fatalf("placeholder counted as publication: %v %v", complete, err)
	}
	store.children[0].Status.EndpointPublication = &platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{AttemptID: "attempt", IntentGeneration: 1, ObservedDatabaseGeneration: 1, GreenClusterUID: "green-uid", Databases: []platformv1alpha1.DatabaseEndpointAcknowledgementStatus{{DatabaseName: "orders", EndpointFingerprint: fingerprint, ConfigMapUID: "db-cm-uid", AcknowledgedAt: &now}}}
	dbCM := store.configMaps["tenant-orders-config"]
	dbCM.Data = proof.Green
	dbCM.Annotations = map[string]string{pgconninfo.AnnotationActiveEnvironment: "green", pgconninfo.AnnotationEndpointAttemptID: "attempt", pgconninfo.AnnotationEndpointIntentGeneration: "1", pgconninfo.AnnotationEndpointFingerprint: fingerprint}
	if complete, err := coordinator.ReconcilePublicationStatus(ctx, key, "attempt"); err != nil || !complete {
		t.Fatalf("valid publication pending: %v %v", complete, err)
	}
	store.configMapReadErr = errors.New("API read unavailable")
	if complete, err := coordinator.ReconcilePublicationStatus(ctx, key, "attempt"); err == nil || complete {
		t.Fatalf("canonical API read failure was hidden: complete=%v err=%v", complete, err)
	}
	store.configMapReadErr = nil
	originalRW := canonical.Data[pgconninfo.KeyClusterRWEndpoint]
	canonical.Data[pgconninfo.KeyClusterRWEndpoint] = "different-rw"
	if complete, err := coordinator.ReconcilePublicationStatus(ctx, key, "attempt"); err != nil || complete {
		t.Fatalf("canonical routing drift passed: %v %v", complete, err)
	}
	canonical.Data[pgconninfo.KeyClusterRWEndpoint] = originalRW
	dbCM.UID = "recreated"
	if complete, err := coordinator.ReconcilePublicationStatus(ctx, key, "attempt"); err != nil || complete {
		t.Fatalf("recreated ConfigMap passed: %v %v", complete, err)
	}
	if currentBG(store).EndpointPublication.Acknowledgements[0].AcknowledgedAt != nil {
		t.Fatal("stale publication aggregate was not cleared")
	}
}

func TestCommittedRoutingRequiresOriginalCanonicalConfigMapUID(t *testing.T) {
	coordinator, store, key, proof := fixture(t)
	if err := coordinator.PublishPreparationIntent(context.Background(), key, "attempt"); err != nil {
		t.Fatal(err)
	}
	bg := currentBG(store)
	fingerprint := pgconninfo.FingerprintEndpointData(proof.Green)
	bg.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{
		Sequence:           1,
		CanonicalConfigMap: &corev1.ObjectReference{Name: "cluster-config", UID: "canonical-uid"},
		Target:             &corev1.ObjectReference{Name: "green", UID: "green-uid"},
	}
	store.parent.Status.ProvisionerRef = &corev1.ObjectReference{Name: "green", UID: "green-uid"}
	canonical := store.configMaps["cluster-config"]
	canonical.Data = proof.Green
	canonical.Annotations = map[string]string{
		pgconninfo.AnnotationActiveEnvironment:        "green",
		pgconninfo.AnnotationEndpointAttemptID:        "attempt",
		pgconninfo.AnnotationEndpointIntentGeneration: "1",
		pgconninfo.AnnotationEndpointFingerprint:      fingerprint,
	}
	if _, err := coordinator.committedRouting(t.Context(), store.parent, bg, 1, fingerprint); err != nil {
		t.Fatalf("valid canonical proof rejected: %v", err)
	}
	canonical.UID = "recreated-canonical-uid"
	if _, err := coordinator.committedRouting(t.Context(), store.parent, bg, 1, fingerprint); !errors.Is(err, ErrCanonicalMismatch) {
		t.Fatalf("recreated canonical ConfigMap passed parent publication barrier: %v", err)
	}
}

func TestCommitCanonicalMakesConfigMapTheCommitPoint(t *testing.T) {
	ctx := context.Background()
	coordinator, store, key, proof := fixture(t)
	if err := coordinator.PublishPreparationIntent(ctx, key, "attempt"); err != nil {
		t.Fatal(err)
	}
	fingerprint := pgconninfo.FingerprintEndpointData(proof.Green)
	now := metav1.Now()
	store.children[0].Status.EndpointPreparation = &platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{
		AttemptID: "attempt", IntentGeneration: 1, ObservedDatabaseGeneration: 1, GreenClusterUID: "green-uid",
		Databases: []platformv1alpha1.DatabaseEndpointAcknowledgementStatus{{DatabaseName: "orders", EndpointFingerprint: fingerprint, AcknowledgedAt: &now}},
	}
	if err := coordinator.CommitCanonical(ctx, key, "attempt"); err != nil {
		t.Fatal(err)
	}
	canonical := store.configMaps["cluster-config"]
	if !pgconninfo.EndpointDataMatches(canonical.Data, proof.Green) || !pgendpointprotocol.PublicationStampMatches(canonical, currentBG(store), 1, fingerprint) {
		t.Fatalf("canonical ConfigMap did not prove the green commit: %+v", canonical)
	}
	bg := currentBG(store)
	if bg.CommitReceipt == nil || bg.CommitReceipt.Sequence != 1 || bg.Switchover == nil || bg.Switchover.EndpointCommittedAt == nil {
		t.Fatalf("commit receipt was not persisted: %+v", bg)
	}
	if store.parent.Status.ProvisionerRef == nil || store.parent.Status.ProvisionerRef.Name != "green" || store.parent.Status.ProvisionerRef.UID != "green-uid" {
		t.Fatalf("green provider authority was not recorded atomically with receipt: %+v", store.parent.Status.ProvisionerRef)
	}
	if err := coordinator.CommitCanonical(ctx, key, "attempt"); err != nil {
		t.Fatalf("idempotent commit retry failed: %v", err)
	}
	if currentBG(store).CommitReceipt.Sequence != 1 {
		t.Fatalf("idempotent retry advanced receipt sequence: %+v", currentBG(store).CommitReceipt)
	}
}

func TestPreparationStillRequiresLiveGreenProof(t *testing.T) {
	coordinator, store, key, proof := fixture(t)
	coordinator.proof = fixedProof{proof: proof, failGreen: true}
	if err := coordinator.PublishPreparationIntent(t.Context(), key, "attempt"); err == nil {
		t.Fatal("preparation accepted unavailable green endpoints")
	}
	if currentBG(store).EndpointPreparation != nil {
		t.Fatal("failed preparation persisted an intent")
	}
}

func TestInventoryAndCancellationRevision(t *testing.T) {
	ctx := t.Context()
	coordinator, store, key, proof := fixture(t)
	store.children = append(store.children, platformv1alpha1.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "new", Namespace: "ns", UID: "new-uid", Generation: 1}, Spec: platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "cluster"}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "extra"}}}})
	if err := coordinator.PublishPreparationIntent(ctx, key, "attempt"); !errors.Is(err, ErrIncompleteInventory) {
		t.Fatalf("added child did not block intent: %v", err)
	}
	store.children = store.children[:1]
	if err := coordinator.PublishPreparationIntent(ctx, key, "attempt"); err != nil {
		t.Fatal(err)
	}
	coordinator.proof = fixedProof{proof: proof, failGreen: true}
	if err := coordinator.CancelPreparation(ctx, key, "attempt", 1); err != nil {
		t.Fatal(err)
	}
	coordinator.proof = fixedProof{proof: proof}
	if currentBG(store).EndpointPreparation != nil || currentBG(store).EndpointIntentRevision != 1 {
		t.Fatal("cancellation lost monotonic revision")
	}
	if err := coordinator.PublishPreparationIntent(ctx, key, "attempt"); err != nil {
		t.Fatal(err)
	}
	if currentBG(store).EndpointPreparation.Generation != 2 {
		t.Fatal("republication reused cancelled revision")
	}
	if err := coordinator.CancelPreparation(ctx, key, "attempt", 1); !errors.Is(err, ErrStaleAttempt) {
		t.Fatalf("old cancellation removed newer intent: %v", err)
	}
	if err := coordinator.PublishPreparationIntent(ctx, key, "other"); !errors.Is(err, ErrStaleAttempt) {
		t.Fatalf("stale attempt wrote status: %v", err)
	}
}

func TestPreparationIntentRejectsAlreadyCommittedCanonicalMap(t *testing.T) {
	ctx := t.Context()
	coordinator, store, key, _ := fixture(t)
	store.configMaps["cluster-config"].Annotations = map[string]string{pgconninfo.AnnotationActiveEnvironment: "green"}
	if err := coordinator.PublishPreparationIntent(ctx, key, "attempt"); !errors.Is(err, ErrCanonicalMismatch) {
		t.Fatalf("green canonical ConfigMap passed preparation without receipt: %v", err)
	}
	if currentBG(store).EndpointPreparation != nil {
		t.Fatal("preparation intent was persisted after externally observable commit")
	}
}

func TestCancelPreparationRejectsMissingCanonicalConfigMap(t *testing.T) {
	ctx := t.Context()
	coordinator, store, key, _ := fixture(t)
	if err := coordinator.PublishPreparationIntent(ctx, key, "attempt"); err != nil {
		t.Fatal(err)
	}
	delete(store.configMaps, "cluster-config")
	if err := coordinator.CancelPreparation(ctx, key, "attempt", 1); !apierrors.IsNotFound(err) {
		t.Fatalf("missing canonical ConfigMap was accepted as precommit proof: %v", err)
	}
	if currentBG(store).EndpointPreparation == nil {
		t.Fatal("cancellation cleared intent without proving canonical blue")
	}
}

func TestPreparationInventoryDrift(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*memoryStore)
	}{
		{"delete", func(s *memoryStore) { s.children = nil }},
		{"deleting", func(s *memoryStore) { now := metav1.Now(); s.children[0].DeletionTimestamp = &now }},
		{"generation", func(s *memoryStore) { s.children[0].Generation++ }},
		{"clusterRef move", func(s *memoryStore) { s.children[0].Spec.ClusterRef.Name = "other" }},
		{"extra database", func(s *memoryStore) {
			s.children[0].Spec.Databases = append(s.children[0].Spec.Databases, platformv1alpha1.DatabaseDefinition{Name: "extra"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			coordinator, store, key, _ := fixture(t)
			if err := coordinator.PublishPreparationIntent(t.Context(), key, "attempt"); err != nil {
				t.Fatal(err)
			}
			tc.mutate(store)
			complete, err := coordinator.PreparationComplete(t.Context(), key, "attempt")
			if err != nil || complete {
				t.Fatalf("inventory drift passed: complete=%v err=%v", complete, err)
			}
		})
	}
}

func TestPartialMultiChildPreparationRemainsPending(t *testing.T) {
	ctx := t.Context()
	coordinator, store, key, proof := fixture(t)
	second := *store.children[0].DeepCopy()
	second.Name, second.UID = "tenant-two", "child-two-uid"
	store.children = append(store.children, second)
	bg := currentBG(store)
	bg.DatabaseReplication = append(bg.DatabaseReplication, platformv1alpha1.BlueGreenDatabaseReplicationStatus{PostgresDatabaseName: second.Name, PostgresDatabaseUID: string(second.UID), DatabaseName: "orders"})
	secondCM := store.configMaps["tenant-orders-config"].DeepCopy()
	secondCM.Name, secondCM.UID = "tenant-two-orders-config", "second-cm-uid"
	secondCM.OwnerReferences[0].Name, secondCM.OwnerReferences[0].UID = second.Name, second.UID
	store.configMaps[secondCM.Name] = secondCM
	if err := coordinator.PublishPreparationIntent(ctx, key, "attempt"); err != nil {
		t.Fatal(err)
	}
	fingerprint := pgconninfo.FingerprintEndpointData(proof.Green)
	now := metav1.Now()
	store.children[0].Status.EndpointPreparation = &platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{AttemptID: "attempt", IntentGeneration: 1, ObservedDatabaseGeneration: 1, GreenClusterUID: "green-uid", Databases: []platformv1alpha1.DatabaseEndpointAcknowledgementStatus{{DatabaseName: "orders", EndpointFingerprint: fingerprint, AcknowledgedAt: &now}}}
	complete, err := coordinator.PreparationComplete(ctx, key, "attempt")
	if err != nil || complete {
		t.Fatalf("one of two children passed: complete=%v err=%v", complete, err)
	}
	acks := currentBG(store).EndpointPreparation.Acknowledgements
	if acks[0].ValidatedAt == nil || acks[1].ValidatedAt != nil {
		t.Fatalf("unexpected partial aggregation: %+v", acks)
	}
	store.children[1].Status.EndpointPreparation = store.children[0].Status.EndpointPreparation.DeepCopy()
	complete, err = coordinator.PreparationComplete(ctx, key, "attempt")
	if err != nil || !complete {
		t.Fatalf("both children did not pass: complete=%v err=%v", complete, err)
	}
}

func TestCanonicalCommitMismatchBlocksPublication(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*memoryStore)
	}{
		{"routing data", func(s *memoryStore) { s.configMaps["cluster-config"].Data[pgconninfo.KeyDefaultClusterPort] = "6432" }},
		{"attempt annotation", func(s *memoryStore) {
			s.configMaps["cluster-config"].Annotations[pgconninfo.AnnotationEndpointAttemptID] = "stale"
		}},
		{"generation annotation", func(s *memoryStore) {
			s.configMaps["cluster-config"].Annotations[pgconninfo.AnnotationEndpointIntentGeneration] = "2"
		}},
		{"fingerprint annotation", func(s *memoryStore) {
			s.configMaps["cluster-config"].Annotations[pgconninfo.AnnotationEndpointFingerprint] = "wrong"
		}},
		{"ConfigMap UID", func(s *memoryStore) { s.configMaps["cluster-config"].UID = "recreated" }},
		{"receipt target UID", func(s *memoryStore) { currentBG(s).CommitReceipt.Target.UID = "wrong-target" }},
		{"parent provider UID", func(s *memoryStore) { s.parent.Status.ProvisionerRef.UID = "wrong-provider" }},
		{"parent provider name", func(s *memoryStore) { s.parent.Status.ProvisionerRef.Name = "wrong-provider" }},
		{"empty canonical name", func(s *memoryStore) {
			currentBG(s).CommitReceipt.CanonicalConfigMap.Name = ""
			s.parent.Status.Resources.ConfigMapRef.Name = ""
		}},
		{"empty shared candidate UID", func(s *memoryStore) {
			currentBG(s).Green.Ref.UID = ""
			currentBG(s).CommitReceipt.Target.UID = ""
			s.parent.Status.ProvisionerRef.UID = ""
		}},
		{"empty shared candidate name", func(s *memoryStore) {
			currentBG(s).Green.Ref.Name = ""
			currentBG(s).CommitReceipt.Target.Name = ""
			s.parent.Status.ProvisionerRef.Name = ""
			s.configMaps["cluster-config"].Annotations[pgconninfo.AnnotationActiveEnvironment] = ""
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			coordinator, store, key, proof := fixture(t)
			if err := coordinator.PublishPreparationIntent(ctx, key, "attempt"); err != nil {
				t.Fatal(err)
			}
			fingerprint := pgconninfo.FingerprintEndpointData(proof.Green)
			now := metav1.Now()
			store.children[0].Status.EndpointPreparation = &platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{AttemptID: "attempt", IntentGeneration: 1, ObservedDatabaseGeneration: 1, GreenClusterUID: "green-uid", Databases: []platformv1alpha1.DatabaseEndpointAcknowledgementStatus{{DatabaseName: "orders", EndpointFingerprint: fingerprint, AcknowledgedAt: &now}}}
			complete, err := coordinator.PreparationComplete(ctx, key, "attempt")
			if err != nil || !complete {
				t.Fatalf("preparation failed: %v %v", complete, err)
			}
			bg := currentBG(store)
			bg.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CanonicalConfigMap: &corev1.ObjectReference{Name: "cluster-config", UID: "canonical-uid"}, Target: &corev1.ObjectReference{Name: "green", UID: "green-uid"}}
			store.parent.Status.ProvisionerRef = &corev1.ObjectReference{Name: "green", UID: "green-uid"}
			canonical := store.configMaps["cluster-config"]
			canonical.Data = map[string]string{}
			maps.Copy(canonical.Data, proof.Green)
			canonical.Annotations = map[string]string{pgconninfo.AnnotationActiveEnvironment: "green", pgconninfo.AnnotationEndpointAttemptID: "attempt", pgconninfo.AnnotationEndpointIntentGeneration: "1", pgconninfo.AnnotationEndpointFingerprint: fingerprint}
			coordinator.proof = fixedProof{proof: proof, failBlue: true, failGreen: true}
			tc.mutate(store)
			if err := coordinator.PublishPublicationIntent(ctx, key, "attempt"); !errors.Is(err, ErrCanonicalMismatch) {
				t.Fatalf("bad canonical record passed: %v", err)
			}
		})
	}
}

func TestPreparationBarrierClearsStaleProofAndRejectsUnownedConfigMap(t *testing.T) {
	ctx := t.Context()
	coordinator, store, key, proof := fixture(t)
	if err := coordinator.PublishPreparationIntent(ctx, key, "attempt"); err != nil {
		t.Fatal(err)
	}
	fingerprint := pgconninfo.FingerprintEndpointData(proof.Green)
	now := metav1.Now()
	store.children[0].Status.EndpointPreparation = &platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{
		AttemptID: "attempt", IntentGeneration: 1, ObservedDatabaseGeneration: 1, GreenClusterUID: "green-uid",
		Databases: []platformv1alpha1.DatabaseEndpointAcknowledgementStatus{{DatabaseName: "orders", EndpointFingerprint: fingerprint, AcknowledgedAt: &now}},
	}
	if complete, err := coordinator.PreparationComplete(ctx, key, "attempt"); err != nil || !complete {
		t.Fatalf("valid blue preparation was not accepted: complete=%v err=%v", complete, err)
	}
	if currentBG(store).EndpointPreparation.Acknowledgements[0].ValidatedAt == nil {
		t.Fatal("expected aggregated preparation proof")
	}

	// The child can retract its acknowledgement before the canonical commit.
	store.children[0].Status.EndpointPreparation.Databases[0].DatabaseName = "other"
	if complete, err := coordinator.PreparationComplete(ctx, key, "attempt"); err != nil || complete {
		t.Fatalf("proof for a different database passed: complete=%v err=%v", complete, err)
	}
	if currentBG(store).EndpointPreparation.Acknowledgements[0].ValidatedAt != nil {
		t.Fatal("stale preparation aggregate was retained")
	}
	store.children[0].Status.EndpointPreparation.Databases[0].DatabaseName = "orders"

	// An endpoint map that disappears or changes owner cannot prove blue routing.
	databaseMap := store.configMaps["tenant-orders-config"]
	delete(store.configMaps, databaseMap.Name)
	if complete, err := coordinator.PreparationComplete(ctx, key, "attempt"); err != nil || complete {
		t.Fatalf("missing owned endpoint map passed: complete=%v err=%v", complete, err)
	}
	store.configMaps[databaseMap.Name] = databaseMap
	databaseMap.OwnerReferences = nil
	if _, err := coordinator.PreparationComplete(ctx, key, "attempt"); err == nil {
		t.Fatal("unowned endpoint map passed without an error")
	}
}

func TestPreparationBarrierRequiresCurrentEndpointProof(t *testing.T) {
	ctx := t.Context()
	coordinator, store, key, proof := fixture(t)
	if err := coordinator.PublishPreparationIntent(ctx, key, "attempt"); err != nil {
		t.Fatal(err)
	}
	fingerprint := pgconninfo.FingerprintEndpointData(proof.Green)
	now := metav1.Now()
	store.children[0].Status.EndpointPreparation = &platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{
		AttemptID: "attempt", IntentGeneration: 1, ObservedDatabaseGeneration: 1, GreenClusterUID: "green-uid",
		Databases: []platformv1alpha1.DatabaseEndpointAcknowledgementStatus{{DatabaseName: "orders", EndpointFingerprint: fingerprint, AcknowledgedAt: &now}},
	}
	changed := proof
	changed.Green = map[string]string{}
	maps.Copy(changed.Green, proof.Green)
	changed.Green[pgconninfo.KeyClusterRWEndpoint] = "new-green-rw"
	coordinator.proof = fixedProof{proof: changed}
	if complete, err := coordinator.PreparationComplete(ctx, key, "attempt"); err != nil || complete {
		t.Fatalf("drifted green routing passed: complete=%v err=%v", complete, err)
	}
	coordinator.proof = fixedProof{proof: proof, failGreen: true}
	if complete, err := coordinator.PreparationComplete(ctx, key, "attempt"); err == nil || complete {
		t.Fatalf("unavailable live green endpoint passed: complete=%v err=%v", complete, err)
	}
}

func TestFrozenInventoryRejectsMalformedOrMissingChildren(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*memoryStore)
	}{
		{"missing preflight inventory", func(s *memoryStore) { currentBG(s).DatabaseReplication = nil }},
		{"missing database identity", func(s *memoryStore) { currentBG(s).DatabaseReplication[0].DatabaseName = "" }},
		{"duplicate frozen database", func(s *memoryStore) {
			bg := currentBG(s)
			bg.DatabaseReplication = append(bg.DatabaseReplication, bg.DatabaseReplication[0])
		}},
		{"child without UID", func(s *memoryStore) {
			s.children[0].UID = ""
		}},
		{"child without databases", func(s *memoryStore) { s.children[0].Spec.Databases = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			coordinator, store, key, _ := fixture(t)
			tc.mutate(store)
			if err := coordinator.PublishPreparationIntent(t.Context(), key, "attempt"); !errors.Is(err, ErrIncompleteInventory) {
				t.Fatalf("invalid frozen inventory was accepted: %v", err)
			}
			if currentBG(store).EndpointPreparation != nil {
				t.Fatal("intent persisted despite invalid inventory")
			}
		})
	}
}

func TestPublicationBarrierClearsStaleProofAndRejectsUnownedConfigMap(t *testing.T) {
	ctx := t.Context()
	coordinator, store, key, proof := fixture(t)
	if err := coordinator.PublishPreparationIntent(ctx, key, "attempt"); err != nil {
		t.Fatal(err)
	}
	fingerprint := pgconninfo.FingerprintEndpointData(proof.Green)
	now := metav1.Now()
	store.children[0].Status.EndpointPreparation = &platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{
		AttemptID: "attempt", IntentGeneration: 1, ObservedDatabaseGeneration: 1, GreenClusterUID: "green-uid",
		Databases: []platformv1alpha1.DatabaseEndpointAcknowledgementStatus{{DatabaseName: "orders", EndpointFingerprint: fingerprint, AcknowledgedAt: &now}},
	}
	if complete, err := coordinator.PreparationComplete(ctx, key, "attempt"); err != nil || !complete {
		t.Fatalf("preparation failed: complete=%v err=%v", complete, err)
	}
	bg := currentBG(store)
	bg.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CanonicalConfigMap: &corev1.ObjectReference{Name: "cluster-config", UID: "canonical-uid"}, Target: &corev1.ObjectReference{Name: "green", UID: "green-uid"}}
	store.parent.Status.ProvisionerRef = &corev1.ObjectReference{Name: "green", UID: "green-uid"}
	canonical := store.configMaps["cluster-config"]
	canonical.Data = proof.Green
	canonical.Annotations = map[string]string{pgconninfo.AnnotationActiveEnvironment: "green", pgconninfo.AnnotationEndpointAttemptID: "attempt", pgconninfo.AnnotationEndpointIntentGeneration: "1", pgconninfo.AnnotationEndpointFingerprint: fingerprint}
	if err := coordinator.PublishPublicationIntent(ctx, key, "attempt"); err != nil {
		t.Fatal(err)
	}
	store.children[0].Status.EndpointPublication = &platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{
		AttemptID: "attempt", IntentGeneration: 1, ObservedDatabaseGeneration: 1, GreenClusterUID: "green-uid",
		Databases: []platformv1alpha1.DatabaseEndpointAcknowledgementStatus{{DatabaseName: "orders", EndpointFingerprint: fingerprint, ConfigMapUID: "db-cm-uid", AcknowledgedAt: &now}},
	}
	databaseMap := store.configMaps["tenant-orders-config"]
	databaseMap.Data = proof.Green
	databaseMap.Annotations = map[string]string{pgconninfo.AnnotationActiveEnvironment: "green", pgconninfo.AnnotationEndpointAttemptID: "attempt", pgconninfo.AnnotationEndpointIntentGeneration: "1", pgconninfo.AnnotationEndpointFingerprint: fingerprint}
	if complete, err := coordinator.ReconcilePublicationStatus(ctx, key, "attempt"); err != nil || !complete {
		t.Fatalf("valid publication failed: complete=%v err=%v", complete, err)
	}

	store.children[0].Status.EndpointPublication.Databases[0].DatabaseName = "other"
	if complete, err := coordinator.ReconcilePublicationStatus(ctx, key, "attempt"); err != nil || complete {
		t.Fatalf("proof for a different database passed: complete=%v err=%v", complete, err)
	}
	if currentBG(store).EndpointPublication.Acknowledgements[0].AcknowledgedAt != nil {
		t.Fatal("stale publication aggregate was retained")
	}
	store.children[0].Status.EndpointPublication.Databases[0].DatabaseName = "orders"
	delete(store.configMaps, databaseMap.Name)
	if complete, err := coordinator.ReconcilePublicationStatus(ctx, key, "attempt"); err != nil || complete {
		t.Fatalf("missing child endpoint map passed: complete=%v err=%v", complete, err)
	}
	store.configMaps[databaseMap.Name] = databaseMap
	databaseMap.OwnerReferences = nil
	if _, err := coordinator.ReconcilePublicationStatus(ctx, key, "attempt"); err == nil {
		t.Fatal("unowned child endpoint map passed without an error")
	}
}

func preparePublicationBarrierTest(t *testing.T, coordinator *Coordinator, store *memoryStore, key types.NamespacedName, proof Proof) (string, metav1.Time) {
	t.Helper()
	ctx := t.Context()
	if err := coordinator.PublishPreparationIntent(ctx, key, "attempt"); err != nil {
		t.Fatal(err)
	}
	fingerprint := pgconninfo.FingerprintEndpointData(proof.Green)
	now := metav1.Now()
	store.children[0].Status.EndpointPreparation = &platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{AttemptID: "attempt", IntentGeneration: 1, ObservedDatabaseGeneration: 1, GreenClusterUID: "green-uid", Databases: []platformv1alpha1.DatabaseEndpointAcknowledgementStatus{{DatabaseName: "orders", EndpointFingerprint: fingerprint, AcknowledgedAt: &now}}}
	if complete, err := coordinator.PreparationComplete(ctx, key, "attempt"); err != nil || !complete {
		t.Fatalf("preparation failed: %v %v", complete, err)
	}
	if err := coordinator.PublishPublicationIntent(ctx, key, "attempt"); err == nil {
		t.Fatal("publication without canonical commit")
	}
	return fingerprint, now
}

func newTestCoordinator(store Store, proof ProofResolver) (*Coordinator, error) {
	return NewCoordinator(store, proof, func(_ context.Context, _ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, now time.Time) error {
		return mvutypes.AuthorizeCutoverCommit(bg, now)
	}, time.Now)
}
