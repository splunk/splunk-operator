package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/jarcoal/httpmock"
	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	clustermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster"
	//managermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster/manager"
	//peermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster/peer"
	"io/ioutil"
	logz "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"testing"
)

var slog = logz.New().WithName("gateway").WithName("fixture")

func setCreds(t *testing.T) *splunkGateway {
	//ctx := context.TODO()
	sad := &splunkmodel.SplunkCredentials{
		Address:                        "splunk-cm-cluster-master-service",
		Port:                           8089,
		ServicesNamespace:              "",
		User:                           "admin",
		App:                            "",
		CredentialsName:                "admin: abcdefghijklmnopqrstuvwxyz",
		TrustedCAFile:                  "",
		ClientCertificateFile:          "",
		ClientPrivateKeyFile:           "",
		DisableCertificateVerification: true,
	}
	publisher := func(ctx context.Context, eventType, reason, message string) {}
	// TODO fixme how to test the gateway call directly
	//sm := NewGatewayFactory(ctx, &sad, publisher)
	sm := &splunkGateway{
		credentials: sad,
		client:      resty.New(),
		publisher:   publisher,
		log:         slog,
		debugLog:    slog,
	}
	//splunkURL := fmt.Sprintf("https://%s:%d/%s", sad.Address, sad.Port, sad.ServicesNamespace)
	splunkURL := fmt.Sprintf("https://%s:%d", sad.Address, sad.Port)
	sm.client.SetBaseURL(splunkURL)
	sm.client.SetHeader("Content-Type", "application/json")
	sm.client.SetHeader("Accept", "application/json")
	sm.client.SetTimeout(time.Duration(60 * time.Minute))
	sm.client.SetDebug(true)
	return sm
}

func TestGetClusterConfig(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	ctx := context.TODO()
	sm := setCreds(t)
	httpmock.ActivateNonDefault(sm.client.GetClient())
	content, err := ioutil.ReadFile("../fixture/cluster_config.json")
	if err != nil {
		t.Errorf("fixture: error in get cluster config %v", err)
	}
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	fakeUrl := clustermodel.GetClusterConfigUrl
	url := fmt.Sprintf("https://%s:%d/%s", sm.credentials.Address, sm.credentials.Port, clustermodel.GetClusterConfigUrl)
	httpmock.RegisterResponder("GET", fakeUrl, responder)
	httpmock.RegisterResponder("GET", url, responder)

	_, err = sm.GetClusterConfig(ctx)
	if err != nil {
		t.Errorf("fixture: error in get cluster config %v", err)
	}
}

func TestGetClusterManagerBuckets(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	ctx := context.TODO()
	sm := setCreds(t)
	httpmock.ActivateNonDefault(sm.client.GetClient())
	content, err := ioutil.ReadFile("../fixture/cluster_manager_buckets.json")
	if err != nil {
		t.Errorf("fixture: error in get cluster config %v", err)
	}
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	url := clustermodel.GetClusterManagerBucketUrl
	httpmock.RegisterResponder("GET", url, responder)

	_, err = sm.GetClusterManagerBuckets(ctx)
	if err != nil {
		t.Errorf("fixture: error in get cluster config %v", err)
	}
}

func TestGetClusterManagerHealth(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	ctx := context.TODO()
	sm := setCreds(t)
	httpmock.ActivateNonDefault(sm.client.GetClient())
	content, err := ioutil.ReadFile("../fixture/cluster_manager_health.json")
	if err != nil {
		t.Errorf("fixture: error in get cluster manager health %v", err)
	}
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	url := clustermodel.GetClusterManagerHealthUrl
	httpmock.RegisterResponder("GET", url, responder)

	_, err = sm.GetClusterManagerHealth(ctx)
	if err != nil {
		t.Errorf("fixture: error in get cluster manager health %v", err)
	}
}

func TestGetClusterManagerGeneration(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	ctx := context.TODO()
	sm := setCreds(t)
	httpmock.ActivateNonDefault(sm.client.GetClient())
	content, err := ioutil.ReadFile("../fixture/cluster_manager_generation.json")
	if err != nil {
		t.Errorf("fixture: error in get cluster manager generation %v", err)
	}
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	url := clustermodel.GetClusterManagerGenerationUrl
	httpmock.RegisterResponder("GET", url, responder)

	_, err = sm.GetClusterManagerGeneration(ctx)
	if err != nil {
		t.Errorf("fixture: error in get cluster manager generation %v", err)
	}
}

func TestGetClusterManagerIndexes(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	ctx := context.TODO()
	sm := setCreds(t)
	httpmock.ActivateNonDefault(sm.client.GetClient())
	content, err := ioutil.ReadFile("../fixture/cluster_manager_indexes.json")
	if err != nil {
		t.Errorf("fixture: error in get cluster manager indexes %v", err)
	}
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	url := clustermodel.GetClusterManagerIndexesUrl
	httpmock.RegisterResponder("GET", url, responder)

	_, err = sm.GetClusterManagerIndexes(ctx)
	if err != nil {
		t.Errorf("fixture: error in get cluster manager indexes %v", err)
	}
}

func TestGetClusterManagerInfo(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	ctx := context.TODO()
	sm := setCreds(t)
	httpmock.ActivateNonDefault(sm.client.GetClient())
	content, err := ioutil.ReadFile("../fixture/cluster_manager_info.json")
	if err != nil {
		t.Errorf("fixture: error in get cluster manager info %v", err)
	}
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	url := clustermodel.GetClusterManagerInfoUrl
	httpmock.RegisterResponder("GET", url, responder)

	_, err = sm.GetClusterManagerInfo(ctx)
	if err != nil {
		t.Errorf("fixture: error in get cluster manager info %v", err)
	}
}

func TestGetClusterManagerRedundancy(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	ctx := context.TODO()
	sm := setCreds(t)
	httpmock.ActivateNonDefault(sm.client.GetClient())
	content, err := ioutil.ReadFile("../fixture/cluster_manager_redundancy.json")
	if err != nil {
		t.Errorf("fixture: error in get cluster manager redundancy %v", err)
	}
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	url := clustermodel.GetClusterManagerRedundancyUrl
	httpmock.RegisterResponder("GET", url, responder)

	_, err = sm.GetClusterManagerRedundancy(ctx)
	if err != nil {
		t.Errorf("fixture: error in get cluster manager redundancy %v", err)
	}
}

func TestGetClusterManagerSites(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	ctx := context.TODO()
	sm := setCreds(t)
	httpmock.ActivateNonDefault(sm.client.GetClient())
	content, err := ioutil.ReadFile("../fixture/cluster_manager_sites.json")
	if err != nil {
		t.Errorf("fixture: error in get cluster manager sites %v", err)
	}
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	url := clustermodel.GetClusterManagerSitesUrl
	httpmock.RegisterResponder("GET", url, responder)

	_, err = sm.GetClusterManagerSites(ctx)
	if err != nil {
		t.Errorf("fixture: error in get cluster manager sites %v", err)
	}
}

func TestGetClusterManagerSearchHeadStatus(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	ctx := context.TODO()
	sm := setCreds(t)
	httpmock.ActivateNonDefault(sm.client.GetClient())
	content, err := ioutil.ReadFile("../fixture/cluster_manager_searchhead.json")
	if err != nil {
		t.Errorf("fixture: error in get cluster manager searchheads %v", err)
	}
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	url := clustermodel.GetClusterManagerSearchHeadUrl
	httpmock.RegisterResponder("GET", url, responder)

	_, err = sm.GetClusterManagerSearchHeadStatus(ctx)
	if err != nil {
		t.Errorf("fixture: error in get cluster manager searchheads %v", err)
	}
}

func TestGetClusterManagerPeers(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	ctx := context.TODO()
	sm := setCreds(t)
	httpmock.ActivateNonDefault(sm.client.GetClient())
	content, err := ioutil.ReadFile("../fixture/cluster_manager_peers.json")
	if err != nil {
		t.Errorf("fixture: error in get cluster manager peers %v", err)
	}
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	url := clustermodel.GetClusterManagerPeersUrl
	httpmock.RegisterResponder("GET", url, responder)

	peersptr, err := sm.GetClusterManagerPeers(ctx)
	if err != nil {
		t.Errorf("fixture: error in get cluster manager searchheads %v", err)
	}
	if peersptr == nil {
		t.Errorf("fixture: error in get cluster manager searchheads  peers list is empty")
	}
}

func TestGetClusterPeerBuckets(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	ctx := context.TODO()
	sm := setCreds(t)
	httpmock.ActivateNonDefault(sm.client.GetClient())
	content, err := ioutil.ReadFile("../fixture/cluster_peer_buckets.json")
	if err != nil {
		t.Errorf("fixture: error in get cluster peer buckets %v", err)
	}
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	url := clustermodel.GetClusterPeerBucketsUrl
	httpmock.RegisterResponder("GET", url, responder)

	_, err = sm.GetClusterPeerBuckets(ctx)
	if err != nil {
		t.Errorf("fixture: error in get cluster peer buckets %v", err)
	}
}

func TestGetClusterPeerInfo(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	ctx := context.TODO()
	sm := setCreds(t)
	httpmock.ActivateNonDefault(sm.client.GetClient())
	content, err := ioutil.ReadFile("../fixture/cluster_peer_info.json")
	if err != nil {
		t.Errorf("fixture: error in get cluster peer info %v", err)
	}
	fixtureData := string(content)
	responder := httpmock.NewStringResponder(200, fixtureData)
	url := clustermodel.GetClusterPeerInfoUrl
	httpmock.RegisterResponder("GET", url, responder)

	_, err = sm.GetClusterPeerInfo(ctx)
	if err != nil {
		t.Errorf("fixture: error in get cluster peer info %v", err)
	}
}
