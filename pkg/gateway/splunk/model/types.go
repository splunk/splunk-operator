package model

import (
	"github.com/go-logr/logr"
	"github.com/go-resty/resty/v2"
)

// SplunkCredentials contains the information necessary to communicate with
// the Splunk service
type SplunkCredentials struct {

	// Address holds the URL for splunk service
	Address string `json:"address"`

	//Port port to connect
	Port int32 `json:"port"`

	//Namespace where the splunk services are created
	Namespace string `json:"namespace,omitempty"`

	//ServicesNamespace  optional for services endpoints
	ServicesNamespace string `json:"servicesNs,omitempty"`

	//User optional for services endpoints
	User string `json:"user,omitempty"`

	//App optional for services endpoints
	App string `json:"app,omitempty"`

	//CredentialsName The name of the secret containing the Splunk credentials (requires
	// keys "username" and "password").
	// TODO FIXME need to change this to map as key value
	CredentialsName string `json:"credentialsName"`

	//TrustedCAFile Server trusted CA file
	TrustedCAFile string `json:"trustedCAFile,omitempty"`

	//ClientCertificateFile client certification if we are using to connect to server
	ClientCertificateFile string `json:"clientCertificationFile,omitempty"`

	//ClientPrivateKeyFile client private key if we are using to connect to server
	ClientPrivateKeyFile string `json:"clientPrivateKeyFile,omitempty"`

	// DisableCertificateVerification disables verification of splunk
	// certificates when using HTTPS to connect to the Splunk.
	DisableCertificateVerification bool `json:"disableCertificateVerification,omitempty"`
}

type splunkGatewayFactory struct {
	log logr.Logger
	//credentials to log on to splunk
	credentials *SplunkCredentials
	// client for talking to splunk
	client *resty.Client
}
