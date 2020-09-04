package controller

import (
	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	enterprise "github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func init() {
	SplunkControllersToAdd = append(SplunkControllersToAdd, ClusterMasterController{})
}

// blank assignment to verify that ClusterMasterController implements SplunkController
var _ splctrl.SplunkController = &ClusterMasterController{}

// ClusterMasterController is used to manage ClusterMaster custom resources
type ClusterMasterController struct{}

// GetInstance returns an instance of the custom resource managed by the controller
func (ctrl ClusterMasterController) GetInstance() splcommon.MetaObject {
	return &enterprisev1.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: enterprisev1.APIVersion,
			Kind:       "ClusterMaster",
		},
	}
}

// GetWatchTypes returns a list of types owned by the controller that it would like to receive watch events for
func (ctrl ClusterMasterController) GetWatchTypes() []runtime.Object {
	return []runtime.Object{&appsv1.StatefulSet{}}
}

// Reconcile is used to perform an idempotent reconciliation of the custom resource managed by this controller
func (ctrl ClusterMasterController) Reconcile(client client.Client, cr splcommon.MetaObject) (reconcile.Result, error) {
	instance := cr.(*enterprisev1.ClusterMaster)
	return enterprise.ApplyClusterMaster(client, instance)
}
