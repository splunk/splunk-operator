// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
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

package licensemanager

import (
	"context"
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// NewSplunkClientFunc is a package-level seam for LicenseManager health-check tests.
var NewSplunkClientFunc = splclient.NewSplunkClient

// CheckLicenseRelatedPodFailures checks license status via Splunk API and
// publishes a warning event when an expired license is detected.
func CheckLicenseRelatedPodFailures(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.LicenseManager, statefulSet *appsv1.StatefulSet) error {
	logger := logging.FromContext(ctx).With("func", "CheckLicenseRelatedPodFailures")
	eventPublisher := splcommon.GetEventPublisher(ctx)

	replicas := int32(1)
	if statefulSet.Spec.Replicas != nil {
		replicas = *statefulSet.Spec.Replicas
	}

	for i := int32(0); i < replicas; i++ {
		podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), i)
		namespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: podName}
		var pod corev1.Pod
		if err := client.Get(ctx, namespacedName, &pod); err != nil {
			logger.InfoContext(ctx, "pod not found, skipping license check", "podName", podName)
			continue
		}

		if pod.Status.Phase != corev1.PodRunning {
			logger.InfoContext(ctx, "pod not in running state, skipping license check", "podName", podName, "phase", pod.Status.Phase)
			continue
		}

		defaultSecretObjName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
		defaultSecret, err := splutil.GetSecretByName(ctx, client, cr.GetNamespace(), defaultSecretObjName)
		if err != nil {
			return fmt.Errorf("failed to get namespace secret for license check: %w", err)
		}

		adminPassword := string(defaultSecret.Data["password"])
		if adminPassword == "" {
			return fmt.Errorf("admin password not found in secret %s", defaultSecretObjName)
		}

		fqdnName := splutil.GetSplunkStatefulsetURL(cr.GetNamespace(), splcommon.SplunkLicenseManager, cr.GetName(), i, false)
		splunkClient := NewSplunkClientFunc(fmt.Sprintf("https://%s:8089", fqdnName), "admin", adminPassword)
		licenses, err := splunkClient.GetLicenseInfo()
		if err != nil {
			logger.ErrorContext(ctx, "failed to get license information from Splunk API", "error", err, "podName", podName)
			continue
		}

		for licenseName, licenseInfo := range licenses {
			if licenseInfo.Status == "EXPIRED" {
				if eventPublisher != nil {
					eventPublisher.Warning(ctx, "LicenseExpired", fmt.Sprintf("License '%s' has expired", licenseName))
				}
				logger.ErrorContext(ctx, "detected expired license", "licenseName", licenseName, "title", licenseInfo.Title)
			}
		}
	}

	return nil
}
