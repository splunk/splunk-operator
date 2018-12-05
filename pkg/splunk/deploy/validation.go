package deploy

import (
	"errors"
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
)

func ValidateSplunkCustomResource(instance *v1alpha1.SplunkEnterprise) error {
	if instance.Spec.Topology.SearchHeads > 0 && instance.Spec.Topology.Indexers <= 0 {
		return errors.New("You must specify how many indexers the cluster should have.")
	}
	if instance.Spec.Topology.SearchHeads <= 0 && instance.Spec.Topology.Indexers > 0 {
		return errors.New("You must specify how many search heads the cluster should have.")
	}
	return nil
}