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

package k8s

import (
	"context"
	"fmt"
	"maps"
	"slices"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClusterStateStore struct {
	client client.Client
	reader client.Reader
	key    client.ObjectKey
}

// NewClusterStateStore uses the same client for reads and writes. Call
// NewClusterStateStoreWithReader when safety reads require an uncached reader.
func NewClusterStateStore(writer client.Client, key client.ObjectKey) (*ClusterStateStore, error) {
	return NewClusterStateStoreWithReader(writer, writer, key)
}

// NewClusterStateStoreWithReader keeps intent and conflict-retry safety reads
// outside the informer cache while retaining the ordinary client for writes.
func NewClusterStateStoreWithReader(writer client.Client, reader client.Reader, key client.ObjectKey) (*ClusterStateStore, error) {
	if writer == nil || reader == nil || key.Name == "" || key.Namespace == "" {
		return nil, fmt.Errorf("postgres cluster state store requires writer, reader and namespaced parent key")
	}
	return &ClusterStateStore{client: writer, reader: reader, key: key}, nil
}

func (s *ClusterStateStore) GetSpecificationWithAnnotations(ctx context.Context) (*platformv1alpha1.PostgresClusterSpec, map[string]string, error) {
	cluster, err := s.getCluster(ctx)
	if err != nil {
		return nil, nil, err
	}
	return cluster.Spec.DeepCopy(), maps.Clone(cluster.Annotations), nil
}

func (s *ClusterStateStore) GetGeneration(ctx context.Context) (int64, error) {
	cluster, err := s.getCluster(ctx)
	if err != nil {
		return 0, err
	}
	return cluster.Generation, nil
}

// GetReadyToStart checks the authoritative environment before the upgrade
// use case can block normal source reconciliation.
func (s *ClusterStateStore) GetReadyToStart(ctx context.Context) (bool, error) {
	cluster, err := s.getCluster(ctx)
	if err != nil {
		return false, err
	}
	if cluster.Status.Phase == nil || *cluster.Status.Phase != "Ready" ||
		cluster.Status.ObservedGeneration == nil || *cluster.Status.ObservedGeneration != cluster.Generation ||
		!meta.IsStatusConditionTrue(cluster.Status.Conditions, "Ready") {
		return false, nil
	}
	key := s.key
	if ref := cluster.Status.ProvisionerRef; ref != nil && ref.Name != "" {
		key = client.ObjectKey{Namespace: cluster.Namespace, Name: ref.Name}
	}
	source := &cnpgv1.Cluster{}
	if err := s.reader.Get(ctx, key, source); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if cluster.Status.ProvisionerRef != nil && cluster.Status.ProvisionerRef.UID != "" && source.UID != cluster.Status.ProvisionerRef.UID {
		return false, nil
	}
	return source.Status.Phase == cnpgv1.PhaseHealthy && source.Spec.Instances > 0 &&
		source.Status.Instances == source.Spec.Instances && source.Status.ReadyInstances == source.Spec.Instances &&
		source.Status.CurrentPrimary != "" &&
		len(source.Status.InstancesStatus[cnpgv1.PodHealthy]) == source.Spec.Instances &&
		slices.Contains(source.Status.InstancesStatus[cnpgv1.PodHealthy], source.Status.CurrentPrimary), nil
}

func (s *ClusterStateStore) SetAnnotations(ctx context.Context, annotations map[string]string) error {
	cluster, err := s.getCluster(ctx)
	if err != nil {
		return err
	}
	if len(annotations) == 0 {
		return nil
	}
	if cluster.Annotations == nil {
		cluster.Annotations = map[string]string{}
	}
	maps.Copy(cluster.Annotations, annotations)
	return s.client.Update(ctx, cluster)
}

func (s *ClusterStateStore) GetMajorUpgradeStatus(ctx context.Context) ([]platformv1alpha1.PostgresMajorUpgradeStatus, error) {
	cluster, err := s.getCluster(ctx)
	if err != nil {
		return nil, err
	}
	return append([]platformv1alpha1.PostgresMajorUpgradeStatus(nil), cluster.Status.PostgresMajorUpgradeStatus...), nil
}

// UpdateMajorUpgradeStatus binds the merge decision to the parent resource
// version being written. A concurrent coordinator status write causes the
// merge to run again against its durable result rather than replacing it.
func (s *ClusterStateStore) UpdateMajorUpgradeStatus(ctx context.Context, update func([]platformv1alpha1.PostgresMajorUpgradeStatus) ([]platformv1alpha1.PostgresMajorUpgradeStatus, error)) error {
	if update == nil {
		return fmt.Errorf("major upgrade status update is not configured")
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cluster, err := s.getCluster(ctx)
		if err != nil {
			return err
		}
		entries, err := update(cluster.Status.PostgresMajorUpgradeStatus)
		if err != nil {
			return err
		}
		cluster.Status.PostgresMajorUpgradeStatus = entries
		return s.client.Status().Update(ctx, cluster)
	})
}

// GetSourcePgVersion reports the PostgreSQL major version currently running, read
// straight from the live CNPG Cluster (Status.PGDataImageInfo.MajorVersion)
func (s *ClusterStateStore) GetSourcePgVersion(ctx context.Context) (string, error) {
	if s == nil || s.client == nil || s.reader == nil {
		return "", fmt.Errorf("postgres cluster source version reader is not configured")
	}
	cnpgCluster := &cnpgv1.Cluster{}
	if err := s.reader.Get(ctx, s.key, cnpgCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	info := cnpgCluster.Status.PGDataImageInfo
	if info == nil || info.MajorVersion <= 0 {
		return "", nil
	}
	return fmt.Sprintf("%d", info.MajorVersion), nil
}

func (s *ClusterStateStore) getCluster(ctx context.Context) (*platformv1alpha1.PostgresCluster, error) {
	if s == nil || s.client == nil || s.reader == nil {
		return nil, fmt.Errorf("postgres cluster status client is not configured")
	}
	cluster := &platformv1alpha1.PostgresCluster{}
	if err := s.reader.Get(ctx, s.key, cluster); err != nil {
		return nil, err
	}
	return cluster, nil
}
