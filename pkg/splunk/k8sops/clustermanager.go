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
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetClusterManagerList returns ClusterManagers in the current namespace.
func GetClusterManagerList(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []client.ListOption) (enterpriseApi.ClusterManagerList, error) {
	logger := logging.FromContext(ctx).With("func", "GetClusterManagerList", "name", cr.GetName(), "namespace", cr.GetNamespace())
	var list enterpriseApi.ClusterManagerList
	if err := c.List(ctx, &list, listOpts...); err != nil {
		logger.ErrorContext(ctx, "ClusterManager types not found in namespace", "error", err, "namespace", cr.GetNamespace())
		return list, err
	}
	return list, nil
}

// ChangeClusterManagerAnnotations updates the ClusterManager image annotation
// after a LicenseManager becomes ready, triggering its reconciliation loop.
func ChangeClusterManagerAnnotations(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.LicenseManager) error {
	logger := logging.FromContext(ctx).With("func", "ChangeClusterManagerAnnotations", "name", cr.GetName(), "namespace", cr.GetNamespace())
	eventPublisher := splcommon.GetEventPublisher(ctx)

	clusterManagerInstance := &enterpriseApi.ClusterManager{}
	if cr.Spec.ClusterManagerRef.Name != "" {
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.Spec.ClusterManagerRef.Name}
		if err := c.Get(ctx, namespacedName, clusterManagerInstance); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
	} else {
		var objectList enterpriseApi.ClusterManagerList
		if err := c.List(ctx, &objectList, client.InNamespace(cr.GetNamespace())); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		for i := range objectList.Items {
			if objectList.Items[i].Spec.LicenseManagerRef.Name == cr.GetName() {
				clusterManagerInstance = &objectList.Items[i]
				break
			}
		}
		if clusterManagerInstance.GetName() == "" {
			return nil
		}
	}

	statefulSet := &appsv1.StatefulSet{}
	statefulSetName := splutil.GetSplunkStatefulsetName(splcommon.SplunkLicenseManager, cr.GetName())
	if err := c.Get(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: statefulSetName}, statefulSet); err != nil {
		if eventPublisher != nil {
			eventPublisher.Warning(ctx, splcommon.EventReasonAnnotationUpdateFailed, fmt.Sprintf("Could not get the LicenseManager Image. Reason %v", err))
		}
		logger.ErrorContext(ctx, "get LicenseManager Image failed", "error", err)
		return err
	}
	if len(statefulSet.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("unable to get image from LicenseManager statefulset")
	}

	annotations := clusterManagerInstance.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	image := statefulSet.Spec.Template.Spec.Containers[0].Image
	if annotations["splunk/image-tag"] == image {
		return nil
	}
	annotations["splunk/image-tag"] = image
	clusterManagerInstance.SetAnnotations(annotations)
	if err := c.Update(ctx, clusterManagerInstance); err != nil {
		if eventPublisher != nil {
			eventPublisher.Warning(ctx, splcommon.EventReasonAnnotationUpdateFailed, fmt.Sprintf("Could not update annotations. Reason %v", err))
		}
		logger.ErrorContext(ctx, "ClusterManager types update after changing annotations failed", "error", err)
		return err
	}
	return nil
}
