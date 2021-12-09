// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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
	"context"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	enterprise "github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func init() {
	SplunkControllersToAdd = append(SplunkControllersToAdd, ClusterManagerController{})
}

// blank assignment to verify that ClusterManagerController implements SplunkController
var _ splctrl.SplunkController = &ClusterManagerController{}

// ClusterManagerController is used to manage ClusterManager custom resources
type ClusterManagerController struct{}

// GetInstance returns an instance of the custom resource managed by the controller
func (ctrl ClusterManagerController) GetInstance() splcommon.MetaObject {
	return &enterpriseApi.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: enterpriseApi.APIVersion,
			Kind:       "ClusterMaster",
		},
	}
}

// GetWatchTypes returns a list of types owned by the controller that it would like to receive watch events for
func (ctrl ClusterManagerController) GetWatchTypes() []client.Object {
	return []client.Object{&appsv1.StatefulSet{}, &corev1.Secret{}}
}

// Reconcile is used to perform an idempotent reconciliation of the custom resource managed by this controller
func (ctrl ClusterManagerController) Reconcile(client client.Client, cr splcommon.MetaObject) (reconcile.Result, error) {
	instance := cr.(*enterpriseApi.ClusterMaster)
	return enterprise.ApplyClusterManager(context.TODO(), client, instance)
}
