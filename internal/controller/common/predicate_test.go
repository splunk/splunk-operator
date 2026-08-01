package common

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestDeletionTimestampChangedPredicate(t *testing.T) {
	t.Parallel()

	predicate := DeletionTimestampChangedPredicate()
	oldObject := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "object"}}
	newObject := oldObject.DeepCopy()
	now := metav1.Now()
	newObject.DeletionTimestamp = &now

	if !predicate.Update(event.UpdateEvent{ObjectOld: oldObject, ObjectNew: newObject}) {
		t.Fatal("setting deletionTimestamp must trigger reconciliation")
	}
	if predicate.Update(event.UpdateEvent{ObjectOld: newObject, ObjectNew: newObject.DeepCopy()}) {
		t.Fatal("an unchanged deletionTimestamp must not trigger reconciliation")
	}
}
