package indexer

import (
	"context"

	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	managermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster/manager"
)

// EventPublisher is a function type for publishing events associated
// with gateway functions.
type EventPublisher func(ctx context.Context, eventType, reason, message string)

// Factory is the interface for creating new Gateway objects.
type Factory interface {
	NewGateway(ctx context.Context, sad *splunkmodel.SplunkCredentials, publisher EventPublisher) (Gateway, error)
}

// Gateway holds the state information for talking to
// splunk gateway backend.
type Gateway interface {

	// Performs health checks to determine the cluster health and search impact, prior to a rolling upgrade of the indexer cluster.
	// Authentication and Authorization:
	// 		Requires the admin role or list_indexer_cluster capability.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/health
	GetClusterManagerHealth(ctx context.Context) (*[]managermodel.ClusterManagerHealthContent, error)

	// Access information about cluster manager node.
	// get List cluster manager node details.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/info
	GetClusterManagerInfo(ctx context.Context) (*[]managermodel.ClusterManagerInfoContent, error)

	// Access cluster manager peers.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/peers
	GetClusterManagerPeers(ctx context.Context) (*[]managermodel.ClusterManagerPeerContent, error)

	// Access cluster site information.
	// list List available cluster sites.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/sites
	GetClusterManagerSites(ctx context.Context) (*[]managermodel.ClusterManagerSiteContent, error)

	// GetClusterManagerSearchHeadStatus Endpoint to get the status of a rolling restart.
	// GET the status of a rolling restart.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/status
	GetClusterManagerStatus(ctx context.Context) (*[]managermodel.ClusterManagerStatusContent, error)
}
