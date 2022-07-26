// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.
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
	"reflect"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ApplyServiceAccount creates or updates a Kubernetes serviceAccount
func ApplyServiceAccount(ctx context.Context, client splcommon.ControllerClient, serviceAccount *corev1.ServiceAccount) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyServiceAccount").WithValues("serviceAccount", serviceAccount.GetName(),
		"namespace", serviceAccount.GetNamespace())

	namespacedName := types.NamespacedName{Namespace: serviceAccount.GetNamespace(), Name: serviceAccount.GetName()}
	var current corev1.ServiceAccount

	err := client.Get(ctx, namespacedName, &current)
	if err == nil {
		if !reflect.DeepEqual(serviceAccount, &current) {
			scopedLog.Info("Updating service account")
			current = *serviceAccount
			err = splutil.UpdateResource(ctx, client, &current)
			if err != nil {
				return err
			}
			// after update get the latest resource
			err = client.Get(ctx, namespacedName, &current)
		}
	} else if k8serrors.IsNotFound(err) {
		err = splutil.CreateResource(ctx, client, serviceAccount)
	} else if err != nil {
		return err
	}

	return err
}

// GetServiceAccount gets the serviceAccount resource in a given namespace
func GetServiceAccount(ctx context.Context, client splcommon.ControllerClient, namespacedName types.NamespacedName) (*corev1.ServiceAccount, error) {
	var serviceAccount corev1.ServiceAccount
	err := client.Get(ctx, namespacedName, &serviceAccount)
	if err != nil {
		reqLogger := log.FromContext(ctx)
		scopedLog := reqLogger.WithName("GetServiceAccount").WithValues("serviceAccount", namespacedName.Name,
			"namespace", namespacedName.Namespace, "error", err)
		scopedLog.Info("ServiceAccount not found")
		return nil, err
	}
	return &serviceAccount, nil
}
