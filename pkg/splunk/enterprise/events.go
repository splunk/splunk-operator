package enterprise

import (
	"context"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
)

// K8EventPublisher structure used to publish k8s event
type K8EventPublisher struct {
	client   splcommon.ControllerClient
	instance interface{}
}

// private function to get new k8s event publisher
func newK8EventPublisher(client splcommon.ControllerClient, instance interface{}) (*K8EventPublisher, error) {
	eventPublisher := &K8EventPublisher{
		client:   client,
		instance: instance,
	}

	return eventPublisher, nil
}

// publishEvents adds events to k8s
func (k *K8EventPublisher) publishEvent(eventType, reason, message string) {
	var name string
	var namespace string
	var event corev1.Event

	// in the case of testing, client is not passed
	if k.client == nil {
		return
	}

	// based on the custom resource instance type find name, type and create new event
	switch k.instance.(type) {
	case *enterpriseApi.Standalone:
		cr := k.instance.(*enterpriseApi.Standalone)
		name = cr.Name
		namespace = cr.Namespace
		event = cr.NewEvent(eventType, reason, message)
	case *enterpriseApi.LicenseMaster:
		cr := k.instance.(*enterpriseApi.LicenseMaster)
		name = cr.Name
		namespace = cr.Namespace
		event = cr.NewEvent(eventType, reason, message)
	case *enterpriseApi.IndexerCluster:
		cr := k.instance.(*enterpriseApi.IndexerCluster)
		name = cr.Name
		namespace = cr.Namespace
		event = cr.NewEvent(eventType, reason, message)
	case *enterpriseApi.ClusterMaster:
		cr := k.instance.(*enterpriseApi.ClusterMaster)
		name = cr.Name
		namespace = cr.Namespace
		event = cr.NewEvent(eventType, reason, message)
	case *enterpriseApi.MonitoringConsole:
		cr := k.instance.(*enterpriseApi.MonitoringConsole)
		name = cr.Name
		namespace = cr.Namespace
		event = cr.NewEvent(eventType, reason, message)
	case *enterpriseApi.SearchHeadCluster:
		cr := k.instance.(*enterpriseApi.SearchHeadCluster)
		name = cr.Name
		namespace = cr.Namespace
		event = cr.NewEvent(eventType, reason, message)
	default:
		return
	}

	scopedLog := log.WithName("PublishEvent").WithValues("name", name, "namespace", namespace)
	scopedLog.Info("publishing event", "reason", event.Reason, "message", event.Message)

	err := k.client.Create(context.TODO(), &event)
	if err != nil {
		scopedLog.Error(err, "failed to record event, ignoring",
			"reason", event.Reason, "message", event.Message, "error", err)
	}
}

// Normal publish normal events to k8s
func (k *K8EventPublisher) Normal(reason, message string) {
	k.publishEvent(corev1.EventTypeNormal, reason, message)
}

// Warning publish warning events to k8s
func (k *K8EventPublisher) Warning(reason, message string) {
	k.publishEvent(corev1.EventTypeWarning, reason, message)
}
