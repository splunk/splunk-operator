// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

package enterprise

import (
	"context"
	"fmt"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"sigs.k8s.io/controller-runtime/pkg/client"
	rclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ApplyNoahCluster reconciles the state of a Splunk Enterprise cluster manager.
func ApplyNoahCluster(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.NoahCluster) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyNoahCluster")
	eventPublisher, _ := newK8EventPublisher(client, cr)
	cr.Kind = "NoahCluster"

	// validate and updates defaults for CR
	err := validateNoahClusterSpec(ctx, client, cr)
	if err != nil {
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = enterpriseApi.PhaseError
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-%s", cr.GetName(), "cluster-manager")

	// This is to take care of case where AreRemoteVolumeKeysChanged returns an error if it returns false.
	if err != nil {
		return result, err
	}

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr)

	// create or update general config resources
	_, err = ApplyNoahConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkNoahCluster)
	if err != nil {
		scopedLog.Error(err, "create or update general config failed", "error", err.Error())
		eventPublisher.Warning(ctx, "ApplyNoahConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
		return result, err
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {

		// Check if NoahCluster has any remaining references to other CRs, if so don't delete
		err = checkCmRemainingReferences(ctx, client, cr)
		if err != nil {
			return result, err
		}

		return result, err
	}

	// create or update a regular service for the noah cluster
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkNoahCluster, false))
	if err != nil {
		return result, err
	}

	if cr.Spec.DatabaseSecretRef != "" {
		namespaceScopedSecret, err := splutil.GetSecretByName(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.DatabaseSecretRef)
		// Ideally, this should have been detected in Spec validation time
		if err != nil {
			err = fmt.Errorf("not able to access database secret object = %s, reason: %s", cr.Spec.DatabaseSecretRef, err)
			return result, err
		}

		scopedLog.Info("database info", "user", namespaceScopedSecret.Data["NOAH_DB_USER"])
		//namespaceScopedSecret.Data["NOAH_DB_USER"]
		//namespaceScopedSecret.Data["NOAH_DB_PASSWORD"]
		//namespaceScopedSecret.Data["DB_CERT"]
	} else {
		scopedLog.Info("No valid SecretRef for database.  No secret to track.", "databaseName", cr.Spec.Database)
	}

	// create or update statefulset for the noah
	deployment, err := getNoahClusterDeployment(ctx, client, cr)
	if err != nil {
		return result, err
	}

	phase, err := splctrl.ApplyDeployment(ctx, client, deployment)
	if err != nil {
		return result, err
	}
	cr.Status.Phase = phase

	// no need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {

	}
	// RequeueAfter if greater than 0, tells the Controller to requeue the reconcile key after the Duration.
	// Implies that Requeue is true, there is no need to set Requeue to true at the same time as RequeueAfter.
	if !result.Requeue {
		result.RequeueAfter = 0
	}

	return result, nil
}

// noahClusterPodManager is used to manage the cluster manager pod
type noahClusterPodManager struct {
	log             logr.Logger
	cr              *enterpriseApi.NoahCluster
	secrets         *corev1.Secret
	newSplunkClient func(managementURI, username, password string) *splclient.SplunkClient
}

// getNoahClusterClient for noahClusterPodManager returns a SplunkClient for cluster manager
func (mgr *noahClusterPodManager) getNoahClusterClient(cr *enterpriseApi.NoahCluster) *splclient.SplunkClient {
	fqdnName := splcommon.GetServiceFQDN(cr.GetNamespace(), GetSplunkServiceName(SplunkNoahCluster, cr.GetName(), false))
	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(mgr.secrets.Data["password"]))
}

// validateNoahClusterSpec checks validity and makes default updates to a NoahClusterSpec, and returns error if something is wrong.
func validateNoahClusterSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.NoahCluster) error {

	return validateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)

}

// getNoahClusterDeployment returns a Kubernetes Deployment object for a Noah Cluster.
func getNoahClusterDeployment(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.NoahCluster) (*appsv1.Deployment, error) {
	var extraEnvVar []corev1.EnvVar

	ss, err := getNoahDeployment(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkNoahCluster, 1, extraEnvVar)
	if err != nil {
		return ss, err
	}

	return ss, err
}

// CheckIfsmartstoreConfigMapUpdatedToPod checks if the smartstore configMap is updated on Pod or not
func CheckIfNoahConfigMapUpdatedToPod(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.NoahCluster, podExecClient splutil.PodExecClientImpl) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("CheckIfsmartstoreConfigMapUpdatedToPod").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	eventPublisher, _ := newK8EventPublisher(c, cr)

	command := fmt.Sprintf("cat /mnt/splunk-operator/local/%s", configToken)
	streamOptions := splutil.NewStreamOptionsObject(command)

	stdOut, stdErr, err := podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
	if err != nil || stdErr != "" {
		eventPublisher.Warning(ctx, "PodExecCommand", fmt.Sprintf("Failed to check config token value on pod. stdout=%s, stderror=%s, error=%v", stdOut, stdErr, err))
		return fmt.Errorf("failed to check config token value on pod. stdout=%s, stderror=%s, error=%v", stdOut, stdErr, err)
	}

	smartStoreConfigMap := getSmartstoreConfigMap(ctx, c, cr, SplunkNoahCluster)
	if smartStoreConfigMap != nil {
		tokenFromConfigMap := smartStoreConfigMap.Data[configToken]
		if tokenFromConfigMap == stdOut {
			scopedLog.Info("Token Matched.", "on Pod=", stdOut, "from configMap=", tokenFromConfigMap)
			return nil
		}
		eventPublisher.Warning(ctx, "getSmartstoreConfigMap", fmt.Sprintf("waiting for the configMap update to the Pod. Token on Pod=%s, Token from configMap=%s", stdOut, tokenFromConfigMap))
		return fmt.Errorf("waiting for the configMap update to the Pod. Token on Pod=%s, Token from configMap=%s", stdOut, tokenFromConfigMap)
	}

	// Somehow the configmap was deleted, ideally this should not happen
	eventPublisher.Warning(ctx, "getSmartstoreConfigMap", "smartstore ConfigMap is missing")
	return fmt.Errorf("smartstore ConfigMap is missing")
}

// helper function to get the list of NoahCluster types in the current namespace
func getNoahClusterList(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, listOpts []client.ListOption) (int, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getNoahClusterList").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	objectList := enterpriseApi.NoahClusterList{}

	err := c.List(context.TODO(), &objectList, listOpts...)
	numOfObjects := len(objectList.Items)

	if err != nil {
		scopedLog.Error(err, "NoahCluster types not found in namespace", "namsespace", cr.GetNamespace())
		return numOfObjects, err
	}

	return numOfObjects, nil
}

// changeNoahClusterAnnotations updates the splunk/image-tag field of the NoahCluster annotations to trigger the reconcile loop
// on update, and returns error if something is wrong
func changeNoahClusterAnnotations(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.LicenseManager) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("changeNoahClusterAnnotations").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	eventPublisher, _ := newK8EventPublisher(c, cr)

	noahClusterInstance := &enterpriseApi.NoahCluster{}
	if len(cr.Spec.NoahClusterRef.Name) > 0 {
		// if the LicenseManager holds the NoahClusterRef
		namespacedName := types.NamespacedName{
			Namespace: cr.GetNamespace(),
			Name:      cr.Spec.NoahClusterRef.Name,
		}
		err := c.Get(ctx, namespacedName, noahClusterInstance)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			return err
		}
	} else {
		// List out all the NoahCluster instances in the namespace
		opts := []rclient.ListOption{
			rclient.InNamespace(cr.GetNamespace()),
		}
		objectList := enterpriseApi.NoahClusterList{}
		err := c.List(ctx, &objectList, opts...)
		if err != nil {
			if err.Error() == "NotFound" {
				return nil
			}
			return err
		}
		if len(objectList.Items) == 0 {
			return nil
		}

		// check if instance has the required LicenseManagerRef
		for _, cm := range objectList.Items {
			if cm.Spec.LicenseManagerRef.Name == cr.GetName() {
				noahClusterInstance = &cm
				break
			}
		}

		if len(noahClusterInstance.GetName()) == 0 {
			return nil
		}
	}

	image, err := getCurrentImage(ctx, c, cr, SplunkLicenseManager)
	if err != nil {
		eventPublisher.Warning(ctx, "changeNoahClusterAnnotations", fmt.Sprintf("Could not get the LicenseManager Image. Reason %v", err))
		scopedLog.Error(err, "Get Naoh Image failed with", "error", err)
		return err
	}
	err = changeAnnotations(ctx, c, image, noahClusterInstance)
	if err != nil {
		eventPublisher.Warning(ctx, "changeNoahClusterAnnotations", fmt.Sprintf("Could not update annotations. Reason %v", err))
		scopedLog.Error(err, "NoahCluster types update after changing annotations failed with", "error", err)
		return err
	}

	return nil
}
