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

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// ChangeAnnotations updates the splunk/image-tag field to trigger the
// reconcile loop, and returns an error if the Kubernetes update fails.
func ChangeAnnotations(ctx context.Context, c splcommon.ControllerClient, image string, cr splcommon.MetaObject) error {
	annotations := cr.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	if _, ok := annotations["splunk/image-tag"]; ok {
		if annotations["splunk/image-tag"] == image {
			return nil
		}
	}

	// create/update the checkUpdateImage annotation field
	annotations["splunk/image-tag"] = image

	cr.SetAnnotations(annotations)
	return c.Update(ctx, cr)
}
