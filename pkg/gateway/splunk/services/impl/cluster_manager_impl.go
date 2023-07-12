package impl

import (
	"context"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/go-resty/resty/v2"
	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	clustermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster"
	managermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster/manager"
	gateway "github.com/splunk/splunk-operator/pkg/gateway/splunk/services"
)

// splunkGateway implements the gateway.Gateway interface
// and uses gateway to manage the host.
type splunkGateway struct {
	// a logger configured for this host
	log logr.Logger
	// a debug logger configured for this host
	debugLog logr.Logger
	// an event publisher for recording significant events
	publisher gateway.EventPublisher
	// client for talking to splunk
	client *resty.Client
	// credentials
	credentials *splunkmodel.SplunkCredentials
}

// Access cluster node configuration details.
// endpoint: https://<host>:<mPort>/services/cluster/config
func (p *splunkGateway) GetClusterConfig(context context.Context) (*[]clustermodel.ClusterConfigContent, error) {
	url := clustermodel.GetClusterConfigUrl

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &clustermodel.ClusterConfigHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster configuration failed")
	}
	if resp.StatusCode() != http.StatusOK {
		p.log.Info("response failure set to", "result", err)
	}
	if resp.StatusCode() > 400 {
		if len(splunkError.Messages) > 0 {
			p.log.Info("response failure set to", "result", splunkError.Messages[0].Text)
		}
		return nil, splunkError
	}

	contentList := []clustermodel.ClusterConfigContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, err
}

// Provides bucket configuration information for a cluster manager node.
// endpoint: https://<host>:<mPort>/services/cluster/manager/buckets
func (p *splunkGateway) GetClusterManagerBuckets(context context.Context) (*[]managermodel.ClusterManagerBucketContent, error) {
	url := clustermodel.GetClusterManagerBucketUrl

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &managermodel.ClusterManagerBucketHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster manager buckets failed")
	}
	if resp.StatusCode() != http.StatusOK {
		p.log.Info("response failure set to", "result", err)
	}
	if resp.StatusCode() > 400 {
		if len(splunkError.Messages) > 0 {
			p.log.Info("response failure set to", "result", splunkError.Messages[0].Text)
		}
		return nil, splunkError
	}

	contentList := []managermodel.ClusterManagerBucketContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, err
}

// Access current generation cluster manager information and create a cluster generation.
// List peer nodes participating in the current generation for this manager.
// endpoint: https://<host>:<mPort>/services/cluster/manager/generation
func (p *splunkGateway) GetClusterManagerGeneration(context context.Context) (*[]managermodel.ClusterManagerGenerationContent, error) {
	url := clustermodel.GetClusterManagerGenerationUrl

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &managermodel.ClusterManagerGenerationHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster manager generation failed")
	}
	if resp.StatusCode() != http.StatusOK {
		p.log.Info("response failure set to", "result", err)
	}
	if resp.StatusCode() > 400 {
		if len(splunkError.Messages) > 0 {
			p.log.Info("response failure set to", "result", splunkError.Messages[0].Text)
		}
		return nil, splunkError
	}

	contentList := []managermodel.ClusterManagerGenerationContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, err
}

// Used by the load balancers to check the high availability mode of a given cluster manager.
// The active cluster manager will return "HTTP 200", denoting "healthy", and a startup or standby cluster manager will return "HTTP 503".
// Authentication and authorization:
//
//	This endpoint is unauthenticated because some load balancers don't support authentication on a health check endpoint.
//
// endpoint: https://<host>:<mPort>/services/cluster/manager/ha_active_status
// FIXME TODO, not sure how the structure looks
func (p *splunkGateway) GetClusterManagerHAActiveStatus(context context.Context) error {
	return nil
}

// Performs health checks to determine the cluster health and search impact, prior to a rolling upgrade of the indexer cluster.
// Authentication and Authorization:
//
//	Requires the admin role or list_indexer_cluster capability.
//
// endpoint: https://<host>:<mPort>/services/cluster/manager/health
func (p *splunkGateway) GetClusterManagerHealth(context context.Context) (*[]managermodel.ClusterManagerHealthContent, error) {
	url := clustermodel.GetClusterManagerHealthUrl

	p.log.Info("getting cluster manager health information")
	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &managermodel.ClusterManagerHealthHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster manager health failed")
	}
	if resp.StatusCode() != http.StatusOK {
		p.log.Info("response failure set to", "result", err)
	}
	if resp.StatusCode() > 400 {
		if len(splunkError.Messages) > 0 {
			p.log.Info("response failure set to", "result", splunkError.Messages[0].Text)
		}
		return nil, splunkError
	}

	contentList := []managermodel.ClusterManagerHealthContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, err
}

// Access cluster index information.
// endpoint: https://<host>:<mPort>/services/cluster/manager/indexes
func (p *splunkGateway) GetClusterManagerIndexes(context context.Context) (*[]managermodel.ClusterManagerIndexesContent, error) {
	url := clustermodel.GetClusterManagerIndexesUrl

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &managermodel.ClusterManagerIndexesHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster manager indexes failed")
	}
	if resp.StatusCode() != http.StatusOK {
		p.log.Error(err, "call failed")
	}
	if resp.StatusCode() > 400 {
		if len(splunkError.Messages) > 0 {
			p.log.Info("response failure set to", "result", splunkError.Messages[0].Text)
		}
		return nil, splunkError
	}

	contentList := []managermodel.ClusterManagerIndexesContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, err
}

// Access information about cluster manager node.
// get List cluster manager node details.
// endpoint: https://<host>:<mPort>/services/cluster/manager/info
func (p *splunkGateway) GetClusterManagerInfo(context context.Context) (*[]managermodel.ClusterManagerInfoContent, error) {
	url := clustermodel.GetClusterManagerInfoUrl

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &managermodel.ClusterManagerInfoHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster manager info failed")
	}
	if resp.StatusCode() != http.StatusOK {
		p.log.Info("response failure set to", "result", err)
	}
	if resp.StatusCode() > 400 {
		if len(splunkError.Messages) > 0 {
			p.log.Info("response failure set to", "result", splunkError.Messages[0].Text)
		}
		return nil, splunkError
	}

	contentList := []managermodel.ClusterManagerInfoContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, err
}

// Access cluster manager peers.
// endpoint: https://<host>:<mPort>/services/cluster/manager/peers
func (p *splunkGateway) GetClusterManagerPeers(context context.Context) (*[]managermodel.ClusterManagerPeerContent, error) {
	url := clustermodel.GetClusterManagerPeersUrl

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &managermodel.ClusterManagerPeerHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster manager peers failed")
	}
	if resp.StatusCode() != http.StatusOK {
		p.log.Info("response failure set to", "result", err)
	}
	if resp.StatusCode() > 400 {
		if len(splunkError.Messages) > 0 {
			p.log.Info("response failure set to", "result", splunkError.Messages[0].Text)
		}
		return nil, splunkError
	}

	contentList := []managermodel.ClusterManagerPeerContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, err
}

// Display the details of all cluster managers participating in cluster manager redundancy, and switch the HA state of the cluster managers.
// Authentication and authorization
//
//	The GET on this endpoint needs the capability list_indexer_cluster, and the POST on this endpoint needs the capability edit_indexer_cluster.
//
// GET Display the details of all cluster managers participating in cluster manager redundancy
// endpoint: https://<host>:<mPort>/services/cluster/manager/redundancy
func (p *splunkGateway) GetClusterManagerRedundancy(context context.Context) (*[]managermodel.ClusterManagerRedundancyContent, error) {
	url := clustermodel.GetClusterManagerRedundancyUrl

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &managermodel.ClusterManagerRedundancyHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster manager redundancy failed")
	}
	if resp.StatusCode() != http.StatusOK {
		p.log.Info("response failure set to", "result", err)
	}
	if resp.StatusCode() > 400 {
		if len(splunkError.Messages) > 0 {
			p.log.Info("response failure set to", "result", splunkError.Messages[0].Text)
		}
		return nil, splunkError
	}

	contentList := []managermodel.ClusterManagerRedundancyContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, err
}

// Access cluster site information.
// list List available cluster sites.
// endpoint: https://<host>:<mPort>/services/cluster/manager/sites
func (p *splunkGateway) GetClusterManagerSites(context context.Context) (*[]managermodel.ClusterManagerSiteContent, error) {
	url := clustermodel.GetClusterManagerSitesUrl

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &managermodel.ClusterManagerSiteHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster manager sites failed")
	}
	if resp.StatusCode() != http.StatusOK {
		p.log.Info("response failure set to", "result", err)
	}
	if resp.StatusCode() > 400 {
		if len(splunkError.Messages) > 0 {
			p.log.Info("response failure set to", "result", splunkError.Messages[0].Text)
		}
		return nil, splunkError
	}

	contentList := []managermodel.ClusterManagerSiteContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, err
}

// GetClusterManagerSearchHeadStatus Endpoint to get the status of a rolling restart.
// GET the status of a rolling restart.
// endpoint: https://<host>:<mPort>/services/cluster/manager/status
func (p *splunkGateway) GetClusterManagerSearchHeadStatus(context context.Context) (*[]managermodel.SearchHeadContent, error) {
	url := clustermodel.GetClusterManagerSearchHeadUrl

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &managermodel.ClusterMasterSearchHeadHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(url)
	if err != nil {
		p.log.Error(err, "get cluster manager status failed")
	}
	if resp.StatusCode() != http.StatusOK {
		p.log.Info("response failure set to", "result", err)
	}
	if resp.StatusCode() > 400 {
		if len(splunkError.Messages) > 0 {
			p.log.Info("response failure set to", "result", splunkError.Messages[0].Text)
		}
		return nil, splunkError
	}

	contentList := []managermodel.SearchHeadContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, err
}
