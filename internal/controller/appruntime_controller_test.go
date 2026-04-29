package controller

import (
	"context"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("AppRuntime Controller", func() {
	BeforeEach(func() {
		Expect(enterpriseApi.AddToScheme(scheme.Scheme)).To(Succeed())
	})

	It("creates a headless service that exposes the appruntime port", func() {
		ctx := context.Background()
		ar := &enterpriseApi.AppRuntime{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "stack1-standalone-appruntime",
				Namespace: "test",
			},
		}

		reconciler := AppRuntimeReconciler{
			Client: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(ar).Build(),
			Scheme: scheme.Scheme,
		}

		svc, err := reconciler.createHeadlessService(ctx, ar, types.NamespacedName{
			Name:      getHeadlessName(ar.Name),
			Namespace: ar.Namespace,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(svc.Spec.ClusterIP).To(Equal(corev1.ClusterIPNone))
		Expect(svc.Spec.Ports).To(HaveLen(1))
		Expect(svc.Spec.Ports[0].Name).To(Equal("appruntime"))
		Expect(svc.Spec.Ports[0].Port).To(Equal(int32(9000)))
		Expect(svc.Spec.Ports[0].TargetPort.IntValue()).To(Equal(9000))
	})

	It("creates appruntime pods with container port 9000", func() {
		ctx := context.Background()
		ar := &enterpriseApi.AppRuntime{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "stack1-standalone-appruntime",
				Namespace: "test",
			},
			Spec: enterpriseApi.AppRuntimeSpec{
				Image:    "supervisor:0.0.1",
				Replicas: 1,
			},
		}

		reconciler := AppRuntimeReconciler{
			Client: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(ar).Build(),
			Scheme: scheme.Scheme,
		}

		err := reconciler.createPod(ctx, ar, types.NamespacedName{
			Name:      getPodName(ar.Name, 0),
			Namespace: ar.Namespace,
		}, "splunk-stack1-standalone", 0)
		Expect(err).NotTo(HaveOccurred())

		pod := &corev1.Pod{}
		err = reconciler.Get(ctx, types.NamespacedName{
			Name:      getPodName(ar.Name, 0),
			Namespace: ar.Namespace,
		}, pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Spec.HostUsers).To(Equal(ptrTo(false)))
		Expect(pod.Spec.Containers).NotTo(BeEmpty())
		Expect(pod.Spec.Containers[0].Ports).To(HaveLen(1))
		Expect(pod.Spec.Containers[0].Ports[0].Name).To(Equal("appruntime"))
		Expect(pod.Spec.Containers[0].Ports[0].ContainerPort).To(Equal(int32(9000)))
		Expect(pod.Spec.Containers[0].ImagePullPolicy).To(Equal(corev1.PullAlways))
		Expect(pod.Spec.Containers[0].Env).To(ContainElement(corev1.EnvVar{Name: "HOME", Value: "/opt/splunk"}))
		Expect(pod.Spec.Containers[0].Env).To(ContainElement(corev1.EnvVar{Name: "XDG_RUNTIME_DIR", Value: "/tmp/podman-run"}))
		Expect(pod.Spec.Containers[0].Env).To(ContainElement(corev1.EnvVar{Name: "PODMAN_SOCKET_PATH", Value: "/tmp/podman-run/podman/podman.sock"}))
		Expect(pod.Spec.Containers[0].Resources.Limits).NotTo(HaveKey(corev1.ResourceName("github.com/fuse")))
		Expect(pod.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(corev1.ResourceName("github.com/tun"), resource.MustParse("1")))
		Expect(pod.Spec.Containers[0].SecurityContext.Capabilities).To(BeNil())
		Expect(pod.Spec.Containers[0].SecurityContext.ProcMount).To(Equal(ptrTo(corev1.UnmaskedProcMount)))
		Expect(pod.Spec.Containers[0].SecurityContext.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeUnconfined))
		Expect(pod.Spec.InitContainers).NotTo(BeEmpty())
		Expect(pod.Spec.InitContainers[0].Command[2]).To(ContainSubstring("--no-preserve=all"))
		Expect(pod.Spec.InitContainers[0].Command[2]).NotTo(ContainSubstring("chown"))
		Expect(pod.Spec.InitContainers[0].Command[2]).NotTo(ContainSubstring("cp -rp"))
		Expect(pod.Spec.InitContainers[0].SecurityContext.AllowPrivilegeEscalation).To(Equal(ptrTo(false)))
		Expect(pod.Spec.InitContainers[0].SecurityContext.Capabilities).To(BeNil())
		Expect(pod.Spec.InitContainers[0].SecurityContext.RunAsUser).To(Equal(ptrTo(int64(1000))))
	})
})

func ptrTo[T any](value T) *T {
	return &value
}
