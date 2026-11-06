package majorupgradeadapter

import (
	"context"
	"fmt"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	endpointpublication "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/endpoint_publication"
	pgcnpg "github.com/splunk/splunk-operator/pkg/postgresql/shared/cnpg"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// EndpointCoordinatorStore uses the uncached API reader for every safety
// decision. Only status writes use the cached client and Kubernetes CAS.
type EndpointCoordinatorStore struct {
	Writer client.Client
	Reader client.Reader
}

func NewEndpointCoordinator(writer client.Client, reader client.Reader) (*endpointpublication.Coordinator, error) {
	if writer == nil || reader == nil {
		return nil, fmt.Errorf("endpoint coordinator requires a writer and reader")
	}
	return endpointpublication.NewCoordinator(&EndpointCoordinatorStore{Writer: writer, Reader: reader}, &EndpointProofResolver{Reader: reader},
		func(_ context.Context, _ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, now time.Time) error {
			return mvutypes.AuthorizeCutoverCommit(bg, now)
		}, time.Now)
}

// EndpointProofResolver resolves both routing payloads from live CNPG state.
// Green additionally requires the shared candidate Service and Pooler Deployment
// validation. Blue needs only its immutable UID and source endpoint facts so
// cancellation can proceed after a green Service failure.
type EndpointProofResolver struct{ Reader client.Reader }

func (r *EndpointProofResolver) Resolve(ctx context.Context, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (endpointpublication.Proof, error) {
	blue, err := r.ResolveBlue(ctx, parent, bg)
	if err != nil {
		return endpointpublication.Proof{}, err
	}
	greenData, greenUID, err := r.ResolveGreen(ctx, parent, bg)
	if err != nil {
		return endpointpublication.Proof{}, err
	}
	access, err := r.resolveAccess(ctx, parent, bg.Green)
	if err != nil {
		return endpointpublication.Proof{}, err
	}
	return endpointpublication.Proof{Blue: blue, Green: greenData, GreenUID: greenUID, GreenAccess: access}, nil
}

func (r *EndpointProofResolver) ResolveBlueAccess(ctx context.Context, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (map[string]string, error) {
	return r.resolveAccess(ctx, parent, bg.Blue)
}

func (r *EndpointProofResolver) resolveAccess(ctx context.Context, parent *platformv1alpha1.PostgresCluster, environment *platformv1alpha1.BlueGreenEnvironmentStatus) (map[string]string, error) {
	if parent == nil || environment == nil || environment.Ref.Name == "" || environment.Ref.UID == "" {
		return nil, fmt.Errorf("cluster access metadata requires an immutable environment identity")
	}
	cluster := &cnpgv1.Cluster{}
	if err := r.Reader.Get(ctx, types.NamespacedName{Namespace: parent.Namespace, Name: environment.Ref.Name}, cluster); err != nil {
		return nil, err
	}
	if cluster.UID != environment.Ref.UID || cluster.GetSuperuserSecretName() == "" {
		return nil, fmt.Errorf("cluster access metadata identity or superuser selector changed")
	}
	ca := cluster.Status.Certificates.ServerCASecret
	if ca == "" && cluster.Spec.Certificates != nil {
		ca = cluster.Spec.Certificates.ServerCASecret
	}
	if ca == "" {
		return nil, fmt.Errorf("cluster access metadata CA selector is missing")
	}
	return pgconninfo.ClusterEnvironmentAccess(cluster.GetSuperuserSecretName(), ca), nil
}

func (r *EndpointProofResolver) ResolveGreen(ctx context.Context, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (map[string]string, types.UID, error) {
	if r == nil || r.Reader == nil || parent == nil || bg == nil {
		return nil, "", fmt.Errorf("green endpoint proof requires parent, attempt and API reader")
	}
	if bg.Green == nil || bg.Green.Ref.Name == "" || bg.Green.Ref.UID == "" {
		return nil, "", fmt.Errorf("green CNPG Cluster identity is missing")
	}
	green := &cnpgv1.Cluster{}
	if err := r.Reader.Get(ctx, types.NamespacedName{Namespace: parent.Namespace, Name: bg.Green.Ref.Name}, green); err != nil {
		return nil, "", err
	}
	if green.UID != bg.Green.Ref.UID || green.Status.Phase != cnpgv1.PhaseHealthy || green.Status.ReadyInstances == 0 || green.Status.ReadyInstances < green.Spec.Instances {
		return nil, "", fmt.Errorf("green CNPG Cluster identity or health changed")
	}
	var pooler pgcnpg.CandidatePoolerExpectation
	if parent.Status.ConnectionPoolerStatus != nil && parent.Status.ConnectionPoolerStatus.Enabled {
		pooler.Enabled = true
		pooler.RWEnabled = parent.Status.ConnectionPoolerStatus.ReadWriteEnabled
		pooler.ROEnabled = parent.Status.ConnectionPoolerStatus.ReadOnlyEnabled
	}
	greenData, err := pgcnpg.ResolveCandidateConnectionData(ctx, r.Reader, green, bg.CandidateEndpoints, pooler)
	if err != nil {
		return nil, "", err
	}
	return greenData, green.UID, nil
}

func (r *EndpointProofResolver) ResolveBlue(ctx context.Context, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (map[string]string, error) {
	if r == nil || r.Reader == nil || parent == nil || bg == nil || bg.Blue == nil || bg.Blue.Ref.Name == "" || bg.Blue.Ref.UID == "" {
		return nil, fmt.Errorf("blue CNPG Cluster identity and API reader are required")
	}
	blue := &cnpgv1.Cluster{}
	if err := r.Reader.Get(ctx, types.NamespacedName{Namespace: parent.Namespace, Name: bg.Blue.Ref.Name}, blue); err != nil {
		return nil, err
	}
	if blue.UID != bg.Blue.Ref.UID {
		return nil, fmt.Errorf("blue CNPG Cluster UID changed")
	}
	var pooler pgcnpg.PoolerAvailability
	if parent.Status.ConnectionPoolerStatus != nil && parent.Status.ConnectionPoolerStatus.Enabled {
		pooler.Enabled = true
		pooler.RWReady = parent.Status.ConnectionPoolerStatus.ReadWriteEnabled
		pooler.ROReady = parent.Status.ConnectionPoolerStatus.ReadOnlyEnabled
	}
	endpoints, err := pgcnpg.ResolveConnectionEndpoints(blue.Name, parent.Namespace, blue.Status.WriteService, blue.Status.ReadService, blue.Status.ReadyInstances, pooler)
	if err != nil {
		return nil, err
	}
	return pgconninfo.RoutingData(endpoints)
}

func (s *EndpointCoordinatorStore) ReadParent(ctx context.Context, key types.NamespacedName) (*platformv1alpha1.PostgresCluster, error) {
	parent := &platformv1alpha1.PostgresCluster{}
	if err := s.Reader.Get(ctx, key, parent); err != nil {
		return nil, err
	}
	return parent, nil
}

func (s *EndpointCoordinatorStore) ListChildren(ctx context.Context, namespace string) ([]platformv1alpha1.PostgresDatabase, error) {
	list := &platformv1alpha1.PostgresDatabaseList{}
	if err := s.Reader.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (s *EndpointCoordinatorStore) ReadConfigMap(ctx context.Context, key types.NamespacedName) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{}
	if err := s.Reader.Get(ctx, key, cm); err != nil {
		return nil, err
	}
	return cm, nil
}

func (s *EndpointCoordinatorStore) UpdateConfigMap(ctx context.Context, cm *corev1.ConfigMap) error {
	return s.Writer.Update(ctx, cm)
}

func (s *EndpointCoordinatorStore) CreateConfigMap(ctx context.Context, cm *corev1.ConfigMap) error {
	return s.Writer.Create(ctx, cm)
}

func (s *EndpointCoordinatorStore) WriteParentStatus(ctx context.Context, parent *platformv1alpha1.PostgresCluster) error {
	return s.Writer.Status().Update(ctx, parent)
}
