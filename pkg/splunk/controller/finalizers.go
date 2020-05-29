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

package controller

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

func init() {
	SplunkFinalizerRegistry = make(map[string]SplunkFinalizerMethod)
}

// SplunkFinalizerMethod is used to register finalizer callbacks in the registry
type SplunkFinalizerMethod func(splcommon.MetaObject, splcommon.ControllerClient) error

// SplunkFinalizerRegistry is a list of Splunk finalizers processed when deletion is requested
var SplunkFinalizerRegistry map[string]SplunkFinalizerMethod

// CheckForDeletion checks to see if deletion was requested for the custom resource.
// If so, it will process and remove any remaining finalizers.
func CheckForDeletion(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
	scopedLog := log.WithName("CheckSplunkDeletion").WithValues("kind", cr.GetObjectKind().GroupVersionKind().Kind,
		"name", cr.GetName(), "namespace", cr.GetNamespace())
	currentTime := metav1.Now()

	// sanity check: return early if missing GetDeletionTimestamp
	if cr.GetObjectMeta().GetDeletionTimestamp() == nil {
		scopedLog.Info("DeletionTimestamp is nil")
		return false, nil
	}

	// just log warning if deletion time is in the future
	if !cr.GetObjectMeta().GetDeletionTimestamp().Before(&currentTime) {
		scopedLog.Info("DeletionTimestamp is in the future",
			"Now", currentTime,
			"DeletionTimestamp", cr.GetObjectMeta().GetDeletionTimestamp())
		//return false, nil
	}

	scopedLog.Info("Deletion requested")

	// process each finalizer
	for _, finalizer := range cr.GetObjectMeta().GetFinalizers() {
		// check if finalizer name is registered
		callback, ok := SplunkFinalizerRegistry[finalizer]
		if !ok {
			return false, fmt.Errorf("Finalizer in %s %s/%s not recognized: %s", cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName(), finalizer)
		}

		// process finalizer callback
		scopedLog.Info("Processing callback", "Finalizer", finalizer)
		err := callback(cr, c)
		if err != nil {
			return false, err
		}

		// remove finalizer from custom resource
		err = removeSplunkFinalizer(cr, c, finalizer)
		if err != nil {
			return false, err
		}
	}

	scopedLog.Info("Deletion complete")

	return true, nil
}

// removeSplunkFinalizer removes a finalizer from a custom resource.
func removeSplunkFinalizer(cr splcommon.MetaObject, c splcommon.ControllerClient, finalizer string) error {
	scopedLog := log.WithName("RemoveFinalizer").WithValues("kind", cr.GetObjectKind().GroupVersionKind().Kind, "name", cr.GetName(), "namespace", cr.GetNamespace())
	scopedLog.Info("Removing finalizer", "name", finalizer)

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
