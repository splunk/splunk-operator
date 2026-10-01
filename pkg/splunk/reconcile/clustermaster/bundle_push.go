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

package clustermaster

import (
	"context"
	"fmt"
	"time"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

// checkIfMastersmartstoreConfigMapUpdatedToPod checks if the smartstore configMap is updated on Pod or not
func checkIfMastersmartstoreConfigMapUpdatedToPod(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApiV3.ClusterMaster, podExecClient splutil.PodExecClientImpl) error {
	logger := logging.FromContext(ctx).With("func", "CheckIfMastersmartstoreConfigMapUpdatedToPod", "name", cr.GetName(), "namespace", cr.GetNamespace())

	// Get event publisher from context
	eventPublisher := k8sops.GetEventPublisher(ctx, cr)

	command := fmt.Sprintf("cat /mnt/splunk-operator/local/%s", splcommon.ConfigToken)
	streamOptions := splutil.NewStreamOptionsObject(command)

	stdOut, stdErr, err := podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
	if err != nil || stdErr != "" {
		eventPublisher.Warning(ctx, "PodExecCommand", fmt.Sprintf("Failed to check config token value on pod. stdout=%s, stderror=%s, error=%v", stdOut, stdErr, err))
		return fmt.Errorf("failed to check config token value on pod. stdout=%s, stderror=%s, error=%v", stdOut, stdErr, err)
	}

	smartStoreConfigMap := k8sops.GetSmartstoreConfigMap(ctx, c, cr, splcommon.SplunkClusterMaster)
	if smartStoreConfigMap != nil {
		tokenFromConfigMap := smartStoreConfigMap.Data[splcommon.ConfigToken]
		if tokenFromConfigMap == stdOut {
			logger.InfoContext(ctx, "token matched", "podToken", stdOut, "configMapToken", tokenFromConfigMap)
			return nil
		}
		eventPublisher.Warning(ctx, "getSmartstoreConfigMap", fmt.Sprintf("waiting for the configMap update to the Pod. Token on Pod=%s, Token from configMap=%s", stdOut, tokenFromConfigMap))
		return fmt.Errorf("waiting for the configMap update to the Pod. Token on Pod=%s, Token from configMap=%s", stdOut, tokenFromConfigMap)
	}

	// Somehow the configmap was deleted, ideally this should not happen
	eventPublisher.Warning(ctx, "getSmartstoreConfigMap", "smartstore ConfigMap is missing")
	return fmt.Errorf("smartstore ConfigMap is missing")
}

// performCmasterBundlePush initiates the bundle push from cluster manager
// Defined as a variable to allow mocking in unit tests
var performCmasterBundlePush = func(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApiV3.ClusterMaster) error {
	if !cr.Status.BundlePushTracker.NeedToPushMasterApps {
		return nil
	}

	logger := logging.FromContext(ctx).With("func", "PerformCmasterBundlePush", "name", cr.GetName(), "namespace", cr.GetNamespace())
	// Reconciler can be called for multiple reasons. If we are waiting on configMap update to happen,
	// do not increment the Retry Count unless the last check was 5 seconds ago.
	// This helps, to wait for the required time

	currentEpoch := time.Now().Unix()
	if cr.Status.BundlePushTracker.LastCheckInterval+5 > currentEpoch {
		return fmt.Errorf("will re-attempt to push the bundle after the 5 seconds period passed from last check. LastCheckInterval=%d, current epoch=%d", cr.Status.BundlePushTracker.LastCheckInterval, currentEpoch)
	}

	logger.InfoContext(ctx, "attempting to push the bundle")
	cr.Status.BundlePushTracker.LastCheckInterval = currentEpoch

	// The amount of time it takes for the configMap update to Pod depends on
	// how often the Kubelet on the K8 node refreshes its cache with API server.
	// From our tests, the Pod can take as high as 90 seconds. So keep checking
	// for the configMap update to the Pod before proceeding for the manager apps
	// bundle push.

	cmPodName := fmt.Sprintf("splunk-%s-%s-0", cr.GetName(), splcommon.ClusterManager)
	podExecClient := splutil.GetPodExecClient(c, cr, cmPodName)
	err := checkIfMastersmartstoreConfigMapUpdatedToPod(ctx, c, cr, podExecClient)
	if err != nil {
		return err
	}

	// Reset symbolic links for pod
	err = k8sops.ResetSymbolicLinks(ctx, cr, 1, podExecClient)
	if err != nil {
		return err
	}

	err = pushMasterAppsBundle(ctx, c, cr)
	if err == nil {
		logger.InfoContext(ctx, "bundle push success")
		cr.Status.BundlePushTracker.NeedToPushMasterApps = false
	}

	return err
}

// pushMasterAppsBundle issues the REST command to for cluster manager bundle push
func pushMasterAppsBundle(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApiV3.ClusterMaster) error {
	logger := logging.FromContext(ctx).With("func", "PushMasterApps", "name", cr.GetName(), "namespace", cr.GetNamespace())

	// Get event publisher from context
	eventPublisher := k8sops.GetEventPublisher(ctx, cr)

	defaultSecretObjName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
	defaultSecret, err := splutil.GetSecretByName(ctx, c, cr.GetNamespace(), defaultSecretObjName)
	if err != nil {
		eventPublisher.Warning(ctx, "PushMasterAppsBundle", fmt.Sprintf("Could not access default secret object to fetch admin password. Reason %v", err))
		return fmt.Errorf("could not access default secret object to fetch admin password. Reason %v", err)
	}

	//Get the admin password from the secret object
	adminPwd, foundSecret := defaultSecret.Data["password"]
	if !foundSecret {
		eventPublisher.Warning(ctx, "PushMasterAppsBundle", "Could not find admin password while trying to push the manager apps bundle")
		return fmt.Errorf("could not find admin password while trying to push the manager apps bundle")
	}

	logger.InfoContext(ctx, "issuing REST call to push manager aps bundle")

	managerIdxcName := cr.GetName()
	fqdnName := splcommon.GetServiceFQDN(cr.GetNamespace(), splcommon.GetSplunkServiceName(splcommon.SplunkClusterMaster, managerIdxcName, false))

	// Get a Splunk client to execute the REST call
	splunkClient := splclient.NewSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(adminPwd))

	return splunkClient.BundlePush(true)
}
