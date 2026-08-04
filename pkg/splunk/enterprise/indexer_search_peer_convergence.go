// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"context"
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var checkIndexerSearchPeerConvergence = func(
	ctx context.Context,
	mgr *indexerClusterPodManager,
	replacement *corev1.Pod,
) (bool, bool, string, error) {
	return mgr.indexerSearchPeerConvergenceObserved(ctx, replacement)
}

func (mgr *indexerClusterPodManager) indexerSearchPeerConvergenceObserved(
	ctx context.Context,
	replacement *corev1.Pod,
) (bool, bool, string, error) {
	if replacement == nil {
		return true, false, "replacement Pod is unavailable", nil
	}
	searchHeadClusters := &enterpriseApi.SearchHeadClusterList{}
	if err := mgr.c.List(
		ctx,
		searchHeadClusters,
		client.InNamespace(mgr.cr.GetNamespace()),
	); err != nil {
		return true, false, "", fmt.Errorf(
			"list SearchHeadClusters for Indexer peer convergence: %w",
			err,
		)
	}

	managerName := mgr.cr.Spec.ClusterManagerRef.Name
	if managerName == "" {
		managerName = mgr.cr.Spec.ClusterMasterRef.Name
	}
	matchedClusters := make([]*enterpriseApi.SearchHeadCluster, 0)
	for clusterIndex := range searchHeadClusters.Items {
		searchHeadCluster := &searchHeadClusters.Items[clusterIndex]
		deletionTimestamp := searchHeadCluster.GetDeletionTimestamp()
		if (deletionTimestamp != nil && !deletionTimestamp.IsZero()) ||
			searchHeadClusterManagerName(searchHeadCluster) != managerName {
			continue
		}
		matchedClusters = append(matchedClusters, searchHeadCluster)
	}
	if len(matchedClusters) == 0 {
		return false, true, fmt.Sprintf(
			"No SearchHeadCluster references Cluster Manager %s",
			managerName,
		), nil
	}

	clusterPeers, err := GetClusterManagerPeersCall(ctx, mgr)
	if err != nil {
		return true, false, fmt.Sprintf(
			"waiting for Cluster Manager peers before Search Head convergence: %v",
			err,
		), nil
	}
	peer, ok := clusterPeers[replacement.GetName()]
	if !ok || peer.ID == "" {
		return true, false, fmt.Sprintf(
			"Cluster Manager has no GUID for replacement %s",
			replacement.GetName(),
		), nil
	}
	expectedAddress := peer.RegisterSearchAddress
	if expectedAddress == "" {
		expectedAddress = peer.HostPortPair
	}
	if expectedAddress == "" {
		return true, false, fmt.Sprintf(
			"Cluster Manager has no search address for replacement %s",
			replacement.GetName(),
		), nil
	}

	for _, searchHeadCluster := range matchedClusters {
		for ordinal := int32(0); ordinal < searchHeadCluster.Spec.Replicas; ordinal++ {
			podName := GetSplunkStatefulsetPodName(
				SplunkSearchHead,
				searchHeadCluster.GetName(),
				ordinal,
			)
			host := fmt.Sprintf(
				"%s.%s",
				podName,
				splcommon.GetServiceFQDN(
					searchHeadCluster.GetNamespace(),
					splcommon.GetSplunkServiceName(
						SplunkSearchHead,
						searchHeadCluster.GetName(),
						true,
					),
				),
			)
			searchHeadClient := mgr.newSplunkClient(
				fmt.Sprintf("https://%s:8089", host),
				"admin",
				string(mgr.secrets.Data["password"]),
			)
			searchPeers, err := searchHeadClient.GetSearchDistributedPeers()
			searchHeadClient.CloseIdleConnections()
			if err != nil {
				return true, false, fmt.Sprintf(
					"waiting for distributed peers from Search Head %s: %v",
					podName,
					err,
				), nil
			}
			if !searchDistributedPeerConverged(
				searchPeers,
				peer.ID,
				expectedAddress,
			) {
				return true, false, fmt.Sprintf(
					"Search Head %s has not converged peer GUID %s to %s",
					podName,
					peer.ID,
					expectedAddress,
				), nil
			}
		}
	}
	return true, true, fmt.Sprintf(
		"Every Search Head in %d cluster(s) converged peer GUID %s to %s",
		len(matchedClusters),
		peer.ID,
		expectedAddress,
	), nil
}

func searchHeadClusterManagerName(
	searchHeadCluster *enterpriseApi.SearchHeadCluster,
) string {
	if searchHeadCluster == nil {
		return ""
	}
	if searchHeadCluster.Spec.ClusterManagerRef.Name != "" {
		return searchHeadCluster.Spec.ClusterManagerRef.Name
	}
	return searchHeadCluster.Spec.ClusterMasterRef.Name
}

func searchDistributedPeerConverged(
	peers []splclient.SearchDistributedPeerInfo,
	peerID string,
	expectedAddress string,
) bool {
	matches := 0
	for _, peer := range peers {
		if peer.ID != peerID {
			continue
		}
		matches++
		if peer.Name != expectedAddress ||
			peer.Status != "Up" ||
			peer.Disabled {
			return false
		}
	}
	return matches == 1
}
