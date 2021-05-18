package testenv

import (
	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1"
)

// GenerateAppSourceSpec return AppSourceSpec struct with given values
func GenerateAppSourceSpec(appSourceName string, appSourceLocation string, appSourceDefaultSpec enterprisev1.AppSourceDefaultSpec) enterprisev1.AppSourceSpec {
	return enterprisev1.AppSourceSpec{
		Name:                 appSourceName,
		Location:             appSourceLocation,
		AppSourceDefaultSpec: appSourceDefaultSpec,
	}
}
