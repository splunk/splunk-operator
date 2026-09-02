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

package certs

import (
	certlib "github.com/splunk/splunk-operator/pkg/splunk/client/certmanager"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

// AutoDNSNames derives the DNS SAN list for an Enterprise CR. Multi-replica
// StatefulSets get a wildcard headless SAN; single-replica ones get an
// explicit pod-0 FQDN.
func AutoDNSNames(instanceType splcommon.InstanceType, name, namespace string, replicas int32) []string {
	serviceFQDN := splcommon.GetServiceFQDN(namespace, splcommon.GetSplunkServiceName(instanceType, name, false))

	if instanceType == splcommon.SplunkMonitoringConsole {
		return certlib.DeriveDNSNames([]string{serviceFQDN}, "", nil)
	}

	if replicas > 1 {
		headlessFQDN := splcommon.GetServiceFQDN(namespace, splcommon.GetSplunkServiceName(instanceType, name, true))
		return certlib.DeriveDNSNames([]string{serviceFQDN}, headlessFQDN, nil)
	}

	podFQDN := splcommon.GetServiceFQDN(namespace, splutil.GetSplunkStatefulsetPodName(instanceType, name, 0)+"."+splcommon.GetSplunkServiceName(instanceType, name, true))
	return certlib.DeriveDNSNames([]string{serviceFQDN}, "", []string{podFQDN})
}

// AutoDNSNamesSearchHeadCluster derives SANs for a SearchHeadCluster:
// search-head service DNS, wildcard pod FQDN, and deployer service DNS.
func AutoDNSNamesSearchHeadCluster(name, namespace string) []string {
	shServiceFQDN := splcommon.GetServiceFQDN(namespace, splcommon.GetSplunkServiceName(splcommon.SplunkSearchHead, name, false))
	shHeadlessFQDN := splcommon.GetServiceFQDN(namespace, splcommon.GetSplunkServiceName(splcommon.SplunkSearchHead, name, true))
	deployerFQDN := splcommon.GetServiceFQDN(namespace, splcommon.GetSplunkServiceName(splcommon.SplunkDeployer, name, false))
	return certlib.DeriveDNSNames([]string{shServiceFQDN, deployerFQDN}, shHeadlessFQDN, nil)
}
