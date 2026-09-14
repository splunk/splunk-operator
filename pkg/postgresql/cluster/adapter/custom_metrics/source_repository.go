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

package custom_metrics

import (
	"context"
	"fmt"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DataRepository struct {
	client client.Client
}

func NewDataRepository(c client.Client) DataRepository {
	return DataRepository{client: c}
}

// Database spec does not cross this boundary; only committed status is consumed.
func (r DataRepository) ListDatabaseContributions(ctx context.Context, namespace, clusterName string) (mtypes.DatabaseContributionSnapshot, error) {
	list := &platformv1alpha1.PostgresDatabaseList{}
	if err := r.client.List(ctx, list,
		client.InNamespace(namespace),
		client.MatchingFields{platformv1alpha1.PostgresDatabaseClusterRefNameField: clusterName},
	); err != nil {
		list = &platformv1alpha1.PostgresDatabaseList{}
		if fallbackErr := r.client.List(ctx, list, client.InNamespace(namespace)); fallbackErr != nil {
			return mtypes.DatabaseContributionSnapshot{}, fmt.Errorf("listing PostgresDatabases for cluster %s: indexed=%v fallback=%w", clusterName, err, fallbackErr)
		}
	}
	var result mtypes.DatabaseContributionSnapshot
	for i := range list.Items {
		db := &list.Items[i]
		if db.Spec.ClusterRef.Name != clusterName {
			continue
		}
		if db.GetDeletionTimestamp() != nil {
			continue
		}
		publication := db.Status.CustomMetricsPublication
		if publication == nil || publication.ObservedGeneration != db.Generation {
			result.Unpublished = append(result.Unpublished, mtypes.ContributorIdentity{
				PostgresDatabaseName: db.Name,
				PostgresDatabaseUID:  string(db.UID),
				Namespace:            db.Namespace,
			})
			continue
		}
		for _, database := range publication.Contributions {
			identity := mtypes.ContributorIdentity{
				PostgresDatabaseName: db.Name,
				PostgresDatabaseUID:  string(db.UID),
				DatabaseName:         database.DatabaseName,
				Namespace:            db.Namespace,
			}
			contribution := mtypes.DatabaseContribution{
				Identity:          identity,
				Revision:          database.Revision,
				Exists:            database.Exists,
				CreationTimestamp: db.CreationTimestamp.Time,
			}
			for _, selector := range database.CustomQueriesConfigMap {
				contribution.Selectors = append(contribution.Selectors, mtypes.QuerySelector{
					ConfigMapName: selector.Name,
					ConfigMapKey:  selector.Key,
				})
			}
			result.Contributions = append(result.Contributions, contribution)
		}
	}
	return result, nil
}

func (r DataRepository) FetchConfigMap(ctx context.Context, namespace, configMapName, dataKey string) ([]byte, error) {
	var cm corev1.ConfigMap
	if err := r.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: configMapName}, &cm); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, mtypes.ErrSourceNotFound
		}
		return nil, err
	}
	if v, ok := cm.Data[dataKey]; ok {
		return []byte(v), nil
	}
	if v, ok := cm.BinaryData[dataKey]; ok {
		return v, nil
	}
	return nil, mtypes.ErrSourceNotFound
}
