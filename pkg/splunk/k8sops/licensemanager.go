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
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetLicenseManagerList returns LicenseManagers in the current namespace.
func GetLicenseManagerList(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []client.ListOption) (enterpriseApi.LicenseManagerList, error) {
	var list enterpriseApi.LicenseManagerList
	if err := c.List(ctx, &list, listOpts...); err != nil {
		return list, fmt.Errorf("list LicenseManager in namespace %s: %w", cr.GetNamespace(), err)
	}
	return list, nil
}
