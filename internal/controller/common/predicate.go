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
	return err
}

// ConfigMapChangedPredicate .
func ConfigMapChangedPredicate() predicate.Predicate {
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
	return err
}

// ResourceFailedPredicate .
func ResourceFailedPredicate() predicate.Predicate {
	err := predicate.Funcs{
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
	return err
}

// CrdChangedPredicate .
func CrdChangedPredicate() predicate.Predicate {
	err := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// This update is in fact a Delete event, process it
			if _, ok := e.ObjectNew.(*crdv1.CustomResourceDefinition); !ok {
				return false
			}

			if e.ObjectNew.GetDeletionGracePeriodSeconds() != nil {
				return true
			}

			// if old and new data is the same, don't reconcile
			newObj, ok := e.ObjectNew.DeepCopyObject().(*crdv1.CustomResourceDefinition)
			if !ok {
				return false
			}
			oldObj, ok := e.ObjectOld.DeepCopyObject().(*crdv1.CustomResourceDefinition)
			if !ok {
				return false
			}
			if !stringInSlice(newObj.Name, []string{"clustermasters.enterprise.splunk.com",
				"indexerclusters.enterprise.splunk.com",
				"licensemasters.enterprise.splunk.com",
				"licensemanagers.enterprise.splunk.com",
				"monitoringconsoles.enterprise.splunk.com",
				"searchheadclusters.enterprise.splunk.com",
				"standalones.enterprise.splunk.com"}) {
				return false
			}
			return !cmp.Equal(newObj.Spec, oldObj.Spec)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
	return err
}

// ClusterManagerChangedPredicate .
func ClusterManagerChangedPredicate() predicate.Predicate {
	err := predicate.Funcs{
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
	return err
}

// ClusterMasterChangedPredicate .
func ClusterMasterChangedPredicate() predicate.Predicate {
	err := predicate.Funcs{
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
	return err
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}
