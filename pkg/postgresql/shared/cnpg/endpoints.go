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
	"fmt"

	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
)

const poolerServiceNameTemplate = "%s-pooler-%s"

// ResolveConnectionEndpoints returns the CNPG service FQDNs used by PostgreSQL access ConfigMaps.
func ResolveConnectionEndpoints(clusterName, namespace, writeServiceName, readServiceName string, poolerEnabled bool) (pgconninfo.Endpoints, error) {
	if clusterName == "" {
		return pgconninfo.Endpoints{}, fmt.Errorf("cnpg: cluster name is required")
	}
	if writeServiceName == "" {
		return pgconninfo.Endpoints{}, fmt.Errorf("cnpg: write service name is required")
	}
	if readServiceName == "" {
		return pgconninfo.Endpoints{}, fmt.Errorf("cnpg: read service name is required")
	}

	rwHost, err := pgconninfo.ServiceFQDN(writeServiceName, namespace)
	if err != nil {
		return pgconninfo.Endpoints{}, err
	}
	roHost, err := pgconninfo.ServiceFQDN(readServiceName, namespace)
	if err != nil {
		return pgconninfo.Endpoints{}, err
	}
	rHost, err := pgconninfo.ServiceFQDN(fmt.Sprintf("%s-r", clusterName), namespace)
	if err != nil {
		return pgconninfo.Endpoints{}, err
	}

	endpoints := pgconninfo.Endpoints{
		RWHost: rwHost,
		ROHost: roHost,
		RHost:  rHost,
	}
	if poolerEnabled {
		endpoints.PoolerRWHost, err = pgconninfo.ServiceFQDN(fmt.Sprintf(poolerServiceNameTemplate, clusterName, "rw"), namespace)
		if err != nil {
			return pgconninfo.Endpoints{}, err
		}
		endpoints.PoolerROHost, err = pgconninfo.ServiceFQDN(fmt.Sprintf(poolerServiceNameTemplate, clusterName, "ro"), namespace)
		if err != nil {
			return pgconninfo.Endpoints{}, err
		}
	}

	return endpoints, nil
}
