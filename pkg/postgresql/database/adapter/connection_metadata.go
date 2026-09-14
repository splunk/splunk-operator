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

package adapter

import (
	"context"
	"fmt"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/logging"
	dbk8s "github.com/splunk/splunk-operator/pkg/postgresql/database/infrastructure/k8s"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const managedByLabel = "app.kubernetes.io/managed-by"

// ConnectionMetadataPublisher applies database connection ConfigMaps.
type ConnectionMetadataPublisher struct {
	client client.Client
	scheme *runtime.Scheme
}

// NewConnectionMetadataPublisher returns a Kubernetes-backed publisher.
func NewConnectionMetadataPublisher(c client.Client, scheme *runtime.Scheme) *ConnectionMetadataPublisher {
	return &ConnectionMetadataPublisher{client: c, scheme: scheme}
}

func (p *ConnectionMetadataPublisher) Apply(
	ctx context.Context,
	target dbtypes.ConnectionMetadataTarget,
	publication dbtypes.ConnectionMetadataPublication,
) error {
	owner := &platformv1alpha1.PostgresDatabase{
		TypeMeta: metav1.TypeMeta{APIVersion: platformv1alpha1.GroupVersion.String(), Kind: "PostgresDatabase"},
		ObjectMeta: metav1.ObjectMeta{
			Name: target.Name, Namespace: target.Namespace, UID: types.UID(target.UID),
		},
	}
	reAdopted, err := dbk8s.ApplyConnectionConfigMap(ctx, p.client, p.scheme, owner, dbk8s.DesiredConnectionConfigMap{
		Name:      publication.Name,
		Namespace: target.Namespace,
		Labels:    map[string]string{managedByLabel: "splunk-operator"},
		Data:      publication.Data,
	})
	if err != nil {
		wrapped := fmt.Errorf("reconciling ConfigMap %s: %w", publication.Name, err)
		if apierrors.IsConflict(err) {
			return fmt.Errorf("%w: %w", dbtypes.ErrConnectionMetadataConflict, wrapped)
		}
		return wrapped
	}
	if reAdopted {
		logging.FromContext(ctx).InfoContext(ctx, "ConfigMap re-adopted", "name", publication.Name)
	}
	return nil
}
