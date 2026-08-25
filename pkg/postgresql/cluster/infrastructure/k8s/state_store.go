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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClusterStateStore struct {
	client client.Client
	key    client.ObjectKey
}

func NewClusterStateStore(c client.Client, key client.ObjectKey) *ClusterStateStore {
	return &ClusterStateStore{client: c, key: key}
}

func (s *ClusterStateStore) GetSpecificationWithAnnotations(ctx context.Context) (*platformv1alpha1.PostgresClusterSpec, map[string]string, error) {
	cluster, err := s.getCluster(ctx)
	if err != nil {
		return nil, nil, err
	}
	return cluster.Spec.DeepCopy(), maps.Clone(cluster.Annotations), nil
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
	for k, v := range annotations {
		cluster.Annotations[k] = v
	}
	return s.client.Update(ctx, cluster)
}

func (s *ClusterStateStore) GetMajorUpgradeStatus(ctx context.Context) ([]platformv1alpha1.PostgresMajorUpgradeStatus, error) {
	cluster, err := s.getCluster(ctx)
	if err != nil {
		return nil, err
	}
	return append([]platformv1alpha1.PostgresMajorUpgradeStatus(nil), cluster.Status.PostgresMajorUpgradeStatus...), nil
}

func (s *ClusterStateStore) SetMajorUpgradeStatus(ctx context.Context, entries []platformv1alpha1.PostgresMajorUpgradeStatus) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("postgres cluster status client is not configured")
	}
	cluster, err := s.getCluster(ctx)
	if err != nil {
		return err
	}
	cluster.Status.PostgresMajorUpgradeStatus = append([]platformv1alpha1.PostgresMajorUpgradeStatus(nil), entries...)
	return s.client.Status().Update(ctx, cluster)
}

// GetSourcePgVersion reports the PostgreSQL major version currently running, read
// straight from the live CNPG Cluster (Status.PGDataImageInfo.MajorVersion)
func (s *ClusterStateStore) GetSourcePgVersion(ctx context.Context) (string, error) {
	if s == nil || s.client == nil {
		return "", fmt.Errorf("postgres cluster source version reader is not configured")
	}
	cnpgCluster := &cnpgv1.Cluster{}
	if err := s.client.Get(ctx, s.key, cnpgCluster); err != nil {
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
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("postgres cluster status client is not configured")
	}
	cluster := &platformv1alpha1.PostgresCluster{}
	if err := s.client.Get(ctx, s.key, cluster); err != nil {
		return nil, err
	}
	return cluster, nil
}
