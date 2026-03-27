package appframework

import (
	"context"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// AppEngine defines the app framework operation used during reconciliation
type AppEngine interface {
	// EnsureStatus validates and migrates app framework status when needed.
	EnsureStatus(ctx context.Context, client splcommon.ControllerClient, target splcommon.MetaObject, appDeployContext *enterpriseApi.AppDeploymentContext, appFrameworkConfig *enterpriseApi.AppFrameworkSpec, scope enterpriseApi.ScopeType) error

	// EnsureRepoState initializes clients and checks remote app status when app framework is configured.
	EnsureRepoState(ctx context.Context, client splcommon.ControllerClient, target splcommon.MetaObject, appFrameworkConfig *enterpriseApi.AppFrameworkSpec, appDeployContext *enterpriseApi.AppDeploymentContext) error

	// Run runs the app framework pipeline.
	Run(ctx context.Context, client splcommon.ControllerClient, target splcommon.MetaObject, appDeployContext *enterpriseApi.AppDeploymentContext, appFrameworkConfig *enterpriseApi.AppFrameworkSpec) *reconcile.Result

	// EnsureStagingVolume adds the staging volume for app framework use.
	EnsureStagingVolume(ctx context.Context, client splcommon.ControllerClient, target splcommon.MetaObject, podTemplateSpec *corev1.PodTemplateSpec, appFrameworkConfig *enterpriseApi.AppFrameworkSpec)

	// SetDeployStatus records a deployment status transition.
	SetDeployStatus(ctx context.Context, appSrc string, appSrcDeployStatus map[string]enterpriseApi.AppSrcDeployInfo, repoState enterpriseApi.AppRepoState, oldDeployStatus enterpriseApi.AppDeploymentStatus, newDeployStatus enterpriseApi.AppDeploymentStatus)

	// UpdatePhaseInfo updates deployment phase tracking for the given app source.
	UpdatePhaseInfo(ctx context.Context, desiredReplicas int32, appSrc string, appSrcDeployStatus map[string]enterpriseApi.AppSrcDeployInfo)

	// PrunePhaseInfo trims stale aux phase tracking entries.
	PrunePhaseInfo(ctx context.Context, desiredReplicas int32, appSrc string, appSrcDeployStatus map[string]enterpriseApi.AppSrcDeployInfo)
}
