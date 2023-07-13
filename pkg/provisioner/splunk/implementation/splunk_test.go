package impl

import (
	"context"
	"testing"

	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	managermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster/manager"
	splunkgatewayimpl "github.com/splunk/splunk-operator/pkg/gateway/splunk/services/implementation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//var log = logz.New().WithName("provisioner").WithName("fixture")

func setCreds(t *testing.T) *splunkProvisioner {
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
	gatewayFactory := splunkgatewayimpl.NewGatewayFactory()
	gateway, err := gatewayFactory.NewGateway(ctx, sad, publisher)
	if err != nil {
		return nil
	}
	// TODO fixme how to test the provisioner call directly
	//sm := NewProvisionerFactory(ctx, &sad, publisher)
	sm := &splunkProvisioner{
		credentials: sad,
		publisher:   publisher,
		gateway:     gateway,
	}
	return sm
}

func TestSetClusterManagerStatus(t *testing.T) {
	callGetClusterManagerHealth = func(ctx context.Context, p *splunkProvisioner) (*[]managermodel.ClusterManagerHealthContent, error) {
		healthData := []managermodel.ClusterManagerHealthContent{}
		return &healthData, nil
	}
	provisioner := setCreds(t)
	ctx := context.TODO()
	conditions := &[]metav1.Condition{}
	err := provisioner.SetClusterManagerStatus(ctx, conditions)
	if err != nil {
		t.Errorf("fixture: error in set cluster manager %v", err)
	}
}
