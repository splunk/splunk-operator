package fixture

import (
	"context"

	//"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/go-resty/resty/v2"
	"github.com/jarcoal/httpmock"
	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	clustermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster"
	managermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster/manager"

	// peermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster/peer"
	// searchheadmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster/searchhead"
	// commonmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/common"
	// lmmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/license-manager"
	gateway "github.com/splunk/splunk-operator/pkg/gateway/splunk/services"
	logz "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var log = logz.New().WithName("gateway").WithName("fixture")

// fixtureGateway implements the gateway.fixtureGateway interface
// and uses splunk to manage the host.
type fixtureGateway struct {
	// client for talking to splunk
	client *resty.Client
	// the splunk credentials
	credentials splunkmodel.SplunkCredentials
	// a logger configured for this host
	log logr.Logger
	// an event publisher for recording significant events
	publisher gateway.EventPublisher
	// state of the splunk
	state *Fixture
}

// Fixture contains persistent state for a particular splunk instance
type Fixture struct {
}

// NewGateway returns a new Fixture Gateway
func (f *Fixture) NewGateway(ctx context.Context, sad *splunkmodel.SplunkCredentials, publisher gateway.EventPublisher) (gateway.Gateway, error) {
	p := &fixtureGateway{
		log:       log.WithValues("splunk", sad.Address),
		publisher: publisher,
		state:     f,
		client:    resty.New(),
	}
	return p, nil
}

// GetClusterManagerInfo Access information about cluster manager node.
// get List cluster manager node details.
// endpoint: https://<host>:<mPort>/services/cluster/manager/info
func (p *fixtureGateway) GetClusterManagerInfo(ctx context.Context) (*[]managermodel.ClusterManagerInfoContent, error) {
	// Read entire file content, giving us little control but
	// making it very simple. No need to close the file.
	content, err := ioutil.ReadFile("../../../gateway/splunk/services/fixture/cluster_config.json")
	if err != nil {
		log.Error(err, "fixture: error in get cluster config")
		return nil, err
	}
	httpmock.ActivateNonDefault(p.client.GetClient())
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	fakeUrl := clustermodel.GetClusterManagerInfoUrl
	httpmock.RegisterResponder("GET", fakeUrl, responder)
	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &managermodel.ClusterManagerInfoHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(fakeUrl)
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

	contentList := []managermodel.ClusterManagerInfoContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, nil
}

// GetClusterManagerPeersAccess cluster manager peers.
// endpoint: https://<host>:<mPort>/services/cluster/manager/peers
func (p *fixtureGateway) GetClusterManagerPeers(ctx context.Context) (*[]managermodel.ClusterManagerPeerContent, error) {
	// Read entire file content, giving us little control but
	// making it very simple. No need to close the file.
	content, err := ioutil.ReadFile("../../../gateway/splunk/services/fixture/cluster_config.json")
	if err != nil {
		log.Error(err, "fixture: error in get cluster config")
		return nil, err
	}
	httpmock.ActivateNonDefault(p.client.GetClient())
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	fakeUrl := clustermodel.GetClusterManagerPeersUrl
	httpmock.RegisterResponder("GET", fakeUrl, responder)
	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &managermodel.ClusterManagerPeerHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(fakeUrl)
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

	contentList := []managermodel.ClusterManagerPeerContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, nil
}

// GetClusterManagerHealth Performs health checks to determine the cluster health and search impact, prior to a rolling upgrade of the indexer cluster.
// Authentication and Authorization:
//
//	Requires the admin role or list_indexer_cluster capability.
//
// endpoint: https://<host>:<mPort>/services/cluster/manager/health
func (p *fixtureGateway) GetClusterManagerHealth(ctx context.Context) (*[]managermodel.ClusterManagerHealthContent, error) {
	// Read entire file content, giving us little control but
	// making it very simple. No need to close the file.
	content, err := ioutil.ReadFile("../../../gateway/splunk/services/fixture/cluster_config.json")
	if err != nil {
		log.Error(err, "fixture: error in get cluster config")
		return nil, err
	}
	httpmock.ActivateNonDefault(p.client.GetClient())
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	fakeUrl := clustermodel.GetClusterManagerHealthUrl
	httpmock.RegisterResponder("GET", fakeUrl, responder)

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &managermodel.ClusterManagerHealthHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(fakeUrl)
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

	contentList := []managermodel.ClusterManagerHealthContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, nil
}
