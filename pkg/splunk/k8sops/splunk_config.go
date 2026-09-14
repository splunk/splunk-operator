package k8sops

import (
	"context"
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ApplySplunkConfig reconciles the namespace secret and per-CR config map.
func ApplySplunkConfig(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, spec enterpriseApi.CommonSplunkSpec, instanceType splcommon.InstanceType) (*corev1.Secret, error) {
	secret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, cr.GetNamespace())
	if err != nil {
		return nil, err
	}
	if err = splutil.SetSecretOwnerRef(ctx, client, secret.GetName(), cr); err != nil {
		return nil, err
	}
	if spec.Defaults != "" {
		defaults := resources.GetSplunkDefaults(cr.GetName(), cr.GetNamespace(), instanceType, spec.Defaults)
		defaults.SetOwnerReferences(append(defaults.GetOwnerReferences(), splcommon.AsOwner(cr, true)))
		if _, err = ApplyConfigMap(ctx, client, defaults); err != nil {
			return nil, err
		}
	}
	if err = reconcileCRSpecificConfigMap(ctx, client, cr); err != nil {
		return nil, err
	}
	return secret, nil
}

func reconcileCRSpecificConfigMap(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject) error {
	name := fmt.Sprintf("splunk-%s-%s-configmap", splcommon.KindToInstanceString(cr.GroupVersionKind().Kind), cr.GetName())
	key := types.NamespacedName{Namespace: cr.GetNamespace(), Name: name}
	cm, err := GetConfigMap(ctx, client, key)
	if k8serrors.IsNotFound(err) {
		cm = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cr.GetNamespace()}, Data: map[string]string{"manualUpdate": "off"}}
		cm.SetOwnerReferences(append(cm.GetOwnerReferences(), splcommon.AsOwner(cr, true)))
		return splutil.CreateResource(ctx, client, cm)
	}
	if err != nil {
		return err
	}
	if _, ok := cm.Data["manualUpdate"]; !ok {
		cm.Data["manualUpdate"] = "off"
		return splutil.UpdateResource(ctx, client, cm)
	}
	return nil
}

// DeleteOwnerReferencesForS3SecretObjects removes ownership from customer-managed credentials.
func DeleteOwnerReferencesForS3SecretObjects(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, smartstore *enterpriseApi.SmartStoreSpec) error {
	if !resources.IsSmartstoreConfigured(smartstore) {
		return nil
	}
	var err error
	for _, volume := range smartstore.VolList {
		if volume.SecretRef != "" && volume.SecretRef != splcommon.GetNamespaceScopedSecretName(cr.GetNamespace()) {
			_, err = splutil.RemoveSecretOwnerRef(ctx, client, volume.SecretRef, cr)
		}
	}
	return err
}

// DeleteOwnerReferencesForResources removes references that must not survive CR deletion.
func DeleteOwnerReferencesForResources(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, instanceType splcommon.InstanceType) error {
	if _, err := splutil.RemoveSecretOwnerRef(ctx, client, splcommon.GetNamespaceScopedSecretName(cr.GetNamespace()), cr); err != nil {
		return err
	}
	return RemoveUnwantedOwnerRefSs(ctx, client, types.NamespacedName{Namespace: cr.GetNamespace(), Name: splutil.GetSplunkStatefulsetName(instanceType, cr.GetName())}, cr)
}
