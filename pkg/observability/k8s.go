package observability

import (
	"context"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// --- API presence checks (graceful if CRDs not installed) ---

func APIAvailable(groupVersion, kind string) bool {
	cfg := ctrl.GetConfigOrDie()
	disc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return false
	}
	lists, err := disc.ServerPreferredResources()
	if err != nil {
		// aggregated discovery errors are fine; still scan what we have
	}
	for _, l := range lists {
		if strings.HasPrefix(l.GroupVersion, groupVersion) {
			for _, r := range l.APIResources {
				if r.Kind == kind {
					return true
				}
			}
		}
	}
	return false
}

// --- Generic unstructured upsert (spec merge) ---

func UpsertUnstructured(ctx context.Context, c client.Client, u *unstructured.Unstructured, spec map[string]interface{}, owner *metav1.OwnerReference) error {
	key := types.NamespacedName{Namespace: u.GetNamespace(), Name: u.GetName()}

	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(u.GroupVersionKind())

	err := c.Get(ctx, key, current)
	if err != nil {
		// Create path
		obj := &unstructured.Unstructured{Object: map[string]interface{}{}}
		obj.SetGroupVersionKind(u.GroupVersionKind())
		obj.SetNamespace(u.GetNamespace())
		obj.SetName(u.GetName())

		// set labels/annotations carried on u
		obj.SetLabels(u.GetLabels())
		obj.SetAnnotations(u.GetAnnotations())

		// set ownerRef if provided (NOTE: cross-namespace owners are not allowed, so pass nil for those)
		if owner != nil {
			obj.SetOwnerReferences([]metav1.OwnerReference{*owner})
		}
		// set spec
		if spec != nil {
			if err := unstructured.SetNestedField(obj.Object, spec, "spec"); err != nil {
				return err
			}
		}
		return c.Create(ctx, obj)
	}

	// Update path: only replace spec
	if spec != nil {
		if err := unstructured.SetNestedField(current.Object, spec, "spec"); err != nil {
			return err
		}
	}
	// keep labels/annotations as-is (idempotent)
	return c.Update(ctx, current)
}

// --- Service + ServiceMonitor/PodMonitor helpers ---

type SMEndpoint struct {
	Port            string
	Interval        string
	Scheme          string
	TLSSkipVerify   bool
	BearerTokenFile string
}

func EnsureServiceMonitor(ctx context.Context, c client.Client, ns, name string,
	selector map[string]string, nsSelectorAny bool, nsMatch []string, endpoint SMEndpoint,
	labels map[string]string, owner *metav1.OwnerReference) error {

	if labels == nil {
		labels = map[string]string{}
	}
	// Tag for OTel TargetAllocator if you use it.
	if _, ok := labels["otel/scrape"]; !ok {
		labels["otel/scrape"] = "true"
	}
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schemaFor("monitoring.coreos.com/v1", "ServiceMonitor"))
	u.SetNamespace(ns)
	u.SetName(name)
	u.SetLabels(labels)

	spec := map[string]interface{}{
		"selector": map[string]interface{}{"matchLabels": selector},
		"endpoints": []interface{}{
			endpointToMap(endpoint),
		},
	}
	if nsSelectorAny {
		spec["namespaceSelector"] = map[string]interface{}{"any": true}
	} else if len(nsMatch) > 0 {
		spec["namespaceSelector"] = map[string]interface{}{"matchNames": nsMatch}
	}

	return UpsertUnstructured(ctx, c, u, spec, owner) // owner may be nil for cross-ns
}

func EnsurePodMonitor(ctx context.Context, c client.Client, ns, name string,
	selector map[string]interface{}, // supports matchLabels/matchExpressions
	endpoint SMEndpoint, labels map[string]string, owner *metav1.OwnerReference) error {

	if labels == nil {
		labels = map[string]string{}
	}
	if _, ok := labels["otel/scrape"]; !ok {
		labels["otel/scrape"] = "true"
	}
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schemaFor("monitoring.coreos.com/v1", "PodMonitor"))
	u.SetNamespace(ns)
	u.SetName(name)
	u.SetLabels(labels)

	spec := map[string]interface{}{
		"selector": selector,
		"podMetricsEndpoints": []interface{}{
			endpointToMap(endpoint),
		},
	}
	return UpsertUnstructured(ctx, c, u, spec, owner)
}

func EnsureHeadlessMetricsService(ctx context.Context, c client.Client,
	ns, name string, sel map[string]string, port int32, owner metav1.OwnerReference) error {

	svc := &corev1.Service{}
	svc.Namespace, svc.Name = ns, name
	err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, svc)
	if err != nil {
		svc = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: name, Namespace: ns,
				Labels:          sel,
				OwnerReferences: []metav1.OwnerReference{owner},
			},
			Spec: corev1.ServiceSpec{
				ClusterIP: "None",
				Selector:  sel,
				Ports: []corev1.ServicePort{{
					Name:       "metrics",
					Port:       port,
					TargetPort: intstr.FromInt(int(port)),
				}},
			},
		}
		return c.Create(ctx, svc)
	}
	// idempotent update
	svc.Spec.Selector = sel
	svc.Spec.Ports = []corev1.ServicePort{{
		Name: "metrics", Port: port, TargetPort: intstr.FromInt(int(port)),
	}}
	return c.Update(ctx, svc)
}

func EnsureOTelCollector(ctx context.Context, c client.Client, ns, name string,
	serviceMonitorSelector map[string]string, owner *metav1.OwnerReference) error {

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schemaFor("opentelemetry.io/v1beta1", "OpenTelemetryCollector"))
	u.SetNamespace(ns)
	u.SetName(name)

	cfg := `
receivers:
  prometheus:
    config:
      global:
        scrape_interval: 30s
      scrape_configs: [] # injected by targetAllocator
processors:
  k8sattributes:
    extract:
      metadata: [k8s.namespace.name, k8s.pod.name, k8s.node.name]
  batch: {}
exporters:
  logging:
    loglevel: info
service:
  pipelines:
    metrics:
      receivers: [prometheus]
      processors: [k8sattributes, batch]
      exporters: [logging]
`
	spec := map[string]interface{}{
		"mode": "daemonset",
		"targetAllocator": map[string]interface{}{
			"enabled": true,
			"prometheusCR": map[string]interface{}{
				"enabled": true,
				"serviceMonitorSelector": map[string]interface{}{
					"matchLabels": serviceMonitorSelector,
				},
			},
		},
		"config": cfg,
	}
	return UpsertUnstructured(ctx, c, u, spec, owner)
}

// --- small utils ---

func endpointToMap(e SMEndpoint) map[string]interface{} {
	m := map[string]interface{}{
		"port":     e.Port,
		"interval": orDefault(e.Interval, "30s"),
	}
	if e.Scheme != "" {
		m["scheme"] = e.Scheme
	}
	if e.TLSSkipVerify {
		m["tlsConfig"] = map[string]interface{}{"insecureSkipVerify": true}
	}
	if e.BearerTokenFile != "" {
		m["bearerTokenFile"] = e.BearerTokenFile
	}
	return m
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func schemaFor(gv, kind string) schema.GroupVersionKind {
	parts := strings.Split(gv, "/")
	return schema.GroupVersionKind{Group: parts[0], Version: parts[1], Kind: kind}
}

// Sleep helper for eventual consistency (rarely needed, but handy)
func Pause(d time.Duration) { time.Sleep(d) }
