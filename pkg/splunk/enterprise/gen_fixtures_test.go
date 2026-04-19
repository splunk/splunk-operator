//go:build gen_fixtures

package enterprise

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func writeFixtureFile(t *testing.T, name string, v interface{}) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal(%s): %v", name, err)
	}
	path := "testdata/fixtures/" + name
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	t.Logf("Wrote fixture: %s", path)
}

func TestGenClusterManagerFixtures(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	if _, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test"); err != nil {
		t.Fatalf("ApplyNamespaceScopedSecretObject: %v", err)
	}

	// base
	if err := validateClusterManagerSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateClusterManagerSpec: %v", err)
	}
	ss, err := getClusterManagerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getClusterManagerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_cluster_manager_base.json", ss)

	// base_1 (with LicenseManagerRef)
	cr.Spec.LicenseManagerRef.Name = "stack1"
	cr.Spec.LicenseManagerRef.Namespace = "test"
	if err := validateClusterManagerSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateClusterManagerSpec: %v", err)
	}
	ss, err = getClusterManagerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getClusterManagerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_cluster_manager_base_1.json", ss)

	// base_2 (with LicenseURL)
	cr.Spec.LicenseManagerRef.Name = ""
	cr.Spec.LicenseURL = "/mnt/splunk.lic"
	if err := validateClusterManagerSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateClusterManagerSpec: %v", err)
	}
	ss, err = getClusterManagerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getClusterManagerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_cluster_manager_base_2.json", ss)

	// with_apps
	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	if err := validateClusterManagerSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateClusterManagerSpec: %v", err)
	}
	ss, err = getClusterManagerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getClusterManagerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_cluster_manager_with_apps.json", ss)

	// with_service_account
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	if err := validateClusterManagerSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateClusterManagerSpec: %v", err)
	}
	ss, err = getClusterManagerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getClusterManagerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_cluster_manager_with_service_account.json", ss)

	// with_service_account_1 (with ExtraEnv)
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	if err := validateClusterManagerSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateClusterManagerSpec: %v", err)
	}
	ss, err = getClusterManagerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getClusterManagerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_cluster_manager_with_service_account_1.json", ss)

	// with_service_account_2 (with extra label)
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	if err := validateClusterManagerSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateClusterManagerSpec: %v", err)
	}
	ss, err = getClusterManagerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getClusterManagerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_cluster_manager_with_service_account_2.json", ss)
}

func TestGenClusterMasterFixtures(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApiV3.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	if _, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test"); err != nil {
		t.Fatalf("ApplyNamespaceScopedSecretObject: %v", err)
	}

	// base
	if err := validateClusterMasterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateClusterMasterSpec: %v", err)
	}
	ss, err := getClusterMasterStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getClusterMasterStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_cluster_master_base.json", ss)

	// base_1 (with LicenseManagerRef)
	cr.Spec.LicenseManagerRef.Name = "stack1"
	cr.Spec.LicenseManagerRef.Namespace = "test"
	if err := validateClusterMasterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateClusterMasterSpec: %v", err)
	}
	ss, err = getClusterMasterStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getClusterMasterStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_cluster_master_base_1.json", ss)

	// base_1_with_apps (LicenseManagerRef + LicenseURL + DefaultsURLApps)
	cr.Spec.LicenseURL = "/mnt/splunk.lic"
	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	if err := validateClusterMasterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateClusterMasterSpec: %v", err)
	}
	ss, err = getClusterMasterStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getClusterMasterStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_cluster_master_base_1_with_apps.json", ss)

	// base_2 (with LicenseURL, no DefaultsURLApps, no LicenseManagerRef)
	cr.Spec.LicenseManagerRef.Name = ""
	cr.Spec.DefaultsURLApps = ""
	cr.Spec.LicenseURL = "/mnt/splunk.lic"
	if err := validateClusterMasterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateClusterMasterSpec: %v", err)
	}
	ss, err = getClusterMasterStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getClusterMasterStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_cluster_master_base_2.json", ss)

	// with_apps
	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	if err := validateClusterMasterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateClusterMasterSpec: %v", err)
	}
	ss, err = getClusterMasterStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getClusterMasterStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_cluster_master_with_apps.json", ss)

	// with_service_account
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	if err := validateClusterMasterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateClusterMasterSpec: %v", err)
	}
	ss, err = getClusterMasterStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getClusterMasterStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_cluster_master_with_service_account.json", ss)

	// with_service_account_1 (with ExtraEnv)
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	if err := validateClusterMasterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateClusterMasterSpec: %v", err)
	}
	ss, err = getClusterMasterStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getClusterMasterStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_cluster_master_with_service_account_1.json", ss)

	// with_service_account_2 (with extra label)
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	if err := validateClusterMasterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateClusterMasterSpec: %v", err)
	}
	ss, err = getClusterMasterStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getClusterMasterStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_cluster_master_with_service_account_2.json", ss)
}

func TestGenLicenseManagerFixtures(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.LicenseManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	if _, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test"); err != nil {
		t.Fatalf("ApplyNamespaceScopedSecretObject: %v", err)
	}

	// base
	if err := validateLicenseManagerSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateLicenseManagerSpec: %v", err)
	}
	ss, err := getLicenseManagerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getLicenseManagerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_license_manager_base.json", ss)

	// base_1 (with LicenseURL)
	cr.Spec.LicenseURL = "/mnt/splunk.lic"
	if err := validateLicenseManagerSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateLicenseManagerSpec: %v", err)
	}
	ss, err = getLicenseManagerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getLicenseManagerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_license_manager_base_1.json", ss)

	// with_apps
	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	if err := validateLicenseManagerSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateLicenseManagerSpec: %v", err)
	}
	ss, err = getLicenseManagerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getLicenseManagerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_license_manager_with_apps.json", ss)

	// with_service_account
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	if err := validateLicenseManagerSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateLicenseManagerSpec: %v", err)
	}
	ss, err = getLicenseManagerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getLicenseManagerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_license_manager_with_service_account.json", ss)

	// with_service_account_1 (with ExtraEnv)
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	if err := validateLicenseManagerSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateLicenseManagerSpec: %v", err)
	}
	ss, err = getLicenseManagerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getLicenseManagerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_license_manager_with_service_account_1.json", ss)

	// with_service_account_2 (with extra label)
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	if err := validateLicenseManagerSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateLicenseManagerSpec: %v", err)
	}
	ss, err = getLicenseManagerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getLicenseManagerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_license_manager_with_service_account_2.json", ss)
}

func TestGenLicenseMasterFixtures(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApiV3.LicenseMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	if _, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test"); err != nil {
		t.Fatalf("ApplyNamespaceScopedSecretObject: %v", err)
	}

	// base
	if err := validateLicenseMasterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateLicenseMasterSpec: %v", err)
	}
	ss, err := getLicenseMasterStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getLicenseMasterStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_license_master_base.json", ss)

	// base_1 (with LicenseURL)
	cr.Spec.LicenseURL = "/mnt/splunk.lic"
	if err := validateLicenseMasterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateLicenseMasterSpec: %v", err)
	}
	ss, err = getLicenseMasterStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getLicenseMasterStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_license_master_base_1.json", ss)

	// with_apps
	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	if err := validateLicenseMasterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateLicenseMasterSpec: %v", err)
	}
	ss, err = getLicenseMasterStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getLicenseMasterStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_license_master_with_apps.json", ss)

	// with_service_account
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	if err := validateLicenseMasterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateLicenseMasterSpec: %v", err)
	}
	ss, err = getLicenseMasterStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getLicenseMasterStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_license_master_with_service_account.json", ss)

	// with_service_account_1 (with ExtraEnv)
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	if err := validateLicenseMasterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateLicenseMasterSpec: %v", err)
	}
	ss, err = getLicenseMasterStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getLicenseMasterStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_license_master_with_service_account_1.json", ss)

	// with_service_account_2 (with extra label)
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	if err := validateLicenseMasterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateLicenseMasterSpec: %v", err)
	}
	ss, err = getLicenseMasterStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getLicenseMasterStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_license_master_with_service_account_2.json", ss)
}

func TestGenMonitoringConsoleFixtures(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.MonitoringConsole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	if _, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test"); err != nil {
		t.Fatalf("ApplyNamespaceScopedSecretObject: %v", err)
	}

	// with_defaults
	if err := validateMonitoringConsoleSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateMonitoringConsoleSpec: %v", err)
	}
	ss, err := getMonitoringConsoleStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getMonitoringConsoleStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_monitoring_console_with_defaults.json", ss)

	// with_apps (EphemeralStorage=true)
	cr.Spec.EtcVolumeStorageConfig.EphemeralStorage = true
	cr.Spec.VarVolumeStorageConfig.EphemeralStorage = true
	if err := validateMonitoringConsoleSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateMonitoringConsoleSpec: %v", err)
	}
	ss, err = getMonitoringConsoleStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getMonitoringConsoleStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_monitoring_console_with_apps.json", ss)

	// with_service_account_2 (set up with defaults and storage)
	cr.Spec.EtcVolumeStorageConfig.EphemeralStorage = false
	cr.Spec.VarVolumeStorageConfig.EphemeralStorage = false
	cr.Spec.ClusterManagerRef.Name = "stack2"
	cr.Spec.EtcVolumeStorageConfig.StorageClassName = "gp2"
	cr.Spec.VarVolumeStorageConfig.StorageClassName = "gp2"
	cr.Spec.SchedulerName = "custom-scheduler"
	cr.Spec.Defaults = "defaults-string"
	cr.Spec.DefaultsURL = "/mnt/defaults/defaults.yml"
	cr.Spec.Volumes = []corev1.Volume{{Name: "defaults"}}
	if err := validateMonitoringConsoleSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateMonitoringConsoleSpec: %v", err)
	}
	ss, err = getMonitoringConsoleStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getMonitoringConsoleStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_monitoring_console_with_service_account_2.json", ss)

	// base (add DefaultsURLApps)
	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	if err := validateMonitoringConsoleSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateMonitoringConsoleSpec: %v", err)
	}
	ss, err = getMonitoringConsoleStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getMonitoringConsoleStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_monitoring_console_base.json", ss)

	// base_1 (with service account)
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	if err := validateMonitoringConsoleSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateMonitoringConsoleSpec: %v", err)
	}
	ss, err = getMonitoringConsoleStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getMonitoringConsoleStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_monitoring_console_base_1.json", ss)

	// with_service_account (add ExtraEnv)
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	if err := validateMonitoringConsoleSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateMonitoringConsoleSpec: %v", err)
	}
	ss, err = getMonitoringConsoleStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getMonitoringConsoleStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_monitoring_console_with_service_account.json", ss)

	// with_service_account_1 (add extra labels)
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	if err := validateMonitoringConsoleSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateMonitoringConsoleSpec: %v", err)
	}
	ss, err = getMonitoringConsoleStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getMonitoringConsoleStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_monitoring_console_with_service_account_1.json", ss)
}

func TestGenSearchHeadFixtures(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	if _, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test"); err != nil {
		t.Fatalf("ApplyNamespaceScopedSecretObject: %v", err)
	}

	// base (Replicas=3)
	cr.Spec.Replicas = 3
	if err := validateSearchHeadClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateSearchHeadClusterSpec: %v", err)
	}
	ss, err := getSearchHeadStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getSearchHeadStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_search_head_base.json", ss)

	// base_1 (Replicas=4)
	cr.Spec.Replicas = 4
	if err := validateSearchHeadClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateSearchHeadClusterSpec: %v", err)
	}
	ss, err = getSearchHeadStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getSearchHeadStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_search_head_base_1.json", ss)

	// base_2 (Replicas=5, with ClusterManagerRef)
	cr.Spec.Replicas = 5
	cr.Spec.ClusterManagerRef.Name = "stack1"
	if err := validateSearchHeadClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateSearchHeadClusterSpec: %v", err)
	}
	ss, err = getSearchHeadStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getSearchHeadStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_search_head_base_2.json", ss)

	// base_2r4 (Replicas=4, with ClusterManagerRef=stack1) - used by inline JSON after base_2
	cr.Spec.Replicas = 4
	if err := validateSearchHeadClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateSearchHeadClusterSpec: %v", err)
	}
	ss, err = getSearchHeadStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getSearchHeadStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_search_head_base_2r4.json", ss)

	// Reset replicas for next fixtures
	cr.Spec.Replicas = 5

	// base_3 (Replicas=6, with ClusterManagerRef and different namespace)
	cr.Spec.Replicas = 6
	cr.Spec.ClusterManagerRef.Namespace = "test2"
	if err := validateSearchHeadClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateSearchHeadClusterSpec: %v", err)
	}
	ss, err = getSearchHeadStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getSearchHeadStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_search_head_base_3.json", ss)

	// base_4 (with DefaultsURLApps)
	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	if err := validateSearchHeadClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateSearchHeadClusterSpec: %v", err)
	}
	ss, err = getSearchHeadStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getSearchHeadStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_search_head_base_4.json", ss)

	// base_5 (same as base_4, but comment in test says "Define additional service port in CR")
	if err := validateSearchHeadClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateSearchHeadClusterSpec: %v", err)
	}
	ss, err = getSearchHeadStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getSearchHeadStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_search_head_base_5.json", ss)

	// with_service_account
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	if err := validateSearchHeadClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateSearchHeadClusterSpec: %v", err)
	}
	ss, err = getSearchHeadStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getSearchHeadStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_search_head_with_service_account.json", ss)

	// with_service_account_1 (with ExtraEnv)
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	if err := validateSearchHeadClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateSearchHeadClusterSpec: %v", err)
	}
	ss, err = getSearchHeadStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getSearchHeadStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_search_head_with_service_account_1.json", ss)

	// with_service_account_2 (with extra labels on CR metadata)
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	if err := validateSearchHeadClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateSearchHeadClusterSpec: %v", err)
	}
	ss, err = getSearchHeadStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getSearchHeadStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_search_head_with_service_account_2.json", ss)
}

func TestGenDeployerFixtures(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	if _, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test"); err != nil {
		t.Fatalf("ApplyNamespaceScopedSecretObject: %v", err)
	}

	// Set up search head cluster for deployer tests
	cr.Spec.Replicas = 3

	// base
	if err := validateSearchHeadClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateSearchHeadClusterSpec: %v", err)
	}
	ss, err := getDeployerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getDeployerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_deployer_base.json", ss)

	// with_apps
	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	if err := validateSearchHeadClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateSearchHeadClusterSpec: %v", err)
	}
	ss, err = getDeployerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getDeployerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_deployer_with_apps.json", ss)

	// with_service_account
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	if err := validateSearchHeadClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateSearchHeadClusterSpec: %v", err)
	}
	ss, err = getDeployerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getDeployerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_deployer_with_service_account.json", ss)
}

func TestGenIndexerFixtures(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	queue := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-queue",
			Namespace: "test",
		},
	}

	cr := enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			QueueRef: corev1.ObjectReference{
				Name: queue.Name,
			},
		},
	}

	c := spltest.NewMockClient()
	if _, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test"); err != nil {
		t.Fatalf("ApplyNamespaceScopedSecretObject: %v", err)
	}

	cr.Spec.ClusterManagerRef.Name = "manager1"

	// base (Replicas=0)
	cr.Spec.Replicas = 0
	if err := validateIndexerClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateIndexerClusterSpec: %v", err)
	}
	ss, err := getIndexerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getIndexerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_indexer_base.json", ss)

	// base_1 (Replicas=1)
	cr.Spec.Replicas = 1
	if err := validateIndexerClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateIndexerClusterSpec: %v", err)
	}
	ss, err = getIndexerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getIndexerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_indexer_base_1.json", ss)

	// base_2 (with custom service port)
	cr.Spec.ServiceTemplate.Spec.Ports = []corev1.ServicePort{{Name: "user-defined", Port: 32000, Protocol: "UDP"}}
	if err := validateIndexerClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateIndexerClusterSpec: %v", err)
	}
	ss, err = getIndexerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getIndexerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_indexer_base_2.json", ss)

	// base_3 (with DefaultsURLApps - should not be added to SPLUNK_DEFAULTS_URL for indexer)
	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	if err := validateIndexerClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateIndexerClusterSpec: %v", err)
	}
	ss, err = getIndexerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getIndexerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_indexer_base_3.json", ss)

	// with_service_account
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	if err := validateIndexerClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateIndexerClusterSpec: %v", err)
	}
	ss, err = getIndexerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getIndexerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_indexer_with_service_account.json", ss)

	// with_service_account_1 (with ExtraEnv)
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	if err := validateIndexerClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateIndexerClusterSpec: %v", err)
	}
	ss, err = getIndexerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getIndexerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_indexer_with_service_account_1.json", ss)

	// with_service_account_2 (with extra labels on CR metadata)
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	if err := validateIndexerClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateIndexerClusterSpec: %v", err)
	}
	ss, err = getIndexerStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getIndexerStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_indexer_with_service_account_2.json", ss)
}

func TestGenStandaloneFixtures(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	if _, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test"); err != nil {
		t.Fatalf("ApplyNamespaceScopedSecretObject: %v", err)
	}

	// base (EphemeralStorage=false, default)
	if err := validateStandaloneSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateStandaloneSpec: %v", err)
	}
	ss, err := getStandaloneStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getStandaloneStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_standalone_base.json", ss)

	// base_1 (EphemeralStorage=true)
	cr.Spec.EtcVolumeStorageConfig.EphemeralStorage = true
	cr.Spec.VarVolumeStorageConfig.EphemeralStorage = true
	if err := validateStandaloneSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateStandaloneSpec: %v", err)
	}
	ss, err = getStandaloneStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getStandaloneStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_standalone_base_1.json", ss)

	// with_defaults
	cr.Spec.EtcVolumeStorageConfig.EphemeralStorage = false
	cr.Spec.VarVolumeStorageConfig.EphemeralStorage = false
	cr.Spec.ClusterManagerRef.Name = "stack2"
	cr.Spec.EtcVolumeStorageConfig.StorageClassName = "gp2"
	cr.Spec.VarVolumeStorageConfig.StorageClassName = "gp2"
	cr.Spec.SchedulerName = "custom-scheduler"
	cr.Spec.Defaults = "defaults-string"
	cr.Spec.DefaultsURL = "/mnt/defaults/defaults.yml"
	cr.Spec.Volumes = []corev1.Volume{{Name: "defaults"}}
	if err := validateStandaloneSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateStandaloneSpec: %v", err)
	}
	ss, err = getStandaloneStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getStandaloneStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_standalone_with_defaults.json", ss)

	// with_apps
	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	if err := validateStandaloneSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateStandaloneSpec: %v", err)
	}
	ss, err = getStandaloneStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getStandaloneStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_standalone_with_apps.json", ss)

	// with_service_account
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	if err := validateStandaloneSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateStandaloneSpec: %v", err)
	}
	ss, err = getStandaloneStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getStandaloneStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_standalone_with_service_account.json", ss)

	// with_service_account_1 (with ExtraEnv)
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	if err := validateStandaloneSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateStandaloneSpec: %v", err)
	}
	ss, err = getStandaloneStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getStandaloneStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_standalone_with_service_account_1.json", ss)

	// with_service_account_2 (with extra label)
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	if err := validateStandaloneSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateStandaloneSpec: %v", err)
	}
	ss, err = getStandaloneStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getStandaloneStatefulSet: %v", err)
	}
	writeFixtureFile(t, "statefulset_stack1_standalone_with_service_account_2.json", ss)
}
