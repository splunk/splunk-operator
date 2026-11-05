package core

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	dbclusterinfo "github.com/splunk/splunk-operator/pkg/postgresql/database/ports/clusterinfo"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestEndpointProtocolPreparesBlueThenPublishesAfterCanonicalCommit(t *testing.T) {
	ctx := t.Context()
	scheme := testScheme(t)
	child := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "ns", UID: "db-uid", Generation: 3},
		Spec: platformv1alpha1.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "postgres"},
			Databases:  []platformv1alpha1.DatabaseDefinition{{Name: "orders"}, {Name: "ledger"}},
		},
	}
	blue := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "blue", Namespace: "ns", UID: "blue-uid"}, Status: cnpgv1.ClusterStatus{WriteService: "blue-rw", ReadyInstances: 1}}
	green := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "green", Namespace: "ns", UID: "green-uid"}, Spec: cnpgv1.ClusterSpec{Instances: 1}, Status: cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy, WriteService: "green-rw", ReadyInstances: 1}}
	greenRouting := map[string]string{
		pgconninfo.KeyClusterRWEndpoint:  "green-rw.ns.svc.cluster.local",
		pgconninfo.KeyClusterROEndpoint:  "",
		pgconninfo.KeyClusterREndpoint:   "green-r.ns.svc.cluster.local",
		pgconninfo.KeyDefaultClusterPort: pgconninfo.DefaultPort,
		pgconninfo.KeyPoolerRWEndpoint:   "green-pooler-rw.ns.svc.cluster.local",
		pgconninfo.KeyPoolerROEndpoint:   "",
	}
	fingerprint := pgconninfo.FingerprintEndpointData(greenRouting)
	bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		AttemptID:              "attempt-1",
		EndpointIntentRevision: 7,
		Blue:                   &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "blue", UID: "blue-uid"}},
		Green:                  &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}},
		CandidateEndpoints:     &platformv1alpha1.BlueGreenCandidateEndpointsStatus{DirectService: &corev1.ObjectReference{Name: "green-rw", Namespace: "ns", UID: "green-rw-uid"}, PoolerService: &corev1.ObjectReference{Name: "green-pooler-rw", Namespace: "ns", UID: "pooler-service-uid"}},
		EndpointPreparation: &platformv1alpha1.BlueGreenEndpointPreparationStatus{
			Generation: 7, EndpointFingerprint: &fingerprint,
			Acknowledgements: []platformv1alpha1.BlueGreenEndpointPreparationAcknowledgementStatus{
				{PostgresDatabaseName: "orders", PostgresDatabaseUID: "db-uid", PostgresDatabaseGeneration: 3, DatabaseName: "orders"},
				{PostgresDatabaseName: "orders", PostgresDatabaseUID: "db-uid", PostgresDatabaseGeneration: 3, DatabaseName: "ledger"},
			},
		},
	}
	parent := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: "ns", UID: "parent-uid"},
		Status: platformv1alpha1.PostgresClusterStatus{
			ProvisionerRef:             &corev1.ObjectReference{Name: "blue", UID: "blue-uid"},
			ConnectionPoolerStatus:     &platformv1alpha1.ConnectionPoolerStatus{Enabled: true, ReadWriteEnabled: true},
			Resources:                  &platformv1alpha1.PostgresClusterResources{ConfigMapRef: &corev1.LocalObjectReference{Name: "postgres-connection"}},
			PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: bg}},
		},
	}
	configuring := "Configuring"
	parent.Status.Phase = &configuring
	canonical := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "postgres-connection", Namespace: "ns", UID: "canonical-uid"}}
	objects := []client.Object{child, parent, blue, green, canonical}
	for _, name := range []string{"green-rw", "green-r"} {
		uid := types.UID(name + "-uid")
		selector := map[string]string{"cnpg.io/cluster": "green"}
		if name == "green-rw" {
			selector["cnpg.io/instanceRole"] = "primary"
		} else {
			selector["cnpg.io/podRole"] = "instance"
		}
		objects = append(objects,
			&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", UID: uid}, Spec: corev1.ServiceSpec{Selector: selector, Ports: []corev1.ServicePort{{Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(5432)}}}},
		)
	}
	pooler := &cnpgv1.Pooler{ObjectMeta: metav1.ObjectMeta{Name: "green-pooler-rw", Namespace: "ns", UID: "pooler-uid"},
		Spec: cnpgv1.PoolerSpec{Cluster: cnpgv1.LocalObjectReference{Name: "green"}, Type: cnpgv1.PoolerTypeRW}, Status: cnpgv1.PoolerStatus{Phase: cnpgv1.PoolerPhaseActive, Instances: 1}}
	poolerOwner := []metav1.OwnerReference{*metav1.NewControllerRef(pooler, cnpgv1.SchemeGroupVersion.WithKind("Pooler"))}
	poolerSelector := map[string]string{"cnpg.io/poolerName": pooler.Name}
	poolerService := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: pooler.Name, Namespace: "ns", UID: "pooler-service-uid", OwnerReferences: poolerOwner},
		Spec: corev1.ServiceSpec{Selector: poolerSelector, Ports: []corev1.ServicePort{{Name: "pgbouncer", Port: 5432, TargetPort: intstr.FromString("pgbouncer"), Protocol: corev1.ProtocolTCP}}}}
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: pooler.Name, Namespace: "ns", UID: "deployment-uid", Generation: 1, OwnerReferences: poolerOwner},
		Spec: appsv1.DeploymentSpec{Replicas: new(int32(1)), Selector: &metav1.LabelSelector{MatchLabels: poolerSelector},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: poolerSelector}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "pgbouncer", Ports: []corev1.ContainerPort{{Name: "pgbouncer", ContainerPort: 5432}}}}}}},
		Status: appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1}}
	objects = append(objects, pooler, poolerService, deployment)
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&platformv1alpha1.PostgresDatabase{}, &platformv1alpha1.PostgresCluster{}).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, next client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if cm, ok := obj.(*corev1.ConfigMap); ok {
				cm.UID = types.UID(cm.Name + "-uid")
			}
			return next.Create(ctx, obj, opts...)
		},
	}).Build()
	providerSeenByOrdinaryReader := "green"
	rc := &ReconcileContext{Client: c, APIReader: c, Scheme: scheme,
		ClusterReader: clusterReadinessReaderFunc(func(context.Context, string, string) (dbclusterinfo.ResolvedClusterFacts, error) {
			card := testResolvedClusterCard(parent.Name, parent.Namespace, providerSeenByOrdinaryReader)
			if providerSeenByOrdinaryReader == "green" {
				card.Authoritative.Identity.UID = green.UID
			} else {
				card.Authoritative.Identity.UID = blue.UID
			}
			return dbclusterinfo.ResolvedClusterFacts{Cluster: card, Lifecycle: dbclusterinfo.LifecycleReady, ConnectionPoolerStatus: parent.Status.ConnectionPoolerStatus}, nil
		}),
	}
	handledPending, _, pendingErr := reconcileEndpointProtocol(ctx, rc, child)
	require.True(t, handledPending)
	require.ErrorContains(t, pendingErr, "Deployment is not ready")
	require.True(t, apierrors.IsNotFound(c.Get(ctx, types.NamespacedName{Namespace: "ns", Name: configMapName("orders", "orders")}, &corev1.ConfigMap{})))
	pendingChild := &platformv1alpha1.PostgresDatabase{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(child), pendingChild))
	require.Nil(t, pendingChild.Status.EndpointPreparation)
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(deployment), deployment))
	deployment.Status.ReadyReplicas = 1
	deployment.Status.AvailableReplicas = 1
	require.NoError(t, c.Status().Update(ctx, deployment))
	staleParent := &platformv1alpha1.PostgresCluster{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(parent), staleParent))
	currentEndpointAttempt(staleParent).EndpointIntentRevision = 8
	require.NoError(t, c.Status().Update(ctx, staleParent))
	staleHandled, _, staleErr := reconcileEndpointProtocol(ctx, rc, child)
	require.True(t, staleHandled)
	require.ErrorContains(t, staleErr, "incomplete or stale")
	require.True(t, apierrors.IsNotFound(c.Get(ctx, types.NamespacedName{Namespace: "ns", Name: configMapName("orders", "orders")}, &corev1.ConfigMap{})))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(parent), staleParent))
	currentEndpointAttempt(staleParent).EndpointIntentRevision = 7
	require.NoError(t, c.Status().Update(ctx, staleParent))

	handled, _, err := reconcileEndpointProtocol(ctx, rc, child)
	require.True(t, handled)
	require.NoError(t, err)
	blueCM := &corev1.ConfigMap{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: "ns", Name: configMapName("orders", "orders")}, blueCM))
	require.Equal(t, "blue-rw.ns.svc.cluster.local", blueCM.Data[pgconninfo.KeyClusterRWEndpoint])
	require.Empty(t, blueCM.Annotations[pgconninfo.AnnotationEndpointAttemptID])
	currentChild := &platformv1alpha1.PostgresDatabase{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(child), currentChild))
	require.NotNil(t, currentChild.Status.EndpointPreparation)
	require.Nil(t, currentChild.Status.EndpointPublication)
	require.Equal(t, fingerprint, currentChild.Status.EndpointPreparation.Databases[0].EndpointFingerprint)
	require.Len(t, currentChild.Status.EndpointPreparation.Databases, 2)

	// A publication intent without canonical data and receipt cannot write green.
	currentParent := &platformv1alpha1.PostgresCluster{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(parent), currentParent))
	currentBG := currentEndpointAttempt(currentParent)
	currentBG.EndpointPublication = &platformv1alpha1.BlueGreenEndpointPublicationStatus{Generation: 7, EndpointFingerprint: &fingerprint, Acknowledgements: []platformv1alpha1.BlueGreenEndpointPublicationAcknowledgementStatus{
		{PostgresDatabaseName: "orders", PostgresDatabaseUID: "db-uid", PostgresDatabaseGeneration: 3, DatabaseName: "orders"},
		{PostgresDatabaseName: "orders", PostgresDatabaseUID: "db-uid", PostgresDatabaseGeneration: 3, DatabaseName: "ledger"},
	}}
	require.NoError(t, c.Status().Update(ctx, currentParent))
	handled, _, err = reconcileEndpointProtocol(ctx, rc, currentChild)
	require.True(t, handled)
	require.ErrorContains(t, err, "canonical endpoint commit status is incomplete")
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(blueCM), blueCM))
	require.Equal(t, "blue-rw.ns.svc.cluster.local", blueCM.Data[pgconninfo.KeyClusterRWEndpoint])

	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(parent), currentParent))
	currentBG = currentEndpointAttempt(currentParent)
	currentBG.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{
		Sequence:           1,
		CanonicalConfigMap: &corev1.ObjectReference{Name: canonical.Name, UID: canonical.UID},
		Target:             &corev1.ObjectReference{Name: green.Name, UID: green.UID},
	}
	currentParent.Status.ProvisionerRef = &corev1.ObjectReference{Name: green.Name, UID: green.UID}
	require.NoError(t, c.Status().Update(ctx, currentParent))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(canonical), canonical))
	canonical.Data = greenRouting
	canonical.Annotations = map[string]string{
		pgconninfo.AnnotationActiveEnvironment:        green.Name,
		pgconninfo.AnnotationEndpointAttemptID:        bg.AttemptID,
		pgconninfo.AnnotationEndpointIntentGeneration: strconv.FormatInt(7, 10),
		pgconninfo.AnnotationEndpointFingerprint:      fingerprint,
	}
	require.NoError(t, c.Update(ctx, canonical))
	// A committed payload must still publish after its Pooler Deployment and
	// candidate Services disappear and green health is lost.
	require.NoError(t, c.Delete(ctx, deployment))
	require.NoError(t, c.Delete(ctx, poolerService))
	green.Status.Phase = cnpgv1.PhaseUnrecoverable
	green.Status.ReadyInstances = 0
	require.NoError(t, c.Update(ctx, green))
	for _, name := range []string{"green-rw", "green-r"} {
		service := &corev1.Service{}
		require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: "ns", Name: name}, service))
		require.NoError(t, c.Delete(ctx, service))
	}

	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(parent), currentParent))
	currentEndpointAttempt(currentParent).EndpointPublication.Acknowledgements = currentEndpointAttempt(currentParent).EndpointPublication.Acknowledgements[:1]
	require.NoError(t, c.Status().Update(ctx, currentParent))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(child), currentChild))
	handled, _, err = reconcileEndpointProtocol(ctx, rc, currentChild)
	require.True(t, handled)
	require.ErrorContains(t, err, "absent from endpoint publication inventory")
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(blueCM), blueCM))
	require.Equal(t, "blue-rw.ns.svc.cluster.local", blueCM.Data[pgconninfo.KeyClusterRWEndpoint])
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(parent), currentParent))
	currentEndpointAttempt(currentParent).EndpointPublication.Acknowledgements = []platformv1alpha1.BlueGreenEndpointPublicationAcknowledgementStatus{
		{PostgresDatabaseName: "orders", PostgresDatabaseUID: "db-uid", PostgresDatabaseGeneration: 3, DatabaseName: "orders"},
		{PostgresDatabaseName: "orders", PostgresDatabaseUID: "db-uid", PostgresDatabaseGeneration: 3, DatabaseName: "ledger"},
	}
	require.NoError(t, c.Status().Update(ctx, currentParent))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(canonical), canonical))
	canonical.Data[pgconninfo.KeyClusterRWEndpoint] = "uncommitted.ns.svc.cluster.local"
	require.NoError(t, c.Update(ctx, canonical))
	handled, _, err = reconcileEndpointProtocol(ctx, rc, currentChild)
	require.True(t, handled)
	require.ErrorContains(t, err, "does not prove the current green commit")
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(blueCM), blueCM))
	require.Equal(t, "blue-rw.ns.svc.cluster.local", blueCM.Data[pgconninfo.KeyClusterRWEndpoint])
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(canonical), canonical))
	canonical.Data[pgconninfo.KeyClusterRWEndpoint] = greenRouting[pgconninfo.KeyClusterRWEndpoint]
	require.NoError(t, c.Update(ctx, canonical))
	// First database can publish, but a foreign-owned second map prevents the
	// acknowledgement. Deleting that map lets the retry recreate it under the
	// child controller while the parent remains Configuring.
	ledgerCM := &corev1.ConfigMap{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: "ns", Name: configMapName("orders", "ledger")}, ledgerCM))
	ledgerCM.OwnerReferences[0].UID = "foreign-uid"
	require.NoError(t, c.Update(ctx, ledgerCM))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(child), currentChild))
	handled, _, err = reconcileEndpointProtocol(ctx, rc, currentChild)
	require.True(t, handled)
	require.ErrorContains(t, err, "not controlled")
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(blueCM), blueCM))
	require.True(t, pgconninfo.EndpointDataMatches(blueCM.Data, greenRouting))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(child), currentChild))
	require.Nil(t, currentChild.Status.EndpointPublication)
	require.NoError(t, c.Delete(ctx, ledgerCM))
	handled, _, err = reconcileEndpointProtocol(ctx, rc, currentChild)
	require.True(t, handled)
	require.NoError(t, err)
	require.Equal(t, bg.AttemptID, blueCM.Annotations[pgconninfo.AnnotationEndpointAttemptID])
	require.Equal(t, green.Name, blueCM.Annotations[pgconninfo.AnnotationActiveEnvironment])
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: "ns", Name: configMapName("orders", "ledger")}, ledgerCM))
	require.True(t, pgconninfo.EndpointDataMatches(ledgerCM.Data, greenRouting))
	require.Equal(t, green.Name, ledgerCM.Annotations[pgconninfo.AnnotationActiveEnvironment])
	require.True(t, metav1.IsControlledBy(ledgerCM, child))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(child), currentChild))
	require.NotNil(t, currentChild.Status.EndpointPublication)
	require.Len(t, currentChild.Status.EndpointPublication.Databases, 2)
	require.Equal(t, blueCM.UID, currentChild.Status.EndpointPublication.Databases[0].ConfigMapUID)
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(parent), currentParent))
	available := metav1.Now()
	currentEndpointAttempt(currentParent).Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{TargetAvailableAt: &available}
	require.NoError(t, c.Status().Update(ctx, currentParent))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(child), currentChild))
	currentChild.Status.EndpointPublication.Databases[0].ConfigMapUID = ""
	require.NoError(t, c.Status().Update(ctx, currentChild))
	handled, _, err = reconcileEndpointProtocol(ctx, rc, currentChild)
	require.True(t, handled, "publication acknowledgement without ConfigMap UID cannot release the writer")
	require.NoError(t, err)
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(child), currentChild))
	currentChild.Status.EndpointPublication.Databases[0].ConfigMapUID = blueCM.UID
	require.NoError(t, c.Status().Update(ctx, currentChild))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(child), currentChild))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(parent), currentParent))
	currentParent.Status.ProvisionerRef.Name = "wrong-name"
	require.NoError(t, c.Status().Update(ctx, currentParent))
	handled, _, err = reconcileEndpointProtocol(ctx, rc, currentChild)
	require.True(t, handled, "provider name must match receipt target even if UID matches")
	require.ErrorContains(t, err, "canonical endpoint commit status is incomplete")
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(parent), currentParent))
	currentParent.Status.ProvisionerRef.Name = green.Name
	require.NoError(t, c.Status().Update(ctx, currentParent))
	providerSeenByOrdinaryReader = "blue"
	handled, _, err = reconcileEndpointProtocol(ctx, rc, currentChild)
	require.True(t, handled, "a stale blue ordinary reader must remain fenced")
	require.NoError(t, err)
	providerSeenByOrdinaryReader = "green"
	handled, _, err = reconcileEndpointProtocol(ctx, rc, currentChild)
	require.False(t, handled, "verified publication should release the ordinary writer after TargetAvailable")
	require.NoError(t, err)
	// Ordinary reconciliation may now publish fresh green routing. Neither
	// canonical nor child routing must remain pinned to the cutover fingerprint.
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(canonical), canonical))
	canonical.Data[pgconninfo.KeyClusterRWEndpoint] = "green-rw-v2.ns.svc.cluster.local"
	delete(canonical.Annotations, pgconninfo.AnnotationEndpointFingerprint)
	require.NoError(t, c.Update(ctx, canonical))
	ledgerCM.Data[pgconninfo.KeyClusterRWEndpoint] = "green-rw-v2.ns.svc.cluster.local"
	require.NoError(t, c.Update(ctx, ledgerCM))
	_, err = verifyEndpointConfigMaps(ctx, c, currentChild, publicationOperation{endpointOperationIntent: endpointOperationIntent{routing: greenRouting, attemptID: bg.AttemptID, generation: 7, fingerprint: fingerprint}, greenEnvironment: green.Name})
	require.ErrorContains(t, err, "failed read-back verification")
	handled, _, err = reconcileEndpointProtocol(ctx, rc, currentChild)
	require.False(t, handled, "settled cutover must allow ordinary green routing changes")
	require.NoError(t, err)
}

func TestEndpointWriterRejectsForeignOwnerAndNewerGeneration(t *testing.T) {
	ctx := t.Context()
	scheme := testScheme(t)
	child := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "ns", UID: "db-uid", Generation: 1},
		Spec:       platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "postgres"}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "orders"}}},
	}
	spec := platformv1alpha1.DatabaseDefinition{Name: "orders"}
	key := types.NamespacedName{Namespace: "ns", Name: configMapName("orders", "orders")}
	routing := map[string]string{pgconninfo.KeyClusterRWEndpoint: "green-rw", pgconninfo.KeyClusterROEndpoint: "", pgconninfo.KeyClusterREndpoint: "green-r", pgconninfo.KeyDefaultClusterPort: "5432"}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace, UID: "cm-uid", OwnerReferences: []metav1.OwnerReference{{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresDatabase", Name: "other", UID: "other-uid", Controller: new(true)}}}}
	fingerprint := "fingerprint"
	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: "ns"}, Status: platformv1alpha1.PostgresClusterStatus{PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		AttemptID: "attempt", EndpointIntentRevision: 2, Green: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}},
		EndpointPreparation: &platformv1alpha1.BlueGreenEndpointPreparationStatus{Generation: 2, EndpointFingerprint: &fingerprint},
	}}}}}
	c := testClient(t, scheme, child, parent, cm)
	rc := &ReconcileContext{Client: c, APIReader: c, Scheme: scheme}
	publication := publicationOperation{endpointOperationIntent: endpointOperationIntent{routing: routing, attemptID: "attempt", generation: 2, fingerprint: fingerprint}, greenEnvironment: "green"}
	require.ErrorContains(t, writeEndpointConfigMap(ctx, rc, child, spec, publication), "not controlled")
	require.NoError(t, c.Get(ctx, key, cm))
	cm.OwnerReferences = []metav1.OwnerReference{{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresDatabase", Name: child.Name, UID: child.UID, Controller: new(true)}}
	cm.Annotations = map[string]string{pgconninfo.AnnotationEndpointAttemptID: "attempt", pgconninfo.AnnotationEndpointIntentGeneration: "3"}
	require.NoError(t, c.Update(ctx, cm))
	require.ErrorContains(t, writeEndpointConfigMap(ctx, rc, child, spec, publication), "newer publication")
	require.NoError(t, c.Get(ctx, key, cm))
	cm.Annotations[pgconninfo.AnnotationEndpointIntentGeneration] = "2"
	cm.Data = nil
	require.NoError(t, c.Update(ctx, cm))
	require.NoError(t, writeEndpointConfigMap(ctx, rc, child, spec, preparationOperation{endpointOperationIntent{routing: routing, attemptID: "attempt", generation: 2, fingerprint: fingerprint}}))
	require.NoError(t, c.Get(ctx, key, cm))
	require.True(t, pgconninfo.EndpointDataMatches(cm.Data, routing))
}

func TestEndpointWriterDelayedAttemptCannotOverwriteNewerAttempt(t *testing.T) {
	ctx := t.Context()
	scheme := testScheme(t)
	child := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "ns", UID: "db-uid", Generation: 1},
		Spec:       platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "postgres"}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "orders"}}},
	}
	parent := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: "ns"},
		Status:     platformv1alpha1.PostgresClusterStatus{PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt-B"}}}},
	}
	key := types.NamespacedName{Namespace: "ns", Name: configMapName("orders", "orders")}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: key.Name, Namespace: key.Namespace, UID: "cm-uid",
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresDatabase", Name: child.Name, UID: child.UID, Controller: new(true)}},
			Annotations:     map[string]string{pgconninfo.AnnotationEndpointAttemptID: "attempt-B", pgconninfo.AnnotationEndpointIntentGeneration: "1", pgconninfo.AnnotationEndpointFingerprint: "B"},
		},
		Data: map[string]string{pgconninfo.KeyClusterRWEndpoint: "green-B"},
	}
	c := testClient(t, scheme, child, parent, cm)
	rc := &ReconcileContext{Client: c, APIReader: c, Scheme: scheme}
	staleRouting := map[string]string{pgconninfo.KeyClusterRWEndpoint: "green-A", pgconninfo.KeyClusterROEndpoint: "", pgconninfo.KeyClusterREndpoint: "green-A-r", pgconninfo.KeyDefaultClusterPort: "5432"}
	err := writeEndpointConfigMap(ctx, rc, child, child.Spec.Databases[0], publicationOperation{endpointOperationIntent: endpointOperationIntent{routing: staleRouting, attemptID: "attempt-A", generation: 1, fingerprint: "A"}, greenEnvironment: "green-A"})
	require.ErrorContains(t, err, "newer attempt")
	require.NoError(t, c.Get(ctx, key, cm))
	require.Equal(t, "green-B", cm.Data[pgconninfo.KeyClusterRWEndpoint])
	require.Equal(t, "attempt-B", cm.Annotations[pgconninfo.AnnotationEndpointAttemptID])
}

func TestEndpointWriterCASRejectsPublicationAfterAnotherWriterUpdatesConfigMap(t *testing.T) {
	ctx := t.Context()
	scheme := testScheme(t)
	child := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "ns", UID: "db-uid", Generation: 1},
		Spec:       platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "postgres"}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "orders"}}},
	}
	fingerprint := "fingerprint-A"
	parent := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: "ns"},
		Status: platformv1alpha1.PostgresClusterStatus{PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
			AttemptID: "attempt-A", EndpointIntentRevision: 1, Green: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green-A", UID: "green-A-uid"}},
			EndpointPreparation: &platformv1alpha1.BlueGreenEndpointPreparationStatus{Generation: 1, EndpointFingerprint: &fingerprint},
		}}}},
	}
	key := types.NamespacedName{Namespace: "ns", Name: configMapName("orders", "orders")}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace, UID: "cm-uid", OwnerReferences: []metav1.OwnerReference{{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresDatabase", Name: child.Name, UID: child.UID, Controller: new(true)}}},
		Data:       map[string]string{pgconninfo.KeyClusterRWEndpoint: "blue"},
	}
	raced := false
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(child, parent, cm).WithInterceptorFuncs(interceptor.Funcs{
		Update: func(ctx context.Context, next client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			if _, ok := obj.(*corev1.ConfigMap); ok && !raced {
				raced = true
				current := &corev1.ConfigMap{}
				if err := next.Get(ctx, key, current); err != nil {
					return err
				}
				current.Data[pgconninfo.KeyClusterRWEndpoint] = "green-B"
				current.Annotations = map[string]string{pgconninfo.AnnotationEndpointAttemptID: "attempt-B", pgconninfo.AnnotationEndpointIntentGeneration: "1"}
				if err := next.Update(ctx, current); err != nil {
					return err
				}
			}
			return next.Update(ctx, obj, opts...)
		},
	}).Build()
	rc := &ReconcileContext{Client: c, APIReader: c, Scheme: scheme}
	routing := map[string]string{pgconninfo.KeyClusterRWEndpoint: "green-A", pgconninfo.KeyClusterROEndpoint: "", pgconninfo.KeyClusterREndpoint: "green-A-r", pgconninfo.KeyDefaultClusterPort: "5432"}
	err := writeEndpointConfigMap(ctx, rc, child, child.Spec.Databases[0], preparationOperation{endpointOperationIntent{routing: routing, attemptID: "attempt-A", generation: 1, fingerprint: fingerprint}})
	require.True(t, apierrors.IsConflict(err), "expected resourceVersion conflict, got %v", err)
	require.NoError(t, c.Get(ctx, key, cm))
	require.Equal(t, "green-B", cm.Data[pgconninfo.KeyClusterRWEndpoint])
	require.Equal(t, "attempt-B", cm.Annotations[pgconninfo.AnnotationEndpointAttemptID])
}

func TestEndpointPreparationCancellationDiscardsPendingChildWork(t *testing.T) {
	ctx := t.Context()
	scheme := testScheme(t)
	child := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "ns", UID: "db-uid", Generation: 1},
		Spec:       platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "postgres"}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "orders"}}},
	}
	requested := metav1.Now()
	parent := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: "ns"},
		Status: platformv1alpha1.PostgresClusterStatus{PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
			AttemptID: "cancelled", Green: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}},
			EndpointPreparation: &platformv1alpha1.BlueGreenEndpointPreparationStatus{Generation: 1},
			Gates:               &platformv1alpha1.BlueGreenGateStatus{Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: &requested}},
		}}}},
	}
	c := testClient(t, scheme, child, parent)
	rc := &ReconcileContext{Client: c, APIReader: c, Scheme: scheme}
	handled, _, err := reconcileEndpointProtocol(ctx, rc, child)
	require.False(t, handled)
	require.NoError(t, err)
	cm := &corev1.ConfigMap{}
	err = c.Get(ctx, types.NamespacedName{Namespace: "ns", Name: configMapName("orders", "orders")}, cm)
	require.True(t, apierrors.IsNotFound(err))
}

func TestHistoricalEndpointIntentRemainsFencedAfterNewerStatusEntry(t *testing.T) {
	ctx := t.Context()
	scheme := testScheme(t)
	child := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "ns", UID: "db-uid", Generation: 1},
		Spec:       platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "postgres"}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "orders"}}},
	}
	fingerprint := "fingerprint"
	old := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		AttemptID: "old", EndpointIntentRevision: 1,
		Blue:                &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "blue", UID: "blue-uid"}},
		Green:               &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}},
		EndpointPreparation: &platformv1alpha1.BlueGreenEndpointPreparationStatus{Generation: 1, EndpointFingerprint: &fingerprint},
	}
	parent := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: "ns"},
		Status:     platformv1alpha1.PostgresClusterStatus{PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: old}, {}}},
	}
	c := testClient(t, scheme, child, parent)
	rc := &ReconcileContext{Client: c, APIReader: c, Scheme: scheme}
	handled, _, err := reconcileEndpointProtocol(ctx, rc, child)
	require.True(t, handled)
	require.ErrorContains(t, err, "no longer current")
	require.True(t, apierrors.IsNotFound(c.Get(ctx, types.NamespacedName{Namespace: "ns", Name: configMapName("orders", "orders")}, &corev1.ConfigMap{})))
}

func TestTargetAvailableWithoutPublicationProofKeepsOrdinaryWriterFenced(t *testing.T) {
	ctx := t.Context()
	scheme := testScheme(t)
	child := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "ns", UID: "db-uid", Generation: 1},
		Spec:       platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "postgres"}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "orders"}}},
	}
	available := metav1.Now()
	parent := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: "ns"},
		Status: platformv1alpha1.PostgresClusterStatus{
			ProvisionerRef: &corev1.ObjectReference{Name: "green", UID: "green-uid"},
			PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
				AttemptID: "attempt", Green: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}},
				Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{TargetAvailableAt: &available},
			}}},
		},
	}
	c := testClient(t, scheme, child, parent)
	handled, result, err := reconcileEndpointProtocol(ctx, &ReconcileContext{Client: c, APIReader: c, Scheme: scheme}, child)
	require.True(t, handled)
	require.NoError(t, err)
	require.NotZero(t, result.RequeueAfter)
}

func TestTargetAvailableWaitsForOrdinaryReaderToObserveCurrentGreenAttempt(t *testing.T) {
	ctx := t.Context()
	scheme := testScheme(t)
	routing := map[string]string{
		pgconninfo.KeyClusterRWEndpoint: "green-rw.ns.svc.cluster.local", pgconninfo.KeyClusterROEndpoint: "",
		pgconninfo.KeyClusterREndpoint: "green-r.ns.svc.cluster.local", pgconninfo.KeyDefaultClusterPort: pgconninfo.DefaultPort,
	}
	fingerprint := pgconninfo.FingerprintEndpointData(routing)
	now := metav1.Now()
	child := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "ns", UID: "db-uid", Generation: 3},
		Spec:       platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "postgres"}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "orders"}}},
	}
	ack := &platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{
		AttemptID: "attempt-B", IntentGeneration: 7, ObservedDatabaseGeneration: child.Generation, GreenClusterUID: "green-uid",
		Databases: []platformv1alpha1.DatabaseEndpointAcknowledgementStatus{{DatabaseName: "orders", EndpointFingerprint: fingerprint, ConfigMapUID: "db-cm-uid", AcknowledgedAt: &now}},
	}
	child.Status.EndpointPreparation = ack.DeepCopy()
	child.Status.EndpointPublication = ack.DeepCopy()
	roster := platformv1alpha1.BlueGreenEndpointPreparationAcknowledgementStatus{PostgresDatabaseName: child.Name, PostgresDatabaseUID: string(child.UID), PostgresDatabaseGeneration: child.Generation, DatabaseName: "orders"}
	pubRoster := platformv1alpha1.BlueGreenEndpointPublicationAcknowledgementStatus{PostgresDatabaseName: child.Name, PostgresDatabaseUID: string(child.UID), PostgresDatabaseGeneration: child.Generation, DatabaseName: "orders"}
	bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		AttemptID: "attempt-B", EndpointIntentRevision: 7,
		Green:               &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}},
		EndpointPreparation: &platformv1alpha1.BlueGreenEndpointPreparationStatus{Generation: 7, EndpointFingerprint: &fingerprint, Acknowledgements: []platformv1alpha1.BlueGreenEndpointPreparationAcknowledgementStatus{roster}},
		EndpointPublication: &platformv1alpha1.BlueGreenEndpointPublicationStatus{Generation: 7, EndpointFingerprint: &fingerprint, Acknowledgements: []platformv1alpha1.BlueGreenEndpointPublicationAcknowledgementStatus{pubRoster}},
		CommitReceipt:       &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CanonicalConfigMap: &corev1.ObjectReference{Name: "postgres-connection", UID: "canonical-uid"}, Target: &corev1.ObjectReference{Name: "green", UID: "green-uid"}},
		Switchover:          &platformv1alpha1.BlueGreenSwitchoverStatus{TargetAvailableAt: &now},
	}
	parent := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: "ns", UID: "parent-uid", ResourceVersion: "20"},
		Status: platformv1alpha1.PostgresClusterStatus{
			ProvisionerRef:             &corev1.ObjectReference{Name: "green", UID: "green-uid"},
			Resources:                  &platformv1alpha1.PostgresClusterResources{ConfigMapRef: &corev1.LocalObjectReference{Name: "postgres-connection"}},
			PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: bg}},
			ConnectionPoolerStatus:     &platformv1alpha1.ConnectionPoolerStatus{Enabled: true, ReadWriteEnabled: true},
		},
	}
	canonical := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "postgres-connection", Namespace: "ns", UID: "canonical-uid"}, Data: routing}
	stale := parent.DeepCopy()
	stale.Status.PostgresMajorUpgradeStatus[0].BlueGreen.AttemptID = "attempt-A"
	apiReader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(child, parent, canonical).Build()
	cachedClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(child, stale).Build()
	rc := &ReconcileContext{Client: cachedClient, APIReader: apiReader, Scheme: scheme,
		ClusterReader: clusterReadinessReaderFunc(func(context.Context, string, string) (dbclusterinfo.ResolvedClusterFacts, error) {
			card := testResolvedClusterCard(parent.Name, parent.Namespace, "green")
			card.Authoritative.Identity.UID = "green-uid"
			return dbclusterinfo.ResolvedClusterFacts{Cluster: card, ConnectionPoolerStatus: parent.Status.ConnectionPoolerStatus}, nil
		}),
	}
	handled, _, err := reconcileEndpointProtocol(ctx, rc, child)
	require.True(t, handled, "same green provider with stale cached attempt must remain fenced")
	require.NoError(t, err)

	rc.Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(child, parent.DeepCopy()).Build()
	rc.ClusterReader = clusterReadinessReaderFunc(func(context.Context, string, string) (dbclusterinfo.ResolvedClusterFacts, error) {
		card := testResolvedClusterCard(parent.Name, parent.Namespace, "green")
		card.Authoritative.Identity.UID = "green-uid"
		return dbclusterinfo.ResolvedClusterFacts{Cluster: card}, nil
	})
	handled, _, err = reconcileEndpointProtocol(ctx, rc, child)
	require.True(t, handled, "stale pooler status in ordinary reader must remain fenced")
	require.NoError(t, err)

	cleaned := parent.DeepCopy()
	cleaned.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Cleanup = &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateCleaned}
	cleaned.Status.PostgresMajorUpgradeStatus = append(cleaned.Status.PostgresMajorUpgradeStatus, platformv1alpha1.PostgresMajorUpgradeStatus{})
	rc.APIReader = fake.NewClientBuilder().WithScheme(scheme).WithObjects(child, cleaned, canonical).Build()
	rc.Client = cachedClient
	handled, _, err = reconcileEndpointProtocol(ctx, rc, child)
	require.True(t, handled, "cleanup must not release a stale ordinary reader")
	require.NoError(t, err)
	rc.Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(child, cleaned.DeepCopy()).Build()
	rc.ClusterReader = clusterReadinessReaderFunc(func(context.Context, string, string) (dbclusterinfo.ResolvedClusterFacts, error) {
		card := testResolvedClusterCard(parent.Name, parent.Namespace, "green")
		card.Authoritative.Identity.UID = "green-uid"
		return dbclusterinfo.ResolvedClusterFacts{Cluster: card, ConnectionPoolerStatus: parent.Status.ConnectionPoolerStatus}, nil
	})
	premature := cleaned.DeepCopy()
	premature.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Switchover.TargetAvailableAt = nil
	rc.APIReader = fake.NewClientBuilder().WithScheme(scheme).WithObjects(child, premature, canonical).Build()
	handled, _, err = reconcileEndpointProtocol(ctx, rc, child)
	require.True(t, handled, "cleanup without TargetAvailable must not bypass publication proof")
	require.NoError(t, err)
	rc.APIReader = fake.NewClientBuilder().WithScheme(scheme).WithObjects(child, cleaned, canonical).Build()
	handled, _, err = reconcileEndpointProtocol(ctx, rc, child)
	require.False(t, handled, "aligned cache must release cleaned attempt despite trailing non-BlueGreen status")
	require.NoError(t, err)
	rc.APIReader = nil
	handled, _, err = reconcileEndpointProtocol(ctx, rc, child)
	require.True(t, handled, "cleanup still requires an uncached reader before ordinary writes")
	require.ErrorContains(t, err, "uncached API reader is required")
	for _, isCleaned := range []bool{false, true} {
		malformed := cleaned.DeepCopy()
		latest := mostRecentBlueGreenStatus(malformed)
		latest.AttemptID = ""
		if !isCleaned {
			latest.Cleanup = nil
		}
		malformedClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(child, malformed, canonical).Build()
		rc.Client = malformedClient
		rc.APIReader = malformedClient
		handled, _, err = reconcileEndpointProtocol(ctx, rc, child)
		require.True(t, handled, "marker-bearing status with empty attempt ID must remain fenced")
		require.ErrorContains(t, err, "endpoint intent is missing attempt ID")
		settled, settleErr := endpointPublicationSettled(ctx, rc, child, malformed, latest)
		require.False(t, settled)
		require.NoError(t, settleErr)
		rc.APIReader = nil
		handled, _, err = reconcileEndpointProtocol(ctx, rc, child)
		require.True(t, handled)
		require.ErrorContains(t, err, "uncached API reader is required")
	}
}

func TestFinishedAttemptReleasesChildrenOutsideFrozenEndpointInventory(t *testing.T) {
	for _, state := range []finishedEndpointAttemptCase{
		{name: "retained completed", cleanupState: platformv1alpha1.BlueGreenCleanupStateRetained, phase: "Completed", wantRelease: true},
		{name: "retained completed with recreated canonical ConfigMap", cleanupState: platformv1alpha1.BlueGreenCleanupStateRetained, phase: "Completed", wantRelease: true, recreatedCanonical: true},
		{name: "cleaning completed", cleanupState: platformv1alpha1.BlueGreenCleanupStateCleaning, phase: "Completed", wantRelease: true},
		{name: "cleanup failed completed", cleanupState: platformv1alpha1.BlueGreenCleanupStateCleanupFailed, phase: "Completed", wantRelease: true},
		{name: "cleaned completed", cleanupState: platformv1alpha1.BlueGreenCleanupStateCleaned, phase: "Completed", wantRelease: true},
		{name: "cleaned without completed phase", cleanupState: platformv1alpha1.BlueGreenCleanupStateCleaned, phase: "SwitchingOver", wantRelease: true},
		{name: "retained before completion", cleanupState: platformv1alpha1.BlueGreenCleanupStateRetained, phase: "SwitchingOver"},
		{name: "retained before completion with recreated canonical ConfigMap", cleanupState: platformv1alpha1.BlueGreenCleanupStateRetained, phase: "SwitchingOver", recreatedCanonical: true},
	} {
		t.Run(state.name, func(t *testing.T) {

			children := []endpointChildCase{
				{name: "new child after cutover", childName: "invoices", childUID: "invoices-uid", generation: 1, wantReleased: true},
				{name: "existing child changed generation after cutover", childName: "orders", childUID: "orders-uid", generation: 4, wantReleased: true},
				{name: "original roster child without ACK", childName: "orders", childUID: "orders-uid", generation: 3},
			}
			if state.recreatedCanonical {
				children = append(children, endpointChildCase{name: "original roster child with ACK", childName: "orders", childUID: "orders-uid", generation: 3, withACK: true})
			}
			for _, tc := range children {
				t.Run(tc.name, func(t *testing.T) {
					assertFinishedEndpointChildRelease(t, state, tc)
				})
			}
		})
	}
}

func TestCompactedCleanedAttemptReleasesOnlyAfterProviderAuthorityConverges(t *testing.T) {
	for _, childName := range []string{"orders", "invoices"} {
		t.Run(childName, func(t *testing.T) {
			ctx := t.Context()
			scheme := testScheme(t)
			child := &platformv1alpha1.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: childName, Namespace: "ns", UID: types.UID(childName + "-uid"), Generation: 3},
				Spec: platformv1alpha1.PostgresDatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: "postgres"},
					Databases:  []platformv1alpha1.DatabaseDefinition{{Name: childName}},
				},
			}
			cleanedAt := metav1.Now()
			parent := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: "ns", UID: "parent-uid", ResourceVersion: "20"},
				Status: platformv1alpha1.PostgresClusterStatus{
					ProvisionerRef: &corev1.ObjectReference{Name: "green", UID: "green-uid"},
					PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
						AttemptID: "attempt", RearmedAt: &cleanedAt, HistoryCompacted: true,
						Cleanup: &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateCleaned, CompletedAt: &cleanedAt},
					}}},
				},
			}
			require.Equal(t, endpointAttemptCleanedCompacted, inspectEndpointAttempt(parent).state, "fixture must match the state store's compacted cleanup summary")
			blueParent := parent.DeepCopy()
			blueParent.Status.ProvisionerRef = &corev1.ObjectReference{Name: "blue", UID: "blue-uid"}

			for _, tc := range []struct {
				name             string
				apiParent        *platformv1alpha1.PostgresCluster
				cachedParent     *platformv1alpha1.PostgresCluster
				ordinaryProvider string
				wantHandled      bool
				wantError        string
			}{
				{name: "stale cached Blue provider", apiParent: parent, cachedParent: func() *platformv1alpha1.PostgresCluster {
					stale := parent.DeepCopy()
					stale.Status.ProvisionerRef = &corev1.ObjectReference{Name: "blue", UID: "blue-uid"}
					return stale
				}(), ordinaryProvider: "green", wantHandled: true},
				{name: "stale cached resource version", apiParent: parent, cachedParent: func() *platformv1alpha1.PostgresCluster {
					stale := parent.DeepCopy()
					stale.ResourceVersion = "19"
					return stale
				}(), ordinaryProvider: "green", wantHandled: true},
				{name: "stale ordinary Blue authority", apiParent: parent, cachedParent: parent, ordinaryProvider: "blue", wantHandled: true},
				{name: "aligned Green authority", apiParent: parent, cachedParent: parent, ordinaryProvider: "green"},
				{name: "legacy compacted summary without marker", apiParent: func() *platformv1alpha1.PostgresCluster {
					legacy := parent.DeepCopy()
					legacy.Status.PostgresMajorUpgradeStatus[0].BlueGreen.HistoryCompacted = false
					return legacy
				}(), cachedParent: func() *platformv1alpha1.PostgresCluster {
					legacy := parent.DeepCopy()
					legacy.Status.PostgresMajorUpgradeStatus[0].BlueGreen.HistoryCompacted = false
					return legacy
				}(), ordinaryProvider: "green"},
				{name: "cancelled precommit attempt with aligned Blue authority", apiParent: blueParent, cachedParent: blueParent, ordinaryProvider: "blue"},
				{name: "cancelled precommit attempt with stale cached Green provider", apiParent: blueParent, cachedParent: parent, ordinaryProvider: "blue", wantHandled: true},
				{name: "missing authoritative provider", apiParent: func() *platformv1alpha1.PostgresCluster {
					malformed := parent.DeepCopy()
					malformed.Status.ProvisionerRef = nil
					return malformed
				}(), cachedParent: parent, ordinaryProvider: "green", wantHandled: true},
				{name: "missing attempt ID", apiParent: func() *platformv1alpha1.PostgresCluster {
					malformed := parent.DeepCopy()
					malformed.Status.PostgresMajorUpgradeStatus[0].BlueGreen.AttemptID = ""
					return malformed
				}(), cachedParent: parent, ordinaryProvider: "green", wantHandled: true, wantError: "endpoint intent is missing attempt ID"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					apiReader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(child, tc.apiParent).Build()
					cachedClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(child, tc.cachedParent).Build()
					rc := &ReconcileContext{Client: cachedClient, APIReader: apiReader, Scheme: scheme,
						ClusterReader: clusterReadinessReaderFunc(func(context.Context, string, string) (dbclusterinfo.ResolvedClusterFacts, error) {
							return endpointTestClusterFacts(parent, tc.ordinaryProvider), nil
						}),
					}
					handled, _, err := reconcileEndpointProtocol(ctx, rc, child)
					require.Equal(t, tc.wantHandled, handled)
					if tc.wantError != "" {
						require.ErrorContains(t, err, tc.wantError)
					} else {
						require.NoError(t, err)
					}
				})
			}
		})
	}
}

func TestUnavailableBlueGreenHistoryAllowsOrdinaryDatabaseProtocolFallthrough(t *testing.T) {
	failed := string(mvutypes.Failed)
	completed := string(mvutypes.Completed)
	blueGreen := mvutypes.MajorUpgradeFlowBlueGreen
	pgUpgrade := mvutypes.MajorUpgradeFlowPgUpgrade
	for _, tc := range []struct {
		name        string
		mutate      func(*platformv1alpha1.PostgresBlueGreenUpgradeStatus)
		olderOwner  bool
		wantHandled bool
	}{
		{name: "unavailable placeholder allows ordinary database writer"},
		{name: "endpoint intent stays fenced", mutate: func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.EndpointPreparation = &platformv1alpha1.BlueGreenEndpointPreparationStatus{Generation: 1}
		}, wantHandled: true},
		{name: "retained resources stay fenced", mutate: func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Cleanup = &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateRetained}
		}, wantHandled: true},
		{name: "older retained attempt remains fenced behind unavailable placeholder", olderOwner: true, wantHandled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := &platformv1alpha1.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "ns", UID: "child-uid", Generation: 1},
				Spec: platformv1alpha1.PostgresDatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: "postgres"},
					Databases:  []platformv1alpha1.DatabaseDefinition{{Name: "orders"}},
				},
			}
			bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "unavailable"}
			if tc.mutate != nil {
				tc.mutate(bg)
			}
			entries := []platformv1alpha1.PostgresMajorUpgradeStatus{
				{Phase: &failed, Strategy: &blueGreen, BlueGreen: bg, Conditions: []metav1.Condition{{
					Type: mvutypes.ConditionMajorUpgradeTerminalFailure, Status: metav1.ConditionTrue, Reason: mvutypes.ReasonBlueGreenStrategyUnavailable,
				}}},
				{Phase: &completed, Strategy: &pgUpgrade},
			}
			if tc.olderOwner {
				entries = append([]platformv1alpha1.PostgresMajorUpgradeStatus{{
					Phase: &completed, Strategy: &blueGreen,
					BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "owner", Cleanup: &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateRetained}},
				}}, entries...)
			}
			parent := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: "ns", UID: "parent-uid"},
				Status:     platformv1alpha1.PostgresClusterStatus{PostgresMajorUpgradeStatus: entries},
			}
			reader := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(child, parent).Build()
			handled, result, err := reconcileEndpointProtocol(t.Context(), &ReconcileContext{Client: reader, APIReader: reader}, child)
			require.Equal(t, tc.wantHandled, handled)
			require.Equal(t, ctrl.Result{}, result)
			if tc.wantHandled {
				require.ErrorContains(t, err, "no longer current")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestHistoricalBlueGreenAttemptUsesCurrentProviderAuthority(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		cleanup              platformv1alpha1.BlueGreenCleanupState
		compacted            bool
		precommit            bool
		missingCommitReceipt bool
		release              bool
	}{
		{name: "cleaned detailed precommit cancellation", cleanup: platformv1alpha1.BlueGreenCleanupStateCleaned, precommit: true, release: true},
		{name: "cleaned compacted history", cleanup: platformv1alpha1.BlueGreenCleanupStateCleaned, compacted: true, release: true},
		{name: "postcommit status missing receipt stays fenced", cleanup: platformv1alpha1.BlueGreenCleanupStateCleaned, missingCommitReceipt: true},
		{name: "retained history remains fenced", cleanup: platformv1alpha1.BlueGreenCleanupStateRetained},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			scheme := testScheme(t)
			child := &platformv1alpha1.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "ns", UID: "child-uid", Generation: 1}, Spec: platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "postgres"}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "orders"}}}}
			bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "old-attempt", Cleanup: &platformv1alpha1.BlueGreenCleanupStatus{State: tc.cleanup}, HistoryCompacted: tc.compacted}
			if !tc.compacted {
				bg.Blue = &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "blue", UID: "blue-uid"}}
			}
			providerName := "current"
			if tc.precommit {
				providerName = "blue"
			}
			if tc.missingCommitReceipt {
				committed := metav1.Now()
				bg.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{EndpointCommittedAt: &committed, TargetAvailableAt: &committed}
			}
			parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: "ns", UID: "parent-uid", ResourceVersion: "20"}, Status: platformv1alpha1.PostgresClusterStatus{ProvisionerRef: &corev1.ObjectReference{Name: providerName, UID: types.UID(providerName + "-uid")}, PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: bg}, {}}}}
			apiReader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(child, parent).Build()
			provider := "stale"
			rc := &ReconcileContext{Client: apiReader, APIReader: apiReader, Scheme: scheme, ClusterReader: clusterReadinessReaderFunc(func(context.Context, string, string) (dbclusterinfo.ResolvedClusterFacts, error) {
				card := testResolvedClusterCard(parent.Name, parent.Namespace, provider)
				card.Authoritative.Identity.UID = types.UID(provider + "-uid")
				return dbclusterinfo.ResolvedClusterFacts{Cluster: card}, nil
			})}
			handled, _, err := reconcileEndpointProtocol(ctx, rc, child)
			assertHistoricalEndpointError(t, err, tc.cleanup)
			require.True(t, handled, "stale ordinary authority or retained history must keep the writer fenced")
			provider = providerName
			handled, _, err = reconcileEndpointProtocol(ctx, rc, child)
			assertHistoricalEndpointError(t, err, tc.cleanup)
			require.Equal(t, !tc.release, handled)
		})
	}
}

func TestUncachedMissingParentSetsClusterNotFoundWithoutUsingCachedReadiness(t *testing.T) {
	ctx := t.Context()
	scheme := testScheme(t)
	ready := string(readyDBPhase)
	child := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "ns", UID: "db-uid", Generation: 1},
		Spec:       platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "missing"}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "orders"}}},
		Status:     platformv1alpha1.PostgresDatabaseStatus{Phase: &ready},
	}
	c := testClient(t, scheme, child)
	handled, result, err := reconcileEndpointProtocol(ctx, &ReconcileContext{
		Client: c, APIReader: c, Scheme: scheme, Recorder: record.NewFakeRecorder(1),
		ClusterReader: clusterReadinessReaderFunc(func(context.Context, string, string) (dbclusterinfo.ResolvedClusterFacts, error) {
			t.Fatal("cached readiness must not override uncached parent NotFound")
			return dbclusterinfo.ResolvedClusterFacts{}, nil
		}),
	}, child)
	require.True(t, handled)
	require.NoError(t, err)
	require.Equal(t, clusterNotFoundRetryDelay, result.RequeueAfter)
	current := &platformv1alpha1.PostgresDatabase{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(child), current))
	require.Equal(t, string(pendingDBPhase), *current.Status.Phase)
	require.Equal(t, string(reasonClusterNotFound), current.Status.Conditions[0].Reason)
}

func TestEndpointAcknowledgementMatchesAllDatabaseProofs(t *testing.T) {
	now := metav1.Now()
	desired := &platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{
		AttemptID: "attempt", IntentGeneration: 4, ObservedDatabaseGeneration: 3, GreenClusterUID: "green-uid",
		Databases: []platformv1alpha1.DatabaseEndpointAcknowledgementStatus{
			{DatabaseName: "orders", EndpointFingerprint: "fingerprint", ConfigMapUID: "orders-uid", AcknowledgedAt: &now},
			{DatabaseName: "ledger", EndpointFingerprint: "fingerprint", ConfigMapUID: "ledger-uid", AcknowledgedAt: &now},
		},
	}
	for _, tc := range []struct {
		name   string
		change func(*platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus)
		match  bool
	}{
		{name: "same proof", match: true},
		{name: "proof order is irrelevant", change: func(a *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus) {
			a.Databases[0], a.Databases[1] = a.Databases[1], a.Databases[0]
		}, match: true},
		{name: "attempt changed", change: func(a *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus) { a.AttemptID = "next" }},
		{name: "intent generation changed", change: func(a *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus) { a.IntentGeneration++ }},
		{name: "child generation changed", change: func(a *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus) {
			a.ObservedDatabaseGeneration++
		}},
		{name: "green identity changed", change: func(a *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus) { a.GreenClusterUID = "other" }},
		{name: "database omitted", change: func(a *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus) { a.Databases = a.Databases[:1] }},
		{name: "database name changed", change: func(a *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus) {
			a.Databases[1].DatabaseName = "other"
		}},
		{name: "fingerprint changed", change: func(a *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus) {
			a.Databases[1].EndpointFingerprint = "old"
		}},
		{name: "configmap UID changed", change: func(a *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus) {
			a.Databases[1].ConfigMapUID = "replaced"
		}},
		{name: "acknowledgement timestamp missing", change: func(a *platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus) {
			a.Databases[1].AcknowledgedAt = nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := desired.DeepCopy()
			if tc.change != nil {
				tc.change(got)
			}
			require.Equal(t, tc.match, endpointAcknowledgementMatches(got, desired))
		})
	}
	require.False(t, endpointAcknowledgementMatches(nil, desired))
}

func TestEndpointProtocolInputAndPreparationGuards(t *testing.T) {
	ctx := t.Context()
	scheme := testScheme(t)
	child := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "ns", UID: "child-uid", Generation: 3},
		Spec: platformv1alpha1.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "parent"},
			Databases:  []platformv1alpha1.DatabaseDefinition{{Name: "orders"}},
		},
	}
	fingerprint := "prepared-fingerprint"
	bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		AttemptID: "attempt", EndpointIntentRevision: 5,
		Blue:  &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "blue", UID: "blue-uid"}},
		Green: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}},
		EndpointPreparation: &platformv1alpha1.BlueGreenEndpointPreparationStatus{Generation: 5, EndpointFingerprint: &fingerprint,
			Acknowledgements: []platformv1alpha1.BlueGreenEndpointPreparationAcknowledgementStatus{{PostgresDatabaseName: child.Name, PostgresDatabaseUID: string(child.UID), PostgresDatabaseGeneration: child.Generation, DatabaseName: "orders"}}},
	}
	parent := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "ns", UID: "parent-uid"},
		Status: platformv1alpha1.PostgresClusterStatus{
			ProvisionerRef:             &corev1.ObjectReference{Name: "blue", UID: "blue-uid"},
			Resources:                  &platformv1alpha1.PostgresClusterResources{ConfigMapRef: &corev1.LocalObjectReference{Name: "canonical"}},
			PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: bg}},
		},
	}
	canonical := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "canonical", Namespace: "ns", UID: "canonical-uid"}}
	c := testClient(t, scheme, child, parent, canonical)
	rc := &ReconcileContext{Client: c, APIReader: c, Scheme: scheme, Recorder: record.NewFakeRecorder(5)}

	// Without an uncached reader, even a valid cached preparation is fenced.
	handled, _, err := reconcileEndpointProtocol(ctx, &ReconcileContext{Client: c, Scheme: scheme}, child)
	require.True(t, handled)
	require.ErrorContains(t, err, "uncached API reader is required")
	require.NoError(t, checkCurrentPreparation(ctx, c, child, bg, 5, fingerprint))
	require.ErrorContains(t, checkCurrentPreparation(ctx, c, child, bg, 5, "stale"), "intent changed")

	changed := child.DeepCopy()
	changed.Generation++
	_, err = readCurrentEndpointChild(ctx, c, changed)
	require.ErrorContains(t, err, "changed while processing")
	missing := child.DeepCopy()
	missing.Name = "missing"
	_, err = readCurrentEndpointChild(ctx, c, missing)
	require.Error(t, err)

	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(canonical), canonical))
	canonical.Annotations = map[string]string{pgconninfo.AnnotationActiveEnvironment: "green"}
	require.NoError(t, c.Update(ctx, canonical))
	require.ErrorContains(t, checkCurrentPreparation(ctx, c, child, bg, 5, fingerprint), "canonical endpoint commit")
	canonical.Annotations = nil
	require.NoError(t, c.Update(ctx, canonical))

	_, _, err = candidateRoutingData(ctx, c, child.Namespace, parent, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{})
	require.ErrorContains(t, err, "identity is missing")
	_, _, err = candidateRoutingData(ctx, c, child.Namespace, parent, bg)
	require.ErrorContains(t, err, "reading green CNPG Cluster")
	green := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "green", Namespace: "ns", UID: "wrong-uid"}}
	require.NoError(t, c.Create(ctx, green))
	_, _, err = candidateRoutingData(ctx, c, child.Namespace, parent, bg)
	require.ErrorContains(t, err, "not the expected healthy candidate")
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(green), green))
	green.UID = "green-uid"
	green.Spec.Instances = 1
	green.Status.Phase = cnpgv1.PhaseHealthy
	green.Status.ReadyInstances = 1
	require.NoError(t, c.Update(ctx, green))
	parent.Status.ConnectionPoolerStatus = &platformv1alpha1.ConnectionPoolerStatus{Enabled: true, ReadWriteEnabled: true}
	_, _, err = candidateRoutingData(ctx, c, child.Namespace, parent, bg)
	require.ErrorContains(t, err, "validating green endpoint Services")
	err = prepareDatabaseEndpoints(ctx, rc, child, parent, bg)
	require.ErrorContains(t, err, "validating green endpoint Services")

	// A stale write cannot proceed even when the ConfigMap has not been created.
	key := types.NamespacedName{Namespace: child.Namespace, Name: configMapName(child.Name, "orders")}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace, UID: "cm-uid",
		Annotations:     map[string]string{pgconninfo.AnnotationEndpointAttemptID: bg.AttemptID, pgconninfo.AnnotationEndpointIntentGeneration: "invalid"},
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresDatabase", Name: child.Name, UID: child.UID, Controller: new(true)}},
	}}
	require.NoError(t, c.Create(ctx, cm))
	routing := map[string]string{pgconninfo.KeyClusterRWEndpoint: "green-rw", pgconninfo.KeyClusterROEndpoint: "", pgconninfo.KeyClusterREndpoint: "green-r", pgconninfo.KeyDefaultClusterPort: pgconninfo.DefaultPort}
	op := preparationOperation{endpointOperationIntent{routing: routing, attemptID: bg.AttemptID, generation: 5, fingerprint: fingerprint}}
	require.ErrorContains(t, writeEndpointConfigMap(ctx, rc, child, child.Spec.Databases[0], op), "invalid intent generation")
	rc.APIReader = nil
	require.ErrorContains(t, validateEndpointWriterIntent(ctx, rc, child, op), "uncached API reader is required")
}

func TestEndpointProtocolOrdinaryReaderReleaseGuards(t *testing.T) {
	ctx := t.Context()
	scheme := testScheme(t)
	child := &platformv1alpha1.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "ns", UID: "child-uid", Generation: 1}, Spec: platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "parent"}}}
	bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt", EndpointIntentRevision: 2, Green: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}}}
	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "ns", UID: "parent-uid"}, Status: platformv1alpha1.PostgresClusterStatus{ProvisionerRef: &corev1.ObjectReference{Name: "green", UID: "green-uid"}, PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: bg}}}}
	c := testClient(t, scheme, child, parent)
	rc := &ReconcileContext{Client: c, APIReader: c, Scheme: scheme}
	observed := &platformv1alpha1.PostgresCluster{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(parent), observed))

	ok, err := ordinaryReaderObservesGreen(ctx, rc, child, observed, bg)
	require.False(t, ok)
	require.ErrorContains(t, err, "cluster reader is required")
	rc.ClusterReader = clusterReadinessReaderFunc(func(context.Context, string, string) (dbclusterinfo.ResolvedClusterFacts, error) {
		return dbclusterinfo.ResolvedClusterFacts{}, fmt.Errorf("reader unavailable")
	})
	ok, err = ordinaryReaderObservesGreen(ctx, rc, child, observed, bg)
	require.False(t, ok)
	require.ErrorContains(t, err, "reader unavailable")
	rc.ClusterReader = clusterReadinessReaderFunc(func(context.Context, string, string) (dbclusterinfo.ResolvedClusterFacts, error) {
		return dbclusterinfo.ResolvedClusterFacts{}, nil
	})
	ok, err = ordinaryReaderObservesGreen(ctx, rc, child, observed, bg)
	require.False(t, ok)
	require.ErrorContains(t, err, "no cluster identity")

	stale := observed.DeepCopy()
	stale.Status.ProvisionerRef.UID = "blue-uid"
	staleClient := testClient(t, scheme, child, stale)
	rc.Client = staleClient
	ok, err = ordinaryReaderObservesGreen(ctx, rc, child, observed, bg)
	require.False(t, ok)
	require.NoError(t, err)
	rc.Client = testClient(t, scheme, child)
	ok, err = ordinaryReaderObservesGreen(ctx, rc, child, observed, bg)
	require.False(t, ok)
	require.Error(t, err)
}

func TestEndpointPublicationRejectsChangedDurableProof(t *testing.T) {
	ctx := t.Context()
	scheme := testScheme(t)
	routing := map[string]string{
		pgconninfo.KeyClusterRWEndpoint:  "green-rw.ns.svc.cluster.local",
		pgconninfo.KeyClusterROEndpoint:  "",
		pgconninfo.KeyClusterREndpoint:   "green-r.ns.svc.cluster.local",
		pgconninfo.KeyDefaultClusterPort: pgconninfo.DefaultPort,
	}
	fingerprint := pgconninfo.FingerprintEndpointData(routing)
	now := metav1.Now()
	newFixture := func() (*platformv1alpha1.PostgresDatabase, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus, *corev1.ConfigMap) {
		child := &platformv1alpha1.PostgresDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "ns", UID: "child-uid", Generation: 3},
			Spec:       platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "parent"}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "orders"}}},
		}
		child.Status.EndpointPreparation = &platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{
			AttemptID: "attempt", IntentGeneration: 5, ObservedDatabaseGeneration: child.Generation, GreenClusterUID: "green-uid",
			Databases: []platformv1alpha1.DatabaseEndpointAcknowledgementStatus{{DatabaseName: "orders", EndpointFingerprint: fingerprint, ConfigMapUID: "db-map-uid", AcknowledgedAt: &now}},
		}
		bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
			AttemptID: "attempt", EndpointIntentRevision: 5,
			Green: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}},
			EndpointPreparation: &platformv1alpha1.BlueGreenEndpointPreparationStatus{Generation: 5, EndpointFingerprint: &fingerprint,
				Acknowledgements: []platformv1alpha1.BlueGreenEndpointPreparationAcknowledgementStatus{{PostgresDatabaseName: child.Name, PostgresDatabaseUID: string(child.UID), PostgresDatabaseGeneration: child.Generation, DatabaseName: "orders"}}},
			EndpointPublication: &platformv1alpha1.BlueGreenEndpointPublicationStatus{Generation: 5, EndpointFingerprint: &fingerprint,
				Acknowledgements: []platformv1alpha1.BlueGreenEndpointPublicationAcknowledgementStatus{{PostgresDatabaseName: child.Name, PostgresDatabaseUID: string(child.UID), PostgresDatabaseGeneration: child.Generation, DatabaseName: "orders"}}},
			CommitReceipt: &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CanonicalConfigMap: &corev1.ObjectReference{Name: "canonical", UID: "canonical-uid"}, Target: &corev1.ObjectReference{Name: "green", UID: "green-uid"}},
		}
		parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "ns", UID: "parent-uid"},
			Status: platformv1alpha1.PostgresClusterStatus{
				ProvisionerRef:             &corev1.ObjectReference{Name: "green", UID: "green-uid"},
				Resources:                  &platformv1alpha1.PostgresClusterResources{ConfigMapRef: &corev1.LocalObjectReference{Name: "canonical"}},
				PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: bg}},
			},
		}
		canonical := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "canonical", Namespace: "ns", UID: "canonical-uid",
			Annotations: map[string]string{
				pgconninfo.AnnotationActiveEnvironment: "green", pgconninfo.AnnotationEndpointAttemptID: "attempt",
				pgconninfo.AnnotationEndpointIntentGeneration: "5", pgconninfo.AnnotationEndpointFingerprint: fingerprint,
			}}, Data: maps.Clone(routing)}
		return child, parent, bg, canonical
	}
	t.Run("valid canonical commit", func(t *testing.T) {
		child, parent, bg, canonical := newFixture()
		c := testClient(t, scheme, child, parent, canonical)
		require.NoError(t, checkCurrentPublication(ctx, c, child, bg, 5, fingerprint, routing))
	})
	for _, tc := range []struct {
		name   string
		change func(*platformv1alpha1.PostgresDatabase, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus, *corev1.ConfigMap)
		want   string
	}{
		{"preparation acknowledgement disappeared", func(child *platformv1alpha1.PostgresDatabase, _ *platformv1alpha1.PostgresCluster, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus, _ *corev1.ConfigMap) {
			child.Status.EndpointPreparation = nil
		}, "preparation acknowledgement disappeared"},
		{"publication roster changed", func(_ *platformv1alpha1.PostgresDatabase, _ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, _ *corev1.ConfigMap) {
			bg.EndpointPublication.Acknowledgements = nil
		}, "publication inventory"},
		{"intent revision changed", func(_ *platformv1alpha1.PostgresDatabase, _ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, _ *corev1.ConfigMap) {
			bg.EndpointIntentRevision++
		}, "publication intent changed"},
		{"canonical UID changed", func(_ *platformv1alpha1.PostgresDatabase, _ *platformv1alpha1.PostgresCluster, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus, canonical *corev1.ConfigMap) {
			canonical.UID = "recreated-uid"
		}, "UID changed"},
		{"provider changed", func(_ *platformv1alpha1.PostgresDatabase, parent *platformv1alpha1.PostgresCluster, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus, _ *corev1.ConfigMap) {
			parent.Status.ProvisionerRef.UID = "other-uid"
		}, "commit status is incomplete"},
		{"commit stamp changed", func(_ *platformv1alpha1.PostgresDatabase, _ *platformv1alpha1.PostgresCluster, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus, canonical *corev1.ConfigMap) {
			canonical.Annotations[pgconninfo.AnnotationEndpointIntentGeneration] = "4"
		}, "does not prove"},
		{"canonical routing changed", func(_ *platformv1alpha1.PostgresDatabase, _ *platformv1alpha1.PostgresCluster, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus, canonical *corev1.ConfigMap) {
			canonical.Data[pgconninfo.KeyClusterRWEndpoint] = "uncommitted"
		}, "does not prove"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child, parent, bg, canonical := newFixture()
			tc.change(child, parent, bg, canonical)
			c := testClient(t, scheme, child, parent, canonical)
			require.ErrorContains(t, checkCurrentPublication(ctx, c, child, bg, 5, fingerprint, routing), tc.want)
		})
	}
	t.Run("caller expected different routing", func(t *testing.T) {
		child, parent, bg, canonical := newFixture()
		c := testClient(t, scheme, child, parent, canonical)
		expected := map[string]string{}
		maps.Copy(expected, routing)
		expected[pgconninfo.KeyClusterRWEndpoint] = "different"
		require.ErrorContains(t, checkCurrentPublication(ctx, c, child, bg, 5, fingerprint, expected), "routing changed during publication")
	})
	t.Run("parent disappeared", func(t *testing.T) {
		child, _, bg, canonical := newFixture()
		c := testClient(t, scheme, child, canonical)
		require.ErrorContains(t, checkCurrentPublication(ctx, c, child, bg, 5, fingerprint, routing), "reading endpoint intent")
	})
	t.Run("child generation changed", func(t *testing.T) {
		child, parent, bg, canonical := newFixture()
		c := testClient(t, scheme, child, parent, canonical)
		stale := child.DeepCopy()
		stale.Generation--
		require.ErrorContains(t, checkCurrentPublication(ctx, c, stale, bg, 5, fingerprint, routing), "changed while processing")
	})
	t.Run("canonical ConfigMap disappeared", func(t *testing.T) {
		child, parent, bg, _ := newFixture()
		c := testClient(t, scheme, child, parent)
		require.ErrorContains(t, checkCurrentPublication(ctx, c, child, bg, 5, fingerprint, routing), "reading canonical endpoint ConfigMap")
	})
	t.Run("child passed to writer lost acknowledgement", func(t *testing.T) {
		child, parent, bg, canonical := newFixture()
		c := testClient(t, scheme, child, parent, canonical)
		stale := child.DeepCopy()
		stale.Status.EndpointPreparation = nil
		rc := &ReconcileContext{Client: c, APIReader: c, Scheme: scheme}
		require.ErrorContains(t, publishDatabaseEndpoints(ctx, rc, stale, parent, bg), "preparation acknowledgement is missing")
	})
	t.Run("publication attempt is historical", func(t *testing.T) {
		child, parent, bg, canonical := newFixture()
		c := testClient(t, scheme, child, parent, canonical)
		stale := bg.DeepCopy()
		stale.AttemptID = "prior-attempt"
		rc := &ReconcileContext{Client: c, APIReader: c, Scheme: scheme}
		require.ErrorContains(t, publishDatabaseEndpoints(ctx, rc, child, parent, stale), "no longer current")
	})
	t.Run("publication intent is incomplete", func(t *testing.T) {
		child, parent, bg, canonical := newFixture()
		c := testClient(t, scheme, child, parent, canonical)
		stale := bg.DeepCopy()
		stale.EndpointPublication = nil
		rc := &ReconcileContext{Client: c, APIReader: c, Scheme: scheme}
		require.ErrorContains(t, publishDatabaseEndpoints(ctx, rc, child, parent, stale), "incomplete endpoint publication intent")
	})
	t.Run("preparation roster lost database", func(t *testing.T) {
		child, parent, bg, canonical := newFixture()
		c := testClient(t, scheme, child, parent, canonical)
		stale := bg.DeepCopy()
		stale.EndpointPreparation.Acknowledgements = nil
		rc := &ReconcileContext{Client: c, APIReader: c, Scheme: scheme}
		require.ErrorContains(t, publishDatabaseEndpoints(ctx, rc, child, parent, stale), "absent from endpoint preparation inventory")
	})
}

func TestEndpointProofShapeGuards(t *testing.T) {
	routing := map[string]string{
		pgconninfo.KeyClusterRWEndpoint: "green-rw", pgconninfo.KeyClusterROEndpoint: "",
		pgconninfo.KeyClusterREndpoint: "green-r", pgconninfo.KeyDefaultClusterPort: pgconninfo.DefaultPort,
	}
	require.True(t, completeCommittedRouting(routing))
	missing := maps.Clone(routing)
	delete(missing, pgconninfo.KeyClusterRWEndpoint)
	require.False(t, completeCommittedRouting(missing), "missing write endpoint cannot be committed")
	empty := maps.Clone(routing)
	empty[pgconninfo.KeyClusterRWEndpoint] = ""
	require.False(t, completeCommittedRouting(empty), "empty write endpoint cannot be committed")
	wrongPort := maps.Clone(routing)
	wrongPort[pgconninfo.KeyDefaultClusterPort] = "5444"
	require.False(t, completeCommittedRouting(wrongPort), "unexpected port cannot be committed")
	halfPooler := maps.Clone(routing)
	halfPooler[pgconninfo.KeyPoolerRWEndpoint] = "pooler-rw"
	require.False(t, completeCommittedRouting(halfPooler), "one-sided pooler cannot be committed")

	child := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "ns", UID: "child-uid", Generation: 2},
		Spec:       platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "parent"}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "orders"}}},
	}
	fingerprint := pgconninfo.FingerprintEndpointData(routing)
	bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		AttemptID: "attempt", EndpointIntentRevision: 3,
		Blue:                &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "blue", UID: "blue-uid"}},
		Green:               &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}},
		EndpointPreparation: &platformv1alpha1.BlueGreenEndpointPreparationStatus{Generation: 3, EndpointFingerprint: &fingerprint},
	}
	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "ns", UID: "parent-uid"}, Status: platformv1alpha1.PostgresClusterStatus{PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: bg}}}}
	_, _, err := preparationIntent(parent, bg, child)
	require.ErrorContains(t, err, "absent from endpoint preparation inventory")
	bg.EndpointPreparation.Acknowledgements = []platformv1alpha1.BlueGreenEndpointPreparationAcknowledgementStatus{{PostgresDatabaseName: child.Name, PostgresDatabaseUID: string(child.UID), PostgresDatabaseGeneration: child.Generation, DatabaseName: "orders"}}
	parent.Status.ProvisionerRef = &corev1.ObjectReference{Name: "green", UID: "green-uid"}
	_, _, err = preparationIntent(parent, bg, child)
	require.ErrorContains(t, err, "green provider selected before canonical commit")

	scheme := testScheme(t)
	c := testClient(t, scheme, child, parent)
	_, err = verifyEndpointConfigMaps(t.Context(), c, child, preparationOperation{endpointOperationIntent{routing: routing}})
	require.Error(t, err, "a missing database ConfigMap cannot prove preparation")
	rc := &ReconcileContext{Client: c, APIReader: c, Scheme: scheme}
	require.ErrorContains(t, validateEndpointWriterIntent(t.Context(), rc, child, preparationOperation{endpointOperationIntent{routing: routing, attemptID: "prior-attempt", generation: 3, fingerprint: fingerprint}}), "intent changed before ConfigMap mutation")
}

type finishedEndpointAttemptCase struct {
	name               string
	cleanupState       platformv1alpha1.BlueGreenCleanupState
	phase              string
	wantRelease        bool
	recreatedCanonical bool
}

type endpointChildCase struct {
	name         string
	childName    string
	childUID     types.UID
	generation   int64
	wantReleased bool
	withACK      bool
}

func assertFinishedEndpointChildRelease(t *testing.T, state finishedEndpointAttemptCase, tc endpointChildCase) {
	t.Helper()
	ctx := t.Context()
	scheme := testScheme(t)
	fingerprint := pgconninfo.FingerprintEndpointData(map[string]string{
		pgconninfo.KeyClusterRWEndpoint:  "green-rw.ns.svc.cluster.local",
		pgconninfo.KeyClusterROEndpoint:  "",
		pgconninfo.KeyClusterREndpoint:   "green-r.ns.svc.cluster.local",
		pgconninfo.KeyDefaultClusterPort: pgconninfo.DefaultPort,
	})
	available := metav1.Now()
	child := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: tc.childName, Namespace: "ns", UID: tc.childUID, Generation: tc.generation},
		Spec: platformv1alpha1.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "postgres"},
			Databases:  []platformv1alpha1.DatabaseDefinition{{Name: tc.childName}},
		},
	}
	if tc.withACK {
		ack := &platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{
			AttemptID: "attempt", IntentGeneration: 7, ObservedDatabaseGeneration: child.Generation, GreenClusterUID: "green-uid",
			Databases: []platformv1alpha1.DatabaseEndpointAcknowledgementStatus{{DatabaseName: "orders", EndpointFingerprint: fingerprint, ConfigMapUID: "db-cm-uid", AcknowledgedAt: &available}},
		}
		child.Status.EndpointPreparation = ack.DeepCopy()
		child.Status.EndpointPublication = ack.DeepCopy()
	}
	// The frozen cutover roster describes the old orders generation only.
	// A later child is managed by ordinary reconciliation after cutover.
	prepared := platformv1alpha1.BlueGreenEndpointPreparationAcknowledgementStatus{
		PostgresDatabaseName: "orders", PostgresDatabaseUID: "orders-uid", PostgresDatabaseGeneration: 3, DatabaseName: "orders",
	}
	published := platformv1alpha1.BlueGreenEndpointPublicationAcknowledgementStatus{
		PostgresDatabaseName: "orders", PostgresDatabaseUID: "orders-uid", PostgresDatabaseGeneration: 3, DatabaseName: "orders",
	}
	bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		AttemptID: "attempt", EndpointIntentRevision: 7,
		Green:               &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}},
		EndpointPreparation: &platformv1alpha1.BlueGreenEndpointPreparationStatus{Generation: 7, EndpointFingerprint: &fingerprint, Acknowledgements: []platformv1alpha1.BlueGreenEndpointPreparationAcknowledgementStatus{prepared}},
		EndpointPublication: &platformv1alpha1.BlueGreenEndpointPublicationStatus{Generation: 7, EndpointFingerprint: &fingerprint, Acknowledgements: []platformv1alpha1.BlueGreenEndpointPublicationAcknowledgementStatus{published}},
		CommitReceipt:       &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CanonicalConfigMap: &corev1.ObjectReference{Name: "postgres-connection", UID: "canonical-uid"}, Target: &corev1.ObjectReference{Name: "green", UID: "green-uid"}},
		Switchover:          &platformv1alpha1.BlueGreenSwitchoverStatus{TargetAvailableAt: &available},
		Cleanup:             &platformv1alpha1.BlueGreenCleanupStatus{State: state.cleanupState},
	}
	var completedAt *metav1.Time
	if state.phase == "Completed" {
		completedAt = &available
	}
	parent := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: "ns", UID: "parent-uid", ResourceVersion: "20"},
		Status: platformv1alpha1.PostgresClusterStatus{
			ProvisionerRef:             &corev1.ObjectReference{Name: "green", UID: "green-uid"},
			Resources:                  &platformv1alpha1.PostgresClusterResources{ConfigMapRef: &corev1.LocalObjectReference{Name: "postgres-connection"}},
			PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{Phase: &state.phase, CompletedAt: completedAt, BlueGreen: bg}},
		},
	}
	canonicalUID := types.UID("canonical-uid")
	if state.recreatedCanonical {
		canonicalUID = "recreated-canonical-uid"
	}
	canonical := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "postgres-connection", Namespace: "ns", UID: canonicalUID}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(child, parent, canonical).Build()
	ordinaryProvider := "blue"
	rc := &ReconcileContext{Client: c, APIReader: c, Scheme: scheme,
		ClusterReader: clusterReadinessReaderFunc(func(context.Context, string, string) (dbclusterinfo.ResolvedClusterFacts, error) {
			return endpointTestClusterFacts(parent, ordinaryProvider), nil
		}),
	}
	handled, _, err := reconcileEndpointProtocol(ctx, rc, child)
	require.True(t, handled, "a stale blue ordinary reader must remain fenced")
	if state.wantRelease {
		require.NoError(t, err)
	} else if state.recreatedCanonical {
		require.ErrorContains(t, err, "canonical endpoint ConfigMap UID changed after commit", "a recreated canonical ConfigMap must remain fenced before completion")
	}

	ordinaryProvider = "green"
	handled, _, err = reconcileEndpointProtocol(ctx, rc, child)
	wantRelease := state.wantRelease && tc.wantReleased || tc.withACK && state.phase == "Completed"
	require.Equal(t, !wantRelease, handled, "an original roster child requires ACK; a recreated canonical ConfigMap may release it only after completion")
	if state.wantRelease {
		require.NoError(t, err)
	}
}

func endpointTestClusterFacts(parent *platformv1alpha1.PostgresCluster, provider string) dbclusterinfo.ResolvedClusterFacts {
	card := testResolvedClusterCard(parent.Name, parent.Namespace, provider)
	if provider == "green" {
		card.Authoritative.Identity.UID = "green-uid"
	} else {
		card.Authoritative.Identity.UID = "blue-uid"
	}
	return dbclusterinfo.ResolvedClusterFacts{Cluster: card}
}

func assertHistoricalEndpointError(t *testing.T, err error, cleanup platformv1alpha1.BlueGreenCleanupState) {
	t.Helper()
	if cleanup == platformv1alpha1.BlueGreenCleanupStateRetained {
		require.ErrorContains(t, err, "no longer current")
	} else {
		require.NoError(t, err)
	}
}
