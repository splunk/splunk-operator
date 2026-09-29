// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package k8sops

import (
	"context"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// GetObjectStorage returns an ObjectStorage by namespaced name.
func GetObjectStorage(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, namespacedName types.NamespacedName) (*enterpriseApi.ObjectStorage, error) {
	logger := logging.FromContext(ctx).With("func", "GetObjectStorage", "name", cr.GetName(), "namespace", cr.GetNamespace(), "objectStorage", namespacedName.Name)
	object := &enterpriseApi.ObjectStorage{}
	if err := c.Get(ctx, namespacedName, object); err != nil {
		if !k8serrors.IsNotFound(err) {
			logger.ErrorContext(ctx, "failed to get ObjectStorage", "error", err, "namespace", namespacedName.Namespace)
		}
		return nil, err
	}
	return object, nil
}
