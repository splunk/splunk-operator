package operatorreadiness

import (
	"fmt"
	"os"
	"strings"
)

const serviceAccountNamespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// ResolvePodNamespace returns the downward-API value when present and falls
// back to the same service-account namespace file controller-runtime uses for
// an in-cluster leader Lease.
func ResolvePodNamespace(explicit string) (string, error) {
	return resolvePodNamespace(explicit, serviceAccountNamespacePath)
}

func resolvePodNamespace(explicit, fallbackPath string) (string, error) {
	if namespace := strings.TrimSpace(explicit); namespace != "" {
		return namespace, nil
	}
	value, err := os.ReadFile(fallbackPath)
	if err != nil {
		return "", fmt.Errorf("read service-account namespace: %w", err)
	}
	namespace := strings.TrimSpace(string(value))
	if namespace == "" {
		return "", fmt.Errorf("service-account namespace file %q is empty", fallbackPath)
	}
	return namespace, nil
}
