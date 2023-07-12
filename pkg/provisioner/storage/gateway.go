package gateway

import "context"

// Credentials contains the information necessary to communicate with
// the Splunk service
type Credentials struct {

	// Address holds the URL for splunk service
	Address string `json:"address"`

	//Port port to connect
	Port int32 `json:"port"`

	//ServicesNamespace  optional for services endpoints
	ServicesNamespace string `json:"servicesNs,omitempty"`

	//User optional for services endpoints
	User string `json:"user,omitempty"`

	//App optional for services endpoints
	App string `json:"app,omitempty"`

	//CredentialsName The name of the secret containing the Splunk credentials (requires
	// keys "username" and "password").
	CredentialsName string `json:"credentialsName"`

	//TrustedCAFile Server trusted CA file
	TrustedCAFile string `json:"trustedCAFile,omitempty"`

	//ClientCertificateFile client certification if we are using to connect to server
	ClientCertificateFile string `json:"clientCertificationFile,omitempty"`

	//ClientPrivateKeyFile client private key if we are using to connect to server
	ClientPrivateKeyFile string `json:"clientPrivateKeyFile,omitempty"`

	// DisableCertificateVerification disables verification of splunk
	// certificates when using HTTPS to connect to the .
	DisableCertificateVerification bool `json:"disableCertificateVerification,omitempty"`
}

// EventPublisher is a function type for publishing events associated
// with provisioning.
type EventPublisher func(reason, message string)

// Factory is the interface for creating new Gateway objects.
type Factory interface {
	NewGateway(ctx context.Context, sad *Credentials, publisher EventPublisher) (Gateway, error)
}

// Gateway holds the state information for talking to
// splunk gateway backend.
type Gateway interface {
	GetClusterConfig() error
}
