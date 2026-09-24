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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/splunk/splunk-operator/pkg/splunk/common"
)

const (
	sokSecretMountDir  = "/mnt/sok-conf-secrets"
	sokSecretMountFile = "conf-defaults.yml"
	sokSecretVolName   = "mnt-sok-conf-secret"
)

// SecretMountPath returns the path at which the SOK defaults Secret is mounted inside the pod.
func SecretMountPath() string {
	return sokSecretMountDir + "/" + sokSecretMountFile
}

// DefaultsSecret is an immutable Secret carrying a splunk-ansible defaults.yml that holds
// only sensitive SmartBus credentials (access_key / secret_key), together with the knowledge
// of how to mount itself onto a Splunk StatefulSet.
//
// It is a credential-only analog of DefaultsConfigMap: the structural SmartBus config lives in
// a ConfigMap, while credentials live here so they are never written into a ConfigMap. Both are
// mounted and joined into SPLUNK_DEFAULTS_URL; because each renders its stanza into a distinct
// app directory, Splunk's btool layering unions the disjoint keys at runtime.
type DefaultsSecret struct {
	corev1.Secret
}

// NewDefaultsSecret builds a DefaultsSecret for the given CR, computing its
// content-addressed name from entries. owner, when non-nil, is set as an owner
// reference on the Secret; pass splcommon.AsOwner(cr, true) from the reconciler.
func NewDefaultsSecret(cr CRObject, entries []common.ConfFileEntry, owner *metav1.OwnerReference) (DefaultsSecret, error) {
	namespace := cr.GetNamespace()
	crKind := cr.GetObjectKind().GroupVersionKind().Kind
	crName := cr.GetName()

	name, err := DefaultsSecretName(crKind, crName, entries)
	if err != nil {
		return DefaultsSecret{}, err
	}
	data, err := marshalDefaultYML(entries)
	if err != nil {
		return DefaultsSecret{}, fmt.Errorf("marshal defaults for %s/%s: %w", crKind, crName, err)
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
	return DefaultsSecret{
		Secret: corev1.Secret{
			ObjectMeta: meta,
			Immutable:  &immutable,
			Data: map[string][]byte{
				sokSecretMountFile: data,
			},
		},
	}, nil
}

// AsStatefulSetOption adapts this Secret into a StatefulSetOption that mounts it into
// every container and appends its path to SPLUNK_DEFAULTS_URL. A zero-value DefaultsSecret
// (empty name) is a no-op.
func (s DefaultsSecret) AsStatefulSetOption() StatefulSetOption {
	return func(ss *appsv1.StatefulSet) {
		if s.Name == "" {
			return
		}
		ss.Spec.Template.Spec.Volumes = append(ss.Spec.Template.Spec.Volumes, secretVolume(s.Name))
		for i := range ss.Spec.Template.Spec.Containers {
			ss.Spec.Template.Spec.Containers[i].VolumeMounts = append(ss.Spec.Template.Spec.Containers[i].VolumeMounts, secretVolumeMount())
			ss.Spec.Template.Spec.Containers[i].Env = appendDefaultsURL(ss.Spec.Template.Spec.Containers[i].Env, SecretMountPath())
		}
	}
}

// DefaultsSecretName returns the content-addressed name for a credentials Secret.
// The name embeds the first 6 hex characters of the SHA-256 of the serialized entries,
// giving a stable, change-sensitive identifier.
func DefaultsSecretName(crKind, crName string, entries []common.ConfFileEntry) (string, error) {
	data, err := marshalDefaultYML(entries)
	if err != nil {
		return "", fmt.Errorf("marshal defaults for %s/%s: %w", crKind, crName, err)
	}
	sum := sha256.Sum256(append([]byte(crKind+crName+"\x00"), data...))
	hash := fmt.Sprintf("%x", sum[:3]) // 3 bytes → 6 hex chars
	return fmt.Sprintf("sok-%s-creds-%s", strings.ToLower(crKind), hash), nil
}

func secretVolume(secretName string) corev1.Volume {
	mode := int32(420)
	return corev1.Volume{
		Name: sokSecretVolName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  secretName,
				DefaultMode: &mode,
			},
		},
	}
}

func secretVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      sokSecretVolName,
		MountPath: sokSecretMountDir,
		ReadOnly:  true,
	}
}
