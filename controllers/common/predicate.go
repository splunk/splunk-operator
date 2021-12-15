package common

import (
	"reflect"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// LabelChangedPredicate .
func LabelChangedPredicate() predicate.Predicate {
	err := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return !reflect.DeepEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels())
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
	return err
}

// SecretChangedPredicate .
func SecretChangedPredicate() predicate.Predicate {
	err := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			if _, ok := e.ObjectNew.(*corev1.Secret); !ok {
				return false
			}

			// This update is in fact a Delete event, process it
			if e.ObjectNew.GetDeletionGracePeriodSeconds() != nil {
				return true
			}

			// if old and new data is the same, don't reconcile
			newObj := e.ObjectNew.DeepCopyObject().(*corev1.Secret)
			oldObj := e.ObjectOld.DeepCopyObject().(*corev1.Secret)
			return !cmp.Equal(newObj.Data, oldObj.Data)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
	return err
}

// ConfigChangedPredicate .
func ConfigChangedPredicate() predicate.Predicate {
	err := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// This update is in fact a Delete event, process it
			if _, ok := e.ObjectNew.(*corev1.ConfigMap); !ok {
				return false
			}

			if e.ObjectNew.GetDeletionGracePeriodSeconds() != nil {
				return true
			}

			// if old and new data is the same, don't reconcile
			newObj := e.ObjectNew.DeepCopyObject().(*corev1.ConfigMap)
			oldObj := e.ObjectOld.DeepCopyObject().(*corev1.ConfigMap)
			return !cmp.Equal(newObj.Data, oldObj.Data)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
	return err
}

// StatefulsetChangedPredicate .
func StatefulsetChangedPredicate() predicate.Predicate {
	err := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// This update is in fact a Delete event, process it
			if _, ok := e.ObjectNew.(*appsv1.StatefulSet); !ok {
				return false
			}

			if e.ObjectNew.GetDeletionGracePeriodSeconds() != nil {
				return true
			}

			// if old and new data is the same, don't reconcile
			newObj := e.ObjectNew.DeepCopyObject().(*appsv1.StatefulSet)
			oldObj := e.ObjectOld.DeepCopyObject().(*appsv1.StatefulSet)
			return !cmp.Equal(newObj.Spec, oldObj.Spec)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
	return err
}

// PodChangedPredicate .
func PodChangedPredicate() predicate.Predicate {
	err := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// This update is in fact a Delete event, process it
			if _, ok := e.ObjectNew.(*corev1.Pod); !ok {
				return false
			}

			if e.ObjectNew.GetDeletionGracePeriodSeconds() != nil {
				return true
			}

			// if old and new data is the same, don't reconcile
			newObj := e.ObjectNew.DeepCopyObject().(*corev1.Pod)
			oldObj := e.ObjectOld.DeepCopyObject().(*corev1.Pod)
			return !cmp.Equal(newObj.Spec, oldObj.Spec)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
	return err
}
