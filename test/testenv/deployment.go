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

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
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
func (d *Deployment) DeployStandalone(name string) (*enterprisev1.Standalone, error) {
	standalone := newStandalone(name, d.testenv.namespace)
	deployed, err := d.deployCR(name, standalone)
	if err != nil {
		return nil, err
	}
	return deployed.(*enterprisev1.Standalone), err
}

// GetInstance retrieves the standalone, indexer, searchhead, licensemaster instance
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

//DeployLicenseMaster deploys the license master instance
func (d *Deployment) DeployLicenseMaster(name string) (*enterprisev1.LicenseMaster, error) {

	if d.testenv.licenseFilePath == "" {
		return nil, fmt.Errorf("No license file path specified")
	}

	lm := newLicenseMaster(name, d.testenv.namespace, d.testenv.licenseCMName)
	deployed, err := d.deployCR(name, lm)
	if err != nil {
		return nil, err
	}
	return deployed.(*enterprisev1.LicenseMaster), err
}

//DeployIndexerCluster deploys the indexer cluster
func (d *Deployment) DeployIndexerCluster(name, licenseMasterName string, count int, indexerClusterRef string, ansibleConfig string) (*enterprisev1.IndexerCluster, error) {
	indexer := newIndexerCluster(name, d.testenv.namespace, licenseMasterName, count, indexerClusterRef, ansibleConfig)
	deployed, err := d.deployCR(name, indexer)
	if err != nil {
		return nil, err
	}
	return deployed.(*enterprisev1.IndexerCluster), err
}

// DeploySearchHeadCluster deploys a search head cluster
func (d *Deployment) DeploySearchHeadCluster(name, indexerClusterName, licenseMasterName string, ansibleConfig string) (*enterprisev1.SearchHeadCluster, error) {
	indexer := newSearchHeadCluster(name, d.testenv.namespace, indexerClusterName, licenseMasterName, ansibleConfig)
	deployed, err := d.deployCR(name, indexer)
	return deployed.(*enterprisev1.SearchHeadCluster), err
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

// DeployCluster deploys a lm, indexer and sh clusters
func (d *Deployment) DeployCluster(name string, indexerReplicas int) error {

	var licenseMaster string

	// If license file specified, deploy License Master
	if d.testenv.licenseFilePath != "" {
		// Deploy the license master
		_, err := d.DeployLicenseMaster(name)
		if err != nil {
			return err
		}

		licenseMaster = name
	}

	// Deploy the indexer cluster
	_, err := d.DeployIndexerCluster(name, licenseMaster, indexerReplicas, "", "")
	if err != nil {
		return err
	}

	_, err = d.DeploySearchHeadCluster(name, name, licenseMaster, "")
	if err != nil {
		return err
	}

	return nil
}

// DeployMultisiteCluster deploys a lm, cluster-master, indexers in multiple sites and sh clusters
func (d *Deployment) DeployMultisiteCluster(name string, indexerReplicas int, siteCount int) error {

	var licenseMaster string

	// If license file specified, deploy License Master
	if d.testenv.licenseFilePath != "" {
		// Deploy the license master
		_, err := d.DeployLicenseMaster(name)
		if err != nil {
			return err
		}

		licenseMaster = name
	}

	// Deploy the cluster-master
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
	_, err := d.DeployIndexerCluster(name, licenseMaster, 0, "", defaults)
	if err != nil {
		return err
	}

	// Deploy indexer sites
	for site := 1; site <= siteCount; site++ {
		siteName := fmt.Sprintf("site%d", site)
		siteDefaults := fmt.Sprintf(`splunk:
  multisite_master: splunk-%s-cluster-master-service
  site: %s
`, name, siteName)
		_, err := d.DeployIndexerCluster(name+"-"+siteName, licenseMaster, indexerReplicas, name, siteDefaults)
		if err != nil {
			return err
		}
	}

	siteDefaults := fmt.Sprintf(`splunk:
  multisite_master: splunk-%s-cluster-master-service
  site: site0
`, name)
	_, err = d.DeploySearchHeadCluster(name, name, licenseMaster, siteDefaults)
	if err != nil {
		return err
	}

	return nil
}
