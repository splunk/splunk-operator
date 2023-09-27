package indexer

import (
	"context"

	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	provmodel "github.com/splunk/splunk-operator/pkg/provisioner/splunk/model"
	model "github.com/splunk/splunk-operator/pkg/splunk/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Factory is the interface for creating new Provisioner objects.
type Factory interface {
	NewProvisioner(ctx context.Context, sad *splunkmodel.SplunkCredentials, publisher model.EventPublisher) (Provisioner, error)
}

// Provisioner holds the state information for talking to
// splunk provisioner backend.
type Provisioner interface {

	// GetClusterManagerStatus set cluster manager status
	GetClusterManagerStatus(ctx context.Context, conditions *[]metav1.Condition) (result provmodel.Result, err error)

	// CheckClusterManagerHealth
	CheckClusterManagerHealth(ctx context.Context) (result provmodel.Result, err error)

	//SetClusterInMaintenanceMode
	SetClusterInMaintenanceMode(ctx context.Context, mode bool) error

	// IsClusterInMaintenanceMode
	IsClusterInMaintenanceMode(ctx context.Context) (bool, error)

	// GetLicenseLocalPeer
	GetLicenseLocalPeer(ctx context.Context, conditions *[]metav1.Condition) (result provmodel.Result, err error)

	GetLicenseStatus(ctx context.Context, conditions *[]metav1.Condition) (result provmodel.Result, err error)
}
