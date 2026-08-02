package main

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	clustercore "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
)

func TestOptionalObjectStoreInformerRequiresInstalledCRD(t *testing.T) {
	mapper := meta.NewDefaultRESTMapper(nil)
	object, err := optionalObjectStoreInformer(mapper)
	if err != nil {
		t.Fatalf("optionalObjectStoreInformer() absent error = %v", err)
	}
	if object != nil {
		t.Fatalf("optionalObjectStoreInformer() absent object = %T, want nil", object)
	}

	gvk := clustercore.ObjectStoreGVK
	mapper = meta.NewDefaultRESTMapper([]schema.GroupVersion{gvk.GroupVersion()})
	mapper.Add(gvk, meta.RESTScopeNamespace)
	object, err = optionalObjectStoreInformer(mapper)
	if err != nil {
		t.Fatalf("optionalObjectStoreInformer() installed error = %v", err)
	}
	if object == nil {
		t.Fatal("optionalObjectStoreInformer() installed object = nil")
	}
	if object.GetObjectKind().GroupVersionKind() != gvk {
		t.Fatalf("optionalObjectStoreInformer() GVK = %v, want %v", object.GetObjectKind().GroupVersionKind(), gvk)
	}
}
