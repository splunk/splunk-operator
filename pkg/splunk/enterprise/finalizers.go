package enterprise

import (
	"context"
	"fmt"
	"log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
)

const (
	SPLUNK_FINALIZER_DELETE_PVC="enterprise.splunk.com/delete-pvc"
)


func CheckSplunkDeletion(cr *v1alpha1.SplunkEnterprise, c client.Client) (bool, error) {
	namespace := GetNamespace(cr)
	identifier := GetIdentifier(cr)
	currentTime := metav1.Now()

	// nothing to do if deletion time is in the future
	if ! cr.ObjectMeta.DeletionTimestamp.Before(&currentTime) {
		log.Printf("Deletion deferred to future for SplunkEnterprise %s/%s (Now='%s', DeletionTimestamp='%s')\n",
			namespace, identifier, currentTime, cr.ObjectMeta.DeletionTimestamp)
		return false, nil
	}

	log.Printf("Deletion requested for SplunkEnterprise %s/%s\n", namespace, identifier)

	// process each finalizer
	for _, finalizer := range cr.ObjectMeta.Finalizers {
		switch finalizer {
		case SPLUNK_FINALIZER_DELETE_PVC:
			if err := DeleteSplunkPvc(cr, c); err != nil {
				return false, err
			}
			if err := RemoveSplunkFinalizer(cr, c, finalizer); err != nil {
				return false, err
			}
		default:
			return false, fmt.Errorf("Finalizer in SplunkEnterprise %s/%s not recognized: %s", namespace, identifier, finalizer)
		}
	}

	log.Printf("Deletion complete for SplunkEnterprise %s/%s\n", namespace, identifier)

	return true, nil
}


func DeleteSplunkPvc(cr *v1alpha1.SplunkEnterprise, c client.Client) error {

	// get list of PVCs for this cluster
	labels :=  map[string]string{
		"app": "splunk",
		"for": GetIdentifier(cr),
	}
	var listopts client.ListOptions
	listopts.InNamespace(GetNamespace(cr))
	listopts.MatchingLabels(labels)
	pvclist := corev1.PersistentVolumeClaimList{}
	if err := c.List(context.Background(), &listopts, &pvclist); err != nil {
		return err
	}

	// delete each PVC
	for _, pvc := range pvclist.Items {
		log.Printf("Deleting PVC for SplunkEnterprise %s/%s: %s\n", GetNamespace(cr), GetIdentifier(cr), pvc.ObjectMeta.Name)
		if err := c.Delete(context.Background(), &pvc); err != nil {
			return err
		}
	}

	return nil
}

func RemoveSplunkFinalizer(cr *v1alpha1.SplunkEnterprise, c client.Client, finalizer string) error {
	log.Printf("Removing finalizer for SplunkEnterprise %s/%s: %s\n", GetNamespace(cr), GetIdentifier(cr), finalizer)

	// create new list of finalizers that doesn't include the one being removed
	var newFinalizers []string

	// handles multiple occurrences (performance is not significant)
	for _, f := range cr.ObjectMeta.Finalizers {
		if f != finalizer {
			newFinalizers = append(newFinalizers, f)
		}
	}

	// update object
	cr.ObjectMeta.Finalizers = newFinalizers
	return c.Update(context.Background(), cr)
}
