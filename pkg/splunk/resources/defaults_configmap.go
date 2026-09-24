// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package resources

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/splunk/splunk-operator/pkg/splunk/common"
)

// CRObject is the minimal interface a Kubernetes custom resource must satisfy
// for SOK resource construction. It is implemented by every controller-runtime
// client.Object, so callers can pass their CR directly.
//
// cr.Kind (TypeMeta) must be populated by the reconciler before calling any
// function that accepts CRObject, because the API server strips TypeMeta on
// decode. Reconcilers do this with: cr.Kind = "IndexerCluster".
type CRObject interface {
	metav1.Object
	runtime.Object
}

const (
	// LabelCRName is applied to SOK-managed resources to identify their owning CR.
	LabelCRName = "enterprise.splunk.com/target-cr-name"
	// LabelCRKind is applied to SOK-managed resources to identify their owning CR kind.
	LabelCRKind = "enterprise.splunk.com/target-cr-kind"

	sokDefaultsMountDir  = "/mnt/sok-defaults"
	sokDefaultsMountFile = "conf-defaults.yml"
	sokDefaultsVolName   = "mnt-sok-conf-defaults"
)

// DefaultsMountPath returns the path at which the SOK defaults ConfigMap is mounted inside the pod.
func DefaultsMountPath() string {
	return sokDefaultsMountDir + "/" + sokDefaultsMountFile
}

// DefaultsConfigMap is an immutable ConfigMap carrying a splunk-ansible defaults.yml,
// together with the knowledge of how to mount itself onto a Splunk StatefulSet.
type DefaultsConfigMap struct {
	corev1.ConfigMap
}

// NewDefaultsConfigMap builds a DefaultsConfigMap for the given CR, computing its
// content-addressed name from entries. owner, when non-nil, is set as an owner
// reference on the ConfigMap; pass splcommon.AsOwner(cr, true) from the reconciler.
func NewDefaultsConfigMap(cr CRObject, entries []common.ConfFileEntry, owner *metav1.OwnerReference) (DefaultsConfigMap, error) {
	namespace := cr.GetNamespace()
	crKind := cr.GetObjectKind().GroupVersionKind().Kind
	crName := cr.GetName()

	name, err := DefaultsConfigMapName(crKind, crName, entries)
	if err != nil {
		return DefaultsConfigMap{}, err
	}
	data, err := marshalDefaultYML(entries)
	if err != nil {
		return DefaultsConfigMap{}, fmt.Errorf("marshal defaults for %s/%s: %w", crKind, crName, err)
	}
	immutable := true
	meta := metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
		Labels: map[string]string{
			LabelCRName: crName,
			LabelCRKind: crKind,
		},
	}
	if owner != nil {
		meta.OwnerReferences = []metav1.OwnerReference{*owner}
	}
	return DefaultsConfigMap{
		ConfigMap: corev1.ConfigMap{
			ObjectMeta: meta,
			Immutable:  &immutable,
			Data: map[string]string{
				sokDefaultsMountFile: string(data),
			},
		},
	}, nil
}

// AsStatefulSetOption adapts this ConfigMap into a StatefulSetOption that mounts it into
// every container and appends its path to SPLUNK_DEFAULTS_URL. A zero-value DefaultsConfigMap
// (empty name) is a no-op.
func (cm DefaultsConfigMap) AsStatefulSetOption() StatefulSetOption {
	return func(ss *appsv1.StatefulSet) {
		if cm.Name == "" {
			return
		}
		ss.Spec.Template.Spec.Volumes = append(ss.Spec.Template.Spec.Volumes, defaultsVolume(cm.Name))
		for i := range ss.Spec.Template.Spec.Containers {
			ss.Spec.Template.Spec.Containers[i].VolumeMounts = append(ss.Spec.Template.Spec.Containers[i].VolumeMounts, defaultsVolumeMount())
			ss.Spec.Template.Spec.Containers[i].Env = appendDefaultsURL(ss.Spec.Template.Spec.Containers[i].Env, DefaultsMountPath())
		}
	}
}

// DefaultsConfigMapName returns the content-addressed name for a defaults ConfigMap.
// The name embeds the first 6 hex characters of the SHA-256 of the serialized entries,
// giving a stable, change-sensitive identifier.
func DefaultsConfigMapName(crKind, crName string, entries []common.ConfFileEntry) (string, error) {
	data, err := marshalDefaultYML(entries)
	if err != nil {
		return "", fmt.Errorf("marshal defaults for %s/%s: %w", crKind, crName, err)
	}
	sum := sha256.Sum256(append([]byte(crKind+crName+"\x00"), data...))
	hash := fmt.Sprintf("%x", sum[:3]) // 3 bytes → 6 hex chars
	return fmt.Sprintf("sok-%s-defaults-%s", strings.ToLower(crKind), hash), nil
}

// marshalDefaultYML serializes entries into the splunk-ansible defaults.yml YAML bytes.
func marshalDefaultYML(entries []common.ConfFileEntry) ([]byte, error) {
	d := common.DefaultYML{
		Splunk: common.SplunkDefault{Conf: entries},
	}
	return yaml.Marshal(d)
}

func defaultsVolume(cmName string) corev1.Volume {
	mode := int32(420)
	return corev1.Volume{
		Name: sokDefaultsVolName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: cmName},
				DefaultMode:          &mode,
			},
		},
	}
}

func defaultsVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      sokDefaultsVolName,
		MountPath: sokDefaultsMountDir,
		ReadOnly:  true,
	}
}

// appendDefaultsURL appends mountPath to the SPLUNK_DEFAULTS_URL env var.
// Because splunk-ansible processes defaultsUrl entries left-to-right with later entries winning,
// appending last guarantees SOK settings override any cluster-level or image-baked defaults.
// If SPLUNK_DEFAULTS_URL is absent it is added with mountPath as its sole value.
func appendDefaultsURL(env []corev1.EnvVar, mountPath string) []corev1.EnvVar {
	for i := range env {
		if env[i].Name == "SPLUNK_DEFAULTS_URL" {
			env[i].Value = env[i].Value + "," + mountPath
			return env
		}
	}
	return append(env, corev1.EnvVar{Name: "SPLUNK_DEFAULTS_URL", Value: mountPath})
}
