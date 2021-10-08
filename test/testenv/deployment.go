// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	wait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v2"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// Deployment simply represents the deployment (standalone, clustered,...etc) we create on the testenv
type Deployment struct {
	name              string
	testenv           *TestEnv
	cleanupFuncs      []cleanupFunc
	testTimeoutInSecs time.Duration
}

// GetName returns this deployment name
func (d *Deployment) GetName() string {
	return d.name
}

// GetTimeout returns deployment name
func (d *Deployment) GetTimeout() time.Duration {
	return d.testTimeoutInSecs
}

func (d *Deployment) pushCleanupFunc(fn cleanupFunc) {
	d.cleanupFuncs = append(d.cleanupFuncs, fn)
}

func (d *Deployment) popCleanupFunc() (cleanupFunc, error) {
	if len(d.cleanupFuncs) == 0 {
		return nil, fmt.Errorf("cleanupFuncs is empty")
	}

	fn := d.cleanupFuncs[len(d.cleanupFuncs)-1]
	d.cleanupFuncs = d.cleanupFuncs[:len(d.cleanupFuncs)-1]

	return fn, nil
}

// Teardown teardowns the deployment resources
func (d *Deployment) Teardown() error {
	if d.testenv.SkipTeardown {
		d.testenv.Log.Info("deployment teardown is skipped!\n")
		return nil
	}

	var cleanupErr error

	for fn, err := d.popCleanupFunc(); err == nil; fn, err = d.popCleanupFunc() {
		cleanupErr = fn()
		if cleanupErr != nil {
			d.testenv.Log.Error(cleanupErr, "Deployment cleanupFunc returns an error. Attempt to continue.\n")
		}
	}

	d.testenv.Log.Info("deployment deleted.\n", "name", d.name)
	return cleanupErr
}

// DeployStandalone deploys a standalone splunk enterprise instance on the specified testenv
func (d *Deployment) DeployStandalone(name string) (*enterpriseApi.Standalone, error) {
	standalone := newStandalone(name, d.testenv.namespace)
	deployed, err := d.deployCR(name, standalone)
	if err != nil {
		return nil, err
	}
	return deployed.(*enterpriseApi.Standalone), err
}

// GetInstance retrieves the standalone, indexer, searchhead, licensemanager instance
func (d *Deployment) GetInstance(name string, instance runtime.Object) error {
	key := client.ObjectKey{Name: name, Namespace: d.testenv.namespace}

	err := d.testenv.GetKubeClient().Get(context.TODO(), key, instance)
	if err != nil {
		return err
	}
	return nil
}

// PodExecCommand execute a shell command in the specified pod
func (d *Deployment) PodExecCommand(podName string, cmd []string, stdin string, tty bool) (string, string, error) {
	pod := &corev1.Pod{}
	d.GetInstance(podName, pod)
	gvk, _ := apiutil.GVKForObject(pod, scheme.Scheme)
	restConfig, err := config.GetConfig()
	if err != nil {
		return "", "", err
	}
	restClient, err := apiutil.RESTClientForGVK(gvk, restConfig, serializer.NewCodecFactory(scheme.Scheme))
	if err != nil {
		return "", "", err
	}
	execReq := restClient.Post().Resource("pods").Name(podName).Namespace(d.testenv.namespace).SubResource("exec")
	option := &corev1.PodExecOptions{
		Command: cmd,
		Stdin:   true,
		Stdout:  true,
		Stderr:  true,
		TTY:     tty,
	}
	if stdin == "" {
		option.Stdin = false
	}
	execReq.VersionedParams(
		option,
		scheme.ParameterCodec,
	)
	exec, err := remotecommand.NewSPDYExecutor(restConfig, http.MethodPost, execReq.URL())
	if err != nil {
		return "", "", err
	}
	stdinReader := strings.NewReader(stdin)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  stdinReader,
		Stdout: stdout,
		Stderr: stderr,
	})
	if err != nil {
		return "", "", err
	}
	return stdout.String(), stderr.String(), nil
}

//DeployLicenseManager deploys the license manager instance
func (d *Deployment) DeployLicenseManager(name string) (*enterpriseApi.LicenseMaster, error) {

	if d.testenv.licenseFilePath == "" {
		return nil, fmt.Errorf("no license file path specified")
	}

	lm := newLicenseManager(name, d.testenv.namespace, d.testenv.licenseCMName)
	deployed, err := d.deployCR(name, lm)
	if err != nil {
		return nil, err
	}
	return deployed.(*enterpriseApi.LicenseMaster), err
}

//DeployClusterMaster deploys the cluster manager
func (d *Deployment) DeployClusterMaster(name, licenseMasterName string, ansibleConfig string) (*enterpriseApi.ClusterMaster, error) {
	d.testenv.Log.Info("Deploying "+splcommon.ClusterManager, "name", name)
	cm := newClusterMaster(name, d.testenv.namespace, licenseMasterName, ansibleConfig)
	deployed, err := d.deployCR(name, cm)
	if err != nil {
		return nil, err
	}
	return deployed.(*enterpriseApi.ClusterMaster), err
}

//DeployClusterMasterWithSmartStoreIndexes deploys the cluster manager with smartstore indexes
func (d *Deployment) DeployClusterMasterWithSmartStoreIndexes(name, licenseMasterName string, ansibleConfig string, smartstorespec enterpriseApi.SmartStoreSpec) (*enterpriseApi.ClusterMaster, error) {
	d.testenv.Log.Info("Deploying "+splcommon.ClusterManager, "name", name)
	cm := newClusterMasterWithGivenIndexes(name, d.testenv.namespace, licenseMasterName, ansibleConfig, smartstorespec)
	deployed, err := d.deployCR(name, cm)
	if err != nil {
		return nil, err
	}
	return deployed.(*enterpriseApi.ClusterMaster), err
}

//DeployIndexerCluster deploys the indexer cluster
func (d *Deployment) DeployIndexerCluster(name, licenseMasterName string, count int, clusterMasterRef string, ansibleConfig string) (*enterpriseApi.IndexerCluster, error) {
	d.testenv.Log.Info("Deploying indexer cluster", "name", name)
	indexer := newIndexerCluster(name, d.testenv.namespace, licenseMasterName, count, clusterMasterRef, ansibleConfig)
	deployed, err := d.deployCR(name, indexer)
	if err != nil {
		return nil, err
	}
	return deployed.(*enterpriseApi.IndexerCluster), err
}

// DeploySearchHeadCluster deploys a search head cluster
func (d *Deployment) DeploySearchHeadCluster(name, clusterMasterRef, licenseMasterName string, ansibleConfig string) (*enterpriseApi.SearchHeadCluster, error) {
	d.testenv.Log.Info("Deploying search head cluster", "name", name)
	indexer := newSearchHeadCluster(name, d.testenv.namespace, clusterMasterRef, licenseMasterName, ansibleConfig)
	deployed, err := d.deployCR(name, indexer)
	return deployed.(*enterpriseApi.SearchHeadCluster), err
}

func (d *Deployment) deployCR(name string, cr runtime.Object) (runtime.Object, error) {

	err := d.testenv.GetKubeClient().Create(context.TODO(), cr)
	if err != nil {
		return nil, err
	}

	// Push the clean up func to delete the cr when done
	d.pushCleanupFunc(func() error {
		d.testenv.Log.Info("Deleting cr", "name", name)
		err := d.testenv.GetKubeClient().Delete(context.TODO(), cr)
		if err != nil {
			return err
		}
		if err = wait.PollImmediate(PollInterval, DefaultTimeout, func() (bool, error) {
			key := client.ObjectKey{Name: name, Namespace: d.testenv.namespace}
			err := d.testenv.GetKubeClient().Get(context.TODO(), key, cr)

			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}); err != nil {
			return err
		}

		return nil
	})

	// Returns once we can retrieve the lm instance
	if err := wait.PollImmediate(PollInterval, DefaultTimeout, func() (bool, error) {
		key := client.ObjectKey{Name: name, Namespace: d.testenv.namespace}
		err := d.testenv.GetKubeClient().Get(context.TODO(), key, cr)
		if err != nil {

			// Try again
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		return true, nil
	}); err != nil {
		return nil, err
	}

	return cr, nil
}

// UpdateCR method to update existing CR spec
func (d *Deployment) UpdateCR(cr runtime.Object) error {

	err := d.testenv.GetKubeClient().Update(context.TODO(), cr)
	return err
}

// DeleteCR deletes the given CR
func (d *Deployment) DeleteCR(cr runtime.Object) error {

	err := d.testenv.GetKubeClient().Delete(context.TODO(), cr)
	return err
}

// DeploySingleSiteCluster deploys a lm and indexer cluster (shc optional)
func (d *Deployment) DeploySingleSiteCluster(name string, indexerReplicas int, shc bool) error {

	var licenseMaster string

	// If license file specified, deploy License Manager
	if d.testenv.licenseFilePath != "" {
		// Deploy the license manager
		_, err := d.DeployLicenseManager(name)
		if err != nil {
			return err
		}

		licenseMaster = name
	}

	// Deploy the cluster manager
	_, err := d.DeployClusterMaster(name, licenseMaster, "")
	if err != nil {
		return err
	}

	// Deploy the indexer cluster
	_, err = d.DeployIndexerCluster(name+"-idxc", licenseMaster, indexerReplicas, name, "")
	if err != nil {
		return err
	}

	// Deploy the SH cluster
	if shc {
		_, err = d.DeploySearchHeadCluster(name+"-shc", name, licenseMaster, "")
		if err != nil {
			return err
		}
	}

	return nil
}

// DeployMultisiteClusterWithSearchHead deploys a lm, cluster-manager, indexers in multiple sites and SH clusters
func (d *Deployment) DeployMultisiteClusterWithSearchHead(name string, indexerReplicas int, siteCount int) error {

	var licenseMaster string

	// If license file specified, deploy License Manager
	if d.testenv.licenseFilePath != "" {
		// Deploy the license manager
		_, err := d.DeployLicenseManager(name)
		if err != nil {
			return err
		}

		licenseMaster = name
	}

	// Deploy the cluster-manager
	defaults := `splunk:
  multisite_master: localhost
  all_sites: site1,site2,site3
  site: site1
  multisite_replication_factor_origin: 1
  multisite_replication_factor_total: 2
  multisite_search_factor_origin: 1
  multisite_search_factor_total: 2
  idxc:
    search_factor: 2
    replication_factor: 2
`
	_, err := d.DeployClusterMaster(name, licenseMaster, defaults)
	if err != nil {
		return err
	}

	// Deploy indexer sites
	for site := 1; site <= siteCount; site++ {
		siteName := fmt.Sprintf("site%d", site)
		siteDefaults := fmt.Sprintf(`splunk:
  multisite_master: splunk-%s-%s-service
  site: %s
`, name, splcommon.ClusterManager, siteName)
		_, err := d.DeployIndexerCluster(name+"-"+siteName, licenseMaster, indexerReplicas, name, siteDefaults)
		if err != nil {
			return err
		}
	}

	siteDefaults := fmt.Sprintf(`splunk:
  multisite_master: splunk-%s-%s-service
  site: site0
`, name, splcommon.ClusterManager)
	_, err = d.DeploySearchHeadCluster(name+"-shc", name, licenseMaster, siteDefaults)
	if err != nil {
		return err
	}

	return nil
}

// DeployMultisiteCluster deploys a lm, cluster-manager, indexers in multiple sites
func (d *Deployment) DeployMultisiteCluster(name string, indexerReplicas int, siteCount int) error {

	var licenseMaster string

	// If license file specified, deploy License Manager
	if d.testenv.licenseFilePath != "" {
		// Deploy the license manager
		_, err := d.DeployLicenseManager(name)
		if err != nil {
			return err
		}

		licenseMaster = name
	}

	// Deploy the cluster-manager
	defaults := `splunk:
  multisite_master: localhost
  all_sites: site1,site2,site3
  site: site1
  multisite_replication_factor_origin: 1
  multisite_replication_factor_total: 2
  multisite_search_factor_origin: 1
  multisite_search_factor_total: 2
  idxc:
    search_factor: 2
    replication_factor: 2
`
	_, err := d.DeployClusterMaster(name, licenseMaster, defaults)
	if err != nil {
		return err
	}

	// Deploy indexer sites
	for site := 1; site <= siteCount; site++ {
		siteName := fmt.Sprintf("site%d", site)
		siteDefaults := fmt.Sprintf(`splunk:
  multisite_master: splunk-%s-%s-service
  site: %s
`, name, splcommon.ClusterManager, siteName)
		_, err := d.DeployIndexerCluster(name+"-"+siteName, licenseMaster, indexerReplicas, name, siteDefaults)
		if err != nil {
			return err
		}
	}

	return nil
}

// DeployStandaloneWithLM deploys a standalone splunk enterprise instance with license manager on the specified testenv
func (d *Deployment) DeployStandaloneWithLM(name string) (*enterpriseApi.Standalone, error) {
	var licenseMaster string

	// If license file specified, deploy License Manager
	if d.testenv.licenseFilePath != "" {
		// Deploy the license manager
		_, err := d.DeployLicenseManager(name)
		if err != nil {
			return nil, err
		}
		licenseMaster = name
	}

	standalone := newStandaloneWithLM(name, d.testenv.namespace, licenseMaster)
	deployed, err := d.deployCR(name, standalone)
	if err != nil {
		return nil, err
	}
	return deployed.(*enterpriseApi.Standalone), err
}

// DeployStandalonewithGivenSpec deploys a standalone with given spec
func (d *Deployment) DeployStandalonewithGivenSpec(name string, spec enterpriseApi.StandaloneSpec) (*enterpriseApi.Standalone, error) {
	standalone := newStandaloneWithGivenSpec(name, d.testenv.namespace, spec)
	deployed, err := d.deployCR(name, standalone)
	if err != nil {
		return nil, err
	}
	return deployed.(*enterpriseApi.Standalone), err
}

// DeployStandaloneWithGivenSmartStoreSpec deploys a standalone give smartstore spec
func (d *Deployment) DeployStandaloneWithGivenSmartStoreSpec(name string, smartStoreSpec enterpriseApi.SmartStoreSpec) (*enterpriseApi.Standalone, error) {

	spec := enterpriseApi.StandaloneSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec: splcommon.Spec{
				ImagePullPolicy: "IfNotPresent",
			},
			Volumes: []corev1.Volume{},
		},
		SmartStore: smartStoreSpec,
	}

	standalone := newStandaloneWithSpec(name, d.testenv.namespace, spec)
	deployed, err := d.deployCR(name, standalone)
	if err != nil {
		return nil, err
	}
	return deployed.(*enterpriseApi.Standalone), err
}

// DeployMultisiteClusterWithSearchHeadAndIndexes deploys a lm, cluster-manager, indexers in multiple sites and SH clusters
func (d *Deployment) DeployMultisiteClusterWithSearchHeadAndIndexes(name string, indexerReplicas int, siteCount int, indexesSecret string, smartStoreSpec enterpriseApi.SmartStoreSpec) error {

	var licenseMaster string

	// If license file specified, deploy License Manager
	if d.testenv.licenseFilePath != "" {
		// Deploy the license manager
		_, err := d.DeployLicenseManager(name)
		if err != nil {
			return err
		}

		licenseMaster = name
	}

	// Deploy the cluster-manager
	defaults := `splunk:
  multisite_master: localhost
  all_sites: site1,site2,site3
  site: site1
  multisite_replication_factor_origin: 1
  multisite_replication_factor_total: 2
  multisite_search_factor_origin: 1
  multisite_search_factor_total: 2
  idxc:
    search_factor: 2
    replication_factor: 2
`
	_, err := d.DeployClusterMasterWithSmartStoreIndexes(name, licenseMaster, defaults, smartStoreSpec)
	if err != nil {
		return err
	}

	// Deploy indexer sites
	for site := 1; site <= siteCount; site++ {
		siteName := fmt.Sprintf("site%d", site)
		siteDefaults := fmt.Sprintf(`splunk:
  multisite_master: splunk-%s-%s-service
  site: %s
`, name, splcommon.ClusterManager, siteName)
		_, err := d.DeployIndexerCluster(name+"-"+siteName, licenseMaster, indexerReplicas, name, siteDefaults)
		if err != nil {
			return err
		}
	}

	siteDefaults := fmt.Sprintf(`splunk:
  multisite_master: splunk-%s-%s-service
  site: site0
`, name, splcommon.ClusterManager)
	_, err = d.DeploySearchHeadCluster(name+"-shc", name, licenseMaster, siteDefaults)
	return err
}

// DeployClusterMasterWithGivenSpec deploys the cluster manager with given SPEC
func (d *Deployment) DeployClusterMasterWithGivenSpec(name string, spec enterpriseApi.ClusterMasterSpec) (*enterpriseApi.ClusterMaster, error) {
	d.testenv.Log.Info("Deploying "+splcommon.ClusterManager, "name", name)
	cm := newClusterMasterWithGivenSpec(name, d.testenv.namespace, spec)
	deployed, err := d.deployCR(name, cm)
	if err != nil {
		return nil, err
	}
	return deployed.(*enterpriseApi.ClusterMaster), err
}

// DeploySearchHeadClusterWithGivenSpec deploys a search head cluster
func (d *Deployment) DeploySearchHeadClusterWithGivenSpec(name string, spec enterpriseApi.SearchHeadClusterSpec) (*enterpriseApi.SearchHeadCluster, error) {
	d.testenv.Log.Info("Deploying search head cluster", "name", name)
	indexer := newSearchHeadClusterWithGivenSpec(name, d.testenv.namespace, spec)
	deployed, err := d.deployCR(name, indexer)
	return deployed.(*enterpriseApi.SearchHeadCluster), err
}

// DeployLicenseManagerWithGivenSpec deploys the license manager with given SPEC
func (d *Deployment) DeployLicenseManagerWithGivenSpec(name string, spec enterpriseApi.LicenseMasterSpec) (*enterpriseApi.LicenseMaster, error) {
	d.testenv.Log.Info("Deploying license-manager", "name", name)
	lm := newLicenseManagerWithGivenSpec(name, d.testenv.namespace, spec)
	deployed, err := d.deployCR(name, lm)
	if err != nil {
		return nil, err
	}
	return deployed.(*enterpriseApi.LicenseMaster), err
}

// DeploySingleSiteClusterWithGivenAppFrameworkSpec deploys indexer cluster (lm, shc optional) with app framework spec
func (d *Deployment) DeploySingleSiteClusterWithGivenAppFrameworkSpec(name string, indexerReplicas int, shc bool, appFrameworkSpec enterpriseApi.AppFrameworkSpec, delaySeconds int) error {

	licenseMaster := ""

	// If license file specified, deploy License Manager
	if d.testenv.licenseFilePath != "" {
		// Deploy the license manager
		_, err := d.DeployLicenseManager(name)
		if err != nil {
			return err
		}

		licenseMaster = name
	}

	// Deploy the cluster manager
	cmSpec := enterpriseApi.ClusterMasterSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec: splcommon.Spec{
				ImagePullPolicy: "Always",
			},
			Volumes: []corev1.Volume{},
			LicenseMasterRef: corev1.ObjectReference{
				Name: licenseMaster,
			},
			// LivenessInitialDelaySeconds:  int32(delaySeconds),
			// ReadinessInitialDelaySeconds: int32(delaySeconds),
		},
		AppFrameworkConfig: appFrameworkSpec,
	}
	_, err := d.DeployClusterMasterWithGivenSpec(name, cmSpec)
	if err != nil {
		return err
	}

	// Deploy the indexer cluster
	_, err = d.DeployIndexerCluster(name+"-idxc", licenseMaster, indexerReplicas, name, "")
	if err != nil {
		return err
	}

	shSpec := enterpriseApi.SearchHeadClusterSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec: splcommon.Spec{
				ImagePullPolicy: "Always",
			},
			Volumes: []corev1.Volume{},
			ClusterMasterRef: corev1.ObjectReference{
				Name: name,
			},
			LicenseMasterRef: corev1.ObjectReference{
				Name: licenseMaster,
			},
			// LivenessInitialDelaySeconds:  int32(delaySeconds),
			// ReadinessInitialDelaySeconds: int32(delaySeconds),
		},
		Replicas:           3,
		AppFrameworkConfig: appFrameworkSpec,
	}
	if shc {
		_, err = d.DeploySearchHeadClusterWithGivenSpec(name+"-shc", shSpec)
		if err != nil {
			return err
		}
	}

	return nil
}

// DeployMultisiteClusterWithSearchHeadAndAppFramework deploys cluster-manager, indexers in multiple sites (SHC LM Optional) with app framework spec
func (d *Deployment) DeployMultisiteClusterWithSearchHeadAndAppFramework(name string, indexerReplicas int, siteCount int, appFrameworkSpec enterpriseApi.AppFrameworkSpec, shc bool, delaySeconds int) error {

	licenseMaster := ""

	// If license file specified, deploy License Manager
	if d.testenv.licenseFilePath != "" {
		// Deploy the license manager
		_, err := d.DeployLicenseManager(name)
		if err != nil {
			return err
		}

		licenseMaster = name
	}

	// Deploy the cluster-manager
	defaults := `splunk:
  multisite_master: localhost
  all_sites: site1,site2,site3
  site: site1
  multisite_replication_factor_origin: 1
  multisite_replication_factor_total: 2
  multisite_search_factor_origin: 1
  multisite_search_factor_total: 2
  idxc:
    search_factor: 2
    replication_factor: 2
`

	// Cluster Manager Spec
	cmSpec := enterpriseApi.ClusterMasterSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec: splcommon.Spec{
				ImagePullPolicy: "Always",
			},
			Volumes: []corev1.Volume{},
			LicenseMasterRef: corev1.ObjectReference{
				Name: licenseMaster,
			},
			Defaults: defaults,
			// LivenessInitialDelaySeconds:  int32(delaySeconds),
			// ReadinessInitialDelaySeconds: int32(delaySeconds),
		},
		AppFrameworkConfig: appFrameworkSpec,
	}

	_, err := d.DeployClusterMasterWithGivenSpec(name, cmSpec)
	if err != nil {
		return err
	}

	// Deploy indexer sites
	for site := 1; site <= siteCount; site++ {
		siteName := fmt.Sprintf("site%d", site)
		siteDefaults := fmt.Sprintf(`splunk:
  multisite_master: splunk-%s-%s-service
  site: %s
`, name, splcommon.ClusterManager, siteName)
		_, err := d.DeployIndexerCluster(name+"-"+siteName, licenseMaster, indexerReplicas, name, siteDefaults)
		if err != nil {
			return err
		}
	}

	siteDefaults := fmt.Sprintf(`splunk:
  multisite_master: splunk-%s-%s-service
  site: site0
`, name, splcommon.ClusterManager)
	// Deploy the SH cluster
	shSpec := enterpriseApi.SearchHeadClusterSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec: splcommon.Spec{
				ImagePullPolicy: "Always",
			},
			Volumes: []corev1.Volume{},
			ClusterMasterRef: corev1.ObjectReference{
				Name: name,
			},
			LicenseMasterRef: corev1.ObjectReference{
				Name: licenseMaster,
			},
			Defaults: siteDefaults,
			// LivenessInitialDelaySeconds:  int32(delaySeconds),
			// ReadinessInitialDelaySeconds: int32(delaySeconds),
		},
		Replicas:           3,
		AppFrameworkConfig: appFrameworkSpec,
	}
	if shc {
		_, err = d.DeploySearchHeadClusterWithGivenSpec(name+"-shc", shSpec)
		if err != nil {
			return err
		}
	}
	return nil
}
