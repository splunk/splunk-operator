package model

import (
	"github.com/go-logr/logr"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Instead of passing a zillion arguments to the action of a phase,
// hold them in a context
type ReconcileInfo struct {
	Kind              string
	MetaObject        splcommon.MetaObject
	CommonSpec        enterpriseApi.CommonSplunkSpec
	Client            splcommon.ControllerClient
	Log               logr.Logger
	Namespace         string
	Name              string
	Request           ctrl.Request
	Events            []corev1.Event
	ErrorMessage      string
	PostSaveCallbacks []func()
}
