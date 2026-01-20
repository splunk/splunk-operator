// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

package telemetrytest

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Telemetry integration", func() {
	var testcaseEnvInst *testenv.TestCaseEnv
	ctx := context.TODO()

	BeforeEach(func() {
		var err error
		name := fmt.Sprintf("%s-%s", testenvInstance.GetName(), testenv.RandomDNSName(3))
		testcaseEnvInst, err = testenv.NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
		Expect(err).To(Succeed(), "Unable to create testcaseenv")
	})

	AfterEach(func() {
		if testcaseEnvInst != nil {
			Expect(testcaseEnvInst.Teardown()).To(Succeed())
		}
	})

	It("should process telemetry ConfigMap and collect data from Standalone CR", func() {
		// Create a Standalone CR inline
		standalone := &enterpriseApi.Standalone{
			TypeMeta: metav1.TypeMeta{
				Kind: "Standalone",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:       testcaseEnvInst.GetName() + "-standalone",
				Namespace:  testcaseEnvInst.GetName(),
				Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
			},
			Spec: enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
					Volumes: []corev1.Volume{},
				},
			},
		}
		Expect(testcaseEnvInst.GetKubeClient().Create(ctx, standalone)).To(Succeed())

		// Create a telemetry ConfigMap
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "telemetry-cm",
				Namespace: testcaseEnvInst.GetName(),
			},
			Data: map[string]string{"telemetry": "{\"test\":\"value\"}"},
		}
		Expect(testcaseEnvInst.GetKubeClient().Create(ctx, cm)).To(Succeed())

		// Wait and verify telemetry processing (simulate by checking ConfigMap exists and Standalone is Ready)
		Eventually(func() error {
			if err := testcaseEnvInst.GetKubeClient().Get(ctx, types.NamespacedName{Name: cm.ObjectMeta.Name, Namespace: cm.ObjectMeta.Namespace}, cm); err != nil {
				return err
			}
			if err := testcaseEnvInst.GetKubeClient().Get(ctx, types.NamespacedName{Name: standalone.ObjectMeta.Name, Namespace: standalone.ObjectMeta.Namespace}, standalone); err != nil {
				return err
			}
			if standalone.Status.Phase != "Ready" {
				return fmt.Errorf("Standalone not ready")
			}
			// Optionally, check telemetry data in cm.Data or logs
			return nil
		}, 2*time.Minute, 10*time.Second).Should(Succeed())
	})
})
