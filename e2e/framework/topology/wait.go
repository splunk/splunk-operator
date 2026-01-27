package topology

import (
	"context"
	"fmt"
	"strings"
	"time"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/e2e/framework/k8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	pollInterval           = 5 * time.Second
	consistentPollInterval = 200 * time.Millisecond
	consistentDuration     = 2 * time.Second
)

// WaitStandaloneReady waits for standalone phase ready.
func WaitStandaloneReady(ctx context.Context, kube *k8s.Client, namespace, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		instance := &enterpriseApi.Standalone{}
		if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
			return false, nil
		}
		if err := checkPodForFailure(ctx, kube, namespace, fmt.Sprintf("splunk-%s-standalone-0", name)); err != nil {
			return false, err
		}
		return instance.Status.Phase == enterpriseApi.PhaseReady, nil
	})
}

// WaitClusterManagerReady waits for cluster manager ready.
func WaitClusterManagerReady(ctx context.Context, kube *k8s.Client, namespace, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		instance := &enterpriseApi.ClusterManager{}
		if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
			return false, nil
		}
		return instance.Status.Phase == enterpriseApi.PhaseReady, nil
	})
}

// WaitClusterMasterReady waits for cluster master ready.
func WaitClusterMasterReady(ctx context.Context, kube *k8s.Client, namespace, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		instance := &enterpriseApiV3.ClusterMaster{}
		if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
			return false, nil
		}
		return instance.Status.Phase == enterpriseApi.PhaseReady, nil
	})
}

// WaitIndexerClusterReady waits for indexer cluster ready.
func WaitIndexerClusterReady(ctx context.Context, kube *k8s.Client, namespace, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		instance := &enterpriseApi.IndexerCluster{}
		if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
			return false, nil
		}
		return instance.Status.Phase == enterpriseApi.PhaseReady, nil
	})
}

// WaitSearchHeadClusterReady waits for search head cluster ready.
func WaitSearchHeadClusterReady(ctx context.Context, kube *k8s.Client, namespace, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		instance := &enterpriseApi.SearchHeadCluster{}
		if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
			return false, nil
		}
		return instance.Status.Phase == enterpriseApi.PhaseReady && instance.Status.DeployerPhase == enterpriseApi.PhaseReady, nil
	})
}

// WaitLicenseManagerReady waits for license manager ready and stable.
func WaitLicenseManagerReady(ctx context.Context, kube *k8s.Client, namespace, name string, timeout time.Duration) error {
	if err := wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		instance := &enterpriseApi.LicenseManager{}
		if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
			return false, nil
		}
		return instance.Status.Phase == enterpriseApi.PhaseReady, nil
	}); err != nil {
		return err
	}
	return waitConsistent(ctx, consistentDuration, consistentPollInterval, func(ctx context.Context) (bool, error) {
		instance := &enterpriseApi.LicenseManager{}
		if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
			return false, err
		}
		return instance.Status.Phase == enterpriseApi.PhaseReady, nil
	})
}

// WaitLicenseMasterReady waits for license master ready and stable.
func WaitLicenseMasterReady(ctx context.Context, kube *k8s.Client, namespace, name string, timeout time.Duration) error {
	if err := wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		instance := &enterpriseApiV3.LicenseMaster{}
		if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
			return false, nil
		}
		return instance.Status.Phase == enterpriseApi.PhaseReady, nil
	}); err != nil {
		return err
	}
	return waitConsistent(ctx, consistentDuration, consistentPollInterval, func(ctx context.Context) (bool, error) {
		instance := &enterpriseApiV3.LicenseMaster{}
		if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
			return false, err
		}
		return instance.Status.Phase == enterpriseApi.PhaseReady, nil
	})
}

// WaitMonitoringConsoleReady waits for monitoring console ready and stable.
func WaitMonitoringConsoleReady(ctx context.Context, kube *k8s.Client, namespace, name string, timeout time.Duration) error {
	if err := wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		instance := &enterpriseApi.MonitoringConsole{}
		if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
			return false, nil
		}
		return instance.Status.Phase == enterpriseApi.PhaseReady, nil
	}); err != nil {
		return err
	}
	return waitConsistent(ctx, consistentDuration, consistentPollInterval, func(ctx context.Context) (bool, error) {
		instance := &enterpriseApi.MonitoringConsole{}
		if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
			return false, err
		}
		return instance.Status.Phase == enterpriseApi.PhaseReady, nil
	})
}

// WaitStandaloneStable checks standalone stays ready for a duration.
func WaitStandaloneStable(ctx context.Context, kube *k8s.Client, namespace, name string, duration, interval time.Duration) error {
	return waitConsistent(ctx, duration, interval, func(ctx context.Context) (bool, error) {
		instance := &enterpriseApi.Standalone{}
		if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
			return false, err
		}
		if err := checkPodForFailure(ctx, kube, namespace, fmt.Sprintf("splunk-%s-standalone-0", name)); err != nil {
			return false, err
		}
		return instance.Status.Phase == enterpriseApi.PhaseReady, nil
	})
}

// WaitClusterManagerStable checks cluster manager stays ready for a duration.
func WaitClusterManagerStable(ctx context.Context, kube *k8s.Client, namespace, name string, duration, interval time.Duration) error {
	return waitConsistent(ctx, duration, interval, func(ctx context.Context) (bool, error) {
		instance := &enterpriseApi.ClusterManager{}
		if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
			return false, err
		}
		return instance.Status.Phase == enterpriseApi.PhaseReady, nil
	})
}

// WaitClusterMasterStable checks cluster master stays ready for a duration.
func WaitClusterMasterStable(ctx context.Context, kube *k8s.Client, namespace, name string, duration, interval time.Duration) error {
	return waitConsistent(ctx, duration, interval, func(ctx context.Context) (bool, error) {
		instance := &enterpriseApiV3.ClusterMaster{}
		if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
			return false, err
		}
		return instance.Status.Phase == enterpriseApi.PhaseReady, nil
	})
}

// WaitIndexerClusterStable checks indexer cluster stays ready for a duration.
func WaitIndexerClusterStable(ctx context.Context, kube *k8s.Client, namespace, name string, duration, interval time.Duration) error {
	return waitConsistent(ctx, duration, interval, func(ctx context.Context) (bool, error) {
		instance := &enterpriseApi.IndexerCluster{}
		if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
			return false, err
		}
		return instance.Status.Phase == enterpriseApi.PhaseReady, nil
	})
}

// WaitSearchHeadClusterStable checks search head cluster stays ready for a duration.
func WaitSearchHeadClusterStable(ctx context.Context, kube *k8s.Client, namespace, name string, duration, interval time.Duration) error {
	return waitConsistent(ctx, duration, interval, func(ctx context.Context) (bool, error) {
		instance := &enterpriseApi.SearchHeadCluster{}
		if err := kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance); err != nil {
			return false, err
		}
		return instance.Status.Phase == enterpriseApi.PhaseReady && instance.Status.DeployerPhase == enterpriseApi.PhaseReady, nil
	})
}

func waitConsistent(ctx context.Context, duration, interval time.Duration, check func(context.Context) (bool, error)) error {
	if duration <= 0 {
		duration = consistentDuration
	}
	if interval <= 0 {
		interval = consistentPollInterval
	}

	deadline := time.Now().Add(duration)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		ok, err := check(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("state did not remain ready for %s", duration)
		}
		if time.Now().After(deadline) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func checkPodForFailure(ctx context.Context, kube *k8s.Client, namespace, podName string) error {
	pod := &corev1.Pod{}
	if err := kube.Client.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, pod); err != nil {
		return nil
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Waiting != nil {
			reason := status.State.Waiting.Reason
			switch reason {
			case "ImagePullBackOff", "ErrImagePull", "CrashLoopBackOff", "CreateContainerConfigError":
				message := status.State.Waiting.Message
				if message == "" {
					message = reason
				}
				return fmt.Errorf("pod %s failed: %s", podName, strings.TrimSpace(message))
			}
		}
	}
	return nil
}
