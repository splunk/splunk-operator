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

package enterprise

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	noahAuthSecretKey          = "pass4SymmKey"
	noahAuthVolumeName         = "mnt-noah-auth"
	noahAuthMountPath          = "/mnt/noah-auth"
	noahAuthRevisionAnnotation = "enterprise.splunk.com/noah-auth-secret-revision"
)

// ApplyNoahIndexerCluster reconciles the Kubernetes resources required to
// start a Noah-selected IndexerCluster. It intentionally does not implement
// Noah membership, readiness, rollout, or safe scale-down.
func ApplyNoahIndexerCluster(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (result reconcile.Result, err error) {
	result = reconcile.Result{RequeueAfter: 5 * time.Second}

	eventPublisher := GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "IndexerCluster"

	isPaused := cr.GetAnnotations()[enterpriseApi.IndexerClusterPausedAnnotation] == "true"
	setPhaseAndConditions := func(phase enterpriseApi.Phase, message string) {
		status := splcommon.SetPhaseAndConditions(cr.Status.Conditions, splcommon.PhaseConditionInput{
			Phase:      phase,
			IsPaused:   isPaused,
			Message:    message,
			Generation: cr.GetGeneration(),
		})
		cr.Status.Phase = status.Phase
		cr.Status.Conditions = status.Conditions
		cr.Status.ObservedGeneration = cr.GetGeneration()
	}
	setPhaseAndConditions(enterpriseApi.PhaseError, "")
	defer updateCRStatus(ctx, client, cr, &err)

	if cr.Spec.NoahClusterRef == nil || cr.Spec.NoahClusterRef.Name == "" {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Noah Cluster reference is required")
		return reconcile.Result{}, splcommon.NewTerminalError(
			EventReasonValidateSpecFailed,
			"Noah IndexerCluster spec validation failed",
			fmt.Errorf("noahClusterRef.name must not be empty"),
		)
	}

	if cr.Spec.Replicas == 0 {
		cr.Spec.Replicas = 1
	}
	if err = validateCommonSplunkSpec(ctx, client, &cr.Spec.CommonSplunkSpec, cr); err != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Indexer Cluster spec validation failed")
		return reconcile.Result{}, splcommon.NewTerminalError(
			EventReasonValidateSpecFailed,
			"Noah IndexerCluster spec validation failed",
			err,
		)
	}

	cr.Status.Replicas = cr.Spec.Replicas
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-indexer", cr.GetName())

	if cr.GetDeletionTimestamp() != nil {
		if cleanupErr := DeleteOwnerReferencesForResources(ctx, client, cr, SplunkIndexer); cleanupErr != nil {
			setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Failed to clean up owned resources")
			return result, cleanupErr
		}
		terminating, deletionErr := k8sops.CheckForDeletion(ctx, cr, client)
		if terminating && deletionErr != nil {
			setPhaseAndConditions(enterpriseApi.PhaseTerminating, "Resource deletion is in progress")
		} else {
			result.RequeueAfter = 0
		}
		return result, deletionErr
	}

	statefulSet, phase, applyErr := applyNoahIndexerResources(ctx, client, cr)
	if applyErr != nil {
		setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to apply Noah IndexerCluster resources")
		return result, applyErr
	}

	cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	setPhaseAndConditions(phase, "")
	if phase == enterpriseApi.PhaseReady {
		result.RequeueAfter = 0
	}
	return result, nil
}

func applyNoahIndexerResources(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (*appsv1.StatefulSet, enterpriseApi.Phase, error) {
	noahCluster := &enterpriseApi.NoahCluster{}
	noahClusterKey := types.NamespacedName{
		Name:      cr.Spec.NoahClusterRef.Name,
		Namespace: cr.Namespace,
	}
	if err := client.Get(ctx, noahClusterKey, noahCluster); err != nil {
		return nil, enterpriseApi.PhaseError, fmt.Errorf("get referenced NoahCluster %s: %w", noahClusterKey, err)
	}
	noahAuthSecret, err := resolveNoahAuthSecret(ctx, client, cr.Namespace, noahCluster.Spec.AuthSecretRef)
	if err != nil {
		return nil, enterpriseApi.PhaseError, err
	}

	if _, err := ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkIndexer); err != nil {
		return nil, enterpriseApi.PhaseError, fmt.Errorf("apply Splunk config: %w", err)
	}

	services := []struct {
		headless bool
		name     string
	}{
		{headless: true, name: "headless"},
		{headless: false, name: "regular"},
	}
	for _, service := range services {
		if err := k8sops.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, service.headless)); err != nil {
			return nil, enterpriseApi.PhaseError, fmt.Errorf("apply %s Service: %w", service.name, err)
		}
	}

	noahConf := splunkconfig.NoahIndexerConf(noahCluster.Spec.Endpoint, noahCluster.Spec.Tenant)
	defaultsConfigMap, defaultsSecret, err := ensureIndexerDefaults(ctx, client, cr, noahConf...)
	if err != nil {
		return nil, enterpriseApi.PhaseError, fmt.Errorf("ensure indexer defaults: %w", err)
	}

	statefulSet, err := getNoahIndexerStatefulSet(ctx, client, cr,
		defaultsConfigMap.AsStatefulSetOption(),
		defaultsSecret.AsStatefulSetOption(),
		noahAuthSecretOption(noahAuthSecret),
	)
	if err != nil {
		return nil, enterpriseApi.PhaseError, fmt.Errorf("build Noah indexer StatefulSet: %w", err)
	}

	phase, err := k8sops.ApplyStatefulSet(ctx, client, statefulSet)
	if err != nil {
		return statefulSet, enterpriseApi.PhaseError, fmt.Errorf("apply Noah indexer StatefulSet: %w", err)
	}
	// Applying the object is enough for this scaffold. Do not invoke the generic
	// pod manager: it would also authorize rollout and destructive scale-down
	// before Noah lifecycle safety exists. A StatefulSet using OnDelete may have
	// ready pods from its previous revision, so replica readiness alone cannot
	// prove that the desired pod template is running.
	if phase == enterpriseApi.PhaseReady && !noahIndexerStatefulSetConverged(statefulSet, cr.Spec.Replicas) {
		phase = enterpriseApi.PhaseUpdating
	}

	configworkflow.GarbageCollectConfigMaps(ctx, client, cr, defaultsConfigMap.Name, statefulSet.Spec.Selector)
	configworkflow.GarbageCollectSecrets(ctx, client, cr, defaultsSecret.Name, statefulSet.Spec.Selector)
	return statefulSet, phase, nil
}

// noahIndexerStatefulSetConverged reports whether the StatefulSet controller
// has observed the latest template and every desired pod is running that
// revision. It deliberately does not initiate a rollout.
func noahIndexerStatefulSetConverged(statefulSet *appsv1.StatefulSet, desiredReplicas int32) bool {
	return statefulSet != nil &&
		statefulSet.Status.ObservedGeneration >= statefulSet.Generation &&
		statefulSet.Status.UpdateRevision != "" &&
		statefulSet.Status.CurrentRevision == statefulSet.Status.UpdateRevision &&
		statefulSet.Status.UpdatedReplicas == desiredReplicas &&
		statefulSet.Status.ReadyReplicas == desiredReplicas
}

// getNoahIndexerStatefulSet constructs an indexer StatefulSet with the startup
// staging and stable pod identity required by a Noah-aware Splunk image.
func getNoahIndexerStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster, opts ...resources.StatefulSetOption) (*appsv1.StatefulSet, error) {
	bootstrapOptions := make([]resources.StatefulSetOption, 0, len(opts)+1)
	bootstrapOptions = append(bootstrapOptions, noahInitEtcOption(&cr.Spec.CommonSplunkSpec))
	bootstrapOptions = append(bootstrapOptions, opts...)
	return getIndexerStatefulSet(ctx, client, cr, noahIndexerStatefulSetOptions(os.Getenv(resources.ClusterDomainEnvName), bootstrapOptions...)...)
}

func noahIndexerStatefulSetOptions(clusterDomain string, opts ...resources.StatefulSetOption) []resources.StatefulSetOption {
	result := make([]resources.StatefulSetOption, 0, len(opts)+1)
	result = append(result, opts...)
	return append(result, resources.WithNoahPodIdentity(clusterDomain))
}

// resolveNoahAuthSecret returns the same-namespace credential that must be
// staged before splunk-provision runs its Noah pre-start hook.
func resolveNoahAuthSecret(ctx context.Context, client splcommon.ControllerClient, namespace string, ref corev1.LocalObjectReference) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: namespace, Name: ref.Name}
	if err := client.Get(ctx, key, secret); err != nil {
		return nil, fmt.Errorf("get Noah auth Secret %s: %w", key, err)
	}
	value, found := secret.Data[noahAuthSecretKey]
	if !found {
		return nil, fmt.Errorf("Noah auth Secret %s is missing data.%s", key, noahAuthSecretKey)
	}
	if err := splutil.ValidateSecret(value); err != nil {
		return nil, fmt.Errorf("Noah auth Secret %s has invalid data.%s: %w", key, noahAuthSecretKey, err)
	}
	if strings.ContainsAny(string(value), "\r\n") {
		return nil, fmt.Errorf("Noah auth Secret %s data.%s must be a single line", key, noahAuthSecretKey)
	}
	return secret, nil
}

// noahAuthSecretOption exposes the plaintext credential only to init-etc. The
// main Splunk container receives it through the staged server.conf on its etc
// volume, not through an environment variable or Secret mount.
func noahAuthSecretOption(secret *corev1.Secret) resources.StatefulSetOption {
	return func(statefulSet *appsv1.StatefulSet) {
		mode := int32(0444)
		statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: noahAuthVolumeName,
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
				SecretName:  secret.Name,
				Items:       []corev1.KeyToPath{{Key: noahAuthSecretKey, Path: noahAuthSecretKey}},
				DefaultMode: &mode,
			}},
		})
		if statefulSet.Spec.Template.Annotations == nil {
			statefulSet.Spec.Template.Annotations = make(map[string]string)
		}
		statefulSet.Spec.Template.Annotations[noahAuthRevisionAnnotation] = secret.ResourceVersion
		for i := range statefulSet.Spec.Template.Spec.InitContainers {
			initContainer := &statefulSet.Spec.Template.Spec.InitContainers[i]
			if initContainer.Name != "init-etc" {
				continue
			}
			initContainer.VolumeMounts = append(initContainer.VolumeMounts, corev1.VolumeMount{
				Name:      noahAuthVolumeName,
				MountPath: noahAuthMountPath,
				ReadOnly:  true,
			})
		}
	}
}

// noahInitEtcOption stages the minimum safe Noah configuration required by
// splunk-provision before it applies SPLUNK_DEFAULTS_URL. Noah remains disabled
// during the provisioner's temporary startup and is enabled from the generated
// defaults before the first full splunkd start.
func noahInitEtcOption(spec *enterpriseApi.CommonSplunkSpec) resources.StatefulSetOption {
	// TODO: Remove this operator-owned etc mutation once splunk-provision can
	// stage its Noah pre-start configuration from the resolved defaults. Writing
	// application configuration into the image-owned etc tree belongs in the
	// provisioner, not the operator's StatefulSet construction.
	return func(statefulSet *appsv1.StatefulSet) {
		etcVolumeName := "pvc-etc"
		if len(statefulSet.Spec.Template.Spec.Containers) > 0 {
			for _, mount := range statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts {
				if mount.MountPath == "/opt/splunk/etc" {
					etcVolumeName = mount.Name
					break
				}
			}
		}

		initRunAsUser := int64(41812)
		initRunAsNonRoot := true
		initAllowPrivilegeEscalation := false
		statefulSet.Spec.Template.Spec.InitContainers = append(statefulSet.Spec.Template.Spec.InitContainers, corev1.Container{
			Name:            "init-etc",
			Image:           spec.Image,
			ImagePullPolicy: corev1.PullPolicy(spec.ImagePullPolicy),
			Command: []string{
				"sh", "-c",
				`set -eu
if [ ! -f /mnt/splunk-etc/log.cfg ]; then
  cp --remove-destination -R /opt/splunk/etc/. /mnt/splunk-etc/
  printf '[default]\nSPLUNK_HOME=/opt/splunk\nSPLUNK_DB=/opt/splunk/var/lib/splunk\nPYTHONUTF8=1\n' \
    > /mnt/splunk-etc/splunk-launch.conf
fi
server_conf=/mnt/splunk-etc/system/local/server.conf
mkdir -p "$(dirname "$server_conf")"
touch "$server_conf"
awk -v key_file=/mnt/noah-auth/pass4SymmKey '
  function print_key( key) {
    if ((getline key < key_file) <= 0) exit 42
    close(key_file)
    printf "pass4SymmKey = %s\n", key
  }
  /^\[noahService\][[:space:]]*$/ {
    in_noah=1
    saw_noah=1
    print
    print_key()
    print "disabled = true"
    next
  }
  in_noah && /^[[:space:]]*pass4SymmKey[[:space:]]*=/ { next }
  in_noah && /^[[:space:]]*disabled[[:space:]]*=/ { next }
  /^\[/ { in_noah=0 }
  { print }
  END {
    if (!saw_noah) {
      print ""
      print "[noahService]"
      print "disabled = true"
      print_key()
    }
  }
' "$server_conf" > /tmp/server_clean.conf
mv /tmp/server_clean.conf "$server_conf"`,
			},
			SecurityContext: &corev1.SecurityContext{
				RunAsUser:                &initRunAsUser,
				RunAsNonRoot:             &initRunAsNonRoot,
				AllowPrivilegeEscalation: &initAllowPrivilegeEscalation,
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{"ALL"},
				},
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			VolumeMounts: []corev1.VolumeMount{{
				Name:      etcVolumeName,
				MountPath: "/mnt/splunk-etc",
			}},
		})
	}
}
