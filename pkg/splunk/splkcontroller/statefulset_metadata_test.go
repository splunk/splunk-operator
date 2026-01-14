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

package splkcontroller

import (
	"context"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestApplyStatefulSet_STSOnlyAnnotationNotPersisted verifies that sts-only.* annotations
// (transformed to amadeus.com/* format by SyncParentMetaToStatefulSet) are properly
// persisted after ApplyStatefulSet is called.
func TestApplyStatefulSet_STSOnlyAnnotationNotPersisted(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 1

	// Current StatefulSet (in cluster) - has only existing annotation
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				"existing-annotation": "existing-value",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}

	// Add current to mock client (simulates existing StatefulSet in cluster)
	c.Create(ctx, current)

	// Revised StatefulSet (after SyncParentMetaToStatefulSet was called)
	// This simulates the state after sts-only.amadeus.com/aaa annotation was transformed
	revised := current.DeepCopy()
	revised.Annotations = map[string]string{
		"existing-annotation": "existing-value",
		"amadeus.com/aaa":     "value1", // Added by SyncParentMetaToStatefulSet (prefix stripped)
	}

	t.Logf("Before ApplyStatefulSet - revised has annotation 'amadeus.com/aaa': %s",
		revised.Annotations["amadeus.com/aaa"])

	// Apply the StatefulSet
	phase, err := ApplyStatefulSet(ctx, c, revised)
	if err != nil {
		t.Fatalf("ApplyStatefulSet failed: %v", err)
	}

	// Verify phase indicates an update was triggered
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("Expected PhaseUpdating due to annotation change, got %v", phase)
	}

	t.Logf("After ApplyStatefulSet - revised has annotation 'amadeus.com/aaa': %s",
		revised.Annotations["amadeus.com/aaa"])

	// Verify the annotation is preserved in revised (which now reflects current after merge)
	if val, ok := revised.Annotations["amadeus.com/aaa"]; !ok || val != "value1" {
		t.Errorf("Annotation 'amadeus.com/aaa' was lost after ApplyStatefulSet.\n"+
			"Expected: value1\nGot: %s", val)
	} else {
		t.Log("Annotation 'amadeus.com/aaa' is preserved after ApplyStatefulSet")
	}
}

// TestApplyStatefulSet_MetadataOnlyChangeNoUpdate verifies that metadata-only changes
// (no Pod Template changes) are properly detected and trigger a StatefulSet update.
//
// This test ensures that MergeStatefulSetMetaUpdates is called and hasUpdates is set
// to true even when there are no Pod Template changes.
func TestApplyStatefulSet_MetadataOnlyChangeNoUpdate(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 1

	// Current StatefulSet (in cluster)
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				"existing-annotation": "existing-value",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}

	// Add current to mock client
	c.Create(ctx, current)

	// Revised StatefulSet - ONLY metadata changed, Pod Template is identical
	revised := current.DeepCopy()
	revised.Annotations = map[string]string{
		"existing-annotation": "existing-value",
		"new-sts-annotation":  "new-value", // Only metadata changed
	}

	// Apply the StatefulSet
	phase, err := ApplyStatefulSet(ctx, c, revised)
	if err != nil {
		t.Fatalf("ApplyStatefulSet failed: %v", err)
	}

	// Verify phase indicates an update was triggered (metadata-only change should still trigger update)
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("Expected PhaseUpdating for metadata-only change, got %v", phase)
	}

	// Verify the new annotation is preserved
	if val, ok := revised.Annotations["new-sts-annotation"]; !ok || val != "new-value" {
		t.Errorf("Annotation 'new-sts-annotation' was lost after ApplyStatefulSet.\n"+
			"Expected: new-value\nGot: %s", val)
	} else {
		t.Log("Annotation 'new-sts-annotation' is correctly preserved after ApplyStatefulSet")
	}
}
