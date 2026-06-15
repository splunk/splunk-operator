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

const (
	// CertMountRoot is the base directory under which all cert secrets are mounted.
	CertMountRoot = "/mnt/tls"

	// CertTLSCRTKey is the required Secret key for the certificate.
	CertTLSCRTKey = "tls.crt"

	// CertTLSKeyKey is the required Secret key for the private key.
	CertTLSKeyKey = "tls.key"

	// CertCAKey is the optional Secret key for the CA certificate.
	CertCAKey = "ca.crt"

	// RoleMountFmt is the fixed mount path for Ansible-processed (role-tagged) certs.
	// Ansible reads from /mnt/tls/splunk-<role>-tls-cert/ regardless of the secret name.
	RoleMountFmt = CertMountRoot + "/splunk-%s-tls-cert"

	// CertRevAnnotFmt is the pod annotation key format for cert rotation detection.
	// Uses the enterprise.splunk.com prefix, consistent with other SOK annotations
	// (e.g. enterprise.splunk.com/admin-managed-pv, enterprise.splunk.com/delete-pvc).
	// Value is SHA-256(tls.crt + tls.key + ca.crt) of the mounted secret.
	// Use certRevAnnotKey() to build a safe, length-bounded key from a secret name.
	CertRevAnnotFmt = "enterprise.splunk.com/cert-rev-%s"
)
