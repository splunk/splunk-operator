package model

import (
	"context"

	"github.com/go-logr/logr"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// EventPublisher is a function type for publishing events associated
// with gateway functions.
type EventPublisher func(ctx context.Context, eventType, reason, message string)

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
