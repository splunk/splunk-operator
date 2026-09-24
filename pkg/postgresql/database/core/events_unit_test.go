/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

func newTestRC(bufSize int) (*ReconcileContext, *record.FakeRecorder) {
	fr := record.NewFakeRecorder(bufSize)
	return &ReconcileContext{Recorder: fr}, fr
}

func conditionFalse(condType conditionTypes) metav1.Condition {
	return metav1.Condition{Type: string(condType), Status: metav1.ConditionFalse}
}

func conditionTrue(condType conditionTypes) metav1.Condition {
	return metav1.Condition{Type: string(condType), Status: metav1.ConditionTrue}
}

func TestEmitOnConditionTransition(t *testing.T) {
	obj := &corev1.ConfigMap{}

	t.Run("emits when condition is absent", func(t *testing.T) {
		rc, fr := newTestRC(10)
		rc.emitOnConditionTransition(obj, nil, clusterReady, "Reason", "msg")
		assert.Len(t, fr.Events, 1)
	})

	t.Run("emits when condition is False", func(t *testing.T) {
		rc, fr := newTestRC(10)
		rc.emitOnConditionTransition(obj, []metav1.Condition{conditionFalse(clusterReady)}, clusterReady, "Reason", "msg")
		assert.Len(t, fr.Events, 1)
	})

	t.Run("suppressed when condition is already True", func(t *testing.T) {
		rc, fr := newTestRC(10)
		rc.emitOnConditionTransition(obj, []metav1.Condition{conditionTrue(clusterReady)}, clusterReady, "Reason", "msg")
		assert.Empty(t, fr.Events)
	})
}

func TestEmitOnceBeforeWait(t *testing.T) {
	obj := &corev1.ConfigMap{}

	t.Run("emits when condition is absent", func(t *testing.T) {
		rc, fr := newTestRC(10)
		rc.emitOnceBeforeWait(obj, nil, clusterReady, "Reason", "msg")
		assert.Len(t, fr.Events, 1)
	})

	t.Run("emits when condition is True — first entry into wait", func(t *testing.T) {
		rc, fr := newTestRC(10)
		rc.emitOnceBeforeWait(obj, []metav1.Condition{conditionTrue(clusterReady)}, clusterReady, "Reason", "msg")
		assert.Len(t, fr.Events, 1)
	})

	t.Run("suppressed when condition is already False — subsequent poll", func(t *testing.T) {
		rc, fr := newTestRC(10)
		rc.emitOnceBeforeWait(obj, []metav1.Condition{conditionFalse(clusterReady)}, clusterReady, "Reason", "msg")
		assert.Empty(t, fr.Events)
	})
}

func TestEmitWarnOnceBeforeWait(t *testing.T) {
	obj := &corev1.ConfigMap{}

	t.Run("emits Warning when condition is absent", func(t *testing.T) {
		rc, fr := newTestRC(10)
		rc.emitWarnOnceBeforeWait(obj, nil, clusterReady, "Reason", "msg")
		assert.Len(t, fr.Events, 1)
		event := <-fr.Events
		assert.Contains(t, event, corev1.EventTypeWarning)
	})

	t.Run("emits Warning when condition is True — first entry into degraded wait", func(t *testing.T) {
		rc, fr := newTestRC(10)
		rc.emitWarnOnceBeforeWait(obj, []metav1.Condition{conditionTrue(clusterReady)}, clusterReady, "Reason", "msg")
		assert.Len(t, fr.Events, 1)
		event := <-fr.Events
		assert.Contains(t, event, corev1.EventTypeWarning)
	})

	t.Run("suppressed when condition is already False — subsequent poll", func(t *testing.T) {
		rc, fr := newTestRC(10)
		rc.emitWarnOnceBeforeWait(obj, []metav1.Condition{conditionFalse(clusterReady)}, clusterReady, "Reason", "msg")
		assert.Empty(t, fr.Events)
	})
}
