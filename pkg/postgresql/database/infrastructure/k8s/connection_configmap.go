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

package k8s

import (
	"context"
	"maps"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const retainedFromAnnotation = "platform.splunk.com/retained-from"

// DesiredConnectionConfigMap contains the fields owned by connection metadata.
type DesiredConnectionConfigMap struct {
	Name      string
	Namespace string
	Labels    map[string]string
	Data      map[string]string
}

// ApplyConnectionConfigMap converges the Kubernetes object while preserving
// the established adoption and unrelated-metadata behavior.
func ApplyConnectionConfigMap(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	owner client.Object,
	desired DesiredConnectionConfigMap,
) (bool, error) {
	reAdopted := false
	configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: desired.Name, Namespace: desired.Namespace, Labels: maps.Clone(desired.Labels),
	}}
	_, err := controllerutil.CreateOrUpdate(ctx, c, configMap, func() error {
		configMap.Data = maps.Clone(desired.Data)
		reAdopted = configMap.Annotations[retainedFromAnnotation] == owner.GetName()
		if reAdopted {
			delete(configMap.Annotations, retainedFromAnnotation)
		}
		if !metav1.IsControlledBy(configMap, owner) {
			return controllerutil.SetControllerReference(owner, configMap, scheme)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return reAdopted, nil
}
