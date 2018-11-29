package stub

import (
	"errors"
	"operator/splunk-operator/pkg/apis/splunk-instance/v1alpha1"
)

func ValidateSplunkCustomResource(instance *v1alpha1.SplunkInstance) error {
	if instance.Spec.SearchHeads > 0 && instance.Spec.Indexers <= 0 {
		return errors.New("You must specify how many indexers the cluster should have.")
	}
	if instance.Spec.SearchHeads <= 0 && instance.Spec.Indexers > 0 {
		return errors.New("You must specify how many search heads the cluster should have.")
	}
	return nil
}