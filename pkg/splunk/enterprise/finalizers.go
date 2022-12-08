// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

package enterprise

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
)

func init() {
	splctrl.SplunkFinalizerRegistry["enterprise.splunk.com/delete-pvc"] = DeleteSplunkPvc
}

// DeleteSplunkPvc removes all corresponding PersistentVolumeClaims that are associated with a custom resource.
func DeleteSplunkPvc(ctx context.Context, cr splcommon.MetaObject, c splcommon.ControllerClient) error {
	objectKind := cr.GetObjectKind().GroupVersionKind().Kind

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("DeleteSplunkPvc")

	var components []string
	switch objectKind {
	case "Standalone":
		components = append(components, "standalone")
	case "LicenseMaster":
		components = append(components, splcommon.LicenseManager)
	case "LicenseManager":
		components = append(components, "license-manager")
	case "Deployer":
		components = append(components, "deployer")
	case "SearchHeadCluster":
		components = append(components, "search-head")
	case "IndexerCluster":
		components = append(components, "indexer")
	case "ClusterManager":
		components = append(components, "cluster-manager")
	case "ClusterMaster":
		components = append(components, splcommon.ClusterManager)
	case "MonitoringConsole":
		components = append(components, "monitoring-console")
	default:
		scopedLog.Info("Skipping PVC removal")
		return nil
	}

	for _, component := range components {
		// get list of PVCs associated with this CR
		labels := map[string]string{
			"app.kubernetes.io/instance": fmt.Sprintf("splunk-%s-%s", cr.GetName(), component),
		}
		listOpts := []client.ListOption{
			client.InNamespace(cr.GetNamespace()),
			client.MatchingLabels(labels),
		}
		pvclist := corev1.PersistentVolumeClaimList{}
		if err := c.List(context.Background(), &pvclist, listOpts...); err != nil {
			return err
		}

		if len(pvclist.Items) == 0 {
			scopedLog.Info("No PVC found")
			return nil
		}

		// delete each PVC
		for _, pvc := range pvclist.Items {
			scopedLog.Info("Deleting PVC", "name", pvc.ObjectMeta.Name)
			if err := c.Delete(context.Background(), &pvc); err != nil {
				return err
			}
		}

	}
	return nil
}
