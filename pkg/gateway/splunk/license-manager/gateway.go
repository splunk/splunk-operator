package licensemanager

import (
	"context"

	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	licensemodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/license"
	model "github.com/splunk/splunk-operator/pkg/splunk/model"
)

// Factory is the interface for creating new Gateway objects.
type Factory interface {
	NewGateway(ctx context.Context, sad *splunkmodel.SplunkCredentials, publisher model.EventPublisher) (Gateway, error)
}

// Gateway holds the state information for talking to
// splunk gateway backend.
type Gateway interface {

	// GetLicenseGroup Performs health checks to determine the cluster health and search impact, prior to a rolling upgrade of the indexer cluster.
	// Authentication and Authorization:
	// 		Requires the admin role or list_indexer_cluster capability.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/health
	GetLicenseGroup(ctx context.Context) (*[]licensemodel.LicenseGroup, error)

	// GetLicense Access information about cluster manager node.
	// get List cluster manager node details.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/info
	GetLicense(ctx context.Context) (*[]licensemodel.License, error)

	// GetLicenseLocalPeerAccess cluster manager peers.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/peers
	GetLicenseLocalPeer(ctx context.Context) (*[]licensemodel.LicenseLocalPeer, error)

	// GetLicenseMessage Access cluster site information.
	// list List available cluster sites.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/sites
	GetLicenseMessage(ctx context.Context) (*[]licensemodel.LicenseMessage, error)

	// GetLicensePools Endpoint to get the status of a rolling restart.
	// GET the status of a rolling restart.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/status
	GetLicensePools(ctx context.Context) (*[]licensemodel.LicensePool, error)

	// GetLicensePeers Endpoint to set cluster in maintenance mode.
	// Post the status of a rolling restart.
	// endpoint: https://<host>:<mPort>/services/cluster/manager/control/default/maintenance
	GetLicensePeers(context context.Context) (*[]licensemodel.LicensePeer, error)

	// GetLicenseUsage check if cluster is in maintenance mode
	GetLicenseUsage(ctx context.Context) (*[]licensemodel.LicenseUsage, error)

	// GetLicenseStacks check if cluster is in maintenance mode
	GetLicenseStacks(ctx context.Context) (*[]licensemodel.LicenseStack, error)
}
