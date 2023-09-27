package impl

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/go-resty/resty/v2"
	gateway "github.com/splunk/splunk-operator/pkg/gateway/splunk/license-manager"
	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	"time"

	model "github.com/splunk/splunk-operator/pkg/splunk/model"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type splunkGatewayFactory struct {
	log logr.Logger
	//credentials to log on to splunk
	credentials *splunkmodel.SplunkCredentials
	// client for talking to splunk
	client *resty.Client
}

// NewGatewayFactory  new gateway factory to create gateway interface
func NewGatewayFactory() gateway.Factory {
	factory := splunkGatewayFactory{}
	err := factory.init()
	if err != nil {
		return nil // FIXME we have to throw some kind of exception or error here
	}
	return factory
}

func (f *splunkGatewayFactory) init() error {
	return nil
}

func (f splunkGatewayFactory) splunkGateway(ctx context.Context, sad *splunkmodel.SplunkCredentials, publisher model.EventPublisher) (*splunkGateway, error) {
	gatewayLogger := log.FromContext(ctx)
	reqLogger := log.FromContext(ctx)
	f.log = reqLogger.WithName("splunkGateway")

	f.client = resty.New()
	// Enable debug mode
	f.client.SetDebug(true)
	// or One can disable security check (https)
	f.client.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: sad.DisableCertificateVerification})
	// Set client timeout as per your need
	f.client.SetTimeout(1 * time.Minute)
	namespace := "default"
	if len(sad.Namespace) > 0 {
		namespace = sad.Namespace
	}
	//splunkURL := fmt.Sprintf("https://%s:%d/%s", sad.Address, sad.Port, sad.ServicesNamespace)
	splunkURL := fmt.Sprintf("https://%s.%s:%d", sad.Address, namespace, sad.Port)
	f.client.SetBaseURL(splunkURL)
	f.client.SetBasicAuth("admin", sad.CredentialsName)
	f.client.SetHeader("Content-Type", "application/json")
	f.client.SetHeader("Accept", "application/json")
	f.credentials = sad

	gatewayLogger.Info("new splunk manager created to access rest endpoint")
	newGateway := &splunkGateway{
		credentials: f.credentials,
		client:      f.client,
		log:         f.log,
		debugLog:    f.log,
		publisher:   publisher,
	}
	f.log.Info("splunk settings",
		"endpoint", f.credentials.Address,
		"CACertFile", f.credentials.TrustedCAFile,
		"ClientCertFile", f.credentials.ClientCertificateFile,
		"ClientPrivKeyFile", f.credentials.ClientPrivateKeyFile,
		"TLSInsecure", f.credentials.DisableCertificateVerification,
	)
	return newGateway, nil
}

// NewGateway returns a new Splunk Gateway using global
// configuration for finding the Splunk services.
func (f splunkGatewayFactory) NewGateway(ctx context.Context, sad *splunkmodel.SplunkCredentials, publisher model.EventPublisher) (gateway.Gateway, error) {
	return f.splunkGateway(ctx, sad, publisher)
}
