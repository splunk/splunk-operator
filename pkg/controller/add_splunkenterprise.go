package controller

import (
	"git.splunk.com/splunk-operator/pkg/controller/splunkenterprise"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, splunkenterprise.Add)
}
