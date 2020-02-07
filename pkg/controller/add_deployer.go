package controller

import (
	"github.com/splunk/splunk-operator/pkg/controller/deployer"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, deployer.Add)
}
