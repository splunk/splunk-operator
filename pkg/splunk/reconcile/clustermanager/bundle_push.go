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

package clustermanager

import (
	"context"
	"fmt"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

// performCmBundlePush performs cluster manager bundle push operation.
// Defined as a variable to allow mocking in unit tests.
var performCmBundlePush = func(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.ClusterManager, podExecClient splutil.PodExecClientImpl) error {
	if !cr.Status.BundlePushTracker.NeedToPushManagerApps {
		return nil
	}

	logger := logging.FromContext(ctx).With("func", "PerformCmBundlePush", "name", cr.GetName(), "namespace", cr.GetNamespace())
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
	cmPodName := fmt.Sprintf("splunk-%s-%s-0", cr.GetName(), "cluster-manager")

	// Create podExecClient if not provided
	if podExecClient == nil {
		podExecClient = splutil.GetPodExecClient(c, cr, cmPodName)
	}
	// The client may be reused after another operation changed or did not set its target pod.
	podExecClient.SetTargetPodName(ctx, cmPodName)

	err := checkIfSmartstoreConfigMapUpdatedToPod(ctx, c, cr, podExecClient)
	if err != nil {
		return err
	}

	// Reset symbolic links for pod
	err = k8sops.ResetSymbolicLinks(ctx, cr, 1, podExecClient)
	if err != nil {
		return err
	}

	err = pushManagerAppsBundle(ctx, c, cr)
	if err == nil {
		logger.InfoContext(ctx, "bundle push success")
		cr.Status.BundlePushTracker.NeedToPushManagerApps = false
	}

	return err
}

func pushManagerAppsBundle(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.ClusterManager) error {
	logger := logging.FromContext(ctx).With("func", "PushManagerApps", "name", cr.GetName(), "namespace", cr.GetNamespace())

	// Get event publisher from context
	eventPublisher := k8sops.GetEventPublisher(ctx, cr)

	defaultSecretObjName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
	defaultSecret, err := splutil.GetSecretByName(ctx, c, cr.GetNamespace(), defaultSecretObjName)
	if err != nil {
		eventPublisher.Warning(ctx, splcommon.EventReasonBundlePushFailed, fmt.Sprintf("Could not access default secret object to fetch admin password. Reason %v", err))
		return fmt.Errorf("could not access default secret object to fetch admin password. Reason %v", err)
	}

	//Get the admin password from the secret object
	adminPwd, foundSecret := defaultSecret.Data["password"]
	if !foundSecret {
		return fmt.Errorf("could not find admin password while trying to push the manager apps bundle")
	}

	logger.InfoContext(ctx, "issuing REST call to push manager aps bundle")

	managerIdxcName := cr.GetName()
	fqdnName := splcommon.GetServiceFQDN(cr.GetNamespace(), splcommon.GetSplunkServiceName(splcommon.SplunkClusterManager, managerIdxcName, false))

	// Get a Splunk client to execute the REST call
	splunkClient := splclient.NewSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(adminPwd))

	return splunkClient.BundlePush(true)
}

func checkIfSmartstoreConfigMapUpdatedToPod(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.ClusterManager, podExecClient splutil.PodExecClientImpl) error {
	logger := logging.FromContext(ctx).With("func", "CheckIfsmartstoreConfigMapUpdatedToPod", "name", cr.GetName(), "namespace", cr.GetNamespace())
	eventPublisher := k8sops.GetEventPublisher(ctx, cr)

	command := fmt.Sprintf("cat /mnt/splunk-operator/local/%s", splcommon.ConfigToken)
	streamOptions := splutil.NewStreamOptionsObject(command)

	stdOut, stdErr, err := podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
	if err != nil || stdErr != "" {
		eventPublisher.Warning(ctx, splcommon.EventReasonPodExecFailed, fmt.Sprintf("Failed to check config token value on pod. stdout=%s, stderror=%s, error=%v", stdOut, stdErr, err))
		return fmt.Errorf("failed to check config token value on pod. stdout=%s, stderror=%s, error=%v", stdOut, stdErr, err)
	}

	smartStoreConfigMap := k8sops.GetSmartstoreConfigMap(ctx, c, cr, splcommon.SplunkClusterManager)
	if smartStoreConfigMap != nil {
		tokenFromConfigMap := smartStoreConfigMap.Data[splcommon.ConfigToken]
		if tokenFromConfigMap == stdOut {
			logger.InfoContext(ctx, "token matched", "podToken", stdOut, "configMapToken", tokenFromConfigMap)
			return nil
		}
		eventPublisher.Warning(ctx, splcommon.EventReasonSmartStoreConfigPending, fmt.Sprintf("waiting for the configMap update to the Pod. Token on Pod=%s, Token from configMap=%s", stdOut, tokenFromConfigMap))
		return fmt.Errorf("waiting for the configMap update to the Pod. Token on Pod=%s, Token from configMap=%s", stdOut, tokenFromConfigMap)
	}

	// Somehow the configmap was deleted, ideally this should not happen
	eventPublisher.Warning(ctx, splcommon.EventReasonSmartStoreConfigPending, "smartstore ConfigMap is missing")
	return fmt.Errorf("smartstore ConfigMap is missing")
}
