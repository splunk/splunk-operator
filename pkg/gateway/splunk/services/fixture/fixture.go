package fixture

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"path/filepath"

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

func findFixturePath() (string, error) {
	ext := ".env"
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		dir, err := os.Open(wd)
		if err != nil {
			fmt.Println("Error opening directory:", err)
			return "", err
		}
		defer dir.Close()

		files, err := dir.Readdir(-1)
		if err != nil {
			fmt.Println("Error reading directory:", err)
			return "", err
		}

		for _, file := range files {
			if file.Name() == ext {
				wd, err = filepath.Abs(wd)
				wd += "/pkg/gateway/splunk/services/fixture/"
				return wd, err
			}
		}
		wd += "/.."
	}
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
	relativePath, err := findFixturePath()
	if err != nil {
		log.Error(err, "fixture: unable to find path")
		return nil, err
	}
	content, err := ioutil.ReadFile(relativePath + "/cluster_config.json")
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
	relativePath, err := findFixturePath()
	if err != nil {
		log.Error(err, "fixture: unable to find path")
		return nil, err
	}
	// Read entire file content, giving us little control but
	// making it very simple. No need to close the file.
	content, err := ioutil.ReadFile(relativePath + "cluster_config.json")
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
	relativePath, err := findFixturePath()
	if err != nil {
		log.Error(err, "fixture: unable to find path")
		return nil, err
	}

	// Read entire file content, giving us little control but
	// making it very simple. No need to close the file.
	content, err := ioutil.ReadFile(relativePath + "cluster_config.json")
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

// GetClusterManagerSites Access cluster site information.
// list List available cluster sites.
// endpoint: https://<host>:<mPort>/services/cluster/manager/sites
func (p *fixtureGateway) GetClusterManagerSites(ctx context.Context) (*[]managermodel.ClusterManagerSiteContent, error) {
	relativePath, err := findFixturePath()
	if err != nil {
		log.Error(err, "fixture: unable to find path")
		return nil, err
	}
	// Read entire file content, giving us little control but
	// making it very simple. No need to close the file.
	content, err := ioutil.ReadFile(relativePath + "/cluster_config.json")
	if err != nil {
		log.Error(err, "fixture: error in get cluster config")
		return nil, err
	}
	httpmock.ActivateNonDefault(p.client.GetClient())
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	fakeUrl := clustermodel.GetClusterManagerSitesUrl
	httpmock.RegisterResponder("GET", fakeUrl, responder)
	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &managermodel.ClusterManagerSiteHeader{}
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

	contentList := []managermodel.ClusterManagerSiteContent{}
	for _, entry := range envelop.Entry {
		contentList = append(contentList, entry.Content)
	}
	return &contentList, nil
}

// GetClusterManagerSearchHeadStatus Endpoint to get searchheads connected to cluster manager.
// endpoint: https://<host>:<mPort>/services/cluster/manager/status
func (p *fixtureGateway) GetClusterManagerStatus(ctx context.Context) (*[]managermodel.ClusterManagerStatusContent, error) {
	relativePath, err := findFixturePath()
	if err != nil {
		log.Error(err, "fixture: unable to find path")
		return nil, err
	}
	// Read entire file content, giving us little control but
	// making it very simple. No need to close the file.
	content, err := ioutil.ReadFile(relativePath + "/cluster_manager_status.json")
	if err != nil {
		log.Error(err, "fixture: error in get cluster manager search heads")
		return nil, err
	}
	httpmock.ActivateNonDefault(p.client.GetClient())
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	fakeUrl := clustermodel.GetClusterManagerStatusUrl
	httpmock.RegisterResponder("GET", fakeUrl, responder)
	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	envelop := &managermodel.ClusterManagerStatusHeader{}
	resp, err := p.client.R().
		SetResult(envelop).
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "count": "0"}).
		Get(fakeUrl)
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
	return &contentList, nil
}

// SetClusterInMaintainanceMode Endpoint to set cluster in maintenance mode.
// Post the status of a rolling restart.
// endpoint: https://<host>:<mPort>/services/cluster/manager/control/default/maintenance
func (p *fixtureGateway) SetClusterInMaintenanceMode(context context.Context, mode bool) error {

	relativePath, err := findFixturePath()
	if err != nil {
		log.Error(err, "fixture: unable to find path")
		return err
	}
	// Read entire file content, giving us little control but
	// making it very simple. No need to close the file.
	content, err := ioutil.ReadFile(relativePath + "/cluster_maintenance.json")
	if err != nil {
		log.Error(err, "fixture: error in post cluster maintenance")
		return err
	}
	httpmock.ActivateNonDefault(p.client.GetClient())
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	fakeUrl := clustermodel.SetClusterInMaintenanceMode
	httpmock.RegisterResponder("POST", fakeUrl, responder)

	// featch the configheader into struct
	splunkError := &splunkmodel.SplunkError{}
	resp, err := p.client.R().
		SetError(&splunkError).
		ForceContentType("application/json").
		SetQueryParams(map[string]string{"output_mode": "json", "mode": strconv.FormatBool(mode)}).
		Post(fakeUrl)
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
		return splunkError
	}

	return err
}
