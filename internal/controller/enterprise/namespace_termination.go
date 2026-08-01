package controller

import (
	"context"
	"fmt"

	"github.com/splunk/splunk-operator/pkg/logging"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get

// shouldStopForTerminatingNamespace returns true when ordinary reconciliation
// must not mutate resources in the named namespace. Callers must bypass this
// check once their custom resource has its own deletion timestamp so existing
// finalizers can continue to completion.
func shouldStopForTerminatingNamespace(ctx context.Context, reader client.Reader, namespaceName string) (bool, error) {
	namespace := &corev1.Namespace{}
	if err := reader.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace); err != nil {
		if k8serrors.IsNotFound(err) {
			logging.FromContext(ctx).InfoContext(ctx,
				"namespace is absent; skipping normal reconciliation",
				"targetNamespace", namespaceName,
			)
			return true, nil
		}
		return false, fmt.Errorf("read namespace %q before reconciliation: %w", namespaceName, err)
	}

	if namespace.GetDeletionTimestamp() == nil && namespace.Status.Phase != corev1.NamespaceTerminating {
		return false, nil
	}

	deletionTimestamp := ""
	if namespace.GetDeletionTimestamp() != nil {
		deletionTimestamp = namespace.GetDeletionTimestamp().UTC().Format("2006-01-02T15:04:05.000000000Z")
	}
	logging.FromContext(ctx).InfoContext(ctx,
		"namespace is terminating; skipping normal reconciliation",
		"targetNamespace", namespaceName,
		"namespacePhase", namespace.Status.Phase,
		"namespaceDeletionTimestamp", deletionTimestamp,
	)
	return true, nil
}
