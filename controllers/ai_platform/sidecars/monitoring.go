package sidecars

import (
	"context"
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ReconcilePodMonitor ensures PodMonitors for head and worker pods are created or updated
func (s *Builder) reconcilePodMonitor(ctx context.Context, p *enterpriseApi.SplunkAIPlatform) error {
	if !p.Spec.Sidecars.PrometheusOperator {
		return nil
	}

	// Reconcile PodMonitor for head
	headMonitor := &unstructured.Unstructured{}
	headMonitor.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "PodMonitor",
	})
	headMonitor.SetName(fmt.Sprintf("%s-ray-head-monitor", p.Name))
	headMonitor.SetNamespace(p.Namespace)

	_, err := controllerutil.CreateOrUpdate(ctx, s.Client, headMonitor, func() error {
		headMonitor.Object["spec"] = map[string]interface{}{
			"jobLabel": "ray-head",
			"namespaceSelector": map[string]interface{}{
				"matchNames": []string{p.Namespace},
			},
			"selector": map[string]interface{}{
				"matchLabels": map[string]string{
					"app":              p.Name,
					"ray.io/node-type": "head",
				},
			},
			"podMetricsEndpoints": []map[string]interface{}{
				{
					"port": "metrics",
					"relabelings": []map[string]interface{}{
						{
							"action":       "replace",
							"sourceLabels": []string{"__meta_kubernetes_pod_label_ray_io_cluster"},
							"targetLabel":  "ray_io_cluster",
						},
					},
				},
				{
					"port": "as-metrics",
					"relabelings": []map[string]interface{}{
						{
							"action":       "replace",
							"sourceLabels": []string{"__meta_kubernetes_pod_label_ray_io_cluster"},
							"targetLabel":  "ray_io_cluster",
						},
					},
				},
				{
					"port": "dash-metrics",
					"relabelings": []map[string]interface{}{
						{
							"action":       "replace",
							"sourceLabels": []string{"__meta_kubernetes_pod_label_ray_io_cluster"},
							"targetLabel":  "ray_io_cluster",
						},
					},
				},
			},
		}
		return controllerutil.SetOwnerReference(p, headMonitor, s.Scheme)
	})
	if err != nil {
		return fmt.Errorf("failed to reconcile head PodMonitor: %w", err)
	}

	// Reconcile PodMonitor for workers
	workerMonitor := &unstructured.Unstructured{}
	workerMonitor.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "PodMonitor",
	})
	workerMonitor.SetName(fmt.Sprintf("%s-ray-workers-monitor", p.Name))
	workerMonitor.SetNamespace(p.Namespace)

	_, err = controllerutil.CreateOrUpdate(ctx, s.Client, workerMonitor, func() error {
		workerMonitor.Object["spec"] = map[string]interface{}{
			"jobLabel": "ray-workers",
			"namespaceSelector": map[string]interface{}{
				"matchNames": []string{p.Namespace},
			},
			"selector": map[string]interface{}{
				"matchLabels": map[string]string{
					"app":              p.Name,
					"ray.io/node-type": "worker",
				},
			},
			"podMetricsEndpoints": []map[string]interface{}{
				{
					"port": "metrics",
					"relabelings": []map[string]interface{}{
						{
							"sourceLabels": []string{"__meta_kubernetes_pod_label_ray_io_cluster"},
							"targetLabel":  "ray_io_cluster",
						},
					},
				},
			},
		}
		return controllerutil.SetOwnerReference(p, workerMonitor, s.Scheme)
	})
	if err != nil {
		return fmt.Errorf("failed to reconcile worker PodMonitor: %w", err)
	}

	return nil
}

// reconcilePrometheusRule ensures the PrometheusRule CR is created or updated
func (s *Builder) reconcilePrometheusRule(ctx context.Context, p *enterpriseApi.SplunkAIPlatform) error {
	promRule := &unstructured.Unstructured{}
	promRule.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "PrometheusRule",
	})
	promRule.SetName(fmt.Sprintf("%s-alerts", p.Name))
	promRule.SetNamespace(p.Namespace)

	_, err := controllerutil.CreateOrUpdate(ctx, s.Client, promRule, func() error {
		promRule.Object["spec"] = map[string]interface{}{
			"groups": []map[string]interface{}{
				{
					"name": fmt.Sprintf("%s.rules", p.Name),
					"rules": []map[string]interface{}{
						{
							"record": "ray:kube_pod_status_phase",
							"expr": fmt.Sprintf(
								`kube_pod_status_phase{namespace='%s', pod=~".*worker.*"} * on(pod, namespace) group_left(created_by_name) kube_pod_info{created_by_name=~'%s.*'}`,
								p.Namespace, p.Name,
							),
						},
						{
							"record": "ray:kube_pod_container_status_restarts_total",
							"expr": fmt.Sprintf(
								`kube_pod_container_status_restarts_total{container=~"autoscaler|istio-proxy|ray-head|ray-worker", namespace='%s'} * on(pod, namespace) group_left(created_by_name) kube_pod_info{created_by_name=~'%s.*'}`,
								p.Namespace, p.Name,
							),
						},
						{
							"record": "ray:ray_actors:sum",
							"expr": fmt.Sprintf(
								`sum(ray_actors{namespace='%s', app='%s'}) by (Name, State, app, namespace)`,
								p.Namespace, p.Name,
							),
						},
						{
							"alert": "HeadNodeDown",
							"expr": fmt.Sprintf(
								`up{app='%s', namespace='%s', ray_io_node_type="head"} == 0`,
								p.Name, p.Namespace,
							),
							"for": "5m",
							"annotations": map[string]interface{}{
								"summary":     fmt.Sprintf("Head node for RayService %s in %s is down", p.Name, p.Namespace),
								"description": fmt.Sprintf("Head node for RayService %s in %s has been down for 5 mins", p.Name, p.Namespace),
							},
							"labels": map[string]interface{}{
								"namespace": p.Namespace,
								"severity":  "critical",
							},
						},
					},
				},
			},
		}
		return controllerutil.SetOwnerReference(p, promRule, s.Scheme)
	})
	return err
}
