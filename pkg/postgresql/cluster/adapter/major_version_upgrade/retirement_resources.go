/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package majorupgradeadapter

import (
	"context"
	"fmt"

	certv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	tlsport "github.com/splunk/splunk-operator/pkg/postgresql/cluster/ports/tls"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// Only these resource kinds may become retirement roots.
var retirementRootScheme = func() *runtime.Scheme {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(cnpgv1.SchemeGroupVersion, &cnpgv1.Cluster{}, &cnpgv1.Database{}, &cnpgv1.Pooler{}, &cnpgv1.ScheduledBackup{}, &cnpgv1.Publication{}, &cnpgv1.Subscription{})
	scheme.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.Secret{})
	scheme.AddKnownTypes(certv1.SchemeGroupVersion, &certv1.Certificate{})
	return scheme
}()

func resourceReference(object client.Object) (corev1.ObjectReference, error) {
	gvk, err := apiutil.GVKForObject(object, retirementRootScheme)
	return corev1.ObjectReference{APIVersion: gvk.GroupVersion().String(), Kind: gvk.Kind, Namespace: object.GetNamespace(), Name: object.GetName(), UID: object.GetUID()}, err
}

func cleanupRootObject(ref corev1.ObjectReference) (client.Object, error) {
	if ref.UID == "" || ref.Name == "" || ref.Namespace == "" {
		return nil, fmt.Errorf("retirement root has incomplete identity")
	}
	object, err := retirementRootScheme.New(schema.FromAPIVersionAndKind(ref.APIVersion, ref.Kind))
	if err != nil {
		return nil, fmt.Errorf("unsupported retirement root %s %s: %w", ref.APIVersion, ref.Kind, err)
	}
	return object.(client.Object), nil
}

func mergeRetirementRoots(previous, observed []corev1.ObjectReference, objects ...client.Object) ([]corev1.ObjectReference, error) {
	for _, object := range objects {
		ref, err := resourceReference(object)
		if err != nil {
			return nil, err
		}
		observed = append(observed, ref)
	}
	merged, _, err := mergeCleanupReferences(previous, observed)
	return merged, err
}

func namespaceObjects(ctx context.Context, reader client.Reader, namespace string, lists ...client.ObjectList) ([]client.Object, error) {
	var result []client.Object
	for _, list := range lists {
		if err := reader.List(ctx, list, client.InNamespace(namespace)); err != nil {
			if _, optional := list.(*certv1.CertificateList); optional && meta.IsNoMatchError(err) {
				continue
			}
			return nil, err
		}
		objects, err := meta.ExtractList(list)
		if err != nil {
			return nil, err
		}
		for _, object := range objects {
			result = append(result, object.(client.Object))
		}
	}
	return result, nil
}

func isParentServerTLS(labels map[string]string, parent *platformv1alpha1.PostgresCluster) bool {
	return labels[tlsport.ClusterUIDLabel] == string(parent.UID) && labels[tlsport.PurposeLabel] == tlsport.ServerTLSPurpose
}

func certificateLeaf(ctx context.Context, reader client.Reader, parent *platformv1alpha1.PostgresCluster, certificate *certv1.Certificate) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	if err := reader.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: certificate.Spec.SecretName}, secret); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	owner := metav1.GetControllerOf(secret)
	if secret.Type != corev1.SecretTypeTLS || !isParentServerTLS(secret.Labels, parent) || owner != nil && owner.UID != parent.UID && owner.UID != certificate.UID {
		return nil, fmt.Errorf("TLS Secret %s has conflicting provenance", secret.Name)
	}
	return secret, nil
}

// discoverAttemptCandidates excludes the recorded source; callers enforce lifecycle policy.
func discoverAttemptCandidates(ctx context.Context, reader client.Reader, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) ([]cnpgv1.Cluster, error) {
	var clusters cnpgv1.ClusterList
	if err := reader.List(ctx, &clusters, client.InNamespace(parent.Namespace), client.MatchingLabels{blueGreenAttemptLabel: bg.AttemptID}); err != nil {
		return nil, err
	}
	var candidates []cnpgv1.Cluster
	for i := range clusters.Items {
		candidate := &clusters.Items[i]
		if !metav1.IsControlledBy(candidate, parent) || bg.Blue != nil && candidate.UID == bg.Blue.Ref.UID {
			continue
		}
		candidates = append(candidates, *candidate)
	}
	return candidates, nil
}

type databaseOwners map[types.UID]string

func (owners databaseOwners) owns(object client.Object) bool {
	owner := metav1.GetControllerOf(object)
	return owner != nil && owner.Kind == "PostgresDatabase" && owner.APIVersion == platformv1alpha1.GroupVersion.String() && owners[owner.UID] == owner.Name && owner.UID != ""
}
