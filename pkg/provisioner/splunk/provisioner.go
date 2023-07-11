package indexer

import (
	"context"

	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
	gateway "github.com/splunk/splunk-operator/pkg/gateway/splunk/services"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// EventPublisher is a function type for publishing events associated
// with gateway functions.
type EventPublisher func(ctx context.Context, eventType, reason, message string)

// Factory is the interface for creating new Provisioner objects.
type Factory interface {
	NewProvisioner(ctx context.Context, sad *splunkmodel.SplunkCredentials, publisher gateway.EventPublisher) (Provisioner, error)
}

// Provisioner holds the state information for talking to
// splunk provisioner backend.
type Provisioner interface {

	// SetClusterManagerStatus set cluster manager status
	SetClusterManagerStatus(ctx context.Context, cr splcommon.MetaObject) error
}
