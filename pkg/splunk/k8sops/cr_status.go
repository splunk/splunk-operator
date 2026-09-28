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

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"k8s.io/apimachinery/pkg/types"
)

// GetCurrentCRWithStatusUpdate returns a fresh CR with the original status copied onto it.
func GetCurrentCRWithStatusUpdate(ctx context.Context, c splcommon.ControllerClient, origCR splcommon.MetaObject, crError *error) (splcommon.MetaObject, error) {
	namespacedName := types.NamespacedName{Name: origCR.GetName(), Namespace: origCR.GetNamespace()}

	copyStatus := func(latestCR splcommon.MetaObject) (splcommon.MetaObject, error) {
		if err := c.Get(ctx, namespacedName, latestCR); err != nil {
			return nil, err
		}
		return latestCR, nil
	}

	switch cr := origCR.(type) {
	case *enterpriseApi.Standalone:
		latestCR := &enterpriseApi.Standalone{}
		setStatusMessage(cr, crError)
		if latest, err := copyStatus(latestCR); err != nil {
			return nil, err
		} else {
			cr.Status.DeepCopyInto(&latest.(*enterpriseApi.Standalone).Status)
			return latest, nil
		}
	case *enterpriseApi.IngestorCluster:
		latestCR := &enterpriseApi.IngestorCluster{}
		setStatusMessage(cr, crError)
		if latest, err := copyStatus(latestCR); err != nil {
			return nil, err
		} else {
			cr.Status.DeepCopyInto(&latest.(*enterpriseApi.IngestorCluster).Status)
			return latest, nil
		}
	case *enterpriseApi.Queue:
		latestCR := &enterpriseApi.Queue{}
		setStatusMessage(cr, crError)
		if latest, err := copyStatus(latestCR); err != nil {
			return nil, err
		} else {
			cr.Status.DeepCopyInto(&latest.(*enterpriseApi.Queue).Status)
			return latest, nil
		}
	case *enterpriseApi.ObjectStorage:
		latestCR := &enterpriseApi.ObjectStorage{}
		setStatusMessage(cr, crError)
		if latest, err := copyStatus(latestCR); err != nil {
			return nil, err
		} else {
			cr.Status.DeepCopyInto(&latest.(*enterpriseApi.ObjectStorage).Status)
			return latest, nil
		}
	case *enterpriseApiV3.LicenseMaster:
		latestCR := &enterpriseApiV3.LicenseMaster{}
		if latest, err := copyStatus(latestCR); err != nil {
			return nil, err
		} else {
			cr.Status.DeepCopyInto(&latest.(*enterpriseApiV3.LicenseMaster).Status)
			return latest, nil
		}
	case *enterpriseApi.LicenseManager:
		latestCR := &enterpriseApi.LicenseManager{}
		setStatusMessage(cr, crError)
		if latest, err := copyStatus(latestCR); err != nil {
			return nil, err
		} else {
			cr.Status.DeepCopyInto(&latest.(*enterpriseApi.LicenseManager).Status)
			return latest, nil
		}
	case *enterpriseApi.SearchHeadCluster:
		latestCR := &enterpriseApi.SearchHeadCluster{}
		setStatusMessage(cr, crError)
		if latest, err := copyStatus(latestCR); err != nil {
			return nil, err
		} else {
			cr.Status.DeepCopyInto(&latest.(*enterpriseApi.SearchHeadCluster).Status)
			return latest, nil
		}
	case *enterpriseApi.IndexerCluster:
		latestCR := &enterpriseApi.IndexerCluster{}
		setStatusMessage(cr, crError)
		if latest, err := copyStatus(latestCR); err != nil {
			return nil, err
		} else {
			cr.Status.DeepCopyInto(&latest.(*enterpriseApi.IndexerCluster).Status)
			return latest, nil
		}
	case *enterpriseApiV3.ClusterMaster:
		latestCR := &enterpriseApiV3.ClusterMaster{}
		if latest, err := copyStatus(latestCR); err != nil {
			return nil, err
		} else {
			cr.Status.DeepCopyInto(&latest.(*enterpriseApiV3.ClusterMaster).Status)
			return latest, nil
		}
	case *enterpriseApi.ClusterManager:
		latestCR := &enterpriseApi.ClusterManager{}
		setStatusMessage(cr, crError)
		if latest, err := copyStatus(latestCR); err != nil {
			return nil, err
		} else {
			cr.Status.DeepCopyInto(&latest.(*enterpriseApi.ClusterManager).Status)
			return latest, nil
		}
	case *enterpriseApi.MonitoringConsole:
		latestCR := &enterpriseApi.MonitoringConsole{}
		setStatusMessage(cr, crError)
		if latest, err := copyStatus(latestCR); err != nil {
			return nil, err
		} else {
			cr.Status.DeepCopyInto(&latest.(*enterpriseApi.MonitoringConsole).Status)
			return latest, nil
		}
	}

	return nil, fmt.Errorf("invalid CR Kind")
}
