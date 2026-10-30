// Copyright (c) 2026 Splunk Inc. All rights reserved.
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

package resources

import (
	"cmp"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	PostgresDatabaseMountRoot  = "/opt/splunk/var/run/supervisor/postgresok"
	PostgresDatabaseVolumeName = common.PostgresDatabaseVolumeName
)

// PostgresDatabaseMount contains only references to producer-managed resources.
type PostgresDatabaseMount struct {
	Database      string
	ConfigMapName string
	AdminPassword *corev1.SecretKeySelector
	RWPassword    corev1.SecretKeySelector
	CASecretName  string
}

// PostgresDatabaseBundle projects existing resources without creating, copying,
// or taking ownership of any ConfigMap, Secret, or certificate.
type PostgresDatabaseBundle []PostgresDatabaseMount

// Inject mounts raw ConfigMap values as <database>/<key> files and passwords
// and CA as <database>-admin, <database>-rw and <database>-ca.crt. The whole
// projection is validated before the pod template is changed.
func (bundle PostgresDatabaseBundle) Inject(pod *corev1.PodTemplateSpec) error {
	if len(bundle) == 0 {
		return nil
	}
	index, root, err := postgresDatabaseTarget(pod)
	if err != nil {
		return err
	}
	if err := validatePostgresDatabaseMountSpace(pod, index, root); err != nil {
		return err
	}
	initializer, err := postgresDatabaseRuntimeInitializer(pod, index, root)
	if err != nil {
		return err
	}
	mounts := slices.Clone(bundle)
	slices.SortFunc(mounts, func(a, b PostgresDatabaseMount) int { return cmp.Compare(a.Database, b.Database) })
	var sources []corev1.VolumeProjection
	for _, mount := range mounts {
		metadata := &corev1.ConfigMapProjection{Name: mount.ConfigMapName}
		for _, key := range []string{"CLUSTER_RW_ENDPOINT", "DEFAULT_CLUSTER_PORT", "DATABASE_NAME", "ADMIN_USER_NAME", "RW_USER_NAME"} {
			metadata.Items = append(metadata.Items, corev1.KeyToPath{Key: key, Path: mount.Database + "/" + key})
		}
		sources = append(sources,
			corev1.VolumeProjection{ConfigMap: metadata},
			corev1.VolumeProjection{Secret: &corev1.SecretProjection{
				Name:  mount.RWPassword.Name,
				Items: []corev1.KeyToPath{{Key: mount.RWPassword.Key, Path: mount.Database + "-rw"}},
			}},
			corev1.VolumeProjection{Secret: &corev1.SecretProjection{
				Name:  mount.CASecretName,
				Items: []corev1.KeyToPath{{Key: "ca.crt", Path: mount.Database + "-ca.crt"}},
			}},
		)
		if mount.AdminPassword != nil {
			sources = append(sources, corev1.VolumeProjection{Secret: &corev1.SecretProjection{
				Name:  mount.AdminPassword.Name,
				Items: []corev1.KeyToPath{{Key: mount.AdminPassword.Key, Path: mount.Database + "-admin"}},
			}})
		}
	}
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, initializer)
	pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
		Name:      PostgresDatabaseVolumeName,
		Projected: &corev1.ProjectedVolumeSource{DefaultMode: new(int32(0440)), Sources: sources},
	})
	pod.Spec.Containers[index].VolumeMounts = append(pod.Spec.Containers[index].VolumeMounts, corev1.VolumeMount{
		Name: PostgresDatabaseVolumeName, MountPath: root, ReadOnly: true,
	})
	return nil
}

// Prepare parents on the existing writable volume before the runtime creates
// the main container's nested read-only mount. The initializer never mounts
// producer resources and inherits the pod's non-root user unless overridden by
// the Splunk container. Existing inaccessible directories are not chmod/chowned.
func postgresDatabaseRuntimeInitializer(pod *corev1.PodTemplateSpec, index int, root string) (corev1.Container, error) {
	container := &pod.Spec.Containers[index]
	parent := path.Dir(root)
	var backing *corev1.VolumeMount
	for i := range container.VolumeMounts {
		mount := &container.VolumeMounts[i]
		if !mount.ReadOnly && strings.HasPrefix(parent, strings.TrimSuffix(mount.MountPath, "/")+"/") &&
			(backing == nil || len(mount.MountPath) > len(backing.MountPath)) {
			backing = mount
		}
	}
	if backing == nil {
		return corev1.Container{}, fmt.Errorf("PostgresDatabase runtime directory %q requires a writable parent volume", parent)
	}
	security := &corev1.SecurityContext{
		RunAsNonRoot: new(true), AllowPrivilegeEscalation: new(false),
		Capabilities:   &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
	if container.SecurityContext != nil {
		containerSecurity := container.SecurityContext.DeepCopy()
		security.RunAsUser = containerSecurity.RunAsUser
		security.RunAsGroup = containerSecurity.RunAsGroup
	}
	return corev1.Container{
		Name: "sok-postgresok-init", Image: container.Image, ImagePullPolicy: container.ImagePullPolicy,
		Command:      []string{"/bin/sh", "-ec"},
		Args:         []string{`umask 077; mkdir -p -- "$1"; test -w "$1" || { echo "PostgresDatabase runtime directory must be writable by the Splunk user" >&2; exit 1; }`, "postgresok-init", parent},
		VolumeMounts: []corev1.VolumeMount{*backing.DeepCopy()}, SecurityContext: security,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("128Mi")},
		},
	}, nil
}

func postgresDatabaseTarget(pod *corev1.PodTemplateSpec) (int, string, error) {
	index := -1
	root := PostgresDatabaseMountRoot
	for i, container := range pod.Spec.Containers {
		if container.Name != "splunk" {
			continue
		}
		index = i
		for _, env := range container.Env {
			if env.Name != "SPLUNK_HOME" {
				continue
			}
			if env.ValueFrom != nil || !path.IsAbs(env.Value) || path.Clean(env.Value) != env.Value || strings.Contains(env.Value, "$") {
				return -1, "", fmt.Errorf("PostgresDatabase bundles require a literal absolute SPLUNK_HOME")
			}
			root = path.Join(env.Value, "var/run/supervisor/postgresok")
		}
		break
	}
	if index < 0 {
		return -1, "", fmt.Errorf("PostgresDatabase bundles require a Splunk container")
	}
	return index, root, nil
}

func validatePostgresDatabaseMountSpace(pod *corev1.PodTemplateSpec, index int, root string) error {
	for _, init := range pod.Spec.InitContainers {
		if init.Name == "sok-postgresok-init" {
			return fmt.Errorf("duplicate PostgresDatabase runtime initializer %q", init.Name)
		}
	}
	for _, mount := range pod.Spec.Containers[index].VolumeMounts {
		if mount.MountPath == root || strings.HasPrefix(mount.MountPath, root+"/") {
			return fmt.Errorf("mount path %q conflicts with PostgresDatabase bundles", mount.MountPath)
		}
	}
	for _, volume := range pod.Spec.Volumes {
		if volume.Name == PostgresDatabaseVolumeName {
			return fmt.Errorf("duplicate volume %q for PostgresDatabase bundle", volume.Name)
		}
	}
	return nil
}

// PreservePostgresDatabaseMounts copies only the last resolved projection for
// the same requested bindings and mount location. It never restores a plan for
// a different database or admin-permission setting.
func PreservePostgresDatabaseMounts(previous, desired *corev1.PodTemplateSpec, binding string) (bool, error) {
	if previous.Annotations[common.PostgresDatabaseBindingAnnotation] != binding {
		return false, nil
	}
	index, root, err := postgresDatabaseTarget(desired)
	if err != nil {
		return false, err
	}
	if err := validatePostgresDatabaseMountSpace(desired, index, root); err != nil {
		return false, err
	}
	var volume *corev1.Volume
	for i := range previous.Spec.Volumes {
		if previous.Spec.Volumes[i].Name == PostgresDatabaseVolumeName && previous.Spec.Volumes[i].Projected != nil {
			volume = &previous.Spec.Volumes[i]
			break
		}
	}
	if volume == nil {
		return false, nil
	}
	for _, container := range previous.Spec.Containers {
		if container.Name != "splunk" {
			continue
		}
		for _, mount := range container.VolumeMounts {
			if mount.Name == PostgresDatabaseVolumeName && mount.MountPath == root && mount.ReadOnly && mount.SubPath == "" && mount.SubPathExpr == "" {
				initializer, err := postgresDatabaseRuntimeInitializer(desired, index, root)
				if err != nil {
					return false, err
				}
				desired.Spec.InitContainers = append(desired.Spec.InitContainers, initializer)
				desired.Spec.Volumes = append(desired.Spec.Volumes, *volume.DeepCopy())
				desired.Spec.Containers[index].VolumeMounts = append(desired.Spec.Containers[index].VolumeMounts, *mount.DeepCopy())
				return true, nil
			}
		}
	}
	return false, nil
}
