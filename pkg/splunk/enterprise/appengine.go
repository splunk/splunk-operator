package enterprise

import (
	"context"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// AppFrameworkEngine provides a stable internal API boundary for the app framework pipeline.
type AppFrameworkEngine struct{}

// DefaultAppFrameworkEngine is the shared engine instance for reconcilers.
var DefaultAppFrameworkEngine = &AppFrameworkEngine{}

// EngineInput bundles app framework inputs for pipeline calls.
type EngineInput struct {
	Ctx                context.Context
	Client             splcommon.ControllerClient
	Target             splcommon.MetaObject
	AppFrameworkConfig *enterpriseApi.AppFrameworkSpec
	AppDeployContext   *enterpriseApi.AppDeploymentContext
	Scope              enterpriseApi.ScopeType
	Phase              enterpriseApi.Phase
	PodTemplateSpec    *corev1.PodTemplateSpec
}

// EngineOutput wraps pipeline outputs to keep the API stable.
type EngineOutput struct {
	Result *reconcile.Result
}

// EnsureAppFrameworkStatus validates and migrates app framework status when needed.
func (e *AppFrameworkEngine) EnsureAppFrameworkStatus(input EngineInput) error {
	return checkAndMigrateAppDeployStatus(input.Ctx, input.Client, input.Target, input.AppDeployContext, input.AppFrameworkConfig, input.Scope == enterpriseApi.ScopeLocal)
}

// EnsureAppRepoState initializes clients and checks remote app status when app framework is configured.
func (e *AppFrameworkEngine) EnsureAppRepoState(input EngineInput) error {
	return initAndCheckAppInfoStatus(input.Ctx, input.Client, input.Target, input.AppFrameworkConfig, input.AppDeployContext)
}

// RunAppFrameworkIfReady runs the app framework pipeline only when the CR is ready.
func (e *AppFrameworkEngine) RunAppFrameworkIfReady(input EngineInput) EngineOutput {
	if input.Phase != enterpriseApi.PhaseReady {
		return EngineOutput{Result: nil}
	}

	return EngineOutput{
		Result: handleAppFrameworkActivity(input.Ctx, input.Client, input.Target, input.AppDeployContext, input.AppFrameworkConfig),
	}
}

// EnsureAppFrameworkStagingVolume adds the app framework staging volume when configured.
func (e *AppFrameworkEngine) EnsureAppFrameworkStagingVolume(input EngineInput) {
	setupAppsStagingVolume(input.Ctx, input.Client, input.Target, input.PodTemplateSpec, input.AppFrameworkConfig)
}
