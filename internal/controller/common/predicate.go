package common

import (
	"reflect"

	"github.com/google/go-cmp/cmp"
	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	crdv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// LabelChangedPredicate .
func LabelChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return !reflect.DeepEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels())
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
}

// GenerationChangedPredicate .
func GenerationChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return !reflect.DeepEqual(e.ObjectOld.GetGeneration(), e.ObjectNew.GetGeneration())
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
}

// AnnotationChangedPredicate .
func AnnotationChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return !reflect.DeepEqual(e.ObjectOld.GetAnnotations(), e.ObjectNew.GetAnnotations())
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
}

// SecretChangedPredicate .
func SecretChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			if _, ok := e.ObjectNew.(*corev1.Secret); !ok {
				return false
			}

			// This update is in fact a Delete event, process it
			if e.ObjectNew.GetDeletionGracePeriodSeconds() != nil {
				return true
			}

			// if old and new data is the same, don't reconcile
			newObj, ok := e.ObjectNew.DeepCopyObject().(*corev1.Secret)
			if !ok {
				return false
			}
			oldObj, ok := e.ObjectOld.DeepCopyObject().(*corev1.Secret)
			if !ok {
				return false
			}
			return !cmp.Equal(newObj.Data, oldObj.Data)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
}

// ConfigMapChangedPredicate .
func ConfigMapChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// This update is in fact a Delete event, process it
			if _, ok := e.ObjectNew.(*corev1.ConfigMap); !ok {
				return false
			}

			if e.ObjectNew.GetDeletionGracePeriodSeconds() != nil {
				return true
			}

			// if old and new data is the same, don't reconcile
			newObj, ok := e.ObjectNew.DeepCopyObject().(*corev1.ConfigMap)
			if !ok {
				return false
			}
			oldObj, ok := e.ObjectOld.DeepCopyObject().(*corev1.ConfigMap)
			if !ok {
				return false
			}
			return !cmp.Equal(newObj.Data, oldObj.Data)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
}

// StatefulsetChangedPredicate .
func StatefulsetChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// This update is in fact a Delete event, process it
			if _, ok := e.ObjectNew.(*appsv1.StatefulSet); !ok {
				return false
			}

			if e.ObjectNew.GetDeletionGracePeriodSeconds() != nil {
				return true
			}

			// if old and new data is the same, don't reconcile
			newObj, ok := e.ObjectNew.DeepCopyObject().(*appsv1.StatefulSet)
			if !ok {
				return false
			}
			oldObj, ok := e.ObjectOld.DeepCopyObject().(*appsv1.StatefulSet)
			if !ok {
				return false
			}
			return !cmp.Equal(newObj.Status, oldObj.Status)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
}

// PodChangedPredicate .
func PodChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// This update is in fact a Delete event, process it
			if _, ok := e.ObjectNew.(*corev1.Pod); !ok {
				return false
			}

			if e.ObjectNew.GetDeletionGracePeriodSeconds() != nil {
				return true
			}

			// if old and new data is the same, don't reconcile
			newObj, ok := e.ObjectNew.DeepCopyObject().(*corev1.Pod)
			if !ok {
				return false
			}
			oldObj, ok := e.ObjectOld.DeepCopyObject().(*corev1.Pod)
			if !ok {
				return false
			}
			return !cmp.Equal(newObj.Status, oldObj.Status)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
}

// ResourceFailedPredicate .
func ResourceFailedPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// This update is in fact a Delete event, process it
			if _, ok := e.ObjectNew.(*enterpriseApi.Standalone); !ok {
				return false
			}

			if e.ObjectNew.GetDeletionGracePeriodSeconds() != nil {
				return true
			}

			// if old and new data is the same, don't reconcile
			newObj, ok := e.ObjectNew.DeepCopyObject().(*enterpriseApi.Standalone)
			if !ok {
				return false
			}
			return newObj.Status.Phase == "Error"
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
}

// CrdChangedPredicate with generics support
func CrdChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			newObj, ok := e.ObjectNew.(*crdv1.CustomResourceDefinition)
			oldObj, okOld := e.ObjectOld.(*crdv1.CustomResourceDefinition)

			// Ensure both objects are valid CRDs
			if !ok || !okOld {
				return false
			}

			// Check if the object is marked for deletion
			if newObj.GetDeletionGracePeriodSeconds() != nil {
				return true
			}

			// Process only specific CRD names
			if !stringInSlice(newObj.Name, []string{
				"clustermasters.enterprise.splunk.com",
				"clustermanagers.enterprise.splunk.com",
				"indexerclusters.enterprise.splunk.com",
				"licensemasters.enterprise.splunk.com",
				"licensemanagers.enterprise.splunk.com",
				"monitoringconsoles.enterprise.splunk.com",
				"searchheadclusters.enterprise.splunk.com",
				"standalones.enterprise.splunk.com",
			}) {
				return false
			}

			// Compare specifications to determine changes
			return !cmp.Equal(newObj.Spec, oldObj.Spec)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Return true if the object's deletion state is unknown
			return !e.DeleteStateUnknown
		},
	}
}

// ClusterManagerChangedPredicate .
func ClusterManagerChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// This update is in fact a Delete event, process it
			if _, ok := e.ObjectNew.(*enterpriseApi.ClusterManager); !ok {
				return false
			}

			if e.ObjectNew.GetDeletionGracePeriodSeconds() != nil {
				return true
			}

			// if old and new data is the same, don't reconcile
			newObj, ok := e.ObjectNew.DeepCopyObject().(*enterpriseApi.ClusterManager)
			if !ok {
				return false
			}
			oldObj, ok := e.ObjectOld.DeepCopyObject().(*enterpriseApi.ClusterManager)
			if !ok {
				return false
			}
			return !cmp.Equal(newObj.Status, oldObj.Status)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
}

// ClusterMasterChangedPredicate .
func ClusterMasterChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// This update is in fact a Delete event, process it
			if _, ok := e.ObjectNew.(*enterpriseApiV3.ClusterMaster); !ok {
				return false
			}

			if e.ObjectNew.GetDeletionGracePeriodSeconds() != nil {
				return true
			}

			// if old and new data is the same, don't reconcile
			newObj, ok := e.ObjectNew.DeepCopyObject().(*enterpriseApiV3.ClusterMaster)
			if !ok {
				return false
			}
			oldObj, ok := e.ObjectOld.DeepCopyObject().(*enterpriseApiV3.ClusterMaster)
			if !ok {
				return false
			}
			return !cmp.Equal(newObj.Status, oldObj.Status)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}
