// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.
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

// Package integration contains envtest-based integration tests that exercise
// the real reconciliation logic against a lightweight API server, without
// requiring a full Kubernetes cluster or the real Splunk image.
//
// A "fake kubelet" goroutine patches StatefulSet status and creates Pod
// objects so that the operator sees ReadyReplicas and reaches PhaseReady.
package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/tools/remotecommand"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	controller "github.com/splunk/splunk-operator/internal/controller"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	enterprise "github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

// ---------------------------------------------------------------------------
// mock helpers
// ---------------------------------------------------------------------------

type mockPodExecClient struct {
	targetPodName string
	cr            splcommon.MetaObject
}

func (m *mockPodExecClient) RunPodExecCommand(_ context.Context, _ *remotecommand.StreamOptions, _ []string) (string, string, error) {
	return "", "", nil
}
func (m *mockPodExecClient) SetTargetPodName(_ context.Context, name string) { m.targetPodName = name }
func (m *mockPodExecClient) GetTargetPodName() string                        { return m.targetPodName }
func (m *mockPodExecClient) GetCR() splcommon.MetaObject                     { return m.cr }
func (m *mockPodExecClient) SetCR(cr splcommon.MetaObject)                   { m.cr = cr }

// ---------------------------------------------------------------------------
// shared envtest harness — one API server for all subtests
// ---------------------------------------------------------------------------

type testHarness struct {
	ctx       context.Context
	cancel    context.CancelFunc
	k8sClient client.Client
	testEnv   *envtest.Environment
	mgrCancel context.CancelFunc
}

func setupHarness(t *testing.T) *testHarness {
	t.Helper()
	g := gomega.NewWithT(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)

	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	g.Expect(err).NotTo(gomega.HaveOccurred())

	enterprise.GetReadinessScriptLocation = func() string {
		return filepath.Join(repoRoot, "tools", "k8_probes", "readinessProbe.sh")
	}
	enterprise.GetLivenessScriptLocation = func() string {
		return filepath.Join(repoRoot, "tools", "k8_probes", "livenessProbe.sh")
	}
	enterprise.GetStartupScriptLocation = func() string {
		return filepath.Join(repoRoot, "tools", "k8_probes", "startupProbe.sh")
	}

	origGetPodExecClient := splutil.GetPodExecClient
	splutil.GetPodExecClient = func(_ splcommon.ControllerClient, cr splcommon.MetaObject, _ string) splutil.PodExecClientImpl {
		return &mockPodExecClient{cr: cr}
	}
	t.Cleanup(func() { splutil.GetPodExecClient = origGetPodExecClient })

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join(repoRoot, "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	t.Cleanup(func() { testEnv.Stop() })

	g.Expect(enterpriseApi.AddToScheme(clientgoscheme.Scheme)).To(gomega.Succeed())
	g.Expect(enterpriseApiV3.AddToScheme(clientgoscheme.Scheme)).To(gomega.Succeed())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: clientgoscheme.Scheme})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect((&controller.StandaloneReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: record.NewFakeRecorder(100),
	}).SetupWithManager(mgr)).To(gomega.Succeed())

	mgrCtx, mgrCancel := context.WithCancel(ctx)
	go func() {
		if err := mgr.Start(mgrCtx); err != nil {
			t.Logf("manager exited: %v", err)
		}
	}()
	t.Cleanup(func() { mgrCancel() })

	k8sClient := mgr.GetClient()
	g.Eventually(func() bool {
		return mgr.GetCache().WaitForCacheSync(ctx)
	}, 10*time.Second, 200*time.Millisecond).Should(gomega.BeTrue())

	return &testHarness{
		ctx:       ctx,
		cancel:    cancel,
		k8sClient: k8sClient,
		testEnv:   testEnv,
		mgrCancel: mgrCancel,
	}
}

// createNamespace creates an isolated namespace for a subtest.
func (h *testHarness) createNamespace(t *testing.T, name string) string {
	t.Helper()
	g := gomega.NewWithT(t)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	g.Expect(h.k8sClient.Create(h.ctx, ns)).To(gomega.Succeed())
	return name
}

// ---------------------------------------------------------------------------
// table-driven test
// ---------------------------------------------------------------------------

type standaloneTestCase struct {
	name        string
	crName      string
	spec        enterpriseApi.StandaloneSpec
	annotations map[string]string
	// If set, override SPLUNK_GENERAL_TERMS for this case (restored after).
	sgtOverride *string
	// Whether the fake kubelet should run for this namespace.
	needsFakeKubelet bool
	// validate is called after the CR is created to assert expected behavior.
	validate func(t *testing.T, g gomega.Gomega, ctx context.Context, c client.Client, ns, crName string)
}

func defaultSpec() enterpriseApi.StandaloneSpec {
	return enterpriseApi.StandaloneSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec: enterpriseApi.Spec{
				Image:           "splunk-stub:latest",
				ImagePullPolicy: "IfNotPresent",
			},
		},
		Replicas: 1,
	}
}

func TestStandaloneIntegration(t *testing.T) {
	h := setupHarness(t)
	defer h.cancel()

	emptySGT := ""

	tests := []standaloneTestCase{
		// ---- green path ----
		{
			name:             "deploy standalone reaches PhaseReady",
			crName:           "s1",
			spec:             defaultSpec(),
			needsFakeKubelet: true,
			validate: func(t *testing.T, g gomega.Gomega, ctx context.Context, c client.Client, ns, crName string) {
				stsKey := types.NamespacedName{Name: fmt.Sprintf("splunk-%s-standalone", crName), Namespace: ns}

				g.Eventually(func() error {
					return c.Get(ctx, stsKey, &appsv1.StatefulSet{})
				}, 30*time.Second, 500*time.Millisecond).Should(gomega.Succeed())
				t.Log("  ✓ StatefulSet created")

				g.Eventually(func() error {
					return c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("splunk-%s-standalone-headless", crName), Namespace: ns}, &corev1.Service{})
				}, 10*time.Second, 500*time.Millisecond).Should(gomega.Succeed())
				t.Log("  ✓ Headless service created")

				g.Eventually(func() error {
					return c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("splunk-%s-standalone-service", crName), Namespace: ns}, &corev1.Service{})
				}, 10*time.Second, 500*time.Millisecond).Should(gomega.Succeed())
				t.Log("  ✓ Regular service created")

				g.Eventually(func() error {
					return c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("splunk-%s-secret", ns), Namespace: ns}, &corev1.Secret{})
				}, 10*time.Second, 500*time.Millisecond).Should(gomega.Succeed())
				t.Log("  ✓ Namespace-scoped secret created")

				g.Eventually(func() enterpriseApi.Phase {
					cr := &enterpriseApi.Standalone{}
					if err := c.Get(ctx, types.NamespacedName{Name: crName, Namespace: ns}, cr); err != nil {
						return enterpriseApi.PhaseError
					}
					return cr.Status.Phase
				}, 60*time.Second, 2*time.Second).Should(gomega.Equal(enterpriseApi.PhaseReady))
				t.Log("  ✓ PhaseReady reached")

				cr := &enterpriseApi.Standalone{}
				g.Expect(c.Get(ctx, types.NamespacedName{Name: crName, Namespace: ns}, cr)).To(gomega.Succeed())
				g.Expect(cr.Status.ReadyReplicas).To(gomega.Equal(int32(1)))
				g.Expect(cr.Status.Selector).NotTo(gomega.BeEmpty())
				t.Log("  ✓ Status fields correct")
			},
		},
		{
			name:   "deploy standalone with 3 replicas",
			crName: "s3",
			spec: func() enterpriseApi.StandaloneSpec {
				s := defaultSpec()
				s.Replicas = 3
				return s
			}(),
			needsFakeKubelet: true,
			validate: func(t *testing.T, g gomega.Gomega, ctx context.Context, c client.Client, ns, crName string) {
				g.Eventually(func() enterpriseApi.Phase {
					cr := &enterpriseApi.Standalone{}
					if err := c.Get(ctx, types.NamespacedName{Name: crName, Namespace: ns}, cr); err != nil {
						return enterpriseApi.PhaseError
					}
					return cr.Status.Phase
				}, 60*time.Second, 2*time.Second).Should(gomega.Equal(enterpriseApi.PhaseReady))
				t.Log("  ✓ PhaseReady reached with 3 replicas")

				cr := &enterpriseApi.Standalone{}
				g.Expect(c.Get(ctx, types.NamespacedName{Name: crName, Namespace: ns}, cr)).To(gomega.Succeed())
				g.Expect(cr.Status.ReadyReplicas).To(gomega.Equal(int32(3)))
				t.Log("  ✓ ReadyReplicas == 3")
			},
		},

		// ---- red path: API-level validation errors ----
		// These specs are rejected by kubebuilder validation markers on the CRD,
		// so the CR never reaches the controller. We test that Create() fails.
		{
			name:   "negative liveness delay rejected by API",
			crName: "badlive",
			spec: func() enterpriseApi.StandaloneSpec {
				s := defaultSpec()
				s.CommonSplunkSpec.LivenessInitialDelaySeconds = -5
				return s
			}(),
			needsFakeKubelet: false,
			// validate is nil — creation itself must fail (see loop below).
			validate: nil,
		},
		{
			name:   "negative readiness delay rejected by API",
			crName: "badready",
			spec: func() enterpriseApi.StandaloneSpec {
				s := defaultSpec()
				s.CommonSplunkSpec.ReadinessInitialDelaySeconds = -1
				return s
			}(),
			needsFakeKubelet: false,
			validate:         nil,
		},
		{
			name:             "missing SPLUNK_GENERAL_TERMS → PhaseError",
			crName:           "nosgt",
			spec:             defaultSpec(),
			sgtOverride:      &emptySGT,
			needsFakeKubelet: false,
			validate: func(t *testing.T, g gomega.Gomega, ctx context.Context, c client.Client, ns, crName string) {
				g.Eventually(func() enterpriseApi.Phase {
					cr := &enterpriseApi.Standalone{}
					if err := c.Get(ctx, types.NamespacedName{Name: crName, Namespace: ns}, cr); err != nil {
						return ""
					}
					return cr.Status.Phase
				}, 15*time.Second, 1*time.Second).Should(gomega.Equal(enterpriseApi.PhaseError))
				t.Log("  ✓ PhaseError set when SPLUNK_GENERAL_TERMS is missing")
			},
		},

		// ---- red path: pause annotation ----
		{
			name:   "pause annotation blocks reconciliation",
			crName: "paused",
			spec:   defaultSpec(),
			annotations: map[string]string{
				enterpriseApi.StandalonePausedAnnotation: "true",
			},
			needsFakeKubelet: false,
			validate: func(t *testing.T, g gomega.Gomega, ctx context.Context, c client.Client, ns, crName string) {
				// With the pause annotation, ApplyStandalone is never called.
				// Give the reconciler a few cycles, then confirm no resources were created.
				time.Sleep(5 * time.Second)

				stsKey := types.NamespacedName{Name: fmt.Sprintf("splunk-%s-standalone", crName), Namespace: ns}
				err := c.Get(ctx, stsKey, &appsv1.StatefulSet{})
				g.Expect(k8serrors.IsNotFound(err)).To(gomega.BeTrue(), "StatefulSet should not exist when paused")
				t.Log("  ✓ No StatefulSet created while paused")

				// Phase should remain empty (zero value) — never set.
				cr := &enterpriseApi.Standalone{}
				g.Expect(c.Get(ctx, types.NamespacedName{Name: crName, Namespace: ns}, cr)).To(gomega.Succeed())
				g.Expect(cr.Status.Phase).To(gomega.BeEmpty(), "Phase should be empty when paused")
				t.Log("  ✓ Phase is empty (never reconciled)")
			},
		},

		// ---- red path: delete CR ----
		{
			name:             "CR deletion succeeds and CR disappears",
			crName:           "delme",
			spec:             defaultSpec(),
			needsFakeKubelet: true,
			validate: func(t *testing.T, g gomega.Gomega, ctx context.Context, c client.Client, ns, crName string) {
				crKey := types.NamespacedName{Name: crName, Namespace: ns}
				stsKey := types.NamespacedName{Name: fmt.Sprintf("splunk-%s-standalone", crName), Namespace: ns}

				g.Eventually(func() enterpriseApi.Phase {
					cr := &enterpriseApi.Standalone{}
					if err := c.Get(ctx, crKey, cr); err != nil {
						return enterpriseApi.PhaseError
					}
					return cr.Status.Phase
				}, 60*time.Second, 2*time.Second).Should(gomega.Equal(enterpriseApi.PhaseReady))
				t.Log("  ✓ PhaseReady reached (pre-delete)")

				g.Expect(c.Get(ctx, stsKey, &appsv1.StatefulSet{})).To(gomega.Succeed())
				t.Log("  ✓ StatefulSet exists before delete")

				cr := &enterpriseApi.Standalone{}
				g.Expect(c.Get(ctx, crKey, cr)).To(gomega.Succeed())
				g.Expect(c.Delete(ctx, cr)).To(gomega.Succeed())
				t.Log("  ✓ CR delete accepted")

				g.Eventually(func() bool {
					return k8serrors.IsNotFound(c.Get(ctx, crKey, &enterpriseApi.Standalone{}))
				}, 30*time.Second, 1*time.Second).Should(gomega.BeTrue())
				t.Log("  ✓ CR gone")

				// NOTE: envtest does not run the K8s garbage collector, so
				// owner-reference cascading delete won't remove the StatefulSet.
				// In a real cluster (Kind / GKE) the StatefulSet would be GC'd.
				// We only verify the CR itself is removed.
			},
		},
	}

	// Run sequentially — subtests share global state (env vars, function vars).
	for i, tc := range tests {
		tc := tc
		nsName := fmt.Sprintf("test-s1-%02d", i)
		t.Run(tc.name, func(t *testing.T) {
			g := gomega.NewWithT(t)
			ns := h.createNamespace(t, nsName)

			if tc.sgtOverride != nil {
				orig := os.Getenv("SPLUNK_GENERAL_TERMS")
				os.Setenv("SPLUNK_GENERAL_TERMS", *tc.sgtOverride)
				defer os.Setenv("SPLUNK_GENERAL_TERMS", orig)
			}

			if tc.needsFakeKubelet {
				fkCtx, fkCancel := context.WithCancel(h.ctx)
				defer fkCancel()
				go fakeKubelet(fkCtx, t, h.k8sClient, ns)
			}

			cr := &enterpriseApi.Standalone{
				ObjectMeta: metav1.ObjectMeta{
					Name:        tc.crName,
					Namespace:   ns,
					Annotations: tc.annotations,
				},
				Spec: tc.spec,
			}
			createErr := h.k8sClient.Create(h.ctx, cr)

			if tc.validate == nil {
				// nil validate means we expect Create itself to fail (API validation).
				g.Expect(createErr).To(gomega.HaveOccurred(), "expected API server to reject the CR")
				g.Expect(k8serrors.IsInvalid(createErr)).To(gomega.BeTrue(), "expected Invalid status error")
				t.Logf("  ✓ API rejected CR: %v", createErr)
				return
			}

			g.Expect(createErr).NotTo(gomega.HaveOccurred())
			tc.validate(t, g, h.ctx, h.k8sClient, ns, tc.crName)
		})
	}
}

// ---------------------------------------------------------------------------
// fake kubelet
// ---------------------------------------------------------------------------

// fakeKubelet simulates kubelet: watches for StatefulSets and creates
// Running+Ready pods, then patches the StatefulSet status.
func fakeKubelet(ctx context.Context, t *testing.T, c client.Client, namespace string) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stsList := &appsv1.StatefulSetList{}
			if err := c.List(ctx, stsList, client.InNamespace(namespace)); err != nil {
				continue
			}

			for i := range stsList.Items {
				sts := &stsList.Items[i]
				if sts.Spec.Replicas == nil {
					continue
				}
				desired := *sts.Spec.Replicas
				if sts.Status.ReadyReplicas >= desired {
					continue
				}

				revision := sts.Status.UpdateRevision
				if revision == "" {
					revision = sts.Status.CurrentRevision
				}
				if revision == "" {
					revision = "stub-rev"
				}

				for idx := int32(0); idx < desired; idx++ {
					podName := fmt.Sprintf("%s-%d", sts.Name, idx)
					pod := &corev1.Pod{}
					key := types.NamespacedName{Name: podName, Namespace: namespace}

					if err := c.Get(ctx, key, pod); err != nil {
						pod = &corev1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Name:      podName,
								Namespace: namespace,
								Labels: map[string]string{
									"controller-revision-hash": revision,
								},
								OwnerReferences: []metav1.OwnerReference{{
									APIVersion: "apps/v1",
									Kind:       "StatefulSet",
									Name:       sts.Name,
									UID:        sts.UID,
								}},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name:  "splunk",
									Image: "splunk-stub:latest",
								}},
							},
						}
						if err := c.Create(ctx, pod); err != nil {
							continue
						}
					}

					pod.Status = corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{{
							Name:  "splunk",
							Ready: true,
							Image: "splunk-stub:latest",
						}},
					}
					_ = c.Status().Update(ctx, pod)
				}

				sts.Status.Replicas = desired
				sts.Status.ReadyReplicas = desired
				sts.Status.CurrentReplicas = desired
				sts.Status.UpdatedReplicas = desired
				if sts.Status.CurrentRevision == "" {
					sts.Status.CurrentRevision = revision
				}
				sts.Status.UpdateRevision = revision
				_ = c.Status().Update(ctx, sts)
			}
		}
	}
}
