package indexer

import (
	"context"
	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	clustermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster"
	managermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster/manager"
	peermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster/peer"
	searchheadmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster/searchhead"
	lmmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/license-manager"
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

	// Access cluster node configuration details.
	// endpoint: https://<host>:<mPort>/services/cluster/config
	GetClusterConfig(ctx context.Context) (*[]clustermodel.ClusterConfigContent, error)

	// Provides bucket configuration information for a cluster manager node.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/buckets
	GetClusterManagerBuckets(ctx context.Context) (*[]managermodel.ClusterManagerBucketContent, error)

	// Access current generation cluster manager information and create a cluster generation.
	// List peer nodes participating in the current generation for this manager.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/generation
	GetClusterManagerGeneration(ctx context.Context) (*[]managermodel.ClusterManagerGenerationContent, error)

	// Used by the load balancers to check the high availability mode of a given cluster manager.
	// The active cluster manager will return "HTTP 200", denoting "healthy", and a startup or standby cluster manager will return "HTTP 503".
	// Authentication and authorization:
	// 	This endpoint is unauthenticated because some load balancers don't support authentication on a health check endpoint.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/ha_active_status
	// FIXME TODO, not sure how the structure looks
	GetClusterManagerHAActiveStatus(ctx context.Context) error

	// Performs health checks to determine the cluster health and search impact, prior to a rolling upgrade of the indexer cluster.
	// Authentication and Authorization:
	// 		Requires the admin role or list_indexer_cluster capability.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/health
	GetClusterManagerHealth(ctx context.Context) (*[]managermodel.ClusterManagerHealthContent, error)

	// Access cluster index information.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/indexes
	GetClusterManagerIndexes(ctx context.Context) (*[]managermodel.ClusterManagerIndexesContent, error)

	// Access information about cluster manager node.
	// get List cluster manager node details.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/info
	GetClusterManagerInfo(ctx context.Context) (*[]managermodel.ClusterManagerInfoContent, error)

	// Access cluster manager peers.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/peers
	GetClusterManagerPeers(ctx context.Context) (*[]managermodel.ClusterManagerPeerContent, error)

	// Display the details of all cluster managers participating in cluster manager redundancy, and switch the HA state of the cluster managers.
	// Authentication and authorization
	//		The GET on this endpoint needs the capability list_indexer_cluster, and the POST on this endpoint needs the capability edit_indexer_cluster.
	// GET Display the details of all cluster managers participating in cluster manager redundancy
	// endpoint: https://<host>:<mPort>/services/cluster/manager/redundancy
	GetClusterManagerRedundancy(ctx context.Context) (*[]managermodel.ClusterManagerRedundancyContent, error)

	// Access cluster site information.
	// list List available cluster sites.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/sites
	GetClusterManagerSites(ctx context.Context) (*[]managermodel.ClusterManagerSiteContent, error)

	// GetClusterManagerSearchHeadStatus Endpoint to get the status of a rolling restart.
	// GET the status of a rolling restart.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/status
	GetClusterManagerSearchHeadStatus(ctx context.Context) (*[]managermodel.SearchHeadContent, error)

	// Access cluster peers bucket configuration.
	// GET
	// List cluster peers bucket configuration.
	// endpoint: https://<host>:<mPort>/services/cluster/peer/buckets
	GetClusterPeerBuckets(ctx context.Context) (*[]peermodel.ClusterPeerBucket, error)

	// Manage peer buckets.
	// GET
	// List peer specified bucket information.
	// endpoint: https://<host>:<mPort>/services/cluster/peer/buckets/	}
	GetClusterPeerInfo(ctx context.Context) (*[]peermodel.ClusterPeerInfo, error)

	// GetLicenseManagerPeers Access cluster node configuration details.
	// endpoint: "https://localhost:8089/services/licenser/localpeer?output_mode=json"
	GetLicenseManagerPeers(context context.Context) (*[]lmmodel.LicenseLocalPeerEntry, error)

	GetSearchHeadCaptainInfo(ctx context.Context) (*searchheadmodel.SearchHeadCaptainInfo, error)

	GetSearchHeadCaptainMembers(ctx context.Context) error

	GetSearchHeadClusterMemberInfo(ctx context.Context) error

	SetSearchHeadDetention(ctx context.Context) error

	RemoveSearchHeadClusterMember(ctx context.Context) error

	GetIndexerClusterPeerInfo()

	RemoveIndexerClusterPeer()

	DecommissionIndexerClusterPeer()

	BundlePush()

	AutomateMCApplyChanges()

	GetMonitoringconsoleServerRoles()

	UpdateDMCGroups()

	UpdateDMCClusteringLabelGroup()

	GetMonitoringconsoleAssetTable()

	PostMonitoringConsoleAssetTable()

	GetMonitoringConsoleUISettings()

	UpdateLookupUISettings()

	UpdateMonitoringConsoleApp()

	GetClusterInfo()

	SetIdxcSecret()

	RestartSplunk()
}
