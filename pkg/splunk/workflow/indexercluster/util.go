// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package indexercluster

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

type eventPublisher interface {
	Normal(context.Context, string, string)
	Warning(context.Context, string, string)
}

func getEventPublisher(ctx context.Context) eventPublisher {
	publisher, _ := ctx.Value(splcommon.EventPublisherKey).(eventPublisher)
	return publisher
}

// SetClusterMaintenanceMode executes the Splunk maintenance-mode transition
// on the Cluster Manager pod. The caller owns CR status persistence through
// setState, keeping this workflow independent of a particular CRD version.
func SetClusterMaintenanceMode(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, enable bool, podName string, podExecClient splutil.PodExecClientImpl, setState func(bool)) error {
	// Retrieve admin password from Pod
	adminPassword, err := splutil.GetSpecificSecretTokenFromPod(ctx, c, podName, cr.GetNamespace(), "password")
	if err != nil {
		return err
	}

	var command string
	if enable {
		command = fmt.Sprintf("/opt/splunk/bin/splunk enable maintenance-mode --answer-yes -auth admin:%s", adminPassword)
	} else {
		command = fmt.Sprintf("/opt/splunk/bin/splunk disable maintenance-mode --answer-yes -auth admin:%s", adminPassword)
	}
	streamOptions := splutil.NewStreamOptionsObject(command)

	_, _, err = podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
	if err != nil {
		return err
	}
	if setState != nil {
		// Set cluster manager maintenance mode
		setState(enable)
	}
	return nil
}

// PodManager coordinates the stateful, multi-step operations required while
// an IndexerCluster is scaling, recycling peers, or synchronizing secrets.
// The reconcile package owns object orchestration; this type owns workflow
// state transitions.
type PodManager struct {
	Client          splcommon.ControllerClient
	Log             *slog.Logger
	CR              *enterpriseApi.IndexerCluster
	Secrets         *corev1.Secret
	NewSplunkClient func(string, string, string) *splclient.SplunkClient
	GetManagerInfo  func(context.Context, *PodManager) (*splclient.ClusterManagerInfo, error)
	GetManagerPeers func(context.Context, *PodManager) (map[string]splclient.ClusterManagerPeerInfo, error)
}

func (mgr *PodManager) logger() *slog.Logger {
	if mgr.Log != nil {
		return mgr.Log
	}
	return slog.Default()
}

func (mgr *PodManager) newClient(managementURI, username, password string) *splclient.SplunkClient {
	if mgr.NewSplunkClient != nil {
		return mgr.NewSplunkClient(managementURI, username, password)
	}
	return splclient.NewSplunkClient(managementURI, username, password)
}

// GetMonitoringConsoleClient returns a Splunk client for the Monitoring Console.
func (mgr *PodManager) GetMonitoringConsoleClient(cr *enterpriseApi.IndexerCluster, configRef string) *splclient.SplunkClient {
	fqdnName := splcommon.GetServiceFQDN(cr.GetNamespace(), splcommon.GetSplunkServiceName(splcommon.SplunkMonitoringConsole, configRef, false))
	password := ""
	if mgr.Secrets != nil {
		password = string(mgr.Secrets.Data["password"])
	}
	return mgr.newClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", password)
}

// PrepareScaleDown decommissions and deregisters a peer before removal.
func (mgr *PodManager) PrepareScaleDown(ctx context.Context, n int32) (bool, error) {
	complete, err := mgr.decommission(ctx, n, true)
	if err != nil || !complete {
		return false, err
	}
	c := mgr.GetClusterManagerClient(ctx)
	peerName := splutil.GetSplunkStatefulsetPodName(splcommon.SplunkIndexer, mgr.CR.GetName(), n)
	remainingPeers := int32(len(mgr.CR.Status.Peers)) - 1
	mgr.logger().InfoContext(ctx, "deregistering peer from ClusterManager", "peerName", peerName, "remainingPeers", remainingPeers)
	return true, c.RemoveIndexerClusterPeer(mgr.CR.Status.Peers[n].ID)
}

// PrepareRecycle decommissions a peer before it is recycled.
func (mgr *PodManager) PrepareRecycle(ctx context.Context, n int32) (bool, error) {
	return mgr.decommission(ctx, n, false)
}

// Decommission decommissions a peer and reports whether it is safe to proceed.
func (mgr *PodManager) Decommission(ctx context.Context, n int32, enforceCounts bool) (bool, error) {
	return mgr.decommission(ctx, n, enforceCounts)
}

// FinishUpgrade has no additional IndexerCluster workflow step.
func (mgr *PodManager) FinishUpgrade(context.Context, int32) error { return nil }

// FinishRecycle waits for the peer to return to Up.
func (mgr *PodManager) FinishRecycle(_ context.Context, n int32) (bool, error) {
	if n >= int32(len(mgr.CR.Status.Peers)) {
		return false, fmt.Errorf("incorrect Peer got %d length of peer list %d", n, int32(len(mgr.CR.Status.Peers)))
	}
	return mgr.CR.Status.Peers[n].Status == "Up", nil
}

func (mgr *PodManager) decommission(ctx context.Context, n int32, enforceCounts bool) (bool, error) {
	peerName := splutil.GetSplunkStatefulsetPodName(splcommon.SplunkIndexer, mgr.CR.GetName(), n)
	switch mgr.CR.Status.Peers[n].Status {
	case "Up":
		podExecClient := splutil.GetPodExecClient(mgr.Client, mgr.CR, splutil.GetApplicablePodNameForK8Probes(mgr.CR, n))
		if err := splutil.SetIndexerProbeLevelOnSplunkPod(ctx, podExecClient, livenessProbeLevelOne); err != nil {
			// Don't return error here. We may be reconciling several times, and the actual Pod status is down, but
			// not yet reflecting on the Cluster Manager, in which case, the podExec fails, though the decommission is
			// going fine.
			mgr.logger().WarnContext(ctx, "unable to lower the liveness probe level", "peerName", peerName, "enforceCounts", enforceCounts)
		}
		mgr.logger().InfoContext(ctx, "decommissioning IndexerCluster peer", "peerName", peerName, "enforceCounts", enforceCounts)
		return false, mgr.GetClient(ctx, n).DecommissionIndexerClusterPeer(enforceCounts)
	case "Decommissioning":
		mgr.logger().InfoContext(ctx, "waiting for decommission to complete", "peerName", peerName)
		return false, nil
	case "ReassigningPrimaries":
		mgr.logger().InfoContext(ctx, "waiting for decommission to complete", "peerName", peerName)
		return false, nil
	case "GracefulShutdown":
		mgr.logger().InfoContext(ctx, "decommission complete", "peerName", peerName, "status", mgr.CR.Status.Peers[n].Status)
		return true, nil
	case "Down":
		mgr.logger().InfoContext(ctx, "decommission complete", "peerName", peerName, "status", mgr.CR.Status.Peers[n].Status)
		return true, nil
	case "":
		mgr.logger().InfoContext(ctx, "peer has empty ID", "peerName", peerName)
		return false, nil
	default:
		return false, fmt.Errorf("Status=%s", mgr.CR.Status.Peers[n].Status)
	}
}

// GetClient returns a Splunk client for an IndexerCluster peer.
func (mgr *PodManager) GetClient(ctx context.Context, n int32) *splclient.SplunkClient {
	logger := slog.With("func", "indexerClusterPodManager.getClient", "name", mgr.CR.GetName(), "namespace", mgr.CR.GetNamespace())

	// Get Pod Name
	memberName := splutil.GetSplunkStatefulsetPodName(splcommon.SplunkIndexer, mgr.CR.GetName(), n)

	// Get Fully Qualified Domain Name
	fqdnName := splcommon.GetServiceFQDN(mgr.CR.GetNamespace(), fmt.Sprintf("%s.%s", memberName, splcommon.GetSplunkServiceName(splcommon.SplunkIndexer, mgr.CR.GetName(), true)))

	// Retrieve admin password from Pod
	adminPassword, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.Client, memberName, mgr.CR.GetNamespace(), "password")
	if err != nil {
		logger.WarnContext(ctx, "couldn't retrieve the admin password from pod", "error", err)
	}
	return mgr.newClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", adminPassword)
}

// GetClusterManagerClient returns a Splunk client for the configured manager.
func (mgr *PodManager) GetClusterManagerClient(ctx context.Context) *splclient.SplunkClient {
	logger := slog.With("func", "indexerClusterPodManager.getClusterManagerClient", "name", mgr.CR.GetName(), "namespace", mgr.CR.GetNamespace())

	// Retrieve admin password from Pod
	managerName := ""
	managerType := splcommon.InstanceType("")
	if mgr.CR.Spec.ClusterManagerRef.Name != "" {
		managerName = mgr.CR.Spec.ClusterManagerRef.Name
		managerType = splcommon.SplunkClusterManager
	} else if mgr.CR.Spec.ClusterMasterRef.Name != "" {
		managerName = mgr.CR.Spec.ClusterMasterRef.Name
		managerType = splcommon.SplunkClusterMaster
	} else {
		logger.InfoContext(ctx, "empty ClusterManager reference")
	}

	// Get Fully Qualified Domain Name
	fqdnName := splcommon.GetServiceFQDN(mgr.CR.GetNamespace(), splcommon.GetSplunkServiceName(managerType, managerName, false))

	// Retrieve admin password for Pod
	podName := fmt.Sprintf("splunk-%s-%s-%s", managerName, managerType, "0")
	adminPassword, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.Client, podName, mgr.CR.GetNamespace(), "password")
	if err != nil {
		logger.WarnContext(ctx, "couldn't retrieve the admin password from pod", "error", err.Error())
	}
	return mgr.newClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", adminPassword)
}

// VerifyRFPeers ensures the requested replica count is not below the manager's RF.
func (mgr *PodManager) VerifyRFPeers(ctx context.Context, c splcommon.ControllerClient) error {
	// Get event publisher from context
	eventPublisher := getEventPublisher(ctx)

	if mgr.Client == nil {
		mgr.Client = c
	}
	cm := mgr.GetClusterManagerClient(ctx)
	clusterInfo, err := cm.GetClusterInfo(false)
	if err != nil {
		return fmt.Errorf("could not get cluster info from cluster manager")
	}
	var replicationFactor int32
	if clusterInfo.MultiSite == "true" {
		replicationFactor = siteRepFactorOriginCount(clusterInfo.SiteReplicationFactor)
	} else { // for single site, check replication factor
		replicationFactor = clusterInfo.ReplicationFactor
	}
	requestedReplicas := mgr.CR.Spec.Replicas
	if requestedReplicas < replicationFactor {
		mgr.logger().InfoContext(ctx, "changing number of replicas as it is less than RF number of peers", "replicas", requestedReplicas)
		// Emit event indicating scaling below RF is blocked/adjusted
		if eventPublisher != nil {
			eventPublisher.Warning(ctx, "ScalingBlockedRF", fmt.Sprintf("Cannot scale below replication factor: %d replicas required, %d requested. Adjust replicationFactor or replicas.", replicationFactor, requestedReplicas))
		}
		mgr.CR.Spec.Replicas = replicationFactor
	}
	return nil
}

func (mgr *PodManager) managerInfo(ctx context.Context) (*splclient.ClusterManagerInfo, error) {
	if mgr.GetManagerInfo != nil {
		return mgr.GetManagerInfo(ctx, mgr)
	}
	return mgr.GetClusterManagerClient(ctx).GetClusterManagerInfo()
}

func (mgr *PodManager) managerPeers(ctx context.Context) (map[string]splclient.ClusterManagerPeerInfo, error) {
	if mgr.GetManagerPeers != nil {
		return mgr.GetManagerPeers(ctx, mgr)
	}
	return mgr.GetClusterManagerClient(ctx).GetClusterManagerPeers()
}

// UpdateStatus refreshes IndexerCluster status from Cluster Manager REST data.
func (mgr *PodManager) UpdateStatus(ctx context.Context, statefulSet *appsv1.StatefulSet) error {
	mgr.CR.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	if mgr.CR.Status.ClusterManagerPhase != enterpriseApi.PhaseReady && mgr.CR.Status.ClusterMasterPhase != enterpriseApi.PhaseReady {
		mgr.CR.Status.Initialized = false
		mgr.CR.Status.IndexingReady = false
		mgr.CR.Status.ServiceReady = false
		mgr.CR.Status.MaintenanceMode = false
		return fmt.Errorf("waiting for cluster manager to become ready")
	}

	oldInitialized := mgr.CR.Status.Initialized
	oldIndexingReady := mgr.CR.Status.IndexingReady

	// get indexer cluster info from cluster manager if it's ready
	clusterInfo, err := mgr.managerInfo(ctx)
	if err != nil {
		return err
	}
	mgr.CR.Status.Initialized = clusterInfo.Initialized
	mgr.CR.Status.IndexingReady = clusterInfo.IndexingReady
	mgr.CR.Status.ServiceReady = clusterInfo.ServiceReady
	mgr.CR.Status.MaintenanceMode = clusterInfo.MaintenanceMode

	// get peer information from cluster manager
	peers, err := mgr.managerPeers(ctx)
	if err != nil {
		return err
	}
	totalPeerCount := len(peers)
	clusterName := mgr.CR.GetName()
	for n := int32(0); n < statefulSet.Status.Replicas; n++ {
		peerName := splutil.GetSplunkStatefulsetPodName(splcommon.SplunkIndexer, mgr.CR.GetName(), n)
		peerStatus := enterpriseApi.IndexerClusterMemberStatus{Name: peerName}
		if peerInfo, ok := peers[peerName]; ok {
			peerStatus.ID = peerInfo.ID
			peerStatus.Status = peerInfo.Status
			peerStatus.ActiveBundleID = peerInfo.ActiveBundleID
			peerStatus.BucketCount = peerInfo.BucketCount
			peerStatus.Searchable = peerInfo.Searchable
			slog.InfoContext(ctx, "peer registered with ClusterManager",
				"peerName", peerName,
				"clusterName", clusterName,
				"totalPeerCount", totalPeerCount)
		} else {
			mgr.logger().InfoContext(ctx, "peer is not known by ClusterManager", "peerName", peerName)
		}
		if n < int32(len(mgr.CR.Status.Peers)) {
			mgr.CR.Status.Peers[n] = peerStatus
		} else {
			mgr.CR.Status.Peers = append(mgr.CR.Status.Peers, peerStatus)
		}
	}
	// truncate any extra peers that we didn't check (leftover from scale down)
	if statefulSet.Status.Replicas < int32(len(mgr.CR.Status.Peers)) {
		mgr.CR.Status.Peers = mgr.CR.Status.Peers[:statefulSet.Status.Replicas]
	}

	// Get event publisher from context
	eventPublisher := getEventPublisher(ctx)

	// Emit events only on state transitions
	if eventPublisher != nil {
		// Compute current available peers for quorum-related events
		available := int32(0)
		for _, peer := range mgr.CR.Status.Peers {
			if peer.Status == "Up" && peer.Searchable {
				available++
			}
		}
		totalPeers := len(mgr.CR.Status.Peers)
		if !oldIndexingReady && mgr.CR.Status.IndexingReady {
			if !oldInitialized && mgr.CR.Status.Initialized {
				eventPublisher.Normal(ctx, "ClusterInitialized", fmt.Sprintf("Cluster '%s' initialized with %d peers", mgr.CR.GetName(), totalPeers))
			}
			eventPublisher.Normal(ctx, "ClusterQuorumRestored", fmt.Sprintf("Cluster quorum restored: %d/%d peers available", available, totalPeers))
		}
		if oldIndexingReady && !mgr.CR.Status.IndexingReady {
			eventPublisher.Warning(ctx, "ClusterQuorumLost", fmt.Sprintf("Cluster quorum lost: %d/%d peers available. Investigate peer failures immediately.", available, totalPeers))
		}
	}
	return nil
}

func siteRepFactorOriginCount(siteRepFactor string) int32 {
	re := regexp.MustCompile(".*origin:(?P<rf>.*),.*")
	match := re.FindStringSubmatch(siteRepFactor)
	siteRF, err := strconv.ParseInt(match[1], 10, 32)
	if err != nil {
		return 0
	}
	return int32(siteRF)
}

const livenessProbeLevelOne = 1
