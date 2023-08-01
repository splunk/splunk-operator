package impl

import (
	"context"
	"testing"

	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	managermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster/manager"
	provisioner "github.com/splunk/splunk-operator/pkg/provisioner/splunk"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//var log = logz.New().WithName("provisioner").WithName("fixture")

func setCreds(t *testing.T) provisioner.Provisioner {
	ctx := context.TODO()
	sad := &splunkmodel.SplunkCredentials{
		Address:                        "splunk-cm-cluster-master-service",
		Port:                           8089,
		ServicesNamespace:              "",
		Namespace:                      "default",
		User:                           "admin",
		App:                            "",
		CredentialsName:                "admin: abcdefghijklmnopqrstuvwxyz",
		TrustedCAFile:                  "",
		ClientCertificateFile:          "",
		ClientPrivateKeyFile:           "",
		DisableCertificateVerification: true,
	}
	publisher := func(ctx context.Context, eventType, reason, message string) {}
	sp := NewProvisionerFactory(true)
	provisioner, err := sp.NewProvisioner(ctx, sad, publisher)
	if err != nil {
		return nil
	}
	return provisioner
}

func TestGetClusterManagerStatus(t *testing.T) {
	callGetClusterManagerHealth = func(ctx context.Context, p *splunkProvisioner) (*[]managermodel.ClusterManagerHealthContent, error) {
		healthData := []managermodel.ClusterManagerHealthContent{}
		return &healthData, nil
	}
	provisioner := setCreds(t)
	conditions := &[]metav1.Condition{}

	ctx := context.TODO()

	_, err := provisioner.GetClusterManagerStatus(ctx, conditions)
	if err != nil {
		t.Errorf("fixture: error in set cluster manager %v", err)
	}
}

func TestSetClusterManagerMultiSiteStatus(t *testing.T) {
	callGetClusterManagerHealth = func(ctx context.Context, p *splunkProvisioner) (*[]managermodel.ClusterManagerHealthContent, error) {
		healthData := []managermodel.ClusterManagerHealthContent{
			{
				AllPeersAreUp: "1",
			},
			{
				AllPeersAreUp: "0",
			},
		}
		return &healthData, nil
	}

	callGetClusterManagerInfo = func(ctx context.Context, p *splunkProvisioner) (*[]managermodel.ClusterManagerInfoContent, error) {
		cminfo := &[]managermodel.ClusterManagerInfoContent{
			{
				Multisite: true,
			},
		}
		return cminfo, nil
	}

	callGetClusterManagerPeersStatus = func(ctx context.Context, p *splunkProvisioner) (*[]managermodel.ClusterManagerPeerContent, error) {
		peerlist := &[]managermodel.ClusterManagerPeerContent{
			{
				Site:   "1",
				Label:  "site1",
				Status: "Up",
			},
			{
				Site:   "2",
				Label:  "site1",
				Status: "down",
			},
		}
		return peerlist, nil
	}
	provisioner := setCreds(t)
	conditions := &[]metav1.Condition{}

	ctx := context.TODO()

	_, err := provisioner.GetClusterManagerStatus(ctx, conditions)
	if err != nil {
		t.Errorf("fixture: error in set cluster manager %v", err)
	}
}
