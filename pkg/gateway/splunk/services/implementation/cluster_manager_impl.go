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
func (p *splunkGateway) GetClusterManagerStatus(context context.Context) (*[]managermodel.ClusterManagerStatusContent, error) {
	url := clustermodel.GetClusterManagerStatusUrl

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &managermodel.ClusterManagerStatusHeader{}
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

	contentList := []managermodel.ClusterManagerStatusContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, err
}
