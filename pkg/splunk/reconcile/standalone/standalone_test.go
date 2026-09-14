// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

package standalone

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	enterprise "github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/telapp"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clienttesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	s3AccessKey = "s3_access_key"
	s3SecretKey = "s3_secret_key"
)

func init() {
	// The standalone tests run from a different package directory than the
	// legacy enterprise tests, so keep the probe fixture locations equivalent.
	splutil.GetReadinessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../../tools/k8_probes/readinessProbe.sh")
		return fileLocation
	}
	splutil.GetLivenessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../../tools/k8_probes/livenessProbe.sh")
		return fileLocation
	}
	splutil.GetStartupScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../../tools/k8_probes/startupProbe.sh")
		return fileLocation
	}
}

func TestApplyStandalone(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-standalone-stack1-configmap"},
		{MetaName: "*v1.Service-test-splunk-stack1-standalone-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-standalone-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-standalone-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
		{MetaName: "*v4.Standalone-test-stack1"},
		{MetaName: "*v4.Standalone-test-stack1"},
	}
	updatefuncCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-standalone-stack1-configmap"},
		{MetaName: "*v1.Service-test-splunk-stack1-standalone-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-standalone-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-standalone-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
		//{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
	}
	deltaCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
		{MetaName: "*v4.Standalone-test-stack1"},
		{MetaName: "*v4.Standalone-test-stack1"},
	}
	updateFuncCalls := append(updatefuncCalls, deltaCalls...)

	labels := map[string]string{
		"app.kubernetes.io/component":  "versionedSecrets",
		"app.kubernetes.io/managed-by": "splunk-operator",
	}
	listOpts := []client.ListOption{
		client.InNamespace("test"),
		client.MatchingLabels(labels),
	}
	listmockCall := []spltest.MockFuncCall{
		{ListOpts: listOpts}}

	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[3], funcCalls[4], funcCalls[5], funcCalls[8], funcCalls[11], funcCalls[14]}, "Update": {funcCalls[0]}, "List": {listmockCall[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": updateFuncCalls, "Update": {funcCalls[14]}, "List": {listmockCall[0]}}
	current := enterpriseApi.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	revised := current.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcileFn := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyStandalone(context.Background(), c, cr.(*enterpriseApi.Standalone))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyStandalone", &current, revised, createCalls, updateCalls, reconcileFn, true)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplyStandalone(context.Background(), c, cr.(*enterpriseApi.Standalone))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)

	// Negative testing: spec validation failure is a terminal condition — returns nil (no requeue)
	current.Spec.CommonSplunkSpec.LivenessInitialDelaySeconds = -1
	c := spltest.NewMockClient()
	ctx := context.TODO()
	_ = errors.New(splcommon.Rerr)
	_, err := ApplyStandalone(ctx, c, &current)
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("stalled spec validation failure should return a terminal error, got %v", err)
	}

	// Smartstore spec
	current.Spec.CommonSplunkSpec.LivenessInitialDelaySeconds = 5
	current.Spec.SmartStore = enterpriseApi.SmartStoreSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-secret"},
		},

		IndexList: []enterpriseApi.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3", RemotePath: "remotepath3",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	current.Status.SmartStore = enterpriseApi.SmartStoreSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-secret"},
		},

		IndexList: []enterpriseApi.IndexSpec{
			{Name: "salesdata4", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3", RemotePath: "remotepath3",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}
	ApplyStandalone(ctx, c, &current)
}

func TestApplyPaused(t *testing.T) {
	ctx := context.Background()
	scheme := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(enterpriseApi.AddToScheme(scheme))

	c := newFakeClientBuilder(scheme).
		WithStatusSubresource(&enterpriseApi.Standalone{}).
		Build()
	cr := &enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "standalone",
			Namespace: "test",
			Annotations: map[string]string{
				enterpriseApi.StandalonePausedAnnotation: "true",
			},
		},
	}
	if err := c.Create(ctx, cr); err != nil {
		t.Fatalf("failed to create Standalone: %v", err)
	}

	result, err := Apply(ctx, c, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, nil)
	if err != nil {
		t.Fatalf("Apply() returned error: %v", err)
	}
	if !result.Requeue || result.RequeueAfter != pauseRetryDelay {
		t.Fatalf("Apply() result = %+v; want requeue after %s", result, pauseRetryDelay)
	}

	updated := &enterpriseApi.Standalone{}
	if err := c.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, updated); err != nil {
		t.Fatalf("failed to get updated Standalone: %v", err)
	}
	paused := meta.FindStatusCondition(updated.Status.Conditions, string(enterpriseApi.ConditionPaused))
	if paused == nil {
		t.Fatal("expected Paused condition")
	}
	if paused.Status != metav1.ConditionTrue {
		t.Fatalf("Paused condition status = %s; want %s", paused.Status, metav1.ConditionTrue)
	}
}

func TestApplyStandaloneWithSmartstore(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-standalone-stack1-configmap"},
		{MetaName: "*v1.Service-test-splunk-stack1-standalone-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-standalone-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-standalone-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
		{MetaName: "*v4.Standalone-test-stack1"},
		{MetaName: "*v4.Standalone-test-stack1"},
	}
	createFuncCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-standalone-stack1-configmap"},
		{MetaName: "*v1.Service-test-splunk-stack1-standalone-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-standalone-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-standalone-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
		{MetaName: "*v4.Standalone-test-stack1"},
		{MetaName: "*v4.Standalone-test-stack1"},
	}

	labels := map[string]string{
		"app.kubernetes.io/component":  "versionedSecrets",
		"app.kubernetes.io/managed-by": "splunk-operator",
	}
	listOpts := []client.ListOption{
		client.InNamespace("test"),
		client.MatchingLabels(labels),
	}
	listmockCall := []spltest.MockFuncCall{
		{ListOpts: listOpts}}

	createCalls := map[string][]spltest.MockFuncCall{"Get": createFuncCalls, "Create": {funcCalls[2], funcCalls[6], funcCalls[7], funcCalls[8], funcCalls[10], funcCalls[12], funcCalls[15]}, "Update": {funcCalls[0]}, "List": {listmockCall[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": {funcCalls[9]}, "List": {listmockCall[0]}}

	current := enterpriseApi.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: 1,
			SmartStore: enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-secret"},
				},
				IndexList: []enterpriseApi.IndexSpec{
					{Name: "salesdata1", RemotePath: "remotepath1",
						IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
							VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata2", RemotePath: "remotepath2",
						IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
							VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata3", RemotePath: "remotepath3",
						IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
							VolName: "msos_s2s3_vol"},
					},
				},
			},
		},
	}

	client := spltest.NewMockClient()

	// Without S3 keys, ApplyStandalone should fail
	_, err := ApplyStandalone(context.Background(), client, &current)
	if err == nil {
		t.Errorf("ApplyStandalone should fail without S3 secrets configured")
	}

	// Create namespace scoped secret
	secret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Error(err.Error())
	}

	secret.Data[s3AccessKey] = []byte("abcdJDckRkxhMEdmSk5FekFRRzBFOXV6bGNldzJSWE9IenhVUy80aa")
	secret.Data[s3SecretKey] = []byte("g4NVp0a29PTzlPdGczWk1vekVUcVBSa0o4NkhBWWMvR1NadDV4YVEy")
	_, err = k8sops.ApplySecret(ctx, client, secret)
	if err != nil {
		t.Error(err.Error())
	}

	revised := current.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyStandalone(context.Background(), c, cr.(*enterpriseApi.Standalone))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyStandaloneWithSmartstore", &current, revised, createCalls, updateCalls, reconcile, true, secret)
}

func TestGetStandaloneStatefulSet(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Errorf("Failed to create namespace scoped object")
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := ValidateStandaloneSpec(ctx, c, &cr); err != nil {
				t.Errorf("ValidateStandaloneSpec() returned error: %v", err)
			}
			return GetStandaloneStatefulSet(ctx, c, &cr)
		}
		configTester(t, "GetStandaloneStatefulSet()", f, want)
	}
	test(loadFixture(t, "statefulset_stack1_standalone_base.json"))

	cr.Spec.EtcVolumeStorageConfig.EphemeralStorage = true
	cr.Spec.VarVolumeStorageConfig.EphemeralStorage = true
	test(loadFixture(t, "statefulset_stack1_standalone_base_1.json"))

	cr.Spec.EtcVolumeStorageConfig.EphemeralStorage = false
	cr.Spec.VarVolumeStorageConfig.EphemeralStorage = false

	cr.Spec.ClusterManagerRef.Name = "stack2"
	_ = splutil.CreateResource(ctx, c, &enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack2",
			Namespace: "test",
		},
	})
	cr.Spec.EtcVolumeStorageConfig.StorageClassName = "gp2"
	cr.Spec.VarVolumeStorageConfig.StorageClassName = "gp2"
	cr.Spec.SchedulerName = "custom-scheduler"
	cr.Spec.Defaults = "defaults-string"
	cr.Spec.DefaultsURL = "/mnt/defaults/defaults.yml"
	cr.Spec.Volumes = []corev1.Volume{
		{Name: "defaults"},
	}
	test(loadFixture(t, "statefulset_stack1_standalone_with_defaults.json"))

	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	test(loadFixture(t, "statefulset_stack1_standalone_with_apps.json"))

	// Create a serviceaccount
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	test(loadFixture(t, "statefulset_stack1_standalone_with_service_account.json"))

	// Add extraEnv
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	test(loadFixture(t, "statefulset_stack1_standalone_with_service_account_1.json"))

	// Add additional label to cr metadata to transfer to the statefulset
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	test(loadFixture(t, "statefulset_stack1_standalone_with_service_account_2.json"))
}

func TestGetStandaloneStatefulSetPodAnnotationsOverrideIstioDefaults(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
			Annotations: map[string]string{
				"custom.splunk.com/parent": "from-parent",
			},
		},
	}
	cr.Spec.PodAnnotations = map[string]string{
		splcommon.IstioExcludeOutboundPortsAnnotation: "8089,8191,9997,15020",
		splcommon.IstioIncludeInboundPortsAnnotation:  "8000,8088,15021",
		"custom.splunk.com/pod":                       "from-pod-annotations",
		"custom.splunk.com/parent":                    "overridden-by-pod-annotations",
	}

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Fatalf("Failed to create namespace scoped object: %v", err)
	}
	if err := ValidateStandaloneSpec(ctx, c, &cr); err != nil {
		t.Fatalf("ValidateStandaloneSpec() returned error: %v", err)
	}

	ss, err := GetStandaloneStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("GetStandaloneStatefulSet() returned error: %v", err)
	}

	annotations := ss.Spec.Template.GetAnnotations()
	want := map[string]string{
		splcommon.IstioExcludeOutboundPortsAnnotation: "8089,8191,9997,15020",
		splcommon.IstioIncludeInboundPortsAnnotation:  "8000,8088,15021",
		"custom.splunk.com/pod":                       "from-pod-annotations",
		"custom.splunk.com/parent":                    "overridden-by-pod-annotations",
	}
	for key, value := range want {
		if annotations[key] != value {
			t.Errorf("StatefulSet pod annotation %q = %q; want %q", key, annotations[key], value)
		}
	}
}

func TestGetStandaloneStatefulSetPodAnnotationsPreserveIstioDefaults(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	cr.Spec.PodAnnotations = map[string]string{
		"custom.splunk.com/pod": "from-pod-annotations",
	}

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Fatalf("Failed to create namespace scoped object: %v", err)
	}
	if err := ValidateStandaloneSpec(ctx, c, &cr); err != nil {
		t.Fatalf("ValidateStandaloneSpec() returned error: %v", err)
	}

	ss, err := GetStandaloneStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("GetStandaloneStatefulSet() returned error: %v", err)
	}

	annotations := ss.Spec.Template.GetAnnotations()
	want := map[string]string{
		splcommon.IstioExcludeOutboundPortsAnnotation: "8089,8191,9997",
		splcommon.IstioIncludeInboundPortsAnnotation:  "8000,8088",
		"custom.splunk.com/pod":                       "from-pod-annotations",
	}
	for key, value := range want {
		if annotations[key] != value {
			t.Errorf("StatefulSet pod annotation %q = %q; want %q", key, annotations[key], value)
		}
	}
}

func TestStandaloneSpecNotCreatedWithoutGeneralTerms(t *testing.T) {
	// Unset the SPLUNK_GENERAL_TERMS environment variable
	os.Unsetenv("SPLUNK_GENERAL_TERMS")
	ctx := context.TODO()

	// Create a mock standalone CR
	standalone := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-standalone",
			Namespace: "test",
		},
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: 1,
		},
	}

	// Create a mock client
	c := spltest.NewMockClient()

	// Attempt to apply the standalone spec
	_, err := ApplyStandalone(ctx, c, &standalone)

	// SPLUNK_GENERAL_TERMS unset is a stalled misconfiguration: reconciler returns terminal error (no requeue)
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("stalled spec validation failure should return a terminal error, got %v", err)
	}
}

func TestApplyStandaloneSmartstoreKeyChangeDetection(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	current := enterpriseApi.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: 1,
			SmartStore: enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-secret"},
				},
				IndexList: []enterpriseApi.IndexSpec{
					{Name: "salesdata1", RemotePath: "remotepath1",
						IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
							VolName: "msos_s2s3_vol"},
					},
				},
			},
		},
	}
	client := spltest.NewMockClient()

	// Create namespace scoped secret
	secret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Error(err.Error())
	}

	secret.Data[s3AccessKey] = []byte("abcdJDckRkxhMEdmSk5FekFRRzBFOXV6bGNldzJSWE9IenhVUy80aa")
	secret.Data[s3SecretKey] = []byte("g4NVp0a29PTzlPdGczWk1vekVUcVBSa0o4NkhBWWMvR1NadDV4YVEy")
	_, err = k8sops.ApplySecret(ctx, client, secret)
	if err != nil {
		t.Error(err.Error())
	}

	_, err = ApplyStandalone(context.Background(), client, &current)
	if err != nil {
		t.Errorf("ApplyStandalone should not fail with full configuration")
	}

	// Now change the secret key
	secret.Data[s3AccessKey] = []byte("changed")
	current.Status.ResourceRevMap["splunk-test-secret"] = "3456"

	_, err = k8sops.ApplySecret(ctx, client, secret)
	if err != nil {
		t.Error(err.Error())
	}

	changed := k8sops.AreRemoteVolumeKeysChanged(ctx, client, &current, splcommon.SplunkStandalone, &current.Spec.SmartStore, current.Status.ResourceRevMap, &err)

	if !changed {
		t.Errorf("Key change was not detected %v", err)
	}
}

func TestAppFrameworkApplyStandaloneShouldNotFail(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()
	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "standalone",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: 1,
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret", Type: "s3", Provider: "aws"},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{Name: "adminApps",
						Location: "adminAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
					{Name: "securityApps",
						Location: "securityAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
					{Name: "authenticationApps",
						Location: "authenticationAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
				},
			},
		},
	}

	client := spltest.NewMockClient()

	// Create namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Error(err.Error())
	}

	// Create S3 secret
	s3Secret := spltest.GetMockS3SecretKeys("s3-secret")

	client.AddObject(&s3Secret)
	configmap := spltest.GetMockPerCRConfigMap("splunk-standalone-standalone-configmap")
	client.AddObject(&configmap)

	// to pass the validation stage, add the directory to download apps
	err = os.MkdirAll(splcommon.AppDownloadVolume, 0755)
	defer os.RemoveAll(splcommon.AppDownloadVolume)

	if err != nil {
		t.Errorf("Unable to create download directory for apps :%s", splcommon.AppDownloadVolume)
	}

	_, err = ApplyStandalone(ctx, client, &cr)

	if err != nil {
		t.Errorf("ApplyStandalone should be successful")
	}
}

func TestAppFrameworkApplyStandaloneScalingUpShouldNotFail(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "standalone",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: 1,
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret", Type: "s3", Provider: "aws"},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{Name: "adminApps",
						Location: "adminAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
					{Name: "securityApps",
						Location: "securityAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
					{Name: "authenticationApps",
						Location: "authenticationAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
				},
			},
		},
	}

	client := spltest.NewMockClient()

	// Create namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Error(err.Error())
	}

	// Create S3 secret
	s3Secret := spltest.GetMockS3SecretKeys("s3-secret")

	client.AddObject(&s3Secret)

	configmap := spltest.GetMockPerCRConfigMap("splunk-standalone-standalone-configmap")
	client.AddObject(&configmap)

	// to pass the validation stage, add the directory to download apps
	err = os.MkdirAll(splcommon.AppDownloadVolume, 0755)
	defer os.RemoveAll(splcommon.AppDownloadVolume)

	if err != nil {
		t.Errorf("Unable to create download directory for apps :%s", splcommon.AppDownloadVolume)
	}
	_, err = ApplyStandalone(ctx, client, &cr)

	if err != nil {
		t.Errorf("ApplyStandalone should be successful")
	}

	// now scale up
	cr.Spec.Replicas = 2
	_, err = ApplyStandalone(ctx, client, &cr)
	if err != nil {
		t.Errorf("ApplyStandalone should be successful")
	}
}

func TestApplyStandaloneDeletion(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	stand1 := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		Spec: enterpriseApi.StandaloneSpec{
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				AppsRepoPollInterval: 0,
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol",
						Endpoint:  "https://s3-eu-west-2.amazonaws.com",
						Path:      "testbucket-rs-london",
						SecretRef: "s3-secret",
						Type:      "s3",
						Provider:  "aws"},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{Name: "adminApps",
						Location: "adminAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
					{Name: "securityApps",
						Location: "securityAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
					{Name: "authenticationApps",
						Location: "authenticationAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
				},
			},
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				MonitoringConsoleRef: corev1.ObjectReference{
					Name: "mcName",
				},
				Mock: true,
			},
		},
	}

	c := spltest.NewMockClient()

	// Create S3 secret
	s3Secret := spltest.GetMockS3SecretKeys("s3-secret")

	c.AddObject(&s3Secret)
	configmap := spltest.GetMockPerCRConfigMap("splunk-standalone-stack1-configmap")
	c.AddObject(&configmap)

	// Create namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Error(err.Error())
	}

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	stand1.ObjectMeta.DeletionTimestamp = &currentTime
	stand1.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}

	pvclist := corev1.PersistentVolumeClaimList{
		Items: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "splunk-pvc-stack1-var",
					Namespace: "test",
				},
			},
		},
	}
	c.ListObj = &pvclist

	// to pass the validation stage, add the directory to download apps
	err = os.MkdirAll(splcommon.AppDownloadVolume, 0755)
	defer os.RemoveAll(splcommon.AppDownloadVolume)

	if err != nil {
		t.Errorf("Unable to create download directory for apps :%s", splcommon.AppDownloadVolume)
	}

	_, err = ApplyStandalone(ctx, c, &stand1)
	if err != nil {
		t.Errorf("ApplyStandalone should not have returned error here.")
	}
}

func TestGetStandaloneList(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	standalone := enterpriseApi.Standalone{}

	listOpts := []client.ListOption{
		client.InNamespace("test"),
	}

	client := spltest.NewMockClient()

	var numOfObjects int
	var err error

	// Invalid scenario since we haven't added standalone to the list yet
	_, err = getStandaloneList(ctx, client, &standalone, listOpts)
	if err == nil {
		t.Errorf("getNumOfObjects should have returned error as we haven't added standalone to the list yet")
	}

	standaloneList := &enterpriseApi.StandaloneList{}
	standaloneList.Items = append(standaloneList.Items, standalone)

	client.ListObj = standaloneList

	objList, err := getStandaloneList(ctx, client, &standalone, listOpts)
	if err != nil {
		t.Errorf("getNumOfObjects should not have returned error=%v", err)
	}

	numOfObjects = len(objList.Items)
	if numOfObjects != 1 {
		t.Errorf("Got wrong number of standalone objects. Expected=%d, Got=%d", 1, numOfObjects)
	}
}

func TestStandaloneWitAppFramework(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	// create directory for app framework
	newpath := filepath.Join("/tmp", "appframework")
	_ = os.MkdirAll(newpath, os.ModePerm)

	// adding getapplist to fix test case
	enterprise.GetAppsList = func(ctx context.Context, remoteDataClientMgr enterprise.RemoteDataClientManager) (splcommon.RemoteDataListResponse, error) {
		RemoteDataListResponse := splcommon.RemoteDataListResponse{}
		return RemoteDataListResponse, nil
	}

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.LicenseManager{}).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.Standalone{}).
		WithStatusSubresource(&enterpriseApi.MonitoringConsole{}).
		WithStatusSubresource(&enterpriseApi.IndexerCluster{}).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{})
	c := builder.Build()

	ctx := context.TODO()

	// Create App framework volume
	volumeSpec := []enterpriseApi.VolumeSpec{
		{
			Name:      "testing",
			Endpoint:  "/someendpoint",
			Path:      "s3-test",
			SecretRef: "secretRef",
			Provider:  "aws",
			Type:      "s3",
			Region:    "west",
		},
	}

	// AppSourceDefaultSpec: Remote Storage volume name and Scope of App deployment
	appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
		VolName: "testing",
		Scope:   "local",
	}

	// appSourceSpec: App source name, location and volume name and scope from appSourceDefaultSpec
	appSourceSpec := []enterpriseApi.AppSourceSpec{
		{
			Name:                 "appSourceName",
			Location:             "appSourceLocation",
			AppSourceDefaultSpec: appSourceDefaultSpec,
		},
	}

	// appFrameworkSpec: AppSource settings, Poll Interval, volumes, appSources on volumes
	appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
		Defaults:             appSourceDefaultSpec,
		AppsRepoPollInterval: int64(60),
		VolList:              volumeSpec,
		AppSources:           appSourceSpec,
	}

	// create standalone custom resource
	standalone := &enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: enterpriseApi.StandaloneSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				Volumes: []corev1.Volume{},
			},
			AppFrameworkConfig: appFrameworkSpec,
		},
		Status: enterpriseApi.StandaloneStatus{
			ReadyReplicas: 2,
		},
	}

	replicas := int32(3)
	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-standalone",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "splunk",
							Image: "splunk/splunk:latest",
							Env: []corev1.EnvVar{
								{
									Name:  "test",
									Value: "test",
								},
							},
						},
					},
				},
			},
			Replicas: &replicas,
		},
	}

	// simulate create stateful set
	c.Create(ctx, statefulset)

	// simulate create standalone instance before reconciliation
	c.Create(ctx, standalone)

	// call reconciliation
	_, err := ApplyStandalone(ctx, c, standalone)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for standalone with app framework  %v", err)
		debug.PrintStack()
	}
}

func TestStandaloneWithReadyState(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	// Initialize the global resource tracker to allow app framework to run
	enterprise.InitGlobalResourceTracker()

	// Create temporary directory for app framework operations
	newpath := filepath.Join("/tmp", "appframework")
	_ = os.MkdirAll(newpath, os.ModePerm)
	defer os.RemoveAll(newpath)

	// Create app download directory required by app framework
	err := os.MkdirAll(splcommon.AppDownloadVolume, 0755)
	if err != nil {
		t.Fatalf("Unable to create download directory for apps: %s", splcommon.AppDownloadVolume)
	}
	defer os.RemoveAll(splcommon.AppDownloadVolume)

	// Mock enterprise.GetAppsList to return empty list (no apps to download)
	savedGetAppsList := enterprise.GetAppsList
	enterprise.GetAppsList = func(ctx context.Context, remoteDataClientMgr enterprise.RemoteDataClientManager) (splcommon.RemoteDataListResponse, error) {
		RemoteDataListResponse := splcommon.RemoteDataListResponse{}
		return RemoteDataListResponse, nil
	}
	defer func() { enterprise.GetAppsList = savedGetAppsList }()

	// Mock GetPodExecClient to return a mock client that simulates pod operations locally
	savedGetPodExecClient := splutil.GetPodExecClient
	splutil.GetPodExecClient = func(client splcommon.ControllerClient, cr splcommon.MetaObject, targetPodName string) splutil.PodExecClientImpl {
		mockClient := &spltest.MockPodExecClient{
			Client:        client,
			Cr:            cr,
			TargetPodName: targetPodName,
		}
		// Add mock responses for common commands
		ctx := context.TODO()
		// Mock mkdir command (used by createDirOnSplunkPods)
		mockClient.AddMockPodExecReturnContext(ctx, "mkdir -p", &spltest.MockPodExecReturnContext{
			StdOut: "",
			StdErr: "",
			Err:    nil,
		})
		return mockClient
	}
	defer func() { splutil.GetPodExecClient = savedGetPodExecClient }()

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.LicenseManager{}).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.Standalone{})
	c := builder.Build()
	ctx := context.TODO()

	// Create App framework volume
	volumeSpec := []enterpriseApi.VolumeSpec{
		{
			Name:      "testing",
			Endpoint:  "/someendpoint",
			Path:      "s3-test",
			SecretRef: "secretRef",
			Provider:  "aws",
			Type:      "s3",
			Region:    "west",
		},
	}

	// AppSourceDefaultSpec: Remote Storage volume name and Scope of App deployment
	appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
		VolName: "testing",
		Scope:   "local",
	}

	// appSourceSpec: App source name, location and volume name and scope from appSourceDefaultSpec
	appSourceSpec := []enterpriseApi.AppSourceSpec{
		{
			Name:                 "appSourceName",
			Location:             "appSourceLocation",
			AppSourceDefaultSpec: appSourceDefaultSpec,
		},
	}

	// appFrameworkSpec: AppSource settings, Poll Interval, volumes, appSources on volumes
	appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
		Defaults:             appSourceDefaultSpec,
		AppsRepoPollInterval: int64(60),
		VolList:              volumeSpec,
		AppSources:           appSourceSpec,
	}

	// create standalone custom resource
	standalone := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: enterpriseApi.StandaloneSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				Volumes: []corev1.Volume{},
				MonitoringConsoleRef: corev1.ObjectReference{
					Name: "mcName",
				},
			},
			AppFrameworkConfig: appFrameworkSpec,
			Replicas:           1,
		},
		Status: enterpriseApi.StandaloneStatus{
			ReadyReplicas: 1,
		},
	}

	replicas := int32(1)
	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-standalone",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "splunk",
							Image: "splunk/splunk:latest",
							Env: []corev1.EnvVar{
								{
									Name:  "test",
									Value: "test",
								},
							},
						},
					},
				},
			},
			Replicas: &replicas,
		},
	}

	// simulate create stateful set
	c.Create(ctx, statefulset)

	// simulate create standalone instance before reconciliation
	c.Create(ctx, &standalone)

	_, err = ApplyStandalone(ctx, c, &standalone)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for standalone with app framework  %v", err)
		debug.PrintStack()
	}
	namespacedName := types.NamespacedName{
		Name:      standalone.Name,
		Namespace: standalone.Namespace,
	}
	err = c.Get(ctx, namespacedName, &standalone)
	if err != nil {
		t.Errorf("Unexpected get standalone. Error=%v", err)
		debug.PrintStack()
	}
	// simulate Ready state
	standalone.Status.Phase = enterpriseApi.PhaseReady
	standalone.Spec.ServiceTemplate.Annotations = map[string]string{
		"traffic.sidecar.istio.io/excludeOutboundPorts": "8089,8191,9997",
		"traffic.sidecar.istio.io/includeInboundPorts":  "8000,8088",
	}
	standalone.Spec.ServiceTemplate.Labels = map[string]string{
		"app.kubernetes.io/instance":   "splunk-test-standalone",
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "standalone",
		"app.kubernetes.io/name":       "standalone",
		"app.kubernetes.io/part-of":    "splunk-test-standalone",
	}
	err = c.Status().Update(ctx, &standalone)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for standalone with app framework  %v", err)
		debug.PrintStack()
	}

	err = c.Get(ctx, namespacedName, &standalone)
	if err != nil {
		t.Errorf("Unexpected get standalone %v", err)
		debug.PrintStack()
	}

	// call reconciliation
	_, err = ApplyStandalone(ctx, c, &standalone)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for standalone with app framework  %v", err)
		debug.PrintStack()
	}

	// create pod
	stpod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-standalone-0",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "splunk",
					Image: "splunk/splunk:latest",
					Env: []corev1.EnvVar{
						{
							Name:  "test",
							Value: "test",
						},
					},
				},
			},
		},
	}
	// simulate create stateful set
	c.Create(ctx, stpod)
	if err != nil {
		t.Errorf("Unexpected create pod failed %v", err)
		debug.PrintStack()
	}

	// update statefulset
	stpod.Status.Phase = corev1.PodRunning
	stpod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Image: "splunk/splunk:latest",
			Name:  "splunk",
			Ready: true,
		},
	}
	err = c.Status().Update(ctx, stpod)
	if err != nil {
		t.Errorf("Unexpected update statefulset  %v", err)
		debug.PrintStack()
	}

	stNamespacedName := types.NamespacedName{
		Name:      "splunk-test-standalone",
		Namespace: "default",
	}
	err = c.Get(ctx, stNamespacedName, statefulset)
	if err != nil {
		t.Errorf("Unexpected get standalone %v", err)
		debug.PrintStack()
	}
	// update statefulset
	statefulset.Status.ReadyReplicas = 1
	statefulset.Status.Replicas = 1
	err = c.Status().Update(ctx, statefulset)
	if err != nil {
		t.Errorf("Unexpected update statefulset  %v", err)
		debug.PrintStack()
	}

	err = c.Get(ctx, namespacedName, &standalone)
	if err != nil {
		t.Errorf("Unexpected get standalone %v", err)
		debug.PrintStack()
	}

	//create namespace MC statefulset
	current := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-default-monitoring-console",
			Namespace: "default",
		},
	}
	namespacedName = types.NamespacedName{Namespace: "default", Name: "splunk-default-monitoring-console"}

	// Create MC statefulset
	err = splutil.CreateResource(ctx, c, &current)
	if err != nil {
		t.Errorf("Failed to create owner reference  %s", current.GetName())
	}

	//setownerReference
	err = k8sops.SetStatefulSetOwnerRef(ctx, c, &standalone, namespacedName)
	if err != nil {
		t.Errorf("Couldn't set owner ref for resource %s", current.GetName())
	}

	err = c.Get(ctx, namespacedName, &current)
	if err != nil {
		t.Errorf("Couldn't get the statefulset resource %s", current.GetName())
	}

	configmap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-default-monitoring-console",
			Namespace: "default",
		},
	}

	// Create configmap
	err = splutil.CreateResource(ctx, c, &configmap)
	if err != nil {
		t.Errorf("Failed to create resource  %s", current.GetName())
	}

	// Mock the telapp.AddTelApp function for unit tests
	savedAddTelApp := telapp.AddTelApp
	defer func() { telapp.AddTelApp = savedAddTelApp }()
	telapp.AddTelApp = func(ctx context.Context, podExecClient splutil.PodExecClientImpl, replicas int32, cr splcommon.MetaObject) error {
		return nil
	}

	// call reconciliation
	_, err = ApplyStandalone(ctx, c, &standalone)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for standalone with app framework  %v", err)
		debug.PrintStack()
	}
}

func TestSmartstoreApplyStandaloneFailsOnInvalidSmartStoreConfig(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "standalone",
			Namespace: "test",
		},
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: 1,
			SmartStore: enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "", Path: "testbucket-rs-london"},
				},
				IndexList: []enterpriseApi.IndexSpec{
					{Name: "salesdata1",
						IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
							VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata2", RemotePath: "salesdata2"},
					{Name: "salesdata3", RemotePath: ""},
				},
			},
		},
	}

	client := spltest.NewMockClient()

	_, err := ApplyStandalone(context.Background(), client, &cr)
	// validateSmartstoreSpec is called inside ValidateStandaloneSpec — stalled, returns terminal error
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("stalled spec validation failure should return a terminal error, got %v", err)
	}
}

func TestConfigMapVolAnnotationStamped(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.StandaloneSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Volumes: []corev1.Volume{
					{
						Name: "my-defaults",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "my-defaults-cm",
								},
							},
						},
					},
				},
			},
		},
	}

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	require.NoError(t, err)

	// Pre-create the ConfigMap so GetConfigMapDataHash can find it.
	cmData := map[string]string{"default.yml": "splunk:\n  conf: value1"}
	cm := k8sops.PrepareConfigMap("my-defaults-cm", "test", cmData)
	err = splutil.CreateResource(ctx, c, cm)
	require.NoError(t, err)

	// Build the StatefulSet — this calls updateSplunkPodTemplateWithConfig internally
	if err := ValidateStandaloneSpec(ctx, c, &cr); err != nil {
		t.Fatalf("ValidateStandaloneSpec() error: %v", err)
	}
	ss, err := GetStandaloneStatefulSet(ctx, c, &cr)
	require.NoError(t, err)

	annotations := ss.Spec.Template.ObjectMeta.Annotations
	annotationKey := splcommon.ConfigMapRevAnnotationPrefix + "my-defaults"
	hash, ok := annotations[annotationKey]
	if !ok {
		t.Errorf("expected annotation %q to be present on pod template, got annotations: %v", annotationKey, annotations)
	}
	if hash == "" {
		t.Errorf("expected annotation %q to be non-empty, got empty string", annotationKey)
	}
	// Verify the hash is stable: same data must produce the same hash.
	hash2, err := k8sops.GetConfigMapDataHash(ctx, c, types.NamespacedName{Namespace: "test", Name: "my-defaults-cm"}, nil)
	require.NoError(t, err)
	if hash != hash2 {
		t.Errorf("annotation hash %q does not match expected data hash %q", hash, hash2)
	}
}

func TestConfigMapVolAnnotationAbsentWhenNoVolumes(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	require.NoError(t, err)

	if err := ValidateStandaloneSpec(ctx, c, &cr); err != nil {
		t.Fatalf("ValidateStandaloneSpec() error: %v", err)
	}
	ss, err := GetStandaloneStatefulSet(ctx, c, &cr)
	require.NoError(t, err)

	for k := range ss.Spec.Template.ObjectMeta.Annotations {
		if strings.HasPrefix(k, splcommon.ConfigMapRevAnnotationPrefix) {
			t.Errorf("unexpected configmaprev annotation %q on pod template with no ConfigMap volumes", k)
		}
	}
}

func TestConfigMapVolAnnotationMultipleVolumes(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.StandaloneSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Volumes: []corev1.Volume{
					{
						Name: "cm-vol-a",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "cm-a"},
							},
						},
					},
					{
						Name: "cm-vol-b",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "cm-b"},
							},
						},
					},
					{
						// Secret volume — should not produce a ConfigMapRevAnnotationPrefix annotation
						Name: "secret-vol",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{SecretName: "my-secret"},
						},
					},
				},
			},
		},
	}

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	require.NoError(t, err)

	for _, name := range []string{"cm-a", "cm-b"} {
		cm := k8sops.PrepareConfigMap(name, "test", map[string]string{"default.yml": "val"})
		require.NoError(t, splutil.CreateResource(ctx, c, cm))
	}

	if err := ValidateStandaloneSpec(ctx, c, &cr); err != nil {
		t.Fatalf("ValidateStandaloneSpec() error: %v", err)
	}
	ss, err := GetStandaloneStatefulSet(ctx, c, &cr)
	require.NoError(t, err)

	annotations := ss.Spec.Template.ObjectMeta.Annotations
	// Annotation key uses volume name (not ConfigMap name) as suffix.
	for _, volName := range []string{"cm-vol-a", "cm-vol-b"} {
		key := splcommon.ConfigMapRevAnnotationPrefix + volName
		if _, ok := annotations[key]; !ok {
			t.Errorf("expected annotation %q missing from pod template annotations: %v", key, annotations)
		}
	}
	if _, ok := annotations[splcommon.ConfigMapRevAnnotationPrefix+"secret-vol"]; ok {
		t.Error("unexpected configmaprev annotation for secret volume")
	}
}

func TestProjectedConfigMapAnnotationLongVolName(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	// 63-char volume name: appending ".0" would produce 65 chars — triggers the hash path.
	longVolName := strings.Repeat("a", 63)

	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.StandaloneSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Volumes: []corev1.Volume{
					{
						Name: longVolName,
						VolumeSource: corev1.VolumeSource{
							Projected: &corev1.ProjectedVolumeSource{
								Sources: []corev1.VolumeProjection{
									{
										ConfigMap: &corev1.ConfigMapProjection{
											LocalObjectReference: corev1.LocalObjectReference{Name: "proj-cm"},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	require.NoError(t, err)

	cm := k8sops.PrepareConfigMap("proj-cm", "test", map[string]string{"key": "val"})
	require.NoError(t, splutil.CreateResource(ctx, c, cm))

	if err := ValidateStandaloneSpec(ctx, c, &cr); err != nil {
		t.Fatalf("ValidateStandaloneSpec() error: %v", err)
	}
	ss, err := GetStandaloneStatefulSet(ctx, c, &cr)
	require.NoError(t, err)

	annotations := ss.Spec.Template.ObjectMeta.Annotations

	// The annotation key must use the "p.<hash>.0" form, not the raw long name.
	sum := sha256.Sum256([]byte(longVolName))
	expectedKey := splcommon.ConfigMapRevAnnotationPrefix + "p." + hex.EncodeToString(sum[:])[:8] + ".0"
	if _, ok := annotations[expectedKey]; !ok {
		t.Errorf("expected annotation %q for long projected vol name, got annotations: %v", expectedKey, annotations)
	}

	// Ensure the raw long name does NOT appear as an annotation suffix (collision guard).
	rawKey := splcommon.ConfigMapRevAnnotationPrefix + longVolName + ".0"
	if _, ok := annotations[rawKey]; ok {
		t.Errorf("raw long-name annotation %q must not be present (would exceed 63-char limit)", rawKey)
	}
}

func TestConfigMapVolAnnotationOptOut(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.StandaloneSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Volumes: []corev1.Volume{
					{
						Name: "app-config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "app-config-cm",
								},
							},
						},
					},
				},
			},
		},
	}

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	require.NoError(t, err)

	// ConfigMap opts out of operator-triggered restarts.
	cm := k8sops.PrepareConfigMap("app-config-cm", "test", map[string]string{"config.json": `{"key":"value"}`})
	cm.Annotations = map[string]string{
		splcommon.ConfigMapRestartOptOutAnnotation: "false",
	}
	require.NoError(t, splutil.CreateResource(ctx, c, cm))

	if err := ValidateStandaloneSpec(ctx, c, &cr); err != nil {
		t.Fatalf("ValidateStandaloneSpec() error: %v", err)
	}
	ss, err := GetStandaloneStatefulSet(ctx, c, &cr)
	require.NoError(t, err)

	annotationKey := splcommon.ConfigMapRevAnnotationPrefix + "app-config"
	if _, ok := ss.Spec.Template.ObjectMeta.Annotations[annotationKey]; ok {
		t.Errorf("annotation %q must not be present when ConfigMap opts out of restart", annotationKey)
	}
}

func TestConfigMapVolAnnotationOptOutProjected(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.StandaloneSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Volumes: []corev1.Volume{
					{
						Name: "proj-vol",
						VolumeSource: corev1.VolumeSource{
							Projected: &corev1.ProjectedVolumeSource{
								Sources: []corev1.VolumeProjection{
									{
										ConfigMap: &corev1.ConfigMapProjection{
											LocalObjectReference: corev1.LocalObjectReference{Name: "proj-cm-opt-out"},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	require.NoError(t, err)

	cm := k8sops.PrepareConfigMap("proj-cm-opt-out", "test", map[string]string{"sidecar.conf": "reload=true"})
	cm.Annotations = map[string]string{
		splcommon.ConfigMapRestartOptOutAnnotation: "false",
	}
	require.NoError(t, splutil.CreateResource(ctx, c, cm))

	if err := ValidateStandaloneSpec(ctx, c, &cr); err != nil {
		t.Fatalf("ValidateStandaloneSpec() error: %v", err)
	}
	ss, err := GetStandaloneStatefulSet(ctx, c, &cr)
	require.NoError(t, err)

	for k := range ss.Spec.Template.ObjectMeta.Annotations {
		if strings.HasPrefix(k, splcommon.ConfigMapRevAnnotationPrefix) {
			t.Errorf("unexpected restart annotation %q present when projected ConfigMap opted out", k)
		}
	}
}

func splunkDeletionTester(t *testing.T, cr splcommon.MetaObject, delete func(splcommon.MetaObject, splcommon.ControllerClient) (bool, error)) {
	var component string
	switch cr.GetObjectKind().GroupVersionKind().Kind {
	case "Standalone":
		component = "standalone"
	case "LicenseManager":
		component = "license-manager"
	case "LicenseMaster":
		component = "license-master"
	case "SearchHeadCluster":
		component = "search-head"
	case "IndexerCluster":
		component = "indexer"
	case "ClusterManager":
		component = "cluster-manager"
	case "ClusterMaster":
		component = "cluster-master"
	case "MonitoringConsole":
		component = "monitoring-console"
	case "IngestorCluster":
		component = "ingestor"
	}

	labelsB := map[string]string{
		"app.kubernetes.io/instance": fmt.Sprintf("splunk-%s-%s", cr.GetName(), component),
	}

	listOptsB := []client.ListOption{
		client.InNamespace(cr.GetNamespace()),
		client.MatchingLabels(labelsB),
	}

	pvclist := corev1.PersistentVolumeClaimList{
		Items: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "splunk-pvc-stack1-var",
					Namespace: "test",
				},
			},
		},
	}
	mockCalls := make(map[string][]spltest.MockFuncCall)
	wantDeleted := false
	if cr.GetObjectMeta().GetDeletionTimestamp() != nil {
		wantDeleted = true
		apiVersion, _ := schema.ParseGroupVersion(enterpriseApi.APIVersion)
		if component == "cluster-master" || component == "license-master" {
			apiVersion, _ = schema.ParseGroupVersion("enterprise.splunk.com/v3")
		}
		mockCalls["Update"] = []spltest.MockFuncCall{
			{MetaName: fmt.Sprintf("*%s.%s-%s-%s", apiVersion.Version, cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName())},
		}
		if cr.GetObjectKind().GroupVersionKind().Kind != "IndexerCluster" {
			mockCalls["Update"] = []spltest.MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: fmt.Sprintf("*%s.%s-%s-%s", apiVersion.Version, cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName())},
			}
			mockCalls["Delete"] = []spltest.MockFuncCall{
				{MetaName: "*v1.PersistentVolumeClaim-test-splunk-pvc-stack1-var"},
			}
			mockCalls["List"] = []spltest.MockFuncCall{
				{ListOpts: listOptsB},
			}
			// account for extra calls in the shc case due to the deployer
			if component == "search-head" {
				labelsC := map[string]string{
					"app.kubernetes.io/instance": fmt.Sprintf("splunk-%s-%s", cr.GetName(), "deployer"),
				}
				listOptsC := []client.ListOption{
					client.InNamespace(cr.GetNamespace()),
					client.MatchingLabels(labelsC),
				}
				mockCalls["Delete"] = append(mockCalls["Delete"], spltest.MockFuncCall{MetaName: "*v1.PersistentVolumeClaim-test-splunk-pvc-stack1-var"})
				mockCalls["List"] = append(mockCalls["List"], spltest.MockFuncCall{ListOpts: listOptsC})
			}
			mockCalls["Get"] = []spltest.MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
			}
			mockCalls["Create"] = []spltest.MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
			}
			if component == "monitoring-console" {
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
				}
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
				}
				mockCalls["Update"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: fmt.Sprintf("*%s.%s-%s-%s", apiVersion.Version, cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName())},
				}
				mockCalls["Delete"] = []spltest.MockFuncCall{
					{MetaName: "*v1.PersistentVolumeClaim-test-splunk-pvc-stack1-var"},
				}
			}

			switch cr.GetObjectKind().GroupVersionKind().Kind {
			case "Standalone":
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-standalone-stack1-configmap"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
					{MetaName: "*v4.Standalone-test-stack1"},
					{MetaName: "*v4.Standalone-test-stack1"},
				}
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-standalone-stack1-configmap"},
				}

			case "LicenseMaster":
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-license-master-stack1-configmap"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-license-master"},
					{MetaName: "*v3.LicenseMaster-test-stack1"},
					{MetaName: "*v3.LicenseMaster-test-stack1"},
				}
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-license-master-stack1-configmap"},
				}

			case "LicenseManager":
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-license-manager-stack1-configmap"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-license-manager"},
					{MetaName: "*v4.LicenseManager-test-stack1"},
					{MetaName: "*v4.LicenseManager-test-stack1"},
				}
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-license-manager-stack1-configmap"},
				}

			case "SearchHeadCluster":
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-search-head-stack1-configmap"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
					{MetaName: "*v4.SearchHeadCluster-test-stack1"},
					{MetaName: "*v4.SearchHeadCluster-test-stack1"},
				}
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-search-head-stack1-configmap"},
				}

			case "ClusterMaster":
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-cluster-master-stack1-configmap"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-master"},
					{MetaName: "*v3.ClusterMaster-test-stack1"},
					{MetaName: "*v3.ClusterMaster-test-stack1"},
				}
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-cluster-master-stack1-configmap"},
				}
			case "IndexerCluster":
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-indexer-stack1-configmap"},
				}

			case "ClusterManager":
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-cluster-manager-stack1-configmap"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-manager"},
					{MetaName: "*v4.ClusterManager-test-stack1"},
					{MetaName: "*v4.ClusterManager-test-stack1"},
				}
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-cluster-manager-stack1-configmap"},
				}

				listOptsTest := []client.ListOption{
					client.InNamespace(cr.GetNamespace()),
				}

				mockCalls["List"] = append(mockCalls["List"], []spltest.MockFuncCall{
					{ListOpts: listOptsTest},
					{ListOpts: listOptsTest},
					{ListOpts: listOptsTest},
					{ListOpts: listOptsTest},
				}...)
				mockCalls["List"][0], mockCalls["List"][len(mockCalls["List"])-1] = mockCalls["List"][len(mockCalls["List"])-1], mockCalls["List"][0]
			case "MonitoringConsole":
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-monitoring-console-stack1-configmap"},
					{MetaName: "*v4.MonitoringConsole-test-stack1"},
					{MetaName: "*v4.MonitoringConsole-test-stack1"},
				}
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-monitoring-console-stack1-configmap"},
				}
			}
		} else {
			mockCalls["Update"] = []spltest.MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: fmt.Sprintf("*%s.%s-%s-%s", apiVersion.Version, cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName())},
			}
			mockCalls["Delete"] = []spltest.MockFuncCall{
				{MetaName: "*v1.PersistentVolumeClaim-test-splunk-pvc-stack1-var"},
			}
			mockCalls["List"] = []spltest.MockFuncCall{
				{ListOpts: listOptsB},
			}
			mockCalls["Create"] = []spltest.MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
			}
			mockCalls["Get"] = []spltest.MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v4.ClusterManager-test-manager1"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.StatefulSet-test-splunk-stack1-indexer"},
				{MetaName: "*v4.IndexerCluster-test-stack1"},
				{MetaName: "*v4.IndexerCluster-test-stack1"},
			}
			switch cr.GetObjectKind().GroupVersionKind().Kind {
			case "IndexerCluster":
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-indexer-stack1-configmap"},
				}
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-indexer-stack1-configmap"},
					{MetaName: "*v4.ClusterManager-test-manager1"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-indexer"},
					{MetaName: "*v4.IndexerCluster-test-stack1"},
					{MetaName: "*v4.IndexerCluster-test-stack1"},
				}
			case "IngestorCluster":
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-ingestor-stack1-configmap"},
				}
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-ingestor-stack1-configmap"},
					{MetaName: "*v4.IngestorCluster-test-stack1"},
					{MetaName: "*v4.IngestorCluster-test-stack1"},
				}
			}
		}
	}

	c := spltest.NewMockClient()
	c.ListObj = &pvclist
	var err error
	deleted, err := delete(cr, c)
	if deleted != wantDeleted || err != nil {
		t.Errorf("k8sops.CheckForDeletion() returned %t, %v; want %t, nil", deleted, err, wantDeleted)
	}
	c.CheckCalls(t, "Testk8sops.CheckForDeletion", mockCalls)
}

func configTester(t *testing.T, method string, f func() (interface{}, error), want string) {
	result, err := f()
	if err != nil {
		t.Errorf("%s returned error: %v", method, err)
	}

	// Marshall the result and compare
	marshalAndCompare(t, result, method, want)
}

func marshalAndCompare(t *testing.T, compare interface{}, method string, want string) {
	t.Helper()
	got, err := json.Marshal(compare)
	if err != nil {
		t.Errorf("%s failed to marshall", err)
	}

	require.JSONEq(t, normalizeGeneratedConfigJSON(t, want), normalizeGeneratedConfigJSON(t, string(got)))
}

func normalizeGeneratedConfigJSON(t *testing.T, data string) string {
	t.Helper()

	var value interface{}
	require.NoError(t, json.Unmarshal([]byte(data), &value))
	dropNilCreationTimestamp(value)

	normalized, err := json.Marshal(value)
	require.NoError(t, err)

	return string(normalized)
}

func dropNilCreationTimestamp(value interface{}) {
	switch typed := value.(type) {
	case map[string]interface{}:
		if creationTimestamp, ok := typed["creationTimestamp"]; ok && creationTimestamp == nil {
			delete(typed, "creationTimestamp")
		}
		for _, child := range typed {
			dropNilCreationTimestamp(child)
		}
	case []interface{}:
		for _, child := range typed {
			dropNilCreationTimestamp(child)
		}
	}
}

func loadFixture(t *testing.T, filename string) string {
	t.Helper()
	path := filepath.Join("testdata", "fixtures", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to load fixture %s: %v", filename, err)
	}

	var compactJSON bytes.Buffer
	if err := json.Compact(&compactJSON, data); err != nil {
		t.Fatalf("Failed to compact JSON from fixture %s: %v", filename, err)
	}
	return compactJSON.String()
}

func newFakeClientBuilder(scheme *runtime.Scheme) *fake.ClientBuilder {
	// The controller-runtime v0.24 fake client defaults to a managed-fields
	// tracker, which rejects the uint64 fields used by Splunk CR specs.
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjectTracker(clienttesting.NewObjectTracker(
			scheme,
			serializer.NewCodecFactory(scheme).UniversalDecoder(),
		)).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				err := c.Get(ctx, key, obj, opts...)
				if err != nil {
					return err
				}
				gvk, err := apiutil.GVKForObject(obj, scheme)
				if err == nil {
					obj.GetObjectKind().SetGroupVersionKind(gvk)
				}
				return nil
			},
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				gvk := obj.GetObjectKind().GroupVersionKind()
				err := c.Create(ctx, obj, opts...)
				obj.GetObjectKind().SetGroupVersionKind(gvk)
				return err
			},
		})
}
