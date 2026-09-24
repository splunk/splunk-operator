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

// MinInstancesForReadOnly is the instance count below which CNPG has no usable
// read-only Service, so the RO endpoint (and RO pooler) is suppressed. Applied
// against ready instances when resolving endpoints and against the declared
// count when deciding whether to reconcile the RO pooler resource.
const MinInstancesForReadOnly = 2

// PoolerAvailability gates pooler endpoint publishing. Enabled turns on the
// both-keys-present contract; RWReady/ROReady select which side advertises a host.
type PoolerAvailability struct {
	Enabled bool
	RWReady bool
	ROReady bool
}

// ResolveConnectionEndpoints returns the CNPG service FQDNs used by PostgreSQL
// access ConfigMaps. Below MinInstancesForReadOnly the RO endpoint (and RO pooler)
// are suppressed and the read service name is not required.
func ResolveConnectionEndpoints(clusterName, namespace, writeServiceName, readServiceName string, readyInstances int, pooler PoolerAvailability) (pgconninfo.Endpoints, error) {
	if clusterName == "" {
		return pgconninfo.Endpoints{}, fmt.Errorf("cnpg: cluster name is required")
	}
	if writeServiceName == "" {
		return pgconninfo.Endpoints{}, fmt.Errorf("cnpg: write service name is required")
	}

	roUnavailable := readyInstances < MinInstancesForReadOnly

	rwHost, err := pgconninfo.ServiceFQDN(writeServiceName, namespace)
	if err != nil {
		return pgconninfo.Endpoints{}, err
	}
	rHost, err := pgconninfo.ServiceFQDN(fmt.Sprintf("%s-r", clusterName), namespace)
	if err != nil {
		return pgconninfo.Endpoints{}, err
	}

	endpoints := pgconninfo.Endpoints{
		RWHost:        rwHost,
		RHost:         rHost,
		ROUnavailable: roUnavailable,
	}

	if !roUnavailable {
		if readServiceName == "" {
			return pgconninfo.Endpoints{}, fmt.Errorf("cnpg: read service name is required")
		}
		endpoints.ROHost, err = pgconninfo.ServiceFQDN(readServiceName, namespace)
		if err != nil {
			return pgconninfo.Endpoints{}, err
		}
	}

	if pooler.Enabled {
		endpoints.PoolerEnabled = true
		if pooler.RWReady {
			endpoints.PoolerRWHost, err = pgconninfo.ServiceFQDN(fmt.Sprintf(poolerServiceNameTemplate, clusterName, "rw"), namespace)
			if err != nil {
				return pgconninfo.Endpoints{}, err
			}
		}
		if pooler.ROReady && !roUnavailable {
			endpoints.PoolerROHost, err = pgconninfo.ServiceFQDN(fmt.Sprintf(poolerServiceNameTemplate, clusterName, "ro"), namespace)
			if err != nil {
				return pgconninfo.Endpoints{}, err
			}
		}
	}

	return endpoints, nil
}
