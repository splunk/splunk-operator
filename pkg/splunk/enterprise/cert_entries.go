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

package enterprise

import (
	cmapi "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	certlib "github.com/splunk/splunk-operator/pkg/splunk/client/certmanager"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
)

// toCertEntries converts a []CertSpec into the []CertEntry type expected by
// workflow/certs, keeping that package decoupled from CRD API types.
// dnsNames is the auto-derived SAN list (per §4.1.1) used for any entry that
// does not set its own CertSpec.DNSNames.
func toCertEntries(specs []enterpriseApi.CertSpec, dnsNames []string) []certs.CertEntry {
	if len(specs) == 0 {
		return nil
	}
	entries := make([]certs.CertEntry, len(specs))
	for i, s := range specs {
		names := s.DNSNames
		if len(names) == 0 {
			names = dnsNames
		}
		entries[i] = certs.CertEntry{
			SecretName:     s.SecretRef.Name,
			Role:           string(s.Role),
			IssuerRef:      toIssuerRef(s.IssuerRef),
			DNSNames:       names,
			Duration:       s.Duration,
			RenewBefore:    s.RenewBefore,
			RotationPolicy: cmapi.PrivateKeyRotationPolicy(s.RotationPolicy),
		}
	}
	return entries
}

// toIssuerRef converts a CertSpec.IssuerRef into the certlib.IssuerRef type
// expected by workflow/certs, keeping that package decoupled from CRD API types.
func toIssuerRef(ref *enterpriseApi.IssuerReference) *certlib.IssuerRef {
	if ref == nil {
		return nil
	}
	return &certlib.IssuerRef{Name: ref.Name, Kind: ref.Kind}
}

// autoDNSNames derives the DNS SAN list for a CR per the table in design doc
// §4.1.1. name is the CR's identifier (cr.GetName()); namespace is its
// namespace. Multi-replica StatefulSets get a wildcard headless SAN;
// single-replica ones get an explicit pod-0 FQDN.
func autoDNSNames(instanceType InstanceType, name, namespace string, replicas int32) []string {
	serviceFQDN := splcommon.GetServiceFQDN(namespace, splcommon.GetSplunkServiceName(instanceType, name, false))

	if instanceType == SplunkMonitoringConsole {
		return certlib.DeriveDNSNames([]string{serviceFQDN}, "", nil)
	}

	if replicas > 1 {
		headlessFQDN := splcommon.GetServiceFQDN(namespace, splcommon.GetSplunkServiceName(instanceType, name, true))
		return certlib.DeriveDNSNames([]string{serviceFQDN}, headlessFQDN, nil)
	}

	podFQDN := splcommon.GetServiceFQDN(namespace, GetSplunkStatefulsetPodName(instanceType, name, 0)+"."+splcommon.GetSplunkServiceName(instanceType, name, true))
	return certlib.DeriveDNSNames([]string{serviceFQDN}, "", []string{podFQDN})
}

// autoDNSNamesSearchHeadCluster derives SANs for SearchHeadCluster: SH
// service DNS + wildcard for SH pod FQDNs + deployer service DNS.
func autoDNSNamesSearchHeadCluster(name, namespace string) []string {
	shServiceFQDN := splcommon.GetServiceFQDN(namespace, splcommon.GetSplunkServiceName(SplunkSearchHead, name, false))
	shHeadlessFQDN := splcommon.GetServiceFQDN(namespace, splcommon.GetSplunkServiceName(SplunkSearchHead, name, true))
	deployerFQDN := splcommon.GetServiceFQDN(namespace, splcommon.GetSplunkServiceName(SplunkDeployer, name, false))
	return certlib.DeriveDNSNames([]string{shServiceFQDN, deployerFQDN}, shHeadlessFQDN, nil)
}
