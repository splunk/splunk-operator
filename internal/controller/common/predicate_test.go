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

package common

import (
	"testing"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

func TestDeletionTimestampChangedPredicate(t *testing.T) {
	predicate := DeletionTimestampChangedPredicate[*enterpriseApi.IndexerCluster]()
	active := &enterpriseApi.IndexerCluster{}
	deleting := active.DeepCopy()
	now := metav1.Now()
	deleting.DeletionTimestamp = &now

	assert.True(t, predicate.Update(event.UpdateEvent{ObjectOld: active, ObjectNew: deleting}))
	assert.False(t, predicate.Update(event.UpdateEvent{ObjectOld: active, ObjectNew: active.DeepCopy()}))
	assert.False(t, predicate.Update(event.UpdateEvent{
		ObjectOld: &corev1.Pod{},
		ObjectNew: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now}},
	}))
}

func TestKVServiceNamespacePredicate(t *testing.T) {
	p := KVServiceNamespaceChangedPredicate()
	for _, key := range []string{splcommon.LabelKVService, splcommon.LabelTenant} {
		t.Run(key, func(t *testing.T) {
			old := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "workload"}}
			updated := old.DeepCopy()
			updated.Labels = map[string]string{key: "value"}
			assert.True(t, p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated}))
			assert.True(t, p.Update(event.UpdateEvent{ObjectOld: updated, ObjectNew: old}), "removal must enqueue consumers")
			assert.False(t, p.Update(event.UpdateEvent{ObjectOld: updated, ObjectNew: updated.DeepCopy()}))
		})
	}
	old := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "workload"}}
	updated := old.DeepCopy()
	updated.Labels = map[string]string{"unrelated": "value"}
	updated.Annotations = map[string]string{"unrelated": "value"}
	assert.False(t, p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated}))
	updated.Annotations[splcommon.AnnotationKVServiceAddr] = "shared.backend:8066"
	assert.True(t, p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated}))
	assert.True(t, p.Update(event.UpdateEvent{ObjectOld: updated, ObjectNew: old}), "address removal must enqueue consumers")
	assert.False(t, p.Update(event.UpdateEvent{ObjectOld: updated, ObjectNew: updated.DeepCopy()}))
	changed := updated.DeepCopy()
	changed.Annotations[splcommon.AnnotationKVServiceAddr] = "shared.backend:9000"
	assert.True(t, p.Update(event.UpdateEvent{ObjectOld: updated, ObjectNew: changed}), "port changes must enqueue consumers")
}
