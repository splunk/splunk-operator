// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
)

func init() {
	splctrl.SplunkFinalizerRegistry["enterprise.splunk.com/delete-pvc"] = DeleteSplunkPvc
}

// DeleteSplunkPvc removes all corresponding PersistentVolumeClaims that are associated with a custom resource.
func DeleteSplunkPvc(cr splcommon.MetaObject, c splcommon.ControllerClient) error {
	scopedLog := log.WithName("DeleteSplunkPvc").WithValues("kind", cr.GetObjectKind().GroupVersionKind().Kind,
		"name", cr.GetName(), "namespace", cr.GetNamespace())

	var component string
	switch cr.GetObjectKind().GroupVersionKind().Kind {
	case "Standalone":
		component = "standalone"
	case "LicenseMaster":
		component = "license-master"
	case "MonitoringConsole":
		component = "monitoring-console"
	case "SearchHeadCluster":
		component = "search-head"
	case "IndexerCluster":
		component = "indexer"
	default:
		scopedLog.Info("Skipping PVC removal")
		return nil
	}

	// get list of PVCs for this cluster
	labels := map[string]string{
		"app.kubernetes.io/part-of": fmt.Sprintf("splunk-%s-%s", cr.GetName(), component),
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

	return nil
}
