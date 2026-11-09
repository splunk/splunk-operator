package cnpg

import (
	"context"
	"errors"
	"strings"

	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func candidateTestObjects(names ...string) []client.Object {
	objects := make([]client.Object, 0, len(names)*2)
	for _, name := range names {
		selector := map[string]string{"cnpg.io/cluster": "green"}
		port := corev1.ServicePort{Name: "postgres", Port: 5432, TargetPort: intstr.FromInt32(5432), Protocol: corev1.ProtocolTCP}
		switch {
		case strings.Contains(name, "-pooler-"):
			selector = map[string]string{"cnpg.io/poolerName": name}
			port = corev1.ServicePort{Name: "pgbouncer", Port: 5432, TargetPort: intstr.FromString("pgbouncer"), Protocol: corev1.ProtocolTCP}
		case strings.HasSuffix(name, "-rw"):
			selector["cnpg.io/instanceRole"] = "primary"
		case strings.HasSuffix(name, "-ro"):
			selector["cnpg.io/instanceRole"] = "replica"
		default:
			selector["cnpg.io/podRole"] = "instance"
		}
		service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", UID: types.UID(name + "-uid")}, Spec: corev1.ServiceSpec{Selector: selector, Ports: []corev1.ServicePort{port}}}
		objects = append(objects, service)
		if strings.Contains(name, "-pooler-") {
			side := cnpgv1.PoolerTypeRW
			if strings.HasSuffix(name, "-ro") {
				side = cnpgv1.PoolerTypeRO
			}
			pooler, deployment := readyPoolerWorkload(name, "ns", side)
			service.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(pooler, cnpgv1.SchemeGroupVersion.WithKind("Pooler"))}
			objects = append(objects, pooler, deployment)
		}
	}
	return objects
}

func candidateTestReader(t *testing.T, objects ...client.Object) client.Reader {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func candidateTestCluster(instances int) *cnpgv1.Cluster {
	return &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "green", Namespace: "ns", UID: "green-uid"},
		Status: cnpgv1.ClusterStatus{
			Phase: cnpgv1.PhaseHealthy, ReadyInstances: instances,
			WriteService: "green-rw", ReadService: "green-r",
		},
	}
}

func candidateTestRefs(pooler string) *platformv1alpha1.BlueGreenCandidateEndpointsStatus {
	refs := &platformv1alpha1.BlueGreenCandidateEndpointsStatus{
		DirectService: &corev1.ObjectReference{Name: "green-rw", Namespace: "ns", UID: "green-rw-uid"},
	}
	if pooler != "" {
		refs.PoolerService = &corev1.ObjectReference{Name: pooler, Namespace: "ns", UID: types.UID(pooler + "-uid")}
	}
	return refs
}

func TestResolveCandidateConnectionDataValidatesReadyDirectServices(t *testing.T) {
	objects := candidateTestObjects("green-rw", "green-r")
	reader := candidateTestReader(t, objects...)
	data, err := ResolveCandidateConnectionData(t.Context(), reader, candidateTestCluster(1), candidateTestRefs(""), CandidatePoolerExpectation{})
	require.NoError(t, err)
	assert.Equal(t, "green-rw.ns.svc.cluster.local", data[pgconninfo.KeyClusterRWEndpoint])
	assert.Equal(t, "", data[pgconninfo.KeyClusterROEndpoint])
	assert.Equal(t, pgconninfo.DefaultPort, data[pgconninfo.KeyDefaultClusterPort])

	terminating := candidateTestObjects("green-rw", "green-r")
	now := metav1.Now()
	terminating[0].SetDeletionTimestamp(&now)
	terminating[0].SetFinalizers([]string{"test"})
	_, err = ResolveCandidateConnectionData(t.Context(), candidateTestReader(t, terminating...), candidateTestCluster(1), candidateTestRefs(""), CandidatePoolerExpectation{})
	require.ErrorContains(t, err, "terminating")

	wrongSelector := candidateTestObjects("green-rw", "green-r")
	wrongSelector[0].(*corev1.Service).Spec.Selector["cnpg.io/cluster"] = "blue"
	_, err = ResolveCandidateConnectionData(t.Context(), candidateTestReader(t, wrongSelector...), candidateTestCluster(1), candidateTestRefs(""), CandidatePoolerExpectation{})
	require.ErrorContains(t, err, "does not target expected backend")

	wrongPort := candidateTestObjects("green-rw", "green-r")
	wrongPort[0].(*corev1.Service).Spec.Ports[0].Port = 5433
	_, err = ResolveCandidateConnectionData(t.Context(), candidateTestReader(t, wrongPort...), candidateTestCluster(1), candidateTestRefs(""), CandidatePoolerExpectation{})
	require.ErrorContains(t, err, "expected PostgreSQL port")

	wrongUID := candidateTestRefs("")
	wrongUID.DirectService.UID = "other-uid"
	_, err = ResolveCandidateConnectionData(t.Context(), reader, candidateTestCluster(1), wrongUID, CandidatePoolerExpectation{})
	require.ErrorContains(t, err, "does not match candidate identity")
}

func TestResolveCandidateConnectionDataPreservesEnabledPoolerKeysWhenNoSideIsRoutable(t *testing.T) {
	reader := candidateTestReader(t, candidateTestObjects("green-rw", "green-r")...)
	data, err := ResolveCandidateConnectionData(t.Context(), reader, candidateTestCluster(1), candidateTestRefs(""), CandidatePoolerExpectation{Enabled: true})
	require.NoError(t, err)
	rw, rwPresent := data[pgconninfo.KeyPoolerRWEndpoint]
	ro, roPresent := data[pgconninfo.KeyPoolerROEndpoint]
	require.True(t, rwPresent)
	require.True(t, roPresent)
	assert.Empty(t, rw)
	assert.Empty(t, ro)
}

func TestResolveCandidateConnectionDataRequiresExpectedPoolerSides(t *testing.T) {
	objects := candidateTestObjects("green-rw", "green-r", "green-ro", "green-pooler-rw", "green-pooler-ro")

	reader := candidateTestReader(t, objects...)
	green := candidateTestCluster(2)

	t.Run("RW only", func(t *testing.T) {
		data, err := ResolveCandidateConnectionData(t.Context(), reader, green, candidateTestRefs("green-pooler-rw"), CandidatePoolerExpectation{RWEnabled: true})
		require.NoError(t, err)
		assert.NotEmpty(t, data[pgconninfo.KeyPoolerRWEndpoint])
		assert.Empty(t, data[pgconninfo.KeyPoolerROEndpoint])
	})
	t.Run("RO only", func(t *testing.T) {
		data, err := ResolveCandidateConnectionData(t.Context(), reader, green, candidateTestRefs("green-pooler-ro"), CandidatePoolerExpectation{ROEnabled: true})
		require.NoError(t, err)
		assert.Empty(t, data[pgconninfo.KeyPoolerRWEndpoint])
		assert.NotEmpty(t, data[pgconninfo.KeyPoolerROEndpoint])
	})
	t.Run("reject misdirected Pooler", func(t *testing.T) {
		wrong := make([]client.Object, 0, len(objects))
		for _, obj := range objects {
			if p, ok := obj.(*cnpgv1.Pooler); ok && p.Name == "green-pooler-rw" {
				copy := p.DeepCopy()
				copy.Spec.Cluster.Name = "blue"
				wrong = append(wrong, copy)
			} else {
				wrong = append(wrong, obj)
			}
		}
		_, err := ResolveCandidateConnectionData(t.Context(), candidateTestReader(t, wrong...), green, candidateTestRefs("green-pooler-rw"), CandidatePoolerExpectation{RWEnabled: true})
		require.ErrorContains(t, err, "different Cluster or service side")
	})
	t.Run("both requires RO", func(t *testing.T) {
		missingRO := make([]client.Object, 0, len(objects))
		for _, obj := range objects {
			if obj.GetName() != "green-pooler-ro" {
				missingRO = append(missingRO, obj)
			}
		}
		_, err := ResolveCandidateConnectionData(t.Context(), candidateTestReader(t, missingRO...), green, candidateTestRefs("green-pooler-rw"), CandidatePoolerExpectation{RWEnabled: true, ROEnabled: true})
		require.ErrorContains(t, err, "candidate Pooler")
	})
}

func TestResolveCandidateConnectionDataRequiresReadOnlyServiceAtTwoInstances(t *testing.T) {
	green := candidateTestCluster(2)
	refs := candidateTestRefs("")
	_, err := ResolveCandidateConnectionData(t.Context(), candidateTestReader(t, candidateTestObjects("green-rw", "green-r")...), green, refs, CandidatePoolerExpectation{})
	require.ErrorContains(t, err, "candidate read-only Service")

	reader := candidateTestReader(t, candidateTestObjects("green-rw", "green-r", "green-ro")...)
	data, err := ResolveCandidateConnectionData(t.Context(), reader, green, refs, CandidatePoolerExpectation{})
	require.NoError(t, err)
	assert.Equal(t, "green-r.ns.svc.cluster.local", data[pgconninfo.KeyClusterROEndpoint], "cutover preserves the existing Status.ReadService routing contract")

	refs.PoolerService = &corev1.ObjectReference{Name: "green-pooler-rw", UID: "pooler-uid"}
	_, err = ResolveCandidateConnectionData(t.Context(), reader, green, refs, CandidatePoolerExpectation{})
	require.ErrorContains(t, err, "unexpected Pooler Service reference")

	refs.PoolerService = nil
	_, err = ResolveCandidateConnectionData(t.Context(), reader, green, refs, CandidatePoolerExpectation{RWEnabled: true})
	require.ErrorContains(t, err, "does not match expected topology")

	refs.PoolerService = &corev1.ObjectReference{Name: "green-pooler-ro", UID: "pooler-uid"}
	_, err = ResolveCandidateConnectionData(t.Context(), reader, green, refs, CandidatePoolerExpectation{RWEnabled: true})
	require.ErrorContains(t, err, "does not match expected topology")
}

func TestResolveCandidateConnectionDataRejectsUnreadyPoolerDeployment(t *testing.T) {
	objects := candidateTestObjects("green-rw", "green-r", "green-pooler-rw")
	deployment := objects[4].(*appsv1.Deployment)
	deployment.Status.ReadyReplicas = 0
	deployment.Status.AvailableReplicas = 0
	_, err := ResolveCandidateConnectionData(t.Context(), candidateTestReader(t, objects...), candidateTestCluster(1), candidateTestRefs("green-pooler-rw"), CandidatePoolerExpectation{RWEnabled: true})
	require.ErrorContains(t, err, "Deployment is not ready")

	objects = candidateTestObjects("green-rw", "green-r", "green-pooler-rw")
	objects[2].(*corev1.Service).UID = "replacement-uid"
	_, err = ResolveCandidateConnectionData(t.Context(), candidateTestReader(t, objects...), candidateTestCluster(1), candidateTestRefs("green-pooler-rw"), CandidatePoolerExpectation{RWEnabled: true})
	require.ErrorContains(t, err, "does not match candidate identity")

	objects = candidateTestObjects("green-rw", "green-r", "green-pooler-rw")
	_, err = ResolveCandidateConnectionData(t.Context(), candidateTestReader(t, objects[:4]...), candidateTestCluster(1), candidateTestRefs("green-pooler-rw"), CandidatePoolerExpectation{RWEnabled: true})
	require.ErrorContains(t, err, "Deployment is not ready")
}

func TestResolveCandidateConnectionDataRejectsIncompleteProofAndReaderErrors(t *testing.T) {
	ctx := t.Context()
	objects := candidateTestObjects("green-rw", "green-r")
	reader := candidateTestReader(t, objects...)
	green := candidateTestCluster(1)
	refs := candidateTestRefs("")

	_, err := ResolveCandidateConnectionData(ctx, nil, green, refs, CandidatePoolerExpectation{})
	require.ErrorContains(t, err, "reader, green identity")
	withoutUID := green.DeepCopy()
	withoutUID.UID = ""
	_, err = ResolveCandidateConnectionData(ctx, reader, withoutUID, refs, CandidatePoolerExpectation{})
	require.ErrorContains(t, err, "green identity")

	unhealthy := green.DeepCopy()
	unhealthy.Status.Phase = cnpgv1.PhaseUnrecoverable
	_, err = ResolveCandidateConnectionData(ctx, reader, unhealthy, refs, CandidatePoolerExpectation{})
	require.ErrorContains(t, err, "not healthy")

	noReadyInstances := green.DeepCopy()
	noReadyInstances.Status.ReadyInstances = 0
	_, err = ResolveCandidateConnectionData(ctx, reader, noReadyInstances, refs, CandidatePoolerExpectation{})
	require.ErrorContains(t, err, "not healthy")
	deleting := green.DeepCopy()
	now := metav1.Now()
	deleting.DeletionTimestamp = &now
	_, err = ResolveCandidateConnectionData(ctx, reader, deleting, refs, CandidatePoolerExpectation{})
	require.ErrorContains(t, err, "not healthy")

	wrongWriteService := candidateTestRefs("")
	wrongWriteService.DirectService.Name = "other-rw"
	_, err = ResolveCandidateConnectionData(ctx, reader, green, wrongWriteService, CandidatePoolerExpectation{})
	require.ErrorContains(t, err, "does not match green write Service")

	_, err = ResolveCandidateConnectionData(ctx, candidateTestReader(t, objects[:1]...), green, refs, CandidatePoolerExpectation{})
	require.ErrorContains(t, err, "candidate read Service")

	noServiceUID := candidateTestObjects("green-rw", "green-r")
	noServiceUID[0].(*corev1.Service).UID = ""
	_, err = ResolveCandidateConnectionData(ctx, candidateTestReader(t, noServiceUID...), green, refs, CandidatePoolerExpectation{})
	require.ErrorContains(t, err, "has no UID")

	poolerObjects := candidateTestObjects("green-rw", "green-r", "green-pooler-rw")
	_, err = ResolveCandidateConnectionData(ctx, &failingDeploymentReader{Reader: candidateTestReader(t, poolerObjects...)}, green, candidateTestRefs("green-pooler-rw"), CandidatePoolerExpectation{RWEnabled: true})
	require.ErrorContains(t, err, "reading Pooler Deployment")
	require.True(t, apierrors.IsForbidden(err))

	noReadService := candidateTestCluster(2)
	noReadService.Status.ReadService = ""
	_, err = ResolveCandidateConnectionData(ctx, reader, noReadService, refs, CandidatePoolerExpectation{})
	require.ErrorContains(t, err, "read Service does not match the CNPG read Service")
}

func TestResolveCandidateConnectionDataMatchesCNPGServiceContracts(t *testing.T) {
	green := candidateTestCluster(3)
	green.Spec.ImageName = "postgres:18"
	// CNPG v1.30.1 pkg/specs/services.go builds -rw with instanceRole=primary,
	// -r with podRole=instance, and -ro with instanceRole=replica. Its status
	// updater independently records ReadService=-r, not the replica-only name.
	objects := candidateTestObjects("green-rw", "green-r", "green-ro")
	data, err := ResolveCandidateConnectionData(t.Context(), candidateTestReader(t, objects...), green, candidateTestRefs(""), CandidatePoolerExpectation{})
	require.NoError(t, err, "CNPG Status.ReadService names -r while -ro independently targets replicas")
	assert.Equal(t, "green-rw.ns.svc.cluster.local", data[pgconninfo.KeyClusterRWEndpoint])
	assert.Equal(t, "green-r.ns.svc.cluster.local", data[pgconninfo.KeyClusterREndpoint])
	assert.Equal(t, "green-r.ns.svc.cluster.local", data[pgconninfo.KeyClusterROEndpoint], "existing publication bytes remain unchanged")

	for _, serviceName := range []string{"green-rw", "green-r", "green-ro"} {
		t.Run("reject wrong backend for "+serviceName, func(t *testing.T) {
			misdirected := make([]client.Object, 0, len(objects))
			for _, object := range objects {
				copy := object.DeepCopyObject().(client.Object)
				if service, ok := copy.(*corev1.Service); ok && service.Name == serviceName {
					service.Spec.Selector = map[string]string{"cnpg.io/cluster": "green"}
				}
				misdirected = append(misdirected, copy)
			}
			_, err := ResolveCandidateConnectionData(t.Context(), candidateTestReader(t, misdirected...), green, candidateTestRefs(""), CandidatePoolerExpectation{})
			require.ErrorContains(t, err, "does not target expected backend")
		})
		t.Run("reject missing Service for "+serviceName, func(t *testing.T) {
			missing := make([]client.Object, 0, len(objects))
			for _, object := range objects {
				if object.GetName() != serviceName {
					missing = append(missing, object)
				}
			}
			_, err := ResolveCandidateConnectionData(t.Context(), candidateTestReader(t, missing...), green, candidateTestRefs(""), CandidatePoolerExpectation{})
			require.ErrorContains(t, err, "reading Service")
		})
	}

	wrongStatus := green.DeepCopy()
	wrongStatus.Status.ReadService = green.GetServiceReadOnlyName()
	_, err = ResolveCandidateConnectionData(t.Context(), candidateTestReader(t, objects...), wrongStatus, candidateTestRefs(""), CandidatePoolerExpectation{})
	require.ErrorContains(t, err, "read Service does not match the CNPG read Service")
}

type failingDeploymentReader struct{ client.Reader }

func (r *failingDeploymentReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*appsv1.Deployment); ok {
		return apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, key.Name, errors.New("read denied"))
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}
