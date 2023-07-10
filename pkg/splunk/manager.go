package splunk

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	gateway "github.com/splunk/splunk-operator/pkg/gateway/splunk/services"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

type Factory func(client splcommon.ControllerClient, cr *enterpriseApi.ClusterMaster, gatewayFactory gateway.Factory) (SplunkManager, error)

type SplunkManager interface {
	ApplyClusterManager(ctx context.Context) (reconcile.Result, error)
}
