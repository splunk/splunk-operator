package client

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
)

// addCertManagerSidecar adds a sidecar container that monitors the cert-manager-injected certificates
// and triggers Splunk to reload or restart when the cert changes.
func AddCertManagerSidecar(podTemplateSpec *corev1.PodTemplateSpec) {
	foundVol := false
	for i := range podTemplateSpec.Spec.Volumes {
		if podTemplateSpec.Spec.Volumes[i].Name == "splunk-bin" {
			foundVol = true
			break
		}
	}
	if !foundVol {
		podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, corev1.Volume{
			Name: "splunk-bin",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

	// 2) Construct a sidecar container
	sidecar := corev1.Container{
		Name:    "cert-watch-sidecar",
		Image:   "pstauffer/inotify:stable",
		Command: []string{"bash", "-c"},
		Args: []string{
			`
            #!/usr/bin/env bash
            echo "cert-watch-sidecar started."
            CERT_DIR="/opt/splunk/etc/auth/certs"
            while true; do
              inotifywait -e modify,create,delete,close_write ${CERT_DIR}
              echo "Certificate change detected. Restarting Splunk..."
              /opt/splunk/bin/splunk restart
            done
            `,
		},
		VolumeMounts: []corev1.VolumeMount{
			// This volume must exist if certificates are in /opt/splunk/etc/auth/certs
			{
				Name:      "splunk-certs",
				MountPath: "/opt/splunk/etc/auth/certs",
				ReadOnly:  true,
			},
			{
				Name:      "splunk-bin",
				MountPath: "/opt/splunk/bin",
			},
		},
		Resources: corev1.ResourceRequirements{},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  pointer.Int64(41812),
			RunAsGroup: pointer.Int64(41812),
		},
	}

	// 3) Append the sidecar container to the Pod spec
	podTemplateSpec.Spec.Containers = append(podTemplateSpec.Spec.Containers, sidecar)
}

const SplunkCertMountPath = "/mnt/splunk/certificates"

// addCertManagerCsiVolume configures the PodTemplateSpec for cert-manager's CSI driver.
// It adds a volume named "splunk-certs" that references "csi.cert-manager.io" and
// also ensures 'readOnly: true' is set. We also add a VolumeMount to the Splunk container.
func AddCertManagerCsiVolume(
    podTemplateSpec *corev1.PodTemplateSpec,
    annotations map[string]string,
) {
    // Extract desired secretName, issuerName, issuerKind from CR annotations
    secretName := annotations["splunk.com/cert-secret-name"]
    if secretName == "" {
        secretName = "splunk-cert-secret"
    }

    issuerName := annotations["splunk.com/cert-issuer-name"]
    issuerKind := annotations["splunk.com/cert-issuer-kind"]

    // Optionally set Pod annotations recognized by the cert-manager CSI driver
    if podTemplateSpec.ObjectMeta.Annotations == nil {
        podTemplateSpec.ObjectMeta.Annotations = make(map[string]string)
    }
    podTemplateSpec.ObjectMeta.Annotations["csi.cert-manager.io/secret-name"] = secretName
    //podTemplateSpec.ObjectMeta.Annotations["cert-manager.io/private-key-secret-name"] = secretName
    if issuerName != "" {
        podTemplateSpec.ObjectMeta.Annotations["csi.cert-manager.io/issuer-name"] = issuerName
    }
    if issuerKind != "" {
        podTemplateSpec.ObjectMeta.Annotations["csi.cert-manager.io/issuer-kind"] = issuerKind
    }
    // optionally:
    // podTemplateSpec.ObjectMeta.Annotations["csi.cert-manager.io/issuer-group"] = "cert-manager.io"

    // Create the volume referencing the CSI driver
    csiVol := corev1.Volume{
        Name: "splunk-certs",
        VolumeSource: corev1.VolumeSource{
            CSI: &corev1.CSIVolumeSource{
                Driver:           "csi.cert-manager.io",
                ReadOnly:         pointer.Bool(true), // REQUIRED: must be 'true' for cert-manager CSI
                VolumeAttributes: map[string]string{
                    "csi.cert-manager.io/secret-name":  secretName,
                    "csi.cert-manager.io/issuer-name":  issuerName,
                    "csi.cert-manager.io/issuer-kind":  issuerKind,
                    "csi.cert-manager.io/reuse-private-key": "false",
                    "cert-manager.io/private-key-secret-name": secretName,
                    //"csi.cert-manager.io/issuer-name": "local-ca-issuer",
                    //"csi.cert-manager.io/issuer-kind": "Issuer"
                    //"csi.cert-manager.io/common-name": "cert.splunk.com"
                    //"csi.cert-manager.io/dns-names": "cert.splunk.com"
                    //"csi.cert-manager.io/secret-name": "splunk-cert-secret"
                    //"csi.cert-manager.io/key-usages": "digital signature,key encipherment,server auth"
                    //"csi.cert-manager.io/duration": "720h"
                },
            },
        },
    }

    // Add the new volume to the Pod spec
    podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, csiVol)

    // Also ensure we mount this volume in the Splunk container at the correct path, read-only
    for i := range podTemplateSpec.Spec.Containers {
        // If needed, filter for the Splunk container by name (e.g. "splunk")
        if podTemplateSpec.Spec.Containers[i].Name == "splunk" {
            podTemplateSpec.Spec.Containers[i].VolumeMounts = append(
                podTemplateSpec.Spec.Containers[i].VolumeMounts,
                corev1.VolumeMount{
                    Name:      "splunk-certs",
                    MountPath: "/mnt/splunk/certificates",
                    ReadOnly:  true,
                },
            )
        }
    }
}


// addCertManagerSidecarInjector sets Pod annotations so that an external
// sidecar-injector webhook adds a sidecar using /mnt/splunk/certificates
// for the cert volume.
func AddCertManagerSidecarInjector(podTemplateSpec *corev1.PodTemplateSpec) {
	if podTemplateSpec.ObjectMeta.Annotations == nil {
		podTemplateSpec.ObjectMeta.Annotations = make(map[string]string)
	}

	// Example annotation to trigger an external sidecar injection
	podTemplateSpec.ObjectMeta.Annotations["sidecar-injector-webhook.svc.cluster.local/inject"] = "true"
	// If sidecar injection system reads a "volumeMountPath" from an annotation:
	podTemplateSpec.ObjectMeta.Annotations["sidecar-injector-webhook.svc.cluster.local/cert-mount-path"] = SplunkCertMountPath
	// (Sidecar injection logic might read that path and then mount the same location.)

	// Possibly also pass the secret name if needed:
	podTemplateSpec.ObjectMeta.Annotations["sidecar-injector-webhook.svc.cluster.local/secret-name"] = "splunk-cert-secret"
	// Or other references (issuer, etc.) as required by injection logic
}
