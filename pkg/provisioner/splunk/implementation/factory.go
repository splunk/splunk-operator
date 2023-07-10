package impl

import (
	"context"

	"github.com/go-logr/logr"

	//model "github.com/splunk/splunk-operator/pkg/provisioner/splunk/model"
	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	gateway "github.com/splunk/splunk-operator/pkg/gateway/splunk/services"
	"github.com/splunk/splunk-operator/pkg/gateway/splunk/services/fixture"
	splunkgatewayimpl "github.com/splunk/splunk-operator/pkg/gateway/splunk/services/implementation"
	provisioner "github.com/splunk/splunk-operator/pkg/provisioner/splunk"

	//cmmodel "github.com/splunk/splunk-operator/pkg/provisioner/splunk/cluster-manager/model"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

type splunkProvisionerFactory struct {
	log logr.Logger
	//credentials to log on to splunk
	credentials *splunkmodel.SplunkCredentials
	// Gateway Factory
	gatewayFactory gateway.Factory
}

// NewProvisionerFactory  new provisioner factory to create provisioner interface
func NewProvisionerFactory(runInTestMode bool) provisioner.Factory {
	factory := splunkProvisionerFactory{}

	err := factory.init(runInTestMode)
	if err != nil {
		return nil // FIXME we have to throw some kind of exception or error here
	}
	return factory
}

func (f *splunkProvisionerFactory) init(runInTestMode bool) error {
	if runInTestMode {
		f.gatewayFactory = &fixture.Fixture{}
	} else {
		f.gatewayFactory = splunkgatewayimpl.NewGatewayFactory()
	}
	return nil
}

func (f splunkProvisionerFactory) splunkProvisioner(ctx context.Context, sad *splunkmodel.SplunkCredentials, publisher gateway.EventPublisher) (*splunkProvisioner, error) {
	provisionerLogger := log.FromContext(ctx)
	reqLogger := log.FromContext(ctx)
	f.log = reqLogger.WithName("splunkProvisioner")

	f.credentials = sad

	provisionerLogger.Info("new splunk manager created to access rest endpoint")
	gateway, err := f.gatewayFactory.NewGateway(ctx, sad, publisher)
	if err != nil {
		return nil, err
	}
	newProvisioner := &splunkProvisioner{
		credentials: f.credentials,
		log:         f.log,
		debugLog:    f.log,
		publisher:   publisher,
		gateway:     gateway,
	}

	f.log.Info("splunk settings",
		"endpoint", f.credentials.Address,
		"CACertFile", f.credentials.TrustedCAFile,
		"ClientCertFile", f.credentials.ClientCertificateFile,
		"ClientPrivKeyFile", f.credentials.ClientPrivateKeyFile,
		"TLSInsecure", f.credentials.DisableCertificateVerification,
	)
	return newProvisioner, nil
}

// NewProvisioner returns a new Splunk Provisioner using global
// configuration for finding the Splunk services.
func (f splunkProvisionerFactory) NewProvisioner(ctx context.Context, sad *splunkmodel.SplunkCredentials, publisher gateway.EventPublisher) (provisioner.Provisioner, error) {
	return f.splunkProvisioner(ctx, sad, publisher)
}
