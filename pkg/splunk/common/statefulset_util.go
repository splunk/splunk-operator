// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package common

import (
	"context"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// MergeStatefulSetMetaUpdates compares and merges StatefulSet ObjectMeta (labels and annotations).
// This does NOT trigger pod restarts since it only touches StatefulSet-level metadata.
// Returns true if there were any changes.
func MergeStatefulSetMetaUpdates(ctx context.Context, current, revised *metav1.ObjectMeta, name string) bool {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("MergeStatefulSetMetaUpdates").WithValues("name", name)
	result := false

	// Check Annotations - normalize nil to empty map for comparison
	currentAnnotations := current.Annotations
	if currentAnnotations == nil {
		currentAnnotations = map[string]string{}
	}
	revisedAnnotations := revised.Annotations
	if revisedAnnotations == nil {
		revisedAnnotations = map[string]string{}
	}
	if !reflect.DeepEqual(currentAnnotations, revisedAnnotations) {
		scopedLog.Info("StatefulSet Annotations differ",
			"current", current.Annotations,
			"revised", revised.Annotations)
		current.Annotations = revised.Annotations
		result = true
	}

	// Check Labels - normalize nil to empty map for comparison
	currentLabels := current.Labels
	if currentLabels == nil {
		currentLabels = map[string]string{}
	}
	revisedLabels := revised.Labels
	if revisedLabels == nil {
		revisedLabels = map[string]string{}
	}
	if !reflect.DeepEqual(currentLabels, revisedLabels) {
		scopedLog.Info("StatefulSet Labels differ",
			"current", current.Labels,
			"revised", revised.Labels)
		current.Labels = revised.Labels
		result = true
	}

	return result
}
