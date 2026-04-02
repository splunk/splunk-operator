package core

import (
	"context"
	"fmt"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// metrics
	postgresMetricsServiceSuffix = "-postgres-metrics"
	postgresMetricsPortName      = "metrics"
	postgresMetricsPort          = int32(9187)
	poolerMetricsPortName        = "metrics"
	poolerMetricsPort            = int32(9127)

	// labels
	labelManagedBy              = "app.kubernetes.io/managed-by"
	labelManagedByValue         = "postgrescluster-controller"
	labelObservabilityComponent = "enterprise.splunk.com/observability-component"
	cnpgClusterLabelName        = "cnpg.io/cluster"
	cnpgPoolerNameLabel         = "cnpg.io/poolerName"
	cnpgPodRoleInstance         = "instance"
	cnpgPodRoleLabelName        = "cnpg.io/podRole"
)

func isPostgreSQLMetricsEnabled(cluster *enterprisev4.PostgresCluster, class *enterprisev4.PostgresClusterClass) bool {
	if class == nil || class.Spec.Config == nil || class.Spec.Config.Observability == nil {
		return false
	}
	classCfg := class.Spec.Config.Observability.PostgreSQL
	if classCfg == nil || classCfg.Enabled == nil || !*classCfg.Enabled {
		return false
	}
	if cluster == nil || cluster.Spec.Observability == nil || cluster.Spec.Observability.PostgreSQL == nil {
		return true
	}
	override := cluster.Spec.Observability.PostgreSQL.Disabled
	return override == nil || !*override
}

func isConnectionPoolerEnabled(cluster *enterprisev4.PostgresCluster, class *enterprisev4.PostgresClusterClass) bool {
	if class == nil || class.Spec.Config == nil || class.Spec.Config.ConnectionPoolerEnabled == nil {
		return false
	}
	if !*class.Spec.Config.ConnectionPoolerEnabled {
		return false
	}
	if cluster == nil || cluster.Spec.ConnectionPoolerEnabled == nil {
		return true
	}
	return *cluster.Spec.ConnectionPoolerEnabled
}

func isConnectionPoolerMetricsEnabled(cluster *enterprisev4.PostgresCluster, class *enterprisev4.PostgresClusterClass) bool {
	if !isConnectionPoolerEnabled(cluster, class) {
		return false
	}
	if class == nil || class.Spec.Config == nil || class.Spec.Config.Observability == nil {
		return false
	}
	classCfg := class.Spec.Config.Observability.PgBouncer
	if classCfg == nil || classCfg.Enabled == nil || !*classCfg.Enabled {
		return false
	}
	if cluster == nil || cluster.Spec.Observability == nil || cluster.Spec.Observability.PgBouncer == nil {
		return true
	}
	override := cluster.Spec.Observability.PgBouncer.Disabled
	return override == nil || !*override
}

func buildPostgreSQLMetricsService(scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster) (*corev1.Service, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name + postgresMetricsServiceSuffix,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				labelManagedBy:              labelManagedByValue,
				labelObservabilityComponent: "postgresql-metrics",
				cnpgClusterLabelName:        cluster.Name,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				cnpgClusterLabelName: cluster.Name,
				cnpgPodRoleLabelName: cnpgPodRoleInstance,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       postgresMetricsPortName,
					Port:       postgresMetricsPort,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromString(postgresMetricsPortName),
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(cluster, svc, scheme); err != nil {
		return nil, fmt.Errorf("setting controller reference on PostgreSQL metrics Service: %w", err)
	}

	return svc, nil
}

func poolerMetricsServiceName(clusterName, poolerType string) string {
	return fmt.Sprintf("%s-pooler-%s-metrics", clusterName, poolerType)
}
func buildConnectionPoolerMetricsService(
	scheme *runtime.Scheme,
	cluster *enterprisev4.PostgresCluster,
	poolerType string,
) (*corev1.Service, error) {
	poolerName := poolerResourceName(cluster.Name, poolerType)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      poolerMetricsServiceName(cluster.Name, poolerType),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				labelManagedBy:              labelManagedByValue,
				labelObservabilityComponent: "pgbouncer-metrics",
				cnpgClusterLabelName:        cluster.Name,
				cnpgPoolerNameLabel:         poolerName,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				cnpgPoolerNameLabel: poolerName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       poolerMetricsPortName,
					Port:       poolerMetricsPort,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromString(poolerMetricsPortName),
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(cluster, svc, scheme); err != nil {
		return nil, fmt.Errorf("setting controller reference on PgBouncer metrics Service: %w", err)
	}

	return svc, nil
}

func reconcilePostgreSQLMetricsService(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, enabled bool) error {
	logger := log.FromContext(ctx)
	serviceName := cluster.Name + postgresMetricsServiceSuffix

	if !enabled {
		existing := &corev1.Service{}
		err := c.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: cluster.Namespace}, existing)
		switch {
		case apierrors.IsNotFound(err):
			return nil
		case err != nil:
			return fmt.Errorf("getting PostgreSQL metrics Service %s: %w", serviceName, err)
		}

		logger.Info("Deleting PostgreSQL metrics Service", "name", serviceName)
		if err := c.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting PostgreSQL metrics Service %s: %w", serviceName, err)
		}
		return nil
	}

	desired, err := buildPostgreSQLMetricsService(scheme, cluster)
	if err != nil {
		return fmt.Errorf("building PostgreSQL metrics Service: %w", err)
	}

	live := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, c, live, func() error {
		live.Labels = desired.Labels
		live.Annotations = desired.Annotations
		live.Spec.Type = desired.Spec.Type
		live.Spec.Selector = desired.Spec.Selector
		live.Spec.Ports = desired.Spec.Ports

		if !metav1.IsControlledBy(live, cluster) {
			if err := ctrl.SetControllerReference(cluster, live, scheme); err != nil {
				return fmt.Errorf("setting controller reference on PostgreSQL metrics Service: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("reconciling PostgreSQL metrics Service %s: %w", desired.Name, err)
	}

	return nil
}

func reconcileConnectionPoolerMetricsService(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	cluster *enterprisev4.PostgresCluster,
	poolerType string,
	enabled bool,
) error {
	logger := log.FromContext(ctx)
	serviceName := poolerMetricsServiceName(cluster.Name, poolerType)

	if !enabled {
		existing := &corev1.Service{}
		err := c.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: cluster.Namespace}, existing)
		switch {
		case apierrors.IsNotFound(err):
			return nil
		case err != nil:
			return fmt.Errorf("getting PgBouncer metrics Service %s: %w", serviceName, err)
		}

		logger.Info("Deleting PgBouncer metrics Service", "name", serviceName, "poolerType", poolerType)
		if err := c.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting PgBouncer metrics Service %s: %w", serviceName, err)
		}
		return nil
	}

	desired, err := buildConnectionPoolerMetricsService(scheme, cluster, poolerType)
	if err != nil {
		return fmt.Errorf("building PgBouncer metrics Service for %s pooler: %w", poolerType, err)
	}

	live := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, c, live, func() error {
		live.Labels = desired.Labels
		live.Annotations = desired.Annotations
		live.Spec.Type = desired.Spec.Type
		live.Spec.Selector = desired.Spec.Selector
		live.Spec.Ports = desired.Spec.Ports

		if !metav1.IsControlledBy(live, cluster) {
			if err := ctrl.SetControllerReference(cluster, live, scheme); err != nil {
				return fmt.Errorf("setting controller reference on PgBouncer metrics Service: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("reconciling PgBouncer metrics Service %s: %w", desired.Name, err)
	}

	return nil
}

func postgresMetricsServiceMonitorName(clusterName string) string {
	return clusterName + "-postgres-metrics-monitor"
}

func poolerMetricsServiceMonitorName(clusterName, poolerType string) string {
	return fmt.Sprintf("%s-pooler-%s-metrics-monitor", clusterName, poolerType)
}

func buildPostgreSQLMetricsServiceMonitor(
	scheme *runtime.Scheme,
	cluster *enterprisev4.PostgresCluster,
) (*monitoringv1.ServiceMonitor, error) {
	sm := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      postgresMetricsServiceMonitorName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				labelManagedBy:              labelManagedByValue,
				labelObservabilityComponent: "postgresql-metrics",
				cnpgClusterLabelName:        cluster.Name,
			},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					labelObservabilityComponent: "postgresql-metrics",
					cnpgClusterLabelName:        cluster.Name,
				},
			},
			Endpoints: []monitoringv1.Endpoint{
				{
					Port:   postgresMetricsPortName,
					Path:   "/metrics",
					Scheme: "http",
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(cluster, sm, scheme); err != nil {
		return nil, fmt.Errorf("setting controller reference on PostgreSQL ServiceMonitor: %w", err)
	}

	return sm, nil
}

func buildConnectionPoolerMetricsServiceMonitor(
	scheme *runtime.Scheme,
	cluster *enterprisev4.PostgresCluster,
	poolerType string,
) (*monitoringv1.ServiceMonitor, error) {
	poolerName := poolerResourceName(cluster.Name, poolerType)

	sm := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      poolerMetricsServiceMonitorName(cluster.Name, poolerType),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				labelManagedBy:              labelManagedByValue,
				labelObservabilityComponent: "pgbouncer-metrics",
				cnpgClusterLabelName:        cluster.Name,
				cnpgPoolerNameLabel:         poolerName,
			},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					labelObservabilityComponent: "pgbouncer-metrics",
					cnpgClusterLabelName:        cluster.Name,
					cnpgPoolerNameLabel:         poolerName,
				},
			},
			Endpoints: []monitoringv1.Endpoint{
				{
					Port:   poolerMetricsPortName,
					Path:   "/metrics",
					Scheme: "http",
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(cluster, sm, scheme); err != nil {
		return nil, fmt.Errorf("setting controller reference on PgBouncer ServiceMonitor: %w", err)
	}

	return sm, nil
}

func reconcilePostgreSQLMetricsServiceMonitor(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	cluster *enterprisev4.PostgresCluster,
	enabled bool,
) error {
	logger := log.FromContext(ctx)
	name := postgresMetricsServiceMonitorName(cluster.Name)

	if !enabled {
		existing := &monitoringv1.ServiceMonitor{}
		err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: cluster.Namespace}, existing)
		switch {
		case apierrors.IsNotFound(err):
			return nil
		case err != nil:
			return fmt.Errorf("getting PostgreSQL ServiceMonitor %s: %w", name, err)
		}

		logger.Info("Deleting PostgreSQL ServiceMonitor", "name", name)
		if err := c.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting PostgreSQL ServiceMonitor %s: %w", name, err)
		}
		return nil
	}

	desired, err := buildPostgreSQLMetricsServiceMonitor(scheme, cluster)
	if err != nil {
		return fmt.Errorf("building PostgreSQL ServiceMonitor: %w", err)
	}

	live := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, c, live, func() error {
		live.Labels = desired.Labels
		live.Annotations = desired.Annotations
		live.Spec = desired.Spec

		if !metav1.IsControlledBy(live, cluster) {
			if err := ctrl.SetControllerReference(cluster, live, scheme); err != nil {
				return fmt.Errorf("setting controller reference on PostgreSQL ServiceMonitor: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("reconciling PostgreSQL ServiceMonitor %s: %w", desired.Name, err)
	}

	return nil
}

func reconcileConnectionPoolerMetricsServiceMonitor(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	cluster *enterprisev4.PostgresCluster,
	poolerType string,
	enabled bool,
) error {
	logger := log.FromContext(ctx)
	name := poolerMetricsServiceMonitorName(cluster.Name, poolerType)

	if !enabled {
		existing := &monitoringv1.ServiceMonitor{}
		err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: cluster.Namespace}, existing)
		switch {
		case apierrors.IsNotFound(err):
			return nil
		case err != nil:
			return fmt.Errorf("getting PgBouncer ServiceMonitor %s: %w", name, err)
		}

		logger.Info("Deleting PgBouncer ServiceMonitor", "name", name, "poolerType", poolerType)
		if err := c.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting PgBouncer ServiceMonitor %s: %w", name, err)
		}
		return nil
	}

	desired, err := buildConnectionPoolerMetricsServiceMonitor(scheme, cluster, poolerType)
	if err != nil {
		return fmt.Errorf("building PgBouncer ServiceMonitor for %s pooler: %w", poolerType, err)
	}

	live := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, c, live, func() error {
		live.Labels = desired.Labels
		live.Annotations = desired.Annotations
		live.Spec = desired.Spec

		if !metav1.IsControlledBy(live, cluster) {
			if err := ctrl.SetControllerReference(cluster, live, scheme); err != nil {
				return fmt.Errorf("setting controller reference on PgBouncer ServiceMonitor: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("reconciling PgBouncer ServiceMonitor %s: %w", desired.Name, err)
	}

	return nil
}
