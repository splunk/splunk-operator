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

package certmanager

// WildcardHeadlessSAN returns the wildcard DNS SAN covering every pod behind
// a headless Service, e.g. "*.splunk-shc1-search-head-headless.ns.svc.cluster.local".
// Used for multi-replica StatefulSets where any pod ordinal may serve the cert.
func WildcardHeadlessSAN(headlessServiceFQDN string) string {
	return "*." + headlessServiceFQDN
}

// DeriveDNSNames builds the DNS SAN list for a cert from pre-computed
// service/pod FQDNs. It takes plain strings rather than CR types so this
// package stays decoupled from CRD-specific naming — callers in
// pkg/splunk/enterprise assemble the FQDNs (via names.go helpers) per the
// auto-derived SAN table in the design doc (§4.1.1) and pass them here.
//
// serviceFQDNs are included as-is (e.g. the main service and, for
// SearchHeadCluster, the deployer service). headlessFQDN, when non-empty,
// is wrapped in a wildcard SAN for multi-replica StatefulSets. podFQDNs are
// included as-is for single-replica StatefulSets that use an explicit
// pod-0 FQDN instead of a wildcard.
func DeriveDNSNames(serviceFQDNs []string, headlessFQDN string, podFQDNs []string) []string {
	var names []string
	names = append(names, serviceFQDNs...)
	if headlessFQDN != "" {
		names = append(names, WildcardHeadlessSAN(headlessFQDN))
	}
	names = append(names, podFQDNs...)
	return names
}
