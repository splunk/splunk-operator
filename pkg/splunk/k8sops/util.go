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

package k8sops

import (
	"context"
	"reflect"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// hasProbeChanged checks for changes in given current probe
func hasProbeChanged(currentProbe *corev1.Probe, revisedProbe *corev1.Probe) bool {
	if currentProbe == nil {
		return revisedProbe != nil
	}
	if currentProbe.InitialDelaySeconds != revisedProbe.InitialDelaySeconds {
		return true
	}
	if currentProbe.TimeoutSeconds != revisedProbe.TimeoutSeconds {
		return true
	}
	if currentProbe.PeriodSeconds != revisedProbe.PeriodSeconds {
		return true
	}
	if currentProbe.FailureThreshold != revisedProbe.FailureThreshold {
		return true
	}
	return false
}

// isCurrentCROwner returns true if current CR is the ONLY owner of the automated MC
func isCurrentCROwner(cr splcommon.MetaObject, currentOwners []metav1.OwnerReference) bool {
	// adding extra verification as unit test cases fails since fakeclient do not set UID
	return reflect.DeepEqual(currentOwners[0].UID, cr.GetUID()) &&
		(currentOwners[0].Kind == cr.GetObjectKind().GroupVersionKind().Kind) &&
		(currentOwners[0].Name == cr.GetName())
}

type InstanceType = splcommon.InstanceType

const (
	SplunkStandalone        = splcommon.SplunkStandalone
	SplunkClusterMaster     = splcommon.SplunkClusterMaster
	SplunkClusterManager    = splcommon.SplunkClusterManager
	SplunkDeployer          = splcommon.SplunkDeployer
	SplunkSearchHead        = splcommon.SplunkSearchHead
	SplunkIndexer           = splcommon.SplunkIndexer
	SplunkIngestor          = splcommon.SplunkIngestor
	SplunkLicenseMaster     = splcommon.SplunkLicenseMaster
	SplunkLicenseManager    = splcommon.SplunkLicenseManager
	SplunkMonitoringConsole = splcommon.SplunkMonitoringConsole

	livenessProbeDriverPathEnv  = "SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH"
	configToken                 = "conftoken"
	smartStoreConfigRev         = "SmartStoreConfigRev"
	splunkKVStoreDefaultTypeEnv = "SPLUNK_KVSTORE_DEFAULT_TYPE"
	splunkKVStoreTypeLocal      = "local"
)

// ReadFile is retained for compatibility with callers of the legacy helper location.
// Deprecated: use util.ReadFile.
func ReadFile(ctx context.Context, location string) (string, error) {
	return splutil.ReadFile(ctx, location)
}

func isCMDeployed(instanceType InstanceType) bool {
	return instanceType == SplunkClusterManager || instanceType == SplunkClusterMaster
}
