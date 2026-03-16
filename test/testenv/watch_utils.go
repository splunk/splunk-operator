// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package testenv

import (
	"context"
	"fmt"
	"time"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	corev1 "k8s.io/api/core/v1"
	wait "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WatchForCRPhase is a generic function to wait for any CR to reach expected phase
// Uses PollInterval for consistency with existing test behavior
func (testenv *TestCaseEnv) WatchForCRPhase(ctx context.Context, deployment *Deployment, namespace, crName, crKind string, obj client.Object, expectedPhase enterpriseApi.Phase, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, PollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		err := deployment.testenv.GetKubeClient().Get(ctx, client.ObjectKey{Name: crName, Namespace: namespace}, obj)
		if err != nil {
			testenv.Log.Info("Failed to get CR", "kind", crKind, "name", crName, "error", err)
			return false, nil
		}

		var currentPhase enterpriseApi.Phase
		switch cr := obj.(type) {
		case *enterpriseApi.Standalone:
			currentPhase = cr.Status.Phase
		case *enterpriseApi.ClusterManager:
			currentPhase = cr.Status.Phase
		case *enterpriseApi.SearchHeadCluster:
			if cr.Status.Phase == expectedPhase && cr.Status.DeployerPhase == expectedPhase {
				testenv.Log.Info("CR reached expected phase", "kind", crKind, "name", crName, "phase", expectedPhase)
				return true, nil
			}
			return false, nil
		case *enterpriseApi.IndexerCluster:
			currentPhase = cr.Status.Phase
		case *enterpriseApi.MonitoringConsole:
			currentPhase = cr.Status.Phase
		case *enterpriseApi.LicenseManager:
			currentPhase = cr.Status.Phase
		case *enterpriseApi.IngestorCluster:
			currentPhase = cr.Status.Phase
		case *enterpriseApiV3.LicenseMaster:
			currentPhase = cr.Status.Phase
		case *enterpriseApiV3.ClusterMaster:
			currentPhase = cr.Status.Phase
		default:
			return false, fmt.Errorf("unsupported CR type: %T", obj)
		}

		if currentPhase == expectedPhase {
			testenv.Log.Info("CR reached expected phase", "kind", crKind, "name", crName, "phase", expectedPhase)
			return true, nil
		}
		return false, nil
	})
}

// WatchForStandalonePhase waits for Standalone to reach expected phase
func (testenv *TestCaseEnv) WatchForStandalonePhase(ctx context.Context, deployment *Deployment, namespace, crName string, expectedPhase enterpriseApi.Phase, timeout time.Duration) error {
	return testenv.WatchForCRPhase(ctx, deployment, namespace, crName, "Standalone", &enterpriseApi.Standalone{}, expectedPhase, timeout)
}

// WatchForClusterManagerPhase waits for ClusterManager to reach expected phase
func (testenv *TestCaseEnv) WatchForClusterManagerPhase(ctx context.Context, deployment *Deployment, namespace, crName string, expectedPhase enterpriseApi.Phase, timeout time.Duration) error {
	return testenv.WatchForCRPhase(ctx, deployment, namespace, crName, "ClusterManager", &enterpriseApi.ClusterManager{}, expectedPhase, timeout)
}

// WatchForSearchHeadClusterPhase waits for SearchHeadCluster to reach expected phase (checks both Phase and DeployerPhase)
func (testenv *TestCaseEnv) WatchForSearchHeadClusterPhase(ctx context.Context, deployment *Deployment, namespace, crName string, expectedPhase enterpriseApi.Phase, timeout time.Duration) error {
	return testenv.WatchForCRPhase(ctx, deployment, namespace, crName, "SearchHeadCluster", &enterpriseApi.SearchHeadCluster{}, expectedPhase, timeout)
}

// WatchForIndexerClusterPhase waits for IndexerCluster to reach expected phase
func (testenv *TestCaseEnv) WatchForIndexerClusterPhase(ctx context.Context, deployment *Deployment, namespace, crName string, expectedPhase enterpriseApi.Phase, timeout time.Duration) error {
	return testenv.WatchForCRPhase(ctx, deployment, namespace, crName, "IndexerCluster", &enterpriseApi.IndexerCluster{}, expectedPhase, timeout)
}

// WatchForMonitoringConsolePhase waits for MonitoringConsole to reach expected phase
func (testenv *TestCaseEnv) WatchForMonitoringConsolePhase(ctx context.Context, deployment *Deployment, namespace, crName string, expectedPhase enterpriseApi.Phase, timeout time.Duration) error {
	return testenv.WatchForCRPhase(ctx, deployment, namespace, crName, "MonitoringConsole", &enterpriseApi.MonitoringConsole{}, expectedPhase, timeout)
}

// WatchForLicenseManagerPhase waits for LicenseManager to reach expected phase
func (testenv *TestCaseEnv) WatchForLicenseManagerPhase(ctx context.Context, deployment *Deployment, namespace, crName string, expectedPhase enterpriseApi.Phase, timeout time.Duration) error {
	return testenv.WatchForCRPhase(ctx, deployment, namespace, crName, "LicenseManager", &enterpriseApi.LicenseManager{}, expectedPhase, timeout)
}

// WatchForLicenseMasterPhase waits for LicenseMaster to reach expected phase
func (testenv *TestCaseEnv) WatchForLicenseMasterPhase(ctx context.Context, deployment *Deployment, namespace, crName string, expectedPhase enterpriseApi.Phase, timeout time.Duration) error {
	return testenv.WatchForCRPhase(ctx, deployment, namespace, crName, "LicenseMaster", &enterpriseApiV3.LicenseMaster{}, expectedPhase, timeout)
}

// WatchForClusterMasterPhase waits for ClusterMaster to reach expected phase
func (testenv *TestCaseEnv) WatchForClusterMasterPhase(ctx context.Context, deployment *Deployment, namespace, crName string, expectedPhase enterpriseApi.Phase, timeout time.Duration) error {
	return testenv.WatchForCRPhase(ctx, deployment, namespace, crName, "ClusterMaster", &enterpriseApiV3.ClusterMaster{}, expectedPhase, timeout)
}

// WatchForIngestorClusterPhase waits for IngestorCluster to reach expected phase
func (testenv *TestCaseEnv) WatchForIngestorClusterPhase(ctx context.Context, deployment *Deployment, namespace, crName string, expectedPhase enterpriseApi.Phase, timeout time.Duration) error {
	return testenv.WatchForCRPhase(ctx, deployment, namespace, crName, "IngestorCluster", &enterpriseApi.IngestorCluster{}, expectedPhase, timeout)
}

// WatchForAppPhaseChange uses optimized polling to wait for app phase changes on a CR
func (testenv *TestCaseEnv) WatchForAppPhaseChange(ctx context.Context, deployment *Deployment, namespace, crName, crKind, appSourceName, appName string, expectedPhase enterpriseApi.AppPhaseType, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, PollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		appDeploymentInfo, err := GetAppDeploymentInfo(ctx, deployment, testenv, crName, crKind, appSourceName, appName)
		if err != nil {
			testenv.Log.Info("Failed to get app deployment info", "app", appName, "error", err)
			return false, nil
		}

		if appDeploymentInfo.PhaseInfo.Phase == expectedPhase {
			testenv.Log.Info("App reached expected phase", "app", appName, "phase", expectedPhase, "kind", crKind, "name", crName)
			return true, nil
		}
		return false, nil
	})
}

// WatchForAllAppsPhaseChange uses optimized polling to wait for all apps in a list to reach a specific phase
func (testenv *TestCaseEnv) WatchForAllAppsPhaseChange(ctx context.Context, deployment *Deployment, namespace, crName, crKind, appSourceName string, appList []string, expectedPhase enterpriseApi.AppPhaseType, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, PollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		for _, appName := range appList {
			lookupAppName := appName
			if appInfo, ok := AppInfo[appName]; ok {
				if appFileName, ok := appInfo["filename"]; ok && appFileName != "" {
					lookupAppName = appFileName
				}
			}

			appDeploymentInfo, err := GetAppDeploymentInfo(ctx, deployment, testenv, crName, crKind, appSourceName, lookupAppName)
			if err != nil {
				testenv.Log.Info("Failed to get app deployment info", "app", appName, "error", err)
				return false, nil
			}

			if appDeploymentInfo.PhaseInfo.Phase != expectedPhase {
				return false, nil
			}
		}

		testenv.Log.Info("All apps reached expected phase", "count", len(appList), "phase", expectedPhase)
		return true, nil
	})
}

// WatchForEventWithReason uses optimized polling to wait for a Kubernetes event with specific reason
// Uses ShortPollInterval (2s) for faster response to time-sensitive events
func (testenv *TestCaseEnv) WatchForEventWithReason(ctx context.Context, deployment *Deployment, namespace, crName, eventReason string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, ShortPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		eventList := &corev1.EventList{}
		err := deployment.testenv.GetKubeClient().List(ctx, eventList, client.InNamespace(namespace))
		if err != nil {
			testenv.Log.Info("Failed to list events", "namespace", namespace, "error", err)
			return false, nil
		}

		for _, event := range eventList.Items {
			if event.InvolvedObject.Name == crName && event.Reason == eventReason {
				testenv.Log.Info("Found expected event", "name", crName, "reason", eventReason)
				return true, nil
			}
		}
		return false, nil
	})
}
