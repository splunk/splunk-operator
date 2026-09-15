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

package k8sops

import (
	"context"
	"fmt"

	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	SplunkFinalizerRegistry = map[string]SplunkFinalizerMethod{
		"enterprise.splunk.com/delete-pvc": DeleteSplunkPvc,
	}
}

// DeleteSplunkPvc removes all PersistentVolumeClaims associated with a
// Splunk custom resource. It is shared by every CR reconcile package.
func DeleteSplunkPvc(ctx context.Context, cr splcommon.MetaObject, c splcommon.ControllerClient) error {
	logger := logging.FromContext(ctx).With("func", "DeleteSplunkPvc")
	components := map[string][]string{
		"Standalone":        {"standalone"},
		"LicenseMaster":     {splcommon.LicenseManager},
		"LicenseManager":    {"license-manager"},
		"SearchHeadCluster": {"search-head", "deployer"},
		"IndexerCluster":    {"indexer"},
		"ClusterManager":    {"cluster-manager"},
		"ClusterMaster":     {splcommon.ClusterManager},
		"MonitoringConsole": {"monitoring-console"},
		"IngestorCluster":   {"ingestor"},
	}
	if len(components[cr.GetObjectKind().GroupVersionKind().Kind]) == 0 {
		logger.DebugContext(ctx, "skipping PVC removal")
		return nil
	}
	for _, component := range components[cr.GetObjectKind().GroupVersionKind().Kind] {
		labels := map[string]string{"app.kubernetes.io/instance": fmt.Sprintf("splunk-%s-%s", cr.GetName(), component)}
		listOpts := []client.ListOption{client.InNamespace(cr.GetNamespace()), client.MatchingLabels(labels)}
		pvcList := corev1.PersistentVolumeClaimList{}
		if err := c.List(ctx, &pvcList, listOpts...); err != nil {
			return err
		}
		for i := range pvcList.Items {
			logger.InfoContext(ctx, "deleting PVC", "name", pvcList.Items[i].ObjectMeta.Name)
			if err := c.Delete(ctx, &pvcList.Items[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

// SplunkFinalizerMethod is used to register finalizer callbacks in the registry
type SplunkFinalizerMethod func(context.Context, splcommon.MetaObject, splcommon.ControllerClient) error

// SplunkFinalizerRegistry is a list of Splunk finalizers processed when deletion is requested
var SplunkFinalizerRegistry map[string]SplunkFinalizerMethod

// CheckForDeletion checks to see if deletion was requested for the custom resource.
// If so, it will process and remove any remaining finalizers.
func CheckForDeletion(ctx context.Context, cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
	scopedLog := logging.FromContext(ctx).With("func", "CheckSplunkDeletion", "kind", cr.GetObjectKind().GroupVersionKind().Kind,
		"name", cr.GetName(), "namespace", cr.GetNamespace())
	currentTime := metav1.Now()

	// sanity check: return early if missing GetDeletionTimestamp
	if cr.GetObjectMeta().GetDeletionTimestamp() == nil {
		scopedLog.InfoContext(ctx, "deletionTimestamp is nil")
		return false, nil
	}

	// just log warning if deletion time is in the future
	if !cr.GetObjectMeta().GetDeletionTimestamp().Before(&currentTime) {
		scopedLog.InfoContext(ctx, "deletionTimestamp is in the future",
			"Now", currentTime,
			"DeletionTimestamp", cr.GetObjectMeta().GetDeletionTimestamp())
	}

	scopedLog.InfoContext(ctx, "deletion requested")

	// process each finalizer
	for _, finalizer := range cr.GetObjectMeta().GetFinalizers() {
		// check if finalizer name is registered
		callback, ok := SplunkFinalizerRegistry[finalizer]
		if !ok {
			return false, fmt.Errorf("finalizer in %s %s/%s not recognized: %s", cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName(), finalizer)
		}

		// process finalizer callback
		scopedLog.InfoContext(ctx, "processing callback", "Finalizer", finalizer)
		err := callback(ctx, cr, c)
		if err != nil {
			return false, err
		}

		// remove finalizer from custom resource
		err = removeSplunkFinalizer(ctx, cr, c, finalizer)
		if err != nil {
			return false, err
		}
	}

	scopedLog.InfoContext(ctx, "deletion complete")

	return true, nil
}

// removeSplunkFinalizer removes a finalizer from a custom resource.
func removeSplunkFinalizer(ctx context.Context, cr splcommon.MetaObject, c splcommon.ControllerClient, finalizer string) error {
	scopedLog := logging.FromContext(ctx).With("func", "RemoveFinalizer", "kind", cr.GetObjectKind().GroupVersionKind().Kind, "name", cr.GetName(), "namespace", cr.GetNamespace())
	scopedLog.InfoContext(ctx, "removing finalizer", "name", finalizer)

	// create new list of finalizers that doesn't include the one being removed
	var newFinalizers []string

	// handles multiple occurrences (performance is not significant)
	for _, f := range cr.GetObjectMeta().GetFinalizers() {
		if f != finalizer {
			newFinalizers = append(newFinalizers, f)
		}
	}

	// update object
	cr.GetObjectMeta().SetFinalizers(newFinalizers)
	return c.Update(context.Background(), cr)
}
