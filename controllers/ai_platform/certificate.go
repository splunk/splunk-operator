package ai_platform

import (
	"context"
	"fmt"
	//appsv1 "k8s.io/api/apps/v1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// --- 2️⃣ reconcileCertificate: issue a cert-manager Certificate for mTLS ---
func (r *AIPlatformReconciler) reconcileCertificate(ctx context.Context, p *enterpriseApi.SplunkAIPlatform) error {
	certName := p.Name + "-tls"
	cert := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: p.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cert, func() error {
		cert.Spec = certmanagerv1.CertificateSpec{
			SecretName: certName + "-secret",
			IssuerRef: certmanagermeta.ObjectReference{
				Name: p.Spec.CertificateRef,
				Kind: "ClusterIssuer",
			},
			DNSNames: []string{
				fmt.Sprintf("%s.%s.svc.%s", p.Name, p.Namespace, p.Spec.ClusterDomain),
			},
		}
		return controllerutil.SetOwnerReference(p, cert, r.Scheme)
	})
	return err
}
