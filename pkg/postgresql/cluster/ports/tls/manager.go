/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package tls defines the server TLS boundary used by the PostgreSQL cluster core.
package tls

import (
	"context"
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	ClusterNameLabel = "enterprise.splunk.com/postgrescluster"
	ClusterUIDLabel  = "enterprise.splunk.com/postgrescluster-uid"
	PurposeLabel     = "enterprise.splunk.com/postgres-certificate-purpose"
	ServerTLSPurpose = "server-tls"
)

var (
	ErrCertManagerNotInstalled = errors.New("cert-manager Certificate API is not installed")
	ErrCertificatePending      = errors.New("server certificate is not ready")
	ErrInvalidMaterial         = errors.New("server certificate material is invalid")
	ErrConfiguration           = errors.New("server TLS configuration is invalid")
)

// Identity is the resolved identity and naming contract for one cluster's
// server TLS resources. Core constructs it; adapters only apply it.
type Identity struct {
	ClusterName     string
	Namespace       string
	ClusterUID      types.UID
	OwnerReference  metav1.OwnerReference
	CertificateName string
	ServerTLSSecret string
	ServerCASecret  string
}

func (i Identity) Labels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "splunk-operator",
		ClusterNameLabel:               i.ClusterName,
		ClusterUIDLabel:                string(i.ClusterUID),
		PurposeLabel:                   ServerTLSPurpose,
	}
}

// IssuerReference is the resolved cert-manager issuer identity.
type IssuerReference struct {
	Name  string
	Kind  string
	Group string
}

// Request is the complete, immutable input needed to issue a server certificate.
type Request struct {
	Identity               Identity
	IssuerRef              IssuerReference
	DNSNames               []string
	RetainedPoolerDNSNames []string
	Usages                 []string
	Duration               *metav1.Duration
	RenewBefore            *metav1.Duration
}

// FinalizeRequest contains the class-derived cleanup decision for one
// server-TLS resource set.
type FinalizeRequest struct {
	Identity       Identity
	Retain         bool
	DeleteCASecret bool
}

// Manager applies a fully resolved TLS request and reads only Kubernetes
// state required by core to preserve existing pooler SANs.
type Manager interface {
	ExistingCertificateDNSNames(context.Context, Identity) ([]string, error)
	Reconcile(context.Context, Request) error
	Finalize(context.Context, FinalizeRequest) error
}
