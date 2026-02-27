// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//  http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package certificate

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/certificate"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CertManagerProvider implements certificate provisioning via cert-manager.
//
// The provider:
// - Creates Certificate CRs in the cert-manager.io API group
// - Watches Certificate status for readiness
// - Extracts secret name from Certificate status
// - Returns Ready=true when cert-manager has issued the certificate
type CertManagerProvider struct {
	client client.Client
	logger logr.Logger
}

// NewCertManagerProvider creates a new cert-manager provider.
func NewCertManagerProvider(client client.Client, logger logr.Logger) *CertManagerProvider {
	return &CertManagerProvider{
		client: client,
		logger: logger.WithName("certmanager-provider"),
	}
}

// Name returns the provider name.
func (p *CertManagerProvider) Name() string {
	return "cert-manager"
}

// EnsureCertificate ensures a cert-manager Certificate exists.
func (p *CertManagerProvider) EnsureCertificate(ctx context.Context, req Request) (*certificate.Ref, error) {
	// Build Certificate CR (unstructured, since we don't import cert-manager types)
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Certificate",
	})

	// Check if Certificate exists
	err := p.client.Get(ctx, types.NamespacedName{
		Name:      req.Name,
		Namespace: req.Namespace,
	}, cert)

	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create Certificate CR
			return p.createCertificate(ctx, req)
		}
		return nil, fmt.Errorf("failed to get Certificate: %w", err)
	}

	// Certificate exists, check its status
	return p.checkCertificateStatus(cert, req)
}

// createCertificate creates a new cert-manager Certificate CR.
func (p *CertManagerProvider) createCertificate(ctx context.Context, req Request) (*certificate.Ref, error) {
	p.logger.Info("Creating cert-manager Certificate",
		"name", req.Name,
		"namespace", req.Namespace,
		"dnsNames", req.DNSNames,
	)

	// Build Certificate spec
	spec := map[string]interface{}{
		"secretName":  req.Name,
		"dnsNames":    req.DNSNames,
		"duration":    fmt.Sprintf("%ds", req.Duration),
		"renewBefore": fmt.Sprintf("%ds", req.RenewBefore),
		"usages":      req.Usages,
	}

	if len(req.IPAddresses) > 0 {
		spec["ipAddresses"] = req.IPAddresses
	}

	// Add issuerRef
	if req.IssuerRef != nil {
		issuerRef := map[string]interface{}{
			"name": req.IssuerRef.Name,
		}
		if req.IssuerRef.Kind != "" {
			issuerRef["kind"] = req.IssuerRef.Kind
		} else {
			issuerRef["kind"] = "ClusterIssuer"
		}
		if req.IssuerRef.Group != "" {
			issuerRef["group"] = req.IssuerRef.Group
		} else {
			issuerRef["group"] = "cert-manager.io"
		}
		spec["issuerRef"] = issuerRef
	}

	// Create unstructured Certificate
	cert := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "cert-manager.io/v1",
			"kind":       "Certificate",
			"metadata": map[string]interface{}{
				"name":      req.Name,
				"namespace": req.Namespace,
			},
			"spec": spec,
		},
	}

	err := p.client.Create(ctx, cert)
	if err != nil {
		return nil, fmt.Errorf("failed to create Certificate: %w", err)
	}

	p.logger.Info("Certificate CR created successfully",
		"name", req.Name,
		"namespace", req.Namespace,
	)

	// Return not ready (cert-manager needs time to issue)
	return &certificate.Ref{
		SecretName: req.Name,
		Namespace:  req.Namespace,
		Ready:      false,
		Provider:   "cert-manager",
		Error:      "Certificate is being issued by cert-manager",
	}, nil
}

// checkCertificateStatus checks the status of a cert-manager Certificate.
func (p *CertManagerProvider) checkCertificateStatus(cert *unstructured.Unstructured, req Request) (*certificate.Ref, error) {
	// Extract status
	status, found, err := unstructured.NestedMap(cert.Object, "status")
	if err != nil || !found {
		return &certificate.Ref{
			SecretName: req.Name,
			Namespace:  req.Namespace,
			Ready:      false,
			Provider:   "cert-manager",
			Error:      "Certificate status not yet available",
		}, nil
	}

	// Check conditions
	conditions, found, err := unstructured.NestedSlice(status, "conditions")
	if err != nil || !found {
		return &certificate.Ref{
			SecretName: req.Name,
			Namespace:  req.Namespace,
			Ready:      false,
			Provider:   "cert-manager",
			Error:      "Certificate conditions not yet available",
		}, nil
	}

	// Look for Ready condition
	ready := false
	errorMsg := ""
	for _, cond := range conditions {
		condMap, ok := cond.(map[string]interface{})
		if !ok {
			continue
		}

		condType, _, _ := unstructured.NestedString(condMap, "type")
		if condType != "Ready" {
			continue
		}

		condStatus, _, _ := unstructured.NestedString(condMap, "status")
		ready = (condStatus == string(metav1.ConditionTrue))

		if !ready {
			reason, _, _ := unstructured.NestedString(condMap, "reason")
			message, _, _ := unstructured.NestedString(condMap, "message")
			errorMsg = fmt.Sprintf("%s: %s", reason, message)
		}
		break
	}

	ref := &certificate.Ref{
		SecretName: req.Name,
		Namespace:  req.Namespace,
		Ready:      ready,
		Provider:   "cert-manager",
	}

	if !ready {
		ref.Error = errorMsg
		if errorMsg == "" {
			ref.Error = "Certificate not yet ready"
		}
	}

	// Extract NotBefore and NotAfter from status if available
	if ready {
		if notBefore, found, _ := unstructured.NestedString(status, "notBefore"); found {
			if t, err := time.Parse(time.RFC3339, notBefore); err == nil {
				ref.NotBefore = &t
			}
		}
		if notAfter, found, _ := unstructured.NestedString(status, "notAfter"); found {
			if t, err := time.Parse(time.RFC3339, notAfter); err == nil {
				ref.NotAfter = &t
			}
		}
		if renewalTime, found, _ := unstructured.NestedString(status, "renewalTime"); found {
			if t, err := time.Parse(time.RFC3339, renewalTime); err == nil {
				ref.RenewalTime = &t
			}
		}
	}

	p.logger.V(1).Info("Certificate status checked",
		"name", req.Name,
		"namespace", req.Namespace,
		"ready", ready,
		"error", errorMsg,
	)

	return ref, nil
}

// GetSecret retrieves the certificate secret.
func (p *CertManagerProvider) GetSecret(ctx context.Context, namespace, name string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	err := p.client.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, secret)

	if err != nil {
		return nil, err
	}

	return secret, nil
}
