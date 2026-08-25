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

package cnpg

import (
	"context"
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type MonitoringFeatureOwner struct {
	Object     client.Object
	APIVersion string
	Kind       string
}

func GetMonitoringProviderOwner(
	ctx context.Context,
	c client.Client,
	namespace, name string,
) (*cnpgv1.Cluster, error) {
	cluster := &cnpgv1.Cluster{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, cluster); err != nil {
		return nil, fmt.Errorf("getting CNPG Cluster %s/%s: %w", namespace, name, err)
	}
	return cluster, nil
}

func GetMonitoringFeatureOwner(
	ctx context.Context,
	c client.Client,
	namespace, name, uid string,
) (MonitoringFeatureOwner, error) {
	cluster := &platformv1alpha1.PostgresCluster{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, cluster); err != nil {
		return MonitoringFeatureOwner{}, fmt.Errorf("getting PostgresCluster %s/%s: %w", namespace, name, err)
	}
	if uid != "" && string(cluster.UID) != uid {
		return MonitoringFeatureOwner{}, fmt.Errorf(
			"PostgresCluster %s/%s uid %s does not match custom-metrics target uid %s",
			cluster.Namespace,
			cluster.Name,
			cluster.UID,
			uid,
		)
	}
	return MonitoringFeatureOwner{
		Object:     cluster,
		APIVersion: platformv1alpha1.GroupVersion.String(),
		Kind:       "PostgresCluster",
	}, nil
}
