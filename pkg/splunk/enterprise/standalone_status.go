package enterprise

import (
	"context"

	v4 "github.com/splunk/splunk-operator/api/v4"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type standaloneStatusAdapter struct {
	c   client.Client
	cr  *v4.Standalone
	sts *appsv1.StatefulSet // or keep a copy of the PodTemplate annotations
}

func (a *standaloneStatusAdapter) GetClient() client.Client { return a.c }
func (a *standaloneStatusAdapter) NamespacedName() types.NamespacedName {
	return types.NamespacedName{Namespace: a.cr.Namespace, Name: a.cr.Name}
}
func (a *standaloneStatusAdapter) SpecTLS() *v4.TLSConfig           { return a.cr.Spec.TLS }
func (a *standaloneStatusAdapter) ExistingTLSStatus() *v4.TLSStatus { return a.cr.Status.TLS }
func (a *standaloneStatusAdapter) UpdateTLSStatus(ctx context.Context, st *v4.TLSStatus) error {
	a.cr.Status.TLS = st
	//return a.c.Status().Update(ctx, a.cr)
	return nil
}
func (a *standaloneStatusAdapter) PreTasksConfigMapName() string {
	return a.cr.Name + "-tls-pre"
}
func (a *standaloneStatusAdapter) PodTemplateAnnotations() map[string]string {
	if a.sts != nil && a.sts.Spec.Template.Annotations != nil {
		return a.sts.Spec.Template.Annotations
	}
	return nil
}
