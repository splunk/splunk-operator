package genai

import (
	"context"
	"fmt"
	//"reflect"

	//monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1" // Import Prometheus operator API
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	//"gopkg.in/yaml.v2"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PrometheusRuleReconciler is an interface for reconciling Prometheus rules and associated resources.
type PrometheusRuleReconciler interface {
	Reconcile(ctx context.Context) error
	ReconcileConfigMap(ctx context.Context) error
	ReconcilePrometheusRule(ctx context.Context) error
}

// prometheusRuleReconcilerImpl is the concrete implementation of PrometheusRuleReconciler.
type prometheusRuleReconcilerImpl struct {
	client.Client
	genAIDeployment *enterpriseApi.GenAIDeployment
	eventRecorder   *splutil.K8EventPublisher
}

// NewPrometheusRuleReconciler creates a new instance of PrometheusRuleReconciler.
func NewPrometheusRuleReconciler(c client.Client, genAIDeployment *enterpriseApi.GenAIDeployment, eventRecorder *splutil.K8EventPublisher) PrometheusRuleReconciler {
	return &prometheusRuleReconcilerImpl{
		Client:          c,
		genAIDeployment: genAIDeployment,
		eventRecorder:   eventRecorder,
	}
}

// Reconcile manages the complete reconciliation logic for Prometheus rules and returns an error if any.
func (r *prometheusRuleReconcilerImpl) Reconcile(ctx context.Context) error {
	// Reconcile the ConfigMap for PrometheusRule
	if err := r.ReconcileConfigMap(ctx); err != nil {
		return err
	}

	// Reconcile the PrometheusRule using the data from the ConfigMap
	if err := r.ReconcilePrometheusRule(ctx); err != nil {
		return err
	}

	return nil
}

func (r *prometheusRuleReconcilerImpl) ReconcileConfigMap(ctx context.Context) error {
	configMapName := "prometheus-rules-config"
	desiredConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: r.genAIDeployment.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "prometheus-rules",
				"app.kubernetes.io/managed-by": "Helm",
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(r.genAIDeployment, enterpriseApi.GroupVersion.WithKind("GenAIDeployment")),
			},
		},
		Data: map[string]string{
			"prometheus_rules.yaml": `
groups:
  - name: example.rules
    rules:
      - alert: ExampleAlert
        expr: vector(1)
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "This is an example alert"
`,
		},
	}

	// Fetch the existing ConfigMap
	existingConfigMap := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: r.genAIDeployment.Namespace}, existingConfigMap)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to get existing ConfigMap %s: %w", configMapName, err)
		}

		// ConfigMap does not exist, so create it
		if err := r.Create(ctx, desiredConfigMap); err != nil {
			return fmt.Errorf("failed to create ConfigMap %s: %w", configMapName, err)
		}
		return nil
	}

	return nil
}

func (r *prometheusRuleReconcilerImpl) ReconcilePrometheusRule(ctx context.Context) error {
	configMapName := "prometheus-rules-config"
	//prometheusRuleName := "weaviate-prometheus-rules"

	// Fetch the existing ConfigMap to get Prometheus rules
	existingConfigMap := &corev1.ConfigMap{}
	if err := r.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: r.genAIDeployment.Namespace}, existingConfigMap); err != nil {
		return fmt.Errorf("failed to get ConfigMap %s: %w", configMapName, err)
	}
	/*
		// Extract rules from ConfigMap
		ruleData, ok := existingConfigMap.Data["prometheus_rules.yaml"]
		if !ok {
			return fmt.Errorf("Prometheus rules not found in ConfigMap %s", configMapName)
		}

		// Desired PrometheusRule object
		desiredPrometheusRule := &monitoringv1.PrometheusRule{
			ObjectMeta: metav1.ObjectMeta{
				Name:      prometheusRuleName,
				Namespace: r.genAIDeployment.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/name":       "prometheus-rules",
					"app.kubernetes.io/managed-by": "Helm",
				},
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(r.genAIDeployment, enterpriseApi.GroupVersion.WithKind("GenAIDeployment")),
				},
			},
			Spec: monitoringv1.PrometheusRuleSpec{
				Groups: []monitoringv1.RuleGroup{
					{
						Name:  "example.rules",
						Rules: []monitoringv1.Rule{},
					},
				},
			},
		}

		// Unmarshal rule data into PrometheusRuleSpec
		if err := yaml.Unmarshal([]byte(ruleData), &desiredPrometheusRule.Spec); err != nil {
			return fmt.Errorf("failed to unmarshal Prometheus rules: %w", err)
		}

		// Fetch the existing PrometheusRule
		existingPrometheusRule := &monitoringv1.PrometheusRule{}
		err := r.Get(ctx, client.ObjectKey{Name: prometheusRuleName, Namespace: r.genAIDeployment.Namespace}, existingPrometheusRule)
		if err != nil {
			if client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("failed to get existing PrometheusRule %s: %w", prometheusRuleName, err)
			}

			// PrometheusRule does not exist, so create it
			if err := r.Create(ctx, desiredPrometheusRule); err != nil {
				return fmt.Errorf("failed to create PrometheusRule %s: %w", prometheusRuleName, err)
			}
			return nil
		}

		// If the existing PrometheusRule differs from the desired, update it
		if !reflect.DeepEqual(existingPrometheusRule.Spec, desiredPrometheusRule.Spec) {
			existingPrometheusRule.Spec = desiredPrometheusRule.Spec
			if err := r.Update(ctx, existingPrometheusRule); err != nil {
				return fmt.Errorf("failed to update PrometheusRule %s: %w", prometheusRuleName, err)
			}
		}
	*/
	return nil
}
