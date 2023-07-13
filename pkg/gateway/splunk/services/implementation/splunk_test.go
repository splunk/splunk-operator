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
	"testing"

	logz "sigs.k8s.io/controller-runtime/pkg/log/zap"
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
