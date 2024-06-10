/*
Copyright 2021.

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

package controllers

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
)

// SplunkAppReconciler reconciles a SplunkApp object
type SplunkAppReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=splunkapps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=splunkapps/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=splunkapps/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the SplunkApp object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *SplunkAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reqLogger := log.FromContext(ctx)
	reqLogger = reqLogger.WithValues("SplunkApp", req.NamespacedName)
	// Fetch the SplunkApp instance
	splunkApp := &enterprisev4.SplunkApp{}
	err := r.Get(ctx, req.NamespacedName, splunkApp)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	baseDir := filepath.Join("/tmp", splunkApp.Name)
	os.RemoveAll(baseDir) // Clean up any previous data
	err = os.MkdirAll(baseDir, os.ModePerm)
	if err != nil {
		reqLogger.Error(err, "Failed to create base directory")
		return ctrl.Result{}, err
	}

	// Ensure the referenced ConfigMaps exist
	for _, configFile := range splunkApp.Spec.ConfigFiles {
		configMap := &corev1.ConfigMap{}
		err := r.Get(ctx, client.ObjectKey{
			Namespace: configFile.ConfigMapRef.Namespace,
			Name:      configFile.ConfigMapRef.Name,
		}, configMap)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Error(err, "Referenced ConfigMap not found", "ConfigMap.Namespace", configFile.ConfigMapRef.Namespace, "ConfigMap.Name", configFile.ConfigMapRef.Name)
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}

		relativeDir := filepath.Join(baseDir, configFile.RelativePath)
		err = os.MkdirAll(relativeDir, os.ModePerm)
		if err != nil {
			reqLogger.Error(err, "Failed to create relative directory", "Directory", relativeDir)
			return ctrl.Result{}, err
		}

		filePath := filepath.Join(relativeDir, configFile.ConfigFileName)
		err = ioutil.WriteFile(filePath, []byte(configMap.Data[configFile.ConfigFileName]), os.ModePerm)
		if err != nil {
			reqLogger.Error(err, "Failed to write config file", "File", filePath)
			return ctrl.Result{}, err
		}
	}

	tarGzPath := baseDir + ".tar.gz"
	err = createTarGz(baseDir, tarGzPath)
	if err != nil {
		reqLogger.Error(err, "Failed to create tar.gz file")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func createTarGz(src, dst string) error {
	tarFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer tarFile.Close()

	gzipWriter := gzip.NewWriter(tarFile)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	return filepath.Walk(src, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(fi, fi.Name())
		if err != nil {
			return err
		}

		header.Name = strings.TrimPrefix(file, src)
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		if fi.IsDir() {
			return nil
		}

		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(tarWriter, f)
		return err
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *SplunkAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterprisev4.SplunkApp{}).
		Complete(r)
}
