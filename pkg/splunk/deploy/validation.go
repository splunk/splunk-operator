package deploy

import (
	"errors"
	"fmt"
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"os"
)

func ValidateSplunkCustomResource(instance *v1alpha1.SplunkEnterprise) error {
	// cluster sanity checks
	if instance.Spec.Topology.SearchHeads > 0 && instance.Spec.Topology.Indexers <= 0 {
		return errors.New("You must specify how many indexers the cluster should have.")
	}
	if instance.Spec.Topology.SearchHeads <= 0 && instance.Spec.Topology.Indexers > 0 {
		return errors.New("You must specify how many search heads the cluster should have.")
	}
	if instance.Spec.Topology.Indexers > 0 && instance.Spec.Topology.SearchHeads > 0 && instance.Spec.Config.SplunkLicense.LicensePath == "" {
		return errors.New("You must provide a license to create a cluster.")
	}

	// default to using a single standalone instance
	if instance.Spec.Topology.SearchHeads <= 0 && instance.Spec.Topology.Indexers <= 0 {
		if instance.Spec.Topology.Standalones <= 0 {
			instance.Spec.Topology.Standalones = 1
		}
	}

	// default to a single spark worker
	if instance.Spec.Config.EnableDFS && instance.Spec.Topology.SparkWorkers <= 0 {
		instance.Spec.Topology.SparkWorkers = 1
	}

	// ImagePullPolicy
	if (instance.Spec.Config.ImagePullPolicy == "") {
		instance.Spec.Config.ImagePullPolicy = os.Getenv("IMAGE_PULL_POLICY")
	}
	switch (instance.Spec.Config.ImagePullPolicy) {
	case "":
		instance.Spec.Config.ImagePullPolicy = "IfNotPresent"
		break
	case "Always":
		break
	case "IfNotPresent":
		break
	default:
		return fmt.Errorf("ImagePullPolicy must be one of \"Always\" or \"IfNotPresent\"; value=\"%s\"",
			instance.Spec.Config.ImagePullPolicy)
	}

	return nil
}

