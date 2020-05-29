// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"sigs.k8s.io/controller-runtime/pkg/manager"

	splunkreconcile "github.com/splunk/splunk-operator/pkg/splunk/reconcile"
)

// SplunkControllersToAdd is a list of Splunk controllers to add to the Manager
var SplunkControllersToAdd []splunkreconcile.SplunkController

// AddToManager adds all Splunk Controllers to the Manager
func AddToManager(mgr manager.Manager) error {
	for _, splctrl := range SplunkControllersToAdd {
		if err := splunkreconcile.AddToManager(mgr, splctrl); err != nil {
			return err
		}
	}
	return nil
}
