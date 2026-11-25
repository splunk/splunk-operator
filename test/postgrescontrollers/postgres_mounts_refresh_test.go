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

package postgrescontrollers

import (
	"context"
	_ "embed"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed testdata/postgres_mounts_refresh_probe.py
var postgresMountRefreshProbe string

func verifyPostgresMountRefresh(ctx context.Context, env *testenv.TestCaseEnv) {
	const (
		initialEndpoint = "before.invalid"
		updatedEndpoint = "after.invalid"
		readyFile       = "/tmp/postgresok-watch-ready"
	)
	c := env.GetKubeClient()
	metadata := metav1.ObjectMeta{Name: "postgres-mount-refresh", Namespace: env.GetName()}

	By("creating a test-owned ConfigMap for the operator's PostgreSQL projection")
	connection := &corev1.ConfigMap{ObjectMeta: metadata, Data: map[string]string{
		"CLUSTER_RW_ENDPOINT": initialEndpoint, "DEFAULT_CLUSTER_PORT": "5432",
		"DATABASE_NAME": "appdb", "RW_USER_NAME": "appdb_rw", "ADMIN_USER_NAME": "appdb",
	}}
	Expect(c.Create(ctx, connection)).To(Succeed())
	// The projection requires these keys. The probe never opens either file.
	placeholder := &corev1.Secret{ObjectMeta: metadata, Data: map[string][]byte{"password": {}, "ca.crt": {}}}
	Expect(c.Create(ctx, placeholder)).To(Succeed())

	By("starting a Linux watcher in the Splunk container")
	template := &corev1.PodTemplateSpec{Spec: corev1.PodSpec{
		RestartPolicy:                corev1.RestartPolicyNever,
		AutomountServiceAccountToken: new(false),
		NodeSelector:                 map[string]string{corev1.LabelOSStable: "linux"},
		SecurityContext: &corev1.PodSecurityContext{
			RunAsUser: new(int64(41812)), FSGroup: new(int64(41812)), RunAsNonRoot: new(true),
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
		Volumes: []corev1.Volume{{Name: "runtime", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
		Containers: []corev1.Container{{
			Name: "splunk", Image: env.GetSplunkImage(), ImagePullPolicy: corev1.PullIfNotPresent,
			// Run the bundled Python directly. The splunk launcher requires
			// splunk-launch.conf, which is absent before Splunk initialization.
			Command: []string{"/opt/splunk/bin/python3", "-c", postgresMountRefreshProbe},
			Args:    []string{initialEndpoint, updatedEndpoint, readyFile, strconv.Itoa(int(testenv.ReadinessPollTimeout.Seconds()))},
			Env:     []corev1.EnvVar{{Name: "LD_LIBRARY_PATH", Value: "/opt/splunk/lib"}},
			SecurityContext: &corev1.SecurityContext{
				AllowPrivilegeEscalation: new(false),
				Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			},
			VolumeMounts: []corev1.VolumeMount{{Name: "runtime", MountPath: "/opt/splunk/var"}},
			ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{Command: []string{"/bin/sh", "-c", "test -f \"$1\"", "watch-ready", readyFile}},
			}},
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		}},
	}}
	bundle := resources.PostgresDatabaseBundle{{
		Name: "connection", ConfigMapName: connection.Name, CASecretName: placeholder.Name,
		RWPassword: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: placeholder.Name}, Key: "password"},
	}}
	Expect(bundle.Inject(template)).To(Succeed())
	pod := &corev1.Pod{ObjectMeta: metadata, Spec: template.Spec}
	Expect(c.Create(ctx, pod)).To(Succeed())

	podKey, podUID := client.ObjectKeyFromObject(pod), pod.UID
	observe := func(ctx context.Context, g Gomega) {
		g.Expect(c.Get(ctx, podKey, pod)).To(Succeed())
		if pod.UID != podUID {
			StopTrying("The watch pod was replaced").Now()
		}
		if pod.Status.Phase == corev1.PodFailed {
			StopTrying("The PostgreSQL mount watcher failed").
				Attach("container status", pod.Status.ContainerStatuses).
				Attach("init container status", pod.Status.InitContainerStatuses).Now()
		}
	}
	Eventually(ctx, func(g Gomega, ctx context.Context) {
		observe(ctx, g)
		g.Expect(testenv.VerifyPodReady(pod, "splunk")).To(Succeed())
	}, testenv.ReadinessPollTimeout, testenv.PollInterval).Should(Succeed(), "The watcher must be armed before the update")
	containerID := pod.Status.ContainerStatuses[0].ContainerID

	By("updating the endpoint without changing the pod spec")
	connection.Data["CLUSTER_RW_ENDPOINT"] = updatedEndpoint
	Expect(c.Update(ctx, connection)).To(Succeed())

	By("requiring an inotify rename to ..data and a fresh read in the same container")
	Eventually(ctx, func(g Gomega, ctx context.Context) {
		observe(ctx, g)
		g.Expect(pod.Status.Phase).To(Equal(corev1.PodSucceeded))
	}, testenv.ReadinessPollTimeout, testenv.PollInterval).Should(Succeed(), "The watcher must see the update and read the new endpoint")
	Expect(pod.Status.ContainerStatuses).To(HaveLen(1))
	Expect(pod.Status.ContainerStatuses[0].ContainerID).To(Equal(containerID))
	Expect(pod.Status.ContainerStatuses[0].RestartCount).To(BeZero())
}
