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

package splkcontroller

import (
	"context"
	"errors"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

// ApplySecret creates or updates a Kubernetes Secret, and returns active secrets if successful
func ApplySecret(ctx context.Context, client splcommon.ControllerClient, secret *corev1.Secret) (*corev1.Secret, error) {
	// Invalid secret object
	if secret == nil {
		return nil, errors.New(splcommon.InvalidSecretObjectError)
	}

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplySecret").WithValues(
		"name", secret.GetObjectMeta().GetName(),
		"namespace", secret.GetObjectMeta().GetNamespace())

	var result corev1.Secret

	namespacedName := types.NamespacedName{Namespace: secret.GetNamespace(), Name: secret.GetName()}
	err := client.Get(ctx, namespacedName, &result)
	if err == nil {
		scopedLog.Info("Found existing Secret, update if needed")
		if !reflect.DeepEqual(&result, secret) {
			result = *secret
			err = splutil.UpdateResource(ctx, client, &result)
			if err != nil {
				return nil, err
			}
			secret = &result
		}
	} else if k8serrors.IsNotFound(err) {
		scopedLog.Info("Didn't find secret, creating one")
		err = splutil.CreateResource(ctx, client, secret)
		if err != nil {
			return nil, err
		}
		gerr := client.Get(ctx, namespacedName, secret)
		retryCount := 0
		for ; gerr != nil; gerr = client.Get(ctx, namespacedName, secret) {
			scopedLog.Error(gerr, "Newly created resource still not in cache sleeping for 10 micro second", "secret", namespacedName.Name, "error", gerr.Error())
			time.Sleep(10 * time.Microsecond)

			// Avoid infinite loop
			retryCount++
			if retryCount > 20 {
				return secret, gerr
			}
		}
		result = *secret
	} else {
		return nil, err
	}
	return &result, err
}
