/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package enterprise provides core Splunk Enterprise reconciliation logic.
package enterprise

import (
	"context"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// AppEngine defines the app framework operations used during reconciliation.
type AppEngine interface {
	ensureAppFrameworkStatus(ctx context.Context, client splcommon.ControllerClient, target splcommon.MetaObject, appDeployContext *enterpriseApi.AppDeploymentContext, appFrameworkConfig *enterpriseApi.AppFrameworkSpec, scope enterpriseApi.ScopeType) error
	ensureAppRepoState(ctx context.Context, client splcommon.ControllerClient, target splcommon.MetaObject, appFrameworkConfig *enterpriseApi.AppFrameworkSpec, appDeployContext *enterpriseApi.AppDeploymentContext) error
	runAppFrameworkIfReady(ctx context.Context, client splcommon.ControllerClient, target splcommon.MetaObject, appDeployContext *enterpriseApi.AppDeploymentContext, appFrameworkConfig *enterpriseApi.AppFrameworkSpec) *reconcile.Result
	ensureAppFrameworkStagingVolume(ctx context.Context, client splcommon.ControllerClient, target splcommon.MetaObject, podTemplateSpec *corev1.PodTemplateSpec, appFrameworkConfig *enterpriseApi.AppFrameworkSpec)
	changeAppSrcDeployInfoStatus(ctx context.Context, appSrc string, appSrcDeployStatus map[string]enterpriseApi.AppSrcDeployInfo, repoState enterpriseApi.AppRepoState, oldDeployStatus enterpriseApi.AppDeploymentStatus, newDeployStatus enterpriseApi.AppDeploymentStatus)
	changePhaseInfo(ctx context.Context, desiredReplicas int32, appSrc string, appSrcDeployStatus map[string]enterpriseApi.AppSrcDeployInfo)
	removeStaleEntriesFromAuxPhaseInfo(ctx context.Context, desiredReplicas int32, appSrc string, appSrcDeployStatus map[string]enterpriseApi.AppSrcDeployInfo)
}

// appFrameworkEngine is the default AppEngine implementation.
type appFrameworkEngine struct{}

// NewAppFrameworkEngine returns a default AppEngine implementation.
func NewAppFrameworkEngine() AppEngine {
	return &appFrameworkEngine{}
}

// ensureAppFrameworkStatus validates and migrates app framework status when needed.
func (e *appFrameworkEngine) ensureAppFrameworkStatus(ctx context.Context, client splcommon.ControllerClient, target splcommon.MetaObject, appDeployContext *enterpriseApi.AppDeploymentContext, appFrameworkConfig *enterpriseApi.AppFrameworkSpec, scope enterpriseApi.ScopeType) error {
	return checkAndMigrateAppDeployStatus(ctx, client, target, appDeployContext, appFrameworkConfig, scope == enterpriseApi.ScopeLocal)
}

// ensureAppRepoState initializes clients and checks remote app status when app framework is configured.
func (e *appFrameworkEngine) ensureAppRepoState(ctx context.Context, client splcommon.ControllerClient, target splcommon.MetaObject, appFrameworkConfig *enterpriseApi.AppFrameworkSpec, appDeployContext *enterpriseApi.AppDeploymentContext) error {
	return initAndCheckAppInfoStatus(ctx, client, target, appFrameworkConfig, appDeployContext)
}

// runAppFrameworkIfReady runs the app framework pipeline only when the CR is ready.
func (e *appFrameworkEngine) runAppFrameworkIfReady(ctx context.Context, client splcommon.ControllerClient, target splcommon.MetaObject, appDeployContext *enterpriseApi.AppDeploymentContext, appFrameworkConfig *enterpriseApi.AppFrameworkSpec) *reconcile.Result {
	return handleAppFrameworkActivity(ctx, client, target, appDeployContext, appFrameworkConfig)
}

// changePhaseInfo updates deployment phase tracking for the given app source.
func (e *appFrameworkEngine) changePhaseInfo(ctx context.Context, desiredReplicas int32, appSrc string, appSrcDeployStatus map[string]enterpriseApi.AppSrcDeployInfo) {
	changePhaseInfo(ctx, desiredReplicas, appSrc, appSrcDeployStatus)
}

// removeStaleEntriesFromAuxPhaseInfo trims aux phase tracking entries.
func (e *appFrameworkEngine) removeStaleEntriesFromAuxPhaseInfo(ctx context.Context, desiredReplicas int32, appSrc string, appSrcDeployStatus map[string]enterpriseApi.AppSrcDeployInfo) {
	removeStaleEntriesFromAuxPhaseInfo(ctx, desiredReplicas, appSrc, appSrcDeployStatus)
}

// changeAppSrcDeployInfoStatus records a deployment status transition.
func (e *appFrameworkEngine) changeAppSrcDeployInfoStatus(ctx context.Context, appSrc string, appSrcDeployStatus map[string]enterpriseApi.AppSrcDeployInfo, repoState enterpriseApi.AppRepoState, oldDeployStatus enterpriseApi.AppDeploymentStatus, newDeployStatus enterpriseApi.AppDeploymentStatus) {
	changeAppSrcDeployInfoStatus(ctx, appSrc, appSrcDeployStatus, repoState, oldDeployStatus, newDeployStatus)
}

// ensureAppFrameworkStagingVolume adds the staging volume for app framework use.
func (e *appFrameworkEngine) ensureAppFrameworkStagingVolume(ctx context.Context, client splcommon.ControllerClient, target splcommon.MetaObject, podTemplateSpec *corev1.PodTemplateSpec, appFrameworkConfig *enterpriseApi.AppFrameworkSpec) {
	setupAppsStagingVolume(ctx, client, target, podTemplateSpec, appFrameworkConfig)
}
