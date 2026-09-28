// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package upgrade adapts Kubernetes-backed dependency state into the pure
// upgrade workflow used by reconciler packages.
package upgrade

import (
	"context"
	"fmt"
	"regexp"
	"sort"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	workflowupgrade "github.com/splunk/splunk-operator/pkg/splunk/workflow/upgrade"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	runtime "sigs.k8s.io/controller-runtime/pkg/client"
)

// ClusterInfoFunc supplies current IndexerCluster information from its manager.
type ClusterInfoFunc func(context.Context) (*splclient.ClusterInfo, error)

// UpgradePathValidation collects Kubernetes-backed dependency state and passes
// only pure state to workflow/upgrade. Kubernetes events are emitted here so
// the workflow remains reusable without Kubernetes side effects.
func UpgradePathValidation(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, spec enterpriseApi.CommonSplunkSpec, getClusterInfo ClusterInfoFunc) (bool, error) {
	logger := logging.FromContext(ctx).With("func", "UpgradePathValidation", "name", cr.GetName(), "namespace", cr.GetNamespace())
	eventPublisher := k8sops.GetEventPublisher(ctx, cr)

	state := workflowupgrade.State{
		Kind:                 cr.GroupVersionKind().Kind,
		Name:                 cr.GetName(),
		Image:                spec.Image,
		HasLicenseManagerRef: spec.LicenseManagerRef.Name != "",
		HasClusterManagerRef: spec.ClusterManagerRef.Name != "",
	}
	logger.InfoContext(ctx, "kind is set to", "kind", state.Kind)

	if state.Kind == "Standalone" || state.Kind == "LicenseManager" {
		return true, nil
	}

	if state.HasLicenseManagerRef {
		licenseManager := &enterpriseApi.LicenseManager{}
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: spec.LicenseManagerRef.Name}
		err := c.Get(ctx, namespacedName, licenseManager)
		if err == nil {
			image, imageErr := k8sops.GetStatefulSetImage(ctx, c, licenseManager, splcommon.SplunkLicenseManager)
			if imageErr != nil {
				eventPublisher.Warning(ctx, splcommon.EventReasonUpgradeCheckFailed, "Could not get the License Manager image — check operator logs for details")
				logger.ErrorContext(ctx, "unable to get LicenseManager current image", "error", imageErr)
				return false, imageErr
			}
			state.LicenseManager = &workflowupgrade.ResourceState{Type: "license manager", Name: licenseManager.Name, Image: image, Phase: string(licenseManager.Status.Phase)}
		} else if !k8serrors.IsNotFound(err) {
			return false, err
		}
	}

	if state.Kind == "ClusterManager" {
		if !state.HasLicenseManagerRef {
			return true, nil
		}
		continueReconcile, err := statefulSetGate(ctx, c, cr, splcommon.SplunkClusterManager)
		if err != nil || !continueReconcile {
			return continueReconcile, err
		}
		return validate(ctx, eventPublisher, state)
	}

	if state.HasClusterManagerRef {
		clusterManager := &enterpriseApi.ClusterManager{}
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: spec.ClusterManagerRef.Name}
		err := c.Get(ctx, namespacedName, clusterManager)
		if err == nil {
			image, imageErr := k8sops.GetStatefulSetImage(ctx, c, clusterManager, splcommon.SplunkClusterManager)
			if imageErr != nil {
				eventPublisher.Warning(ctx, splcommon.EventReasonUpgradeCheckFailed, "Could not get the Cluster Manager image — check operator logs for details")
				logger.ErrorContext(ctx, "unable to get ClusterManager current image", "error", imageErr)
				return false, imageErr
			}
			state.ClusterManager = &workflowupgrade.ResourceState{Type: "cluster manager", Name: clusterManager.Name, Image: image, Phase: string(clusterManager.Status.Phase)}
		} else {
			eventPublisher.Warning(ctx, splcommon.EventReasonUpgradeCheckFailed, "Could not find the Cluster Manager — check operator logs for details")
			logger.ErrorContext(ctx, "unable to get ClusterManager", "error", err)
		}
	}

	if state.Kind == "IndexerCluster" && state.ClusterManager != nil {
		return validateIndexerCluster(ctx, c, cr, spec, getClusterInfo, eventPublisher, state)
	}

	if state.Kind == "SearchHeadCluster" {
		continueReconcile, err := statefulSetGate(ctx, c, cr, splcommon.SplunkSearchHead)
		if err != nil || !continueReconcile {
			return continueReconcile, err
		}
		return validate(ctx, eventPublisher, state)
	}

	searchHeadCluster, err := searchHeadClusterState(ctx, c, cr, spec.ClusterManagerRef, eventPublisher, logger)
	if err != nil {
		return false, err
	}
	state.SearchHeadCluster = searchHeadCluster

	if state.Kind == "MonitoringConsole" {
		state.MonitoringConsoleDependencies, err = monitoringConsoleDependencies(ctx, c, cr, eventPublisher, logger)
		if err != nil {
			return false, err
		}
	}

	return validate(ctx, eventPublisher, state)
}

func validateIndexerCluster(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, spec enterpriseApi.CommonSplunkSpec, getClusterInfo ClusterInfoFunc, eventPublisher *k8sops.K8EventPublisher, state workflowupgrade.State) (bool, error) {
	state.ValidateIndexerCluster = true
	if getClusterInfo == nil {
		continueReconcile, err := validate(ctx, eventPublisher, state)
		if err != nil || !continueReconcile {
			return continueReconcile, err
		}
		return false, fmt.Errorf("cluster info provider is required for IndexerCluster upgrade validation")
	}
	clusterInfo, err := getClusterInfo(ctx)
	if err != nil {
		return false, fmt.Errorf("could not get cluster info from cluster manager")
	}
	state.ClusterInfo = clusterInfo
	if clusterInfo.MultiSite == "true" {
		state.PreviousIndexer, err = previousIndexerState(ctx, c, cr, spec.ClusterManagerRef)
		if err != nil {
			return false, err
		}
	}
	return validate(ctx, eventPublisher, state)
}

func validate(ctx context.Context, eventPublisher *k8sops.K8EventPublisher, state workflowupgrade.State) (bool, error) {
	result, err := workflowupgrade.Validate(state)
	for _, event := range result.Events {
		eventPublisher.Warning(ctx, event.Reason, event.Message)
	}
	return result.Continue, err
}

func statefulSetGate(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, instanceType splcommon.InstanceType) (bool, error) {
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: splutil.GetSplunkStatefulsetName(instanceType, cr.GetName())}
	statefulSet := &appsv1.StatefulSet{}
	err := c.Get(ctx, namespacedName, statefulSet)
	if err != nil && !k8serrors.IsNotFound(err) {
		return false, nil
	}
	return true, nil
}

func previousIndexerState(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, managerRef corev1.ObjectReference) (*workflowupgrade.ResourceState, error) {
	indexerList := &enterpriseApi.IndexerClusterList{}
	if err := c.List(ctx, indexerList, runtime.InNamespace(cr.GetNamespace())); err != nil {
		return nil, fmt.Errorf("list IndexerCluster in namespace %s: %w", cr.GetNamespace(), err)
	}

	matching := make([]enterpriseApi.IndexerCluster, 0, len(indexerList.Items))
	for _, indexer := range indexerList.Items {
		if indexer.Spec.ClusterManagerRef == managerRef {
			matching = append(matching, indexer)
		}
	}
	sort.SliceStable(matching, func(i, j int) bool {
		return siteName(matching[i]) < siteName(matching[j])
	})

	for i := range matching {
		if matching[i].Name != cr.GetName() || i == 0 {
			continue
		}
		previous := &matching[i-1]
		previousImage, _ := k8sops.GetStatefulSetImage(ctx, c, previous, splcommon.SplunkIndexer)
		return &workflowupgrade.ResourceState{Type: "indexer", Name: previous.Name, Image: previousImage, Phase: string(previous.Status.Phase)}, nil
	}
	return nil, nil
}

func searchHeadClusterState(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, managerRef corev1.ObjectReference, eventPublisher *k8sops.K8EventPublisher, logger interface {
	ErrorContext(context.Context, string, ...any)
}) (*workflowupgrade.ResourceState, error) {
	searchHeadList := &enterpriseApi.SearchHeadClusterList{}
	if err := c.List(ctx, searchHeadList, runtime.InNamespace(cr.GetNamespace())); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	for i := range searchHeadList.Items {
		searchHead := &searchHeadList.Items[i]
		if searchHead.Spec.ClusterManagerRef.Name != managerRef.Name {
			continue
		}
		searchHeadImage, err := k8sops.GetStatefulSetImage(ctx, c, searchHead, splcommon.SplunkSearchHead)
		if err != nil {
			eventPublisher.Warning(ctx, splcommon.EventReasonUpgradeCheckFailed, "Could not get the Search Head Cluster image — check operator logs for details")
			logger.ErrorContext(ctx, "unable to get SearchHeadCluster current image", "error", err)
			return nil, err
		}
		return &workflowupgrade.ResourceState{Type: "search head", Name: searchHead.Name, Image: searchHeadImage, Phase: string(searchHead.Status.Phase)}, nil
	}
	return nil, nil
}

func monitoringConsoleDependencies(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, eventPublisher *k8sops.K8EventPublisher, logger interface {
	ErrorContext(context.Context, string, ...any)
}) ([]workflowupgrade.ResourceState, error) {
	dependencies := make([]workflowupgrade.ResourceState, 0)
	listOpts := []runtime.ListOption{runtime.InNamespace(cr.GetNamespace())}

	clusterManagers := &enterpriseApi.ClusterManagerList{}
	if err := c.List(ctx, clusterManagers, listOpts...); err != nil && !k8serrors.IsNotFound(err) {
		eventPublisher.Warning(ctx, splcommon.EventReasonUpgradeCheckFailed, "Could not find the Cluster Manager list — check operator logs for details")
		logger.ErrorContext(ctx, "unable to get ClusterManager list", "error", err)
		return nil, err
	}
	for _, manager := range clusterManagers.Items {
		if manager.Spec.MonitoringConsoleRef.Name == cr.GetName() {
			dependencies = append(dependencies, workflowupgrade.ResourceState{Type: "cluster manager", Name: manager.Name, Phase: string(manager.Status.Phase)})
		}
	}

	searchHeads := &enterpriseApi.SearchHeadClusterList{}
	if err := c.List(ctx, searchHeads, listOpts...); err != nil && !k8serrors.IsNotFound(err) {
		eventPublisher.Warning(ctx, splcommon.EventReasonUpgradeCheckFailed, "Could not find the Search Head Cluster list — check operator logs for details")
		logger.ErrorContext(ctx, "unable to get SearchHeadCluster list", "error", err)
		return nil, err
	}
	for _, searchHead := range searchHeads.Items {
		if searchHead.Spec.MonitoringConsoleRef.Name == cr.GetName() {
			dependencies = append(dependencies, workflowupgrade.ResourceState{Type: "search head", Name: searchHead.Name, Phase: string(searchHead.Status.Phase)})
		}
	}

	indexers := &enterpriseApi.IndexerClusterList{}
	if err := c.List(ctx, indexers, listOpts...); err != nil && !k8serrors.IsNotFound(err) {
		eventPublisher.Warning(ctx, splcommon.EventReasonUpgradeCheckFailed, "Could not find the Indexer list — check operator logs for details")
		logger.ErrorContext(ctx, "unable to get IndexerCluster list", "error", err)
		return nil, err
	}
	for _, indexer := range indexers.Items {
		if indexer.Name == cr.GetName() {
			dependencies = append(dependencies, workflowupgrade.ResourceState{Type: "indexer", Name: indexer.Name, Phase: string(indexer.Status.Phase)})
		}
	}

	standalones := &enterpriseApi.StandaloneList{}
	if err := c.List(ctx, standalones, listOpts...); err != nil && !k8serrors.IsNotFound(err) {
		eventPublisher.Warning(ctx, splcommon.EventReasonUpgradeCheckFailed, "Could not find the Standalone list — check operator logs for details")
		logger.ErrorContext(ctx, "unable to get Standalone list", "error", err)
		return nil, err
	}
	for _, standalone := range standalones.Items {
		if standalone.Name == cr.GetName() {
			dependencies = append(dependencies, workflowupgrade.ResourceState{Type: "standalone", Name: standalone.Name, Phase: string(standalone.Status.Phase)})
		}
	}

	return dependencies, nil
}

func siteName(cr enterpriseApi.IndexerCluster) string {
	match := regexp.MustCompile(`site:\s+(\w+)`).FindStringSubmatch(cr.Spec.Defaults)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}
