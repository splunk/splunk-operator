// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package k8sops

import (
	"context"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetStandaloneList returns Standalones in the current namespace.
func GetStandaloneList(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []client.ListOption) (enterpriseApi.StandaloneList, error) {
	logger := logging.FromContext(ctx).With("func", "getStandaloneList")
	objectList := enterpriseApi.StandaloneList{}

	err := c.List(ctx, &objectList, listOpts...)
	if err != nil {
		logger.ErrorContext(ctx, "Standalone types not found in namespace", "namespace", cr.GetNamespace(), "error", err)
		return objectList, err
	}

	return objectList, nil
}
