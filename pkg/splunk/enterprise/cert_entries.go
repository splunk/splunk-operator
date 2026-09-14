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
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
)

// ToCertEntries converts Enterprise CRD CertSpec values into the CR-agnostic
// entries consumed by the certificate workflow. dnsNames is used when an
// entry does not define its own DNS names.
func ToCertEntries(specs []enterpriseApi.CertSpec, dnsNames []string) []certs.CertEntry {
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
