/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package v3

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/conversion"

	hubApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

var _ conversion.Convertible = &IndexerCluster{}

// ConvertTo converts v3 IndexerCluster to the hub
func (c *IndexerCluster) ConvertTo(dstHub conversion.Hub) error {
	dst, ok := dstHub.(*hubApi.IndexerCluster)
	if !ok {
		return fmt.Errorf("unsupported conversion hub for IndexerCluster: %T", dstHub)
	}

	dst.ObjectMeta = c.ObjectMeta
	dst.Spec.CommonSplunkSpec = c.Spec.CommonSplunkSpec
	dst.Spec.Replicas = c.Spec.Replicas

	dst.Status = hubApi.IndexerClusterStatus{
		Phase:                          c.Status.Phase,
		ClusterMasterPhase:             c.Status.ClusterMasterPhase,
		ClusterManagerPhase:            c.Status.ClusterManagerPhase,
		Replicas:                       c.Status.Replicas,
		ReadyReplicas:                  c.Status.ReadyReplicas,
		Selector:                       c.Status.Selector,
		Initialized:                    c.Status.Initialized,
		IndexingReady:                  c.Status.IndexingReady,
		ServiceReady:                   c.Status.ServiceReady,
		IndexerSecretChanged:           c.Status.IndexerSecretChanged,
		NamespaceSecretResourceVersion: c.Status.NamespaceSecretResourceVersion,
		IdxcPasswordChangedSecrets:     c.Status.IdxcPasswordChangedSecrets,
		MaintenanceMode:                c.Status.MaintenanceMode,
		Peers:                          convertPeersTo(c.Status.Peers),
	}

	return nil
}

// ConvertFrom converts the hub into a v3 IndexerCluster
func (c *IndexerCluster) ConvertFrom(srcHub conversion.Hub) error {
	src, ok := srcHub.(*hubApi.IndexerCluster)
	if !ok {
		return fmt.Errorf("unsupported conversion hub for IndexerCluster: %T", srcHub)
	}

	if err := refuseIfUnrepresentable("IndexerCluster", src.Namespace, src.Name,
		unrepresentableIndexerClusterFields(&src.Spec)); err != nil {
		return err
	}

	c.ObjectMeta = src.ObjectMeta
	c.Spec.CommonSplunkSpec = src.Spec.CommonSplunkSpec
	c.Spec.Replicas = src.Spec.Replicas

	c.Status = IndexerClusterStatus{
		Phase:                          src.Status.Phase,
		ClusterMasterPhase:             src.Status.ClusterMasterPhase,
		ClusterManagerPhase:            src.Status.ClusterManagerPhase,
		Replicas:                       src.Status.Replicas,
		ReadyReplicas:                  src.Status.ReadyReplicas,
		Selector:                       src.Status.Selector,
		Initialized:                    src.Status.Initialized,
		IndexingReady:                  src.Status.IndexingReady,
		ServiceReady:                   src.Status.ServiceReady,
		IndexerSecretChanged:           src.Status.IndexerSecretChanged,
		NamespaceSecretResourceVersion: src.Status.NamespaceSecretResourceVersion,
		IdxcPasswordChangedSecrets:     src.Status.IdxcPasswordChangedSecrets,
		MaintenanceMode:                src.Status.MaintenanceMode,
		Peers:                          convertPeersFrom(src.Status.Peers),
	}

	return nil
}

func convertPeersTo(peers []IndexerClusterMemberStatus) []hubApi.IndexerClusterMemberStatus {
	if peers == nil {
		return nil
	}
	converted := make([]hubApi.IndexerClusterMemberStatus, 0, len(peers))
	for _, peer := range peers {
		converted = append(converted, hubApi.IndexerClusterMemberStatus{
			ID:             peer.ID,
			Name:           peer.Name,
			Status:         peer.Status,
			ActiveBundleID: peer.ActiveBundleID,
			BucketCount:    peer.BucketCount,
			Searchable:     peer.Searchable,
		})
	}
	return converted
}

func convertPeersFrom(peers []hubApi.IndexerClusterMemberStatus) []IndexerClusterMemberStatus {
	if peers == nil {
		return nil
	}
	converted := make([]IndexerClusterMemberStatus, 0, len(peers))
	for _, peer := range peers {
		converted = append(converted, IndexerClusterMemberStatus{
			ID:             peer.ID,
			Name:           peer.Name,
			Status:         peer.Status,
			ActiveBundleID: peer.ActiveBundleID,
			BucketCount:    peer.BucketCount,
			Searchable:     peer.Searchable,
		})
	}
	return converted
}
