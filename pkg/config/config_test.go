package config

import (
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestManagerOptionsDisablesNamespaceCache(t *testing.T) {
	t.Setenv(WatchNamespaceEnvVar, "")

	options := ManagerOptionsWithNamespaces(logr.Discard(), ctrl.Options{})
	if options.Client.Cache == nil {
		t.Fatal("client cache options must be initialized")
	}
	if !containsNamespace(options.Client.Cache.DisableFor) {
		t.Fatal("Namespace reads must bypass the manager cache")
	}
}

func TestManagerOptionsPreservesExistingCacheConfiguration(t *testing.T) {
	t.Setenv(WatchNamespaceEnvVar, "first,second")

	existing := &corev1.ConfigMap{}
	options := ctrl.Options{Client: client.Options{Cache: &client.CacheOptions{
		DisableFor: []client.Object{existing},
	}}}
	options = ManagerOptionsWithNamespaces(logr.Discard(), options)

	if len(options.Client.Cache.DisableFor) != 2 {
		t.Fatalf("DisableFor length = %d, want 2", len(options.Client.Cache.DisableFor))
	}
	if _, ok := options.Client.Cache.DisableFor[0].(*corev1.ConfigMap); !ok {
		t.Fatalf("first disabled object = %T, want *corev1.ConfigMap", options.Client.Cache.DisableFor[0])
	}
	if !containsNamespace(options.Client.Cache.DisableFor) {
		t.Fatal("Namespace reads must bypass the manager cache")
	}
	if len(options.Cache.DefaultNamespaces) != 2 {
		t.Fatalf("default namespace count = %d, want 2", len(options.Cache.DefaultNamespaces))
	}
}

func containsNamespace(objects []client.Object) bool {
	for _, object := range objects {
		if _, ok := object.(*corev1.Namespace); ok {
			return true
		}
	}
	return false
}
