package enterprise

import (
	"context"
	"os"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestUpdateSplunkPodTemplateWithConfig_MultiContainerInjectsInitSidecarAndHTTPProbes(t *testing.T) {
	t.Setenv("SPLUNK_POD_ARCH", "multi-container")
	t.Setenv("RELATED_IMAGE_SPLUNK_INIT", "test/splunk-init:latest")
	t.Setenv("RELATED_IMAGE_SPLUNK_SIDECAR", "test/splunk-sidecar:latest")

	ctx := context.TODO()
	client := spltest.NewMockClient()

	cr := &enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	pod := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "splunk", Image: "test/splunk:latest"},
			},
		},
	}

	spec := &enterpriseApi.CommonSplunkSpec{}
	updateSplunkPodTemplateWithConfig(ctx, client, pod, cr, spec, SplunkStandalone, nil, "dummy-secret")

	// Init container injected.
	foundInit := false
	for _, c := range pod.Spec.InitContainers {
		if c.Name == "splunk-init" {
			foundInit = true
		}
	}
	if !foundInit {
		t.Fatalf("expected init container splunk-init to be injected")
	}

	// Sidecar injected.
	foundSidecar := false
	for _, c := range pod.Spec.Containers {
		if c.Name == "splunk-sidecar" {
			foundSidecar = true
		}
	}
	if !foundSidecar {
		t.Fatalf("expected sidecar container splunk-sidecar to be injected")
	}

	// Splunk probes should be HTTP GET, not exec scripts.
	var splunk corev1.Container
	ok := false
	for _, c := range pod.Spec.Containers {
		if c.Name == "splunk" {
			splunk = c
			ok = true
			break
		}
	}
	if !ok {
		t.Fatalf("expected splunk container to exist")
	}
	if splunk.ReadinessProbe == nil || splunk.ReadinessProbe.HTTPGet == nil {
		t.Fatalf("expected splunk readiness probe to be httpGet")
	}
	if splunk.ReadinessProbe.HTTPGet.Path != "/healthz/pod-ready" {
		t.Fatalf("unexpected readiness path: %q", splunk.ReadinessProbe.HTTPGet.Path)
	}
	if splunk.LivenessProbe == nil || splunk.LivenessProbe.HTTPGet == nil {
		t.Fatalf("expected splunk liveness probe to be httpGet")
	}
	if splunk.StartupProbe == nil || splunk.StartupProbe.HTTPGet == nil {
		t.Fatalf("expected splunk startup probe to be httpGet")
	}

	// Ensure we didn't clobber sidecar env with the splunk env set.
	for _, c := range pod.Spec.Containers {
		if c.Name != "splunk-sidecar" {
			continue
		}
		for _, e := range c.Env {
			if e.Name == "SPLUNK_DEFAULTS_URL" {
				t.Fatalf("sidecar should not receive splunk container env SPLUNK_DEFAULTS_URL")
			}
		}
	}

	// Cleanup for any other tests using os.Getenv directly.
	_ = os.Unsetenv("SPLUNK_POD_ARCH")
	_ = os.Unsetenv("RELATED_IMAGE_SPLUNK_INIT")
	_ = os.Unsetenv("RELATED_IMAGE_SPLUNK_SIDECAR")
}
