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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetCluster reads a CNPG Cluster by identity.
func GetCluster(ctx context.Context, reader client.Reader, key client.ObjectKey) (cnpgv1.Cluster, error) {
	cluster := cnpgv1.Cluster{}
	err := reader.Get(ctx, key, &cluster)
	return cluster, err
}
