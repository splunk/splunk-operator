package enterprise

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"

	//model "github.com/splunk/splunk-operator/pkg/provisioner/splunk/model"
	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	provisioner "github.com/splunk/splunk-operator/pkg/provisioner/splunk"
	splunkprovisionerimpl "github.com/splunk/splunk-operator/pkg/provisioner/splunk/implementation"
	manager "github.com/splunk/splunk-operator/pkg/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	types "github.com/splunk/splunk-operator/pkg/splunk/model"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	//cmmodel "github.com/splunk/splunk-operator/pkg/provisioner/splunk/cluster-manager/model"
	model "github.com/splunk/splunk-operator/pkg/splunk/model"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type splunkManagerFactory struct {
	log logr.Logger
	// Gateway Factory
	provisionerFactory provisioner.Factory
	runInTestMode      bool
}

// NewManagerFactory  new manager factory to create manager interface
func NewManagerFactory(runInTestMode bool) manager.Factory {
	factory := splunkManagerFactory{}
	factory.runInTestMode = runInTestMode

	err := factory.init(runInTestMode)
	if err != nil {
		return nil // FIXME we have to throw some kind of exception or error here
	}
	return factory
}

func (f *splunkManagerFactory) init(runInTestMode bool) error {
	f.provisionerFactory = splunkprovisionerimpl.NewProvisionerFactory(runInTestMode)
	return nil
}

func (f splunkManagerFactory) splunkManager(ctx context.Context, info *types.ReconcileInfo, publisher model.EventPublisher) (*splunkManager, error) {
	provisionerLogger := log.FromContext(ctx)
	reqLogger := log.FromContext(ctx)
	f.log = reqLogger.WithName("splunkProvisioner")

	sad := &splunkmodel.SplunkCredentials{}
	if !f.runInTestMode {
		defaultSecretObjName := splcommon.GetNamespaceScopedSecretName(info.Namespace)
		defaultSecret, err := splutil.GetSecretByName(ctx, info.Client, info.Namespace, info.Name, defaultSecretObjName)
		if err != nil {
			publisher(ctx, "Warning", "splunkManager", fmt.Sprintf("Could not access default secret object to fetch admin password. Reason %v", err))
			return nil, fmt.Errorf("could not access default secret object to fetch admin password. Reason %v", err)
		}

		//Get the admin password from the secret object
		adminPwd, foundSecret := defaultSecret.Data["password"]
		if !foundSecret {
			publisher(ctx, "Warning", "splunkManager", fmt.Sprintf("Could not find admin password "))
			return nil, fmt.Errorf("could not find admin password ")
		}

		service := getSplunkService(ctx, info.MetaObject, &info.CommonSpec, GetInstantTypeFromKind(info.Kind), false)

		sad = &splunkmodel.SplunkCredentials{
			Address:                        service.Name,
			Port:                           8089,
			ServicesNamespace:              "-",
			User:                           "admin",
			App:                            "-",
			CredentialsName:                string(adminPwd[:]),
			TrustedCAFile:                  "",
			ClientCertificateFile:          "",
			ClientPrivateKeyFile:           "",
			DisableCertificateVerification: true,
			Namespace:                      info.Namespace,
		}
	}
	provisionerLogger.Info("new splunk manager created to access rest endpoint")
	provisioner, err := f.provisionerFactory.NewProvisioner(ctx, sad, publisher)
	if err != nil {
		return nil, err
	}

	newProvisioner := &splunkManager{
		log:         f.log,
		debugLog:    f.log,
		publisher:   publisher,
		provisioner: provisioner,
		client:      info.Client,
	}

	return newProvisioner, nil
}

// NewProvisioner returns a new Splunk Provisioner using global
// configuration for finding the Splunk services.
func (f splunkManagerFactory) NewManager(ctx context.Context, info *types.ReconcileInfo, publisher model.EventPublisher) (manager.SplunkManager, error) {
	return f.splunkManager(ctx, info, publisher)
}
