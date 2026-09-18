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

package shc

import (
	"context"
	"fmt"
	"strings"

	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"k8s.io/client-go/tools/remotecommand"
)

func ApplyShcSecret(ctx context.Context, mgr *PodManager, replicas int32, podExecClient splutil.PodExecClientImpl) error {
	// Get event publisher from context
	eventPublisher := GetEventPublisher(ctx, mgr.CR)

	// Get namespace scoped secret
	namespaceSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, mgr.Client, mgr.CR.GetNamespace())
	if err != nil {
		return err
	}

	logger := logging.FromContext(ctx).With("func", "ApplyShcSecret", "desiredReplicas", replicas, "shcSecretChanged", mgr.CR.Status.ShcSecretChanged, "adminSecretChanged", mgr.CR.Status.AdminSecretChanged, "crStatusNamespaceSecretResourceVersion", mgr.CR.Status.NamespaceSecretResourceVersion, "namespaceSecretResourceVersion", namespaceSecret.GetObjectMeta().GetResourceVersion())

	// If namespace scoped secret revision is the same ignore
	if len(mgr.CR.Status.NamespaceSecretResourceVersion) == 0 {
		// First time, set resource version in CR
		logger.InfoContext(ctx, "setting CrStatusNamespaceSecretResourceVersion for the first time")
		mgr.CR.Status.NamespaceSecretResourceVersion = namespaceSecret.ObjectMeta.ResourceVersion
		return nil
	} else if mgr.CR.Status.NamespaceSecretResourceVersion == namespaceSecret.ObjectMeta.ResourceVersion {
		// If resource version hasn't changed don't return
		return nil
	}

	logger.InfoContext(ctx, "namespaced scoped secret revision has changed")

	// Retrieve shc_secret password from secret data
	nsShcSecret := string(namespaceSecret.Data["shc_secret"])

	// Retrieve shc_secret password from secret data
	nsAdminSecret := string(namespaceSecret.Data["password"])

	// Loop over all sh pods and get individual pod's shc_secret
	howManyPodsHaveSecretChanged := 0
	for i := int32(0); i <= replicas-1; i++ {
		// Get search head pod's name
		shPodName := splutil.GetSplunkStatefulsetPodName(splcommon.SplunkSearchHead, mgr.CR.GetName(), i)

		podLogger := logging.FromContext(ctx).With("func", "ApplyShcSecretPodLoop", "desiredReplicas", replicas, "shcSecretChanged", mgr.CR.Status.ShcSecretChanged, "adminSecretChanged", mgr.CR.Status.AdminSecretChanged, "namespaceSecretResourceVersion", mgr.CR.Status.NamespaceSecretResourceVersion, "pod", shPodName)

		// Retrieve shc_secret password from Pod
		shcSecret, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.Client, shPodName, mgr.CR.GetNamespace(), "shc_secret")
		if err != nil {
			return fmt.Errorf("couldn't retrieve shc_secret from secret data, error: %s", err.Error())
		}

		// set the targetPodName here
		podExecClient.SetTargetPodName(ctx, shPodName)

		var streamOptions *remotecommand.StreamOptions = &remotecommand.StreamOptions{}

		// Retrieve admin password from Pod
		adminPwd, err := splutil.GetSpecificSecretTokenFromPod(ctx, mgr.Client, shPodName, mgr.CR.GetNamespace(), "password")
		if err != nil {
			return fmt.Errorf("couldn't retrieve admin password from secret data, error: %s", err.Error())
		}

		// If shc secret is different from namespace scoped secret change it
		if shcSecret != nsShcSecret {
			podLogger.InfoContext(ctx, "shcSecret different from namespace scoped secret, changing shc secret")
			// If shc secret already changed, skip the sync below, but still fall through
			// to the independent admin-password check for this pod.
			shcSecretAlreadyChanged := i < int32(len(mgr.CR.Status.ShcSecretChanged)) && mgr.CR.Status.ShcSecretChanged[i]
			if !shcSecretAlreadyChanged {
				// Change shc secret key
				command := fmt.Sprintf("/opt/splunk/bin/splunk edit shcluster-config -auth admin:%s -secret %s", adminPwd, nsShcSecret)
				streamOptions.Stdin = strings.NewReader(command)

				_, _, err = podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
				if err != nil {
					// Emit event for password sync failure
					if eventPublisher != nil {
						eventPublisher.Warning(ctx, splcommon.EventReasonPasswordSyncFailed,
							fmt.Sprintf("Password sync failed for pod '%s': %s. Check pod logs and secret format.", shPodName, err.Error()))
					}
					return err
				}
				podLogger.InfoContext(ctx, "shcSecret changed")

				howManyPodsHaveSecretChanged += 1

				// Get client for Pod and restart splunk instance on pod
				shClient := mgr.getClient(ctx, i)
				err = shClient.RestartSplunk()
				if err != nil {
					// Emit event for password sync failure
					if eventPublisher != nil {
						eventPublisher.Warning(ctx, splcommon.EventReasonPasswordSyncFailed,
							fmt.Sprintf("Password sync failed for pod '%s': %s. Check pod logs and secret format.", shPodName, err.Error()))
					}
					return err
				}
				podLogger.InfoContext(ctx, "restarted Splunk")

				// Set the shc_secret changed flag to true
				if i < int32(len(mgr.CR.Status.ShcSecretChanged)) {
					mgr.CR.Status.ShcSecretChanged[i] = true
				} else {
					mgr.CR.Status.ShcSecretChanged = append(mgr.CR.Status.ShcSecretChanged, true)
				}
			}
		}

		// If admin secret is different from namespace scoped secret change it
		if adminPwd != nsAdminSecret {
			podLogger.InfoContext(ctx, "admin password different from namespace scoped secret, changing admin password")
			// If admin password already changed, ignore
			if i < int32(len(mgr.CR.Status.AdminSecretChanged)) {
				if mgr.CR.Status.AdminSecretChanged[i] {
					continue
				}
			}

			// Change admin password on splunk instance of pod
			command := fmt.Sprintf("/opt/splunk/bin/splunk cmd splunkd rest --noauth POST /services/admin/users/admin 'password=%s'", nsAdminSecret)
			streamOptions.Stdin = strings.NewReader(command)
			_, _, err = podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
			if err != nil {
				return err
			}
			podLogger.InfoContext(ctx, "admin password changed on the splunk instance of pod")

			// Get client for Pod and restart splunk instance on pod
			shClient := mgr.getClient(ctx, i)
			err = shClient.RestartSplunk()
			if err != nil {
				return err
			}
			podLogger.InfoContext(ctx, "restarted Splunk")

			// Set the adminSecretChanged changed flag to true
			if i < int32(len(mgr.CR.Status.AdminSecretChanged)) {
				mgr.CR.Status.AdminSecretChanged[i] = true
			} else {
				podLogger.InfoContext(ctx, "appending to AdminSecretChanged")
				mgr.CR.Status.AdminSecretChanged = append(mgr.CR.Status.AdminSecretChanged, true)
			}

			// Adding to map of secrets to be synced
			podSecret, err := splutil.GetSecretFromPod(ctx, mgr.Client, shPodName, mgr.CR.GetNamespace())
			if err != nil {
				return err
			}
			mgr.CR.Status.AdminPasswordChangedSecrets[podSecret.GetName()] = true
			podLogger.InfoContext(ctx, "secret mounted on pod(to be changed) added to map")
		}
	}

	/*
		When admin password on the secret mounted on SHC pod is different from that on the namespace scoped
		secret the operator updates the admin password on the Splunk Instance running on the Pod. At this point
		the admin password on the secret mounted on SHC pod is different from the Splunk Instance running on it.
		Since the operator utilizes the admin password retrieved from the secret mounted on a SHC pod to make
		REST API calls to the Splunk instances running on SHC Pods, it results in unsuccessful authentication.
		Update the admin password on secret mounted on SHC pod to ensure successful authentication.
	*/
	if len(mgr.CR.Status.AdminPasswordChangedSecrets) > 0 {
		applySecret := mgr.operationSet().ApplySecret
		if applySecret == nil {
			return fmt.Errorf("SHC Secret operations are not configured")
		}

		for podSecretName := range mgr.CR.Status.AdminPasswordChangedSecrets {
			podSecret, err := splutil.GetSecretByName(ctx, mgr.Client, mgr.CR.GetNamespace(), podSecretName)
			if err != nil {
				return fmt.Errorf("could not read secret %s, reason - %v", podSecretName, err)
			}
			podSecret.Data["password"] = []byte(nsAdminSecret)
			_, err = applySecret(ctx, mgr.Client, podSecret)
			if err != nil {
				return err
			}
			logger.InfoContext(ctx, "admin password changed on the secret mounted on pod")
		}
	}

	// Emit event for password sync completed
	if eventPublisher != nil {
		eventPublisher.Normal(ctx, splcommon.EventReasonPasswordSyncCompleted,
			fmt.Sprintf("Password synchronized for %d pods", howManyPodsHaveSecretChanged))
	}

	return nil
}
