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

package testenv

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	wait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// TestCaseEnv represents a namespaced-isolated k8s cluster environment (aka virtual k8s cluster) to run test cases against
type TestCaseEnv struct {
	kubeClient           client.Client
	name                 string
	namespace            string
	serviceAccountName   string
	roleName             string
	roleBindingName      string
	operatorName         string
	operatorImage        string
	splunkImage          string
	initialized          bool
	SkipTeardown         bool
	licenseFilePath      string
	licenseCMName        string
	s3IndexSecret        string
	indexIngestSepSecret string
	Log                  logr.Logger
	cleanupFuncs         []cleanupFunc
	debug                string
	clusterWideOperator  string
	// teardownCtx is an optional parent context for cleanup operations.
	// When set (typically to a Ginkgo SpecContext via SetTeardownContext),
	// per-cleanup deadlines derive from it so that Ginkgo NodeTimeout cancellation
	// propagates cleanly into in-flight Delete/poll calls instead of leaving them
	// orphaned with a Background parent.
	teardownCtx context.Context
}

const maxTestCaseEnvNameLength = 24

// SetTeardownContext sets the parent context used by subsequent Teardown
// cleanup operations. Callers (typically AfterEach blocks) should pass the
// Ginkgo SpecContext so cleanup respects NodeTimeout.
func (testenv *TestCaseEnv) SetTeardownContext(ctx context.Context) {
	testenv.teardownCtx = ctx
}

// cleanupParentCtx returns the parent context for cleanup operations.
// Falls back to context.Background() when SetTeardownContext was not called.
func (testenv *TestCaseEnv) cleanupParentCtx() context.Context {
	if testenv.teardownCtx != nil {
		return testenv.teardownCtx
	}
	return context.Background()
}

// GetKubeClient returns the kube client to talk to kube-apiserver
func (testenv *TestCaseEnv) GetKubeClient() client.Client {
	return testenv.kubeClient
}

// OperatorDeployment returns the (namespace, name) of the active operator
// Deployment. Cluster-wide installs live at splunk-operator/splunk-operator-controller-manager;
// per-testcase installs (-cluster-wide=false) live at splunk-op-<testcase> in
// the testcase namespace.
func (testenv *TestCaseEnv) OperatorDeployment() (namespace, name string) {
	if testenv.clusterWideOperator != "true" {
		return testenv.namespace, testenv.operatorName
	}
	return "splunk-operator", "splunk-operator-controller-manager"
}

// NewDefaultTestCaseEnv creates a default test environment
func NewDefaultTestCaseEnv(kubeClient client.Client, name string) (*TestCaseEnv, error) {
	if os.Getenv("GRAVITON_TESTING") == "true" {
		return NewTestCaseEnv(kubeClient, name, specifiedOperatorImage, os.Getenv("SPLUNK_ENTERPRISE_IMAGE"), specifiedLicenseFilePath)
	} else {
		return NewTestCaseEnv(kubeClient, name, specifiedOperatorImage, specifiedSplunkImage, specifiedLicenseFilePath)
	}
}

// NewTestCaseEnv creates a new test environment to run tests againsts
func NewTestCaseEnv(kubeClient client.Client, name string, operatorImage string, splunkImage string, licenseFilePath string) (*TestCaseEnv, error) {
	// The name are used in various resource label and there is a 63 char limit. Do our part to make sure we do not exceed that limit
	if len(name) > maxTestCaseEnvNameLength {
		return nil, fmt.Errorf("name %s has exceeded 24 chars", name)
	}

	testenv := &TestCaseEnv{
		kubeClient:           kubeClient,
		name:                 name,
		namespace:            name,
		serviceAccountName:   name,
		roleName:             name,
		roleBindingName:      name,
		operatorName:         "splunk-op-" + name,
		operatorImage:        operatorImage,
		splunkImage:          splunkImage,
		SkipTeardown:         specifiedSkipTeardown,
		licenseCMName:        name,
		licenseFilePath:      licenseFilePath,
		s3IndexSecret:        "splunk-s3-index-" + name,
		indexIngestSepSecret: "splunk--index-ingest-sep-" + name,
		debug:                os.Getenv("DEBUG"),
		clusterWideOperator:  installOperatorClusterWide,
	}

	testenv.Log = logf.Log.WithValues("testcaseenv", testenv.name)

	if err := testenv.setup(); err != nil {
		// teardown() should still be invoked
		return nil, err
	}

	return testenv, nil
}

// GetName returns the name of the testenv
func (testenv *TestCaseEnv) GetName() string {
	return testenv.name
}

// GetName returns the Splunk image of the testenv
func (testenv *TestCaseEnv) GetSplunkImage() string {
	return testenv.splunkImage
}

func (testenv *TestCaseEnv) setup() error {
	testenv.Log.Info("testenv initializing.\n")

	var err error
	err = testenv.createNamespace()
	if err != nil {
		return err
	}

	err = testenv.createSA()
	if err != nil {
		return err
	}

	if installOperatorClusterWide != "true" {
		err = testenv.createRole()
		if err != nil {
			return err
		}

		err = testenv.createRoleBinding()
		if err != nil {
			return err
		}

		err = testenv.createOperator()
		if err != nil {
			return err
		}
	}

	// Create secret object for index test
	switch ClusterProvider {
	case "eks":
		testenv.createIndexSecret()
		testenv.createIndexIngestSepSecret()
	case "azure":
		testenv.createIndexSecretAzure()
	case "gcp":
		testenv.createIndexSecretGCP()
	default:
		testenv.Log.Info("Failed to create secret object")
	}

	if testenv.licenseFilePath != "" {
		err = testenv.createLicenseConfigMap()
		if err != nil {
			return err
		}
	}
	testenv.initialized = true
	testenv.Log.Info("testenv initialized.\n", "namespace", testenv.namespace)
	return nil
}

// Teardown cleanup the resources use in this testenv
func (testenv *TestCaseEnv) Teardown() error {

	if testenv.SkipTeardown && testenv.debug == "True" {
		testenv.Log.Info("testenv teardown is skipped!\n")
		return nil
	}

	testenv.initialized = false

	for fn, err := testenv.popCleanupFunc(); err == nil; fn, err = testenv.popCleanupFunc() {
		cleanupErr := fn()
		if cleanupErr != nil {
			testenv.Log.Error(cleanupErr, "CleanupFunc returns an error. Attempt to continue.\n")
		}
	}

	testenv.Log.Info("testenv deleted.\n")
	return nil
}

func (testenv *TestCaseEnv) pushCleanupFunc(fn cleanupFunc) {
	testenv.cleanupFuncs = append(testenv.cleanupFuncs, fn)
}

func (testenv *TestCaseEnv) popCleanupFunc() (cleanupFunc, error) {
	if len(testenv.cleanupFuncs) == 0 {
		return nil, fmt.Errorf("cleanupFuncs is empty")
	}

	fn := testenv.cleanupFuncs[len(testenv.cleanupFuncs)-1]
	testenv.cleanupFuncs = testenv.cleanupFuncs[:len(testenv.cleanupFuncs)-1]

	return fn, nil
}

func (testenv *TestCaseEnv) createNamespace() error {
	ctx := context.Background()

	labels := map[string]string{}
	if specifiedJobID != "" {
		// Job-level label — matches all namespaces for this CI job, used by the
		// post-job bulk cleanup in int-test-workflow.sh.
		labels[SokSmokeJobLabel] = specifiedJobID
		// Suite-level label — value embeds the testenv name (which contains the
		// suite slug, e.g. "4d9f-s1appfw-xyz") so parallel specs from different
		// suites within the same job get distinct values and a targeted cleanup
		// cannot inadvertently delete sibling suite namespaces.
		labels[SokSmokeSuiteLabel] = testenv.name + "-" + specifiedJobID
	}
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   testenv.namespace,
			Labels: labels,
		},
	}

	err := testenv.GetKubeClient().Create(ctx, namespace)
	if err != nil {
		return err
	}

	// Cleanup the namespace when we teardown this testenv.
	testenv.pushCleanupFunc(func() error {
		// Reserve a grace fraction of SetupTeardownTimeout so the surrounding
		// AfterEach (NodeTimeout = SetupTeardownTimeout) still has budget left
		// to run the rest of the cleanup stack and report cleanly even when a
		// namespace gets stuck in Terminating. See timeouts.go.
		cleanupCtx, cleanupCancel := context.WithTimeout(testenv.cleanupParentCtx(), time.Duration(float64(SetupTeardownTimeout)*CleanupGraceFraction))
		defer cleanupCancel()
		if err := testenv.GetKubeClient().Delete(cleanupCtx, namespace); err != nil {
			testenv.Log.Error(err, "Unable to delete namespace")
			return err
		}
		if err := wait.PollUntilContextCancel(cleanupCtx, PollInterval, true, func(ctx context.Context) (bool, error) {
			key := client.ObjectKey{Name: testenv.namespace}
			ns := &corev1.Namespace{}
			err := testenv.GetKubeClient().Get(ctx, key, ns)
			if errors.IsNotFound(err) {
				return true, nil
			}
			if err != nil {
				return false, err
			}
			if ns.Status.Phase == corev1.NamespaceTerminating {
				return false, nil
			}
			return true, nil
		}); err != nil {
			testenv.Log.Error(err, "Namespace did not finish terminating within cleanup budget; continuing teardown", "namespace", testenv.namespace)
			return err
		}
		return nil
	})

	if err := wait.PollUntilContextTimeout(ctx, PollInterval, DefaultTimeout, true, func(ctx context.Context) (bool, error) {
		key := client.ObjectKey{Name: testenv.namespace}
		ns := &corev1.Namespace{}
		err := testenv.GetKubeClient().Get(ctx, key, ns)
		if err != nil {
			// Try again
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		if ns.Status.Phase == corev1.NamespaceActive {
			return true, nil
		}

		return false, nil
	}); err != nil {
		testenv.Log.Error(err, "Unable to get namespace")
		return err
	}

	return nil
}

func (testenv *TestCaseEnv) createSA() error {
	ctx := context.Background()
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testenv.serviceAccountName,
			Namespace: testenv.namespace,
		},
	}

	err := testenv.GetKubeClient().Create(ctx, sa)
	if err != nil {
		testenv.Log.Error(err, "Unable to create service account")
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(testenv.cleanupParentCtx(), sa)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete service account")
			return err
		}
		return nil
	})

	return nil
}

func (testenv *TestCaseEnv) createRole() error {
	ctx := context.Background()
	role := newRole(testenv.roleName, testenv.namespace)

	err := testenv.GetKubeClient().Create(ctx, role)
	if err != nil {
		testenv.Log.Error(err, "Unable to create role")
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(testenv.cleanupParentCtx(), role)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete role")
			return err
		}
		return nil
	})

	return nil
}

func (testenv *TestCaseEnv) createRoleBinding() error {
	ctx := context.Background()
	binding := newRoleBinding(testenv.roleBindingName, testenv.serviceAccountName, testenv.namespace, testenv.roleName)

	err := testenv.GetKubeClient().Create(ctx, binding)
	if err != nil {
		testenv.Log.Error(err, "Unable to create rolebinding")
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(testenv.cleanupParentCtx(), binding)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete rolebinding")
			return err
		}
		return nil
	})

	return nil
}

func (testenv *TestCaseEnv) attachPVCToOperator(name string) error {
	ctx := context.Background()

	// volume name which refers to PVC to be attached
	volumeName := "app-staging"

	namespacedName := client.ObjectKey{Name: testenv.operatorName, Namespace: testenv.namespace}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		operator := &appsv1.Deployment{}
		err := testenv.GetKubeClient().Get(ctx, namespacedName, operator)
		if err != nil {
			testenv.Log.Error(err, "Unable to get operator", "operatorName", testenv.operatorName)
			return err
		}

		foundVolume := false
		for _, volume := range operator.Spec.Template.Spec.Volumes {
			if volume.Name == volumeName {
				foundVolume = true
				break
			}
		}
		if !foundVolume {
			volume := corev1.Volume{
				Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: name,
					},
				},
			}
			operator.Spec.Template.Spec.Volumes = append(operator.Spec.Template.Spec.Volumes, volume)
		}

		foundVolumeMount := false
		for _, volumeMount := range operator.Spec.Template.Spec.Containers[0].VolumeMounts {
			if volumeMount.Name == volumeName {
				foundVolumeMount = true
				break
			}
		}
		if !foundVolumeMount {
			volumeMount := corev1.VolumeMount{
				Name:      volumeName,
				MountPath: splcommon.AppDownloadVolume,
			}
			operator.Spec.Template.Spec.Containers[0].VolumeMounts = append(operator.Spec.Template.Spec.Containers[0].VolumeMounts, volumeMount)
		}

		// update the operator deployment now
		return testenv.GetKubeClient().Update(ctx, operator)
	})
	if err != nil {
		testenv.Log.Error(err, "Unable to update operator", "operatorName", testenv.operatorName)
		return err
	}

	return err
}

func (testenv *TestCaseEnv) createOperator() error {
	ctx := context.Background()
	//op := newOperator(testenv.operatorName, testenv.namespace, testenv.serviceAccountName, testenv.operatorImage, testenv.splunkImage, "nil")
	op := newOperator(testenv.operatorName, testenv.namespace, testenv.serviceAccountName, testenv.operatorImage, testenv.splunkImage)
	err := testenv.GetKubeClient().Create(ctx, op)
	if err != nil {
		testenv.Log.Error(err, "Unable to create operator")
		return err
	}

	// create the PVC to attach to operator for downloading apps
	pvc, err := newPVC(appDownlodPVCName, testenv.namespace, DefaultStorageForAppDownloads, DefaultStorageClassName)
	if err != nil {
		testenv.Log.Error(err, "Unable to create PVC", "pvcName", pvc.ObjectMeta.Name)
		return err
	}
	err = testenv.GetKubeClient().Create(ctx, pvc)
	if err != nil {
		testenv.Log.Error(err, "Unable to create PVC")
		return err
	}

	//attach the PVC to operator
	err = testenv.attachPVCToOperator(pvc.ObjectMeta.Name)
	if err != nil {
		testenv.Log.Error(err, "Unable to attach PVC to operator", "pvcName", pvc.ObjectMeta.Name)
		return err
	}

	testenv.pushCleanupFunc(func() error {
		// Bound the wait so a stuck operator pod doesn't starve later cleanup
		// steps (notably namespace deletion) of their grace budget.
		cleanupCtx, cleanupCancel := context.WithTimeout(testenv.cleanupParentCtx(), time.Duration(float64(SetupTeardownTimeout)*CleanupGraceFraction))
		defer cleanupCancel()
		if err := testenv.GetKubeClient().Delete(cleanupCtx, op); err != nil && !errors.IsNotFound(err) {
			testenv.Log.Error(err, "Unable to delete operator")
			return err
		}
		// Wait for the operator Deployment to be fully gone before returning so
		// the subsequent namespace cleanup does not race with operator pod
		// termination (which is required for CR finalizers to have been
		// processed prior to this point).
		if err := wait.PollUntilContextCancel(cleanupCtx, PollInterval, true, func(ctx context.Context) (bool, error) {
			key := client.ObjectKey{Name: testenv.operatorName, Namespace: testenv.namespace}
			dep := &appsv1.Deployment{}
			err := testenv.GetKubeClient().Get(ctx, key, dep)
			if errors.IsNotFound(err) {
				return true, nil
			}
			if err != nil {
				return false, err
			}
			return false, nil
		}); err != nil {
			testenv.Log.Error(err, "Operator deployment did not finish deleting within cleanup budget; continuing teardown", "operator", testenv.operatorName)
			return err
		}
		return nil
	})

	if err := wait.PollUntilContextTimeout(ctx, PollInterval, DefaultTimeout, true, func(ctx context.Context) (bool, error) {
		key := client.ObjectKey{Name: testenv.operatorName, Namespace: testenv.namespace}
		deployment := &appsv1.Deployment{}
		err := testenv.GetKubeClient().Get(ctx, key, deployment)
		if err != nil {
			testenv.Log.Error(err, "operator not found waiting")
			return false, nil
		}

		DumpGetPods(testenv.namespace)
		if deployment.Status.UpdatedReplicas < deployment.Status.Replicas {
			return false, nil
		}

		if deployment.Status.ReadyReplicas < *op.Spec.Replicas {
			return false, nil
		}

		return true, nil
	}); err != nil {
		testenv.Log.Error(err, "Unable to find operator after creation")
		return err
	}
	return nil
}

// CreateLicenseConfigMap sets the license file path and create config map.
// Required if license file path is not present during TestCaseEnv initialization
func (testenv *TestCaseEnv) CreateLicenseConfigMap(path string) error {
	testenv.licenseFilePath = path
	err := testenv.createLicenseConfigMap()
	return err
}

func (testenv *TestCaseEnv) createLicenseConfigMap() error {
	ctx := context.Background()
	lic, err := newLicenseConfigMap(testenv.licenseCMName, testenv.namespace, testenv.licenseFilePath)
	if err != nil {
		return err
	}

	// Check if config map already exists
	key := client.ObjectKey{Name: testenv.namespace, Namespace: testenv.namespace}
	err = testenv.GetKubeClient().Get(ctx, key, lic)

	if err != nil {
		testenv.Log.Info("No Existing license config map not found. Creating a new License Configmap", "Name", testenv.namespace)
	} else {
		testenv.Log.Info("Existing license config map found.", "License Config Map Name", testenv.namespace)
		return nil
	}

	// Create a new licese config map
	err = testenv.GetKubeClient().Create(ctx, lic)
	if err != nil {
		testenv.Log.Error(err, "Unable to create license configmap")
		return err
	}

	testenv.Log.Info("New License Config Map created.", "License Config Map Name", testenv.namespace)

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(testenv.cleanupParentCtx(), lic)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete license configmap ")
			return err
		}
		return nil
	})

	return nil
}

// CreateServiceAccount Create a service account with given name
func (testenv *TestCaseEnv) CreateServiceAccount(name string) error {
	ctx := context.Background()
	serviceAccountConfig := newServiceAccount(testenv.namespace, name)
	if err := testenv.GetKubeClient().Create(ctx, serviceAccountConfig); err != nil {
		testenv.Log.Error(err, "Unable to create service account")
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(testenv.cleanupParentCtx(), serviceAccountConfig)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete service account")
			return err
		}
		return nil
	})
	return nil
}

// CreateIndexSecret create secret object
func (testenv *TestCaseEnv) createIndexSecret() error {
	ctx := context.Background()
	secretName := testenv.s3IndexSecret
	ns := testenv.namespace

	accessKey := os.Getenv("TEST_S3_ACCESS_KEY_ID")
	if accessKey == "" {
		accessKey = os.Getenv("AWS_ACCESS_KEY_ID")
	}
	secretKey := os.Getenv("TEST_S3_SECRET_ACCESS_KEY")
	if secretKey == "" {
		secretKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	}

	data := map[string][]byte{"s3_access_key": []byte(accessKey),
		"s3_secret_key": []byte(secretKey)}
	secret := newSecretSpec(ns, secretName, data)
	if err := testenv.GetKubeClient().Create(ctx, secret); err != nil {
		testenv.Log.Error(err, "Unable to create s3 index secret object")
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(testenv.cleanupParentCtx(), secret)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete s3 index secret object")
			return err
		}
		return nil
	})
	return nil
}

// CreateIndexSecret create secret object
func (testenv *TestCaseEnv) createIndexSecretGCP() error {
	ctx := context.Background()
	secretName := testenv.s3IndexSecret
	ns := testenv.namespace
	encodedString := os.Getenv("GCP_SERVICE_ACCOUNT_KEY")
	gcpCredentials, err := base64.StdEncoding.DecodeString(encodedString)
	if err != nil {
		testenv.Log.Error(err, "Unable to decode GCP service account key")
		return err
	}
	data := map[string][]byte{"key.json": []byte(gcpCredentials)}
	secret := newSecretSpec(ns, secretName, data)
	if err := testenv.GetKubeClient().Create(ctx, secret); err != nil {
		testenv.Log.Error(err, "Unable to create GCP index secret object")
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(testenv.cleanupParentCtx(), secret)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete GCP index secret object")
			return err
		}
		return nil
	})
	return nil
}

// createIndexSecretAzure create secret object for Azure
func (testenv *TestCaseEnv) createIndexSecretAzure() error {
	ctx := context.Background()
	secretName := testenv.s3IndexSecret
	ns := testenv.namespace
	data := map[string][]byte{"azure_sa_name": []byte(os.Getenv("STORAGE_ACCOUNT")),
		"azure_sa_secret_key": []byte(os.Getenv("STORAGE_ACCOUNT_KEY"))}
	secret := newSecretSpec(ns, secretName, data)
	if err := testenv.GetKubeClient().Create(ctx, secret); err != nil {
		testenv.Log.Error(err, "Unable to create Azure index secret object")
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(testenv.cleanupParentCtx(), secret)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete Azure index secret object")
			return err
		}
		return nil
	})
	return nil
}

// CreateIndexIngestSepSecret creates secret object
func (testenv *TestCaseEnv) createIndexIngestSepSecret() error {
	ctx := context.Background()
	secretName := testenv.indexIngestSepSecret
	ns := testenv.namespace

	data := map[string][]byte{"s3_access_key": []byte(os.Getenv("AWS_INDEX_INGEST_SEP_ACCESS_KEY_ID")),
		"s3_secret_key": []byte(os.Getenv("AWS_INDEX_INGEST_SEP_SECRET_ACCESS_KEY"))}
	secret := newSecretSpec(ns, secretName, data)

	if err := testenv.GetKubeClient().Create(ctx, secret); err != nil {
		testenv.Log.Error(err, "Unable to create index and ingestion sep secret object")
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(testenv.cleanupParentCtx(), secret)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete index and ingestion sep secret object")
			return err
		}
		return nil
	})
	return nil
}

// GetIndexSecretName return index secret object name
func (testenv *TestCaseEnv) GetIndexSecretName() string {
	return testenv.s3IndexSecret
}

// GetIndexSecretName return index and ingestion separation secret object name
func (testenv *TestCaseEnv) GetIndexIngestSepSecretName() string {
	return testenv.indexIngestSepSecret
}

// GetLMConfigMap Return name of license config map
func (testenv *TestCaseEnv) GetLMConfigMap() string {
	return testenv.licenseCMName
}

// NewDeployment creates a new deployment. If timeout is non-nil it overrides
// the default SpecifiedTestTimeout.
func (testenv *TestCaseEnv) NewDeployment(name string, timeout *time.Duration) (*Deployment, error) {
	t := time.Duration(SpecifiedTestTimeout) * time.Second
	if timeout != nil {
		t = *timeout
	}

	d := Deployment{
		name:        testenv.GetName() + "-" + name,
		testenv:     testenv,
		testTimeout: t,
	}

	return &d, nil
}
