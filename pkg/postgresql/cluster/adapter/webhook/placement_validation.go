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

package webhook

import (
	"strings"

	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metavalidation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// Match the toleration rules enforced by the pinned CNPG v1.30.1 webhook,
// including omitted operators (Equal) and negative NoExecute toleration seconds.
func validateClassPlacement(config *platformApi.CNPGConfig) field.ErrorList {
	if config == nil || config.Affinity == nil {
		return nil
	}
	affinity := config.Affinity
	path := field.NewPath("spec", "cnpg", "affinity")
	errs := metavalidation.ValidateLabels(affinity.NodeSelector, path.Child("nodeSelector"))
	if affinity.TopologyKey != "" {
		if reasons := validation.IsQualifiedName(affinity.TopologyKey); len(reasons) > 0 {
			errs = append(errs, field.Invalid(path.Child("topologyKey"), affinity.TopologyKey, strings.Join(reasons, "; ")))
		}
	}
	for i, toleration := range affinity.Tolerations {
		p := path.Child("tolerations").Index(i)
		if toleration.Key != "" {
			if reasons := validation.IsQualifiedName(toleration.Key); len(reasons) > 0 {
				errs = append(errs, field.Invalid(p.Child("key"), toleration.Key, strings.Join(reasons, "; ")))
			}
		} else if toleration.Operator != corev1.TolerationOpExists {
			errs = append(errs, field.Invalid(p.Child("operator"), toleration.Operator, "must be Exists when key is empty"))
		}
		switch toleration.Operator {
		case "", corev1.TolerationOpEqual:
			if reasons := validation.IsValidLabelValue(toleration.Value); len(reasons) > 0 {
				errs = append(errs, field.Invalid(p.Child("value"), toleration.Value, strings.Join(reasons, "; ")))
			}
		case corev1.TolerationOpExists:
			if toleration.Value != "" {
				errs = append(errs, field.Invalid(p.Child("value"), toleration.Value, "must be empty when operator is Exists"))
			}
		default:
			errs = append(errs, field.NotSupported(p.Child("operator"), toleration.Operator, []string{"Equal", "Exists"}))
		}
		switch toleration.Effect {
		case "", corev1.TaintEffectNoSchedule, corev1.TaintEffectPreferNoSchedule, corev1.TaintEffectNoExecute:
		default:
			errs = append(errs, field.NotSupported(p.Child("effect"), toleration.Effect, []string{"NoSchedule", "PreferNoSchedule", "NoExecute"}))
		}
		if toleration.TolerationSeconds != nil && toleration.Effect != corev1.TaintEffectNoExecute {
			errs = append(errs, field.Invalid(p.Child("effect"), toleration.Effect, "must be NoExecute when tolerationSeconds is set"))
		}
	}
	return errs
}
