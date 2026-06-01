package connectioninfo

import (
	"fmt"
	"slices"
)

const (
	// DefaultPort is the shared PostgreSQL client port published in access ConfigMaps,
	// including when clients connect through the published pooler endpoints.
	DefaultPort = "5432"

	serviceDomainSuffix = "svc.cluster.local"

	// Shared connection keys published by both cluster- and database-level ConfigMaps.
	KeyClusterRWEndpoint  = "CLUSTER_RW_ENDPOINT"
	KeyClusterROEndpoint  = "CLUSTER_RO_ENDPOINT"
	KeyClusterREndpoint   = "CLUSTER_R_ENDPOINT"
	KeyDefaultClusterPort = "DEFAULT_CLUSTER_PORT"
	KeyPoolerRWEndpoint   = "CLUSTER_POOLER_RW_ENDPOINT"
	KeyPoolerROEndpoint   = "CLUSTER_POOLER_RO_ENDPOINT"
)

var requiredKeys = []string{
	KeyClusterRWEndpoint,
	KeyClusterROEndpoint,
	KeyClusterREndpoint,
	KeyDefaultClusterPort,
}

// Endpoints contains the fully qualified service hostnames published in access
// ConfigMaps. RWHost, ROHost, and RHost are always required. PoolerRWHost and
// PoolerROHost are optional, but must either both be set or both be empty.
type Endpoints struct {
	RWHost       string
	ROHost       string
	RHost        string
	PoolerRWHost string
	PoolerROHost string
}

// Builder accumulates ConfigMap data and tracks which keys must be populated.
type Builder struct {
	data     map[string]string
	required []string
}

// Option mutates a ConfigMap Builder.
type Option func(*Builder)

// SetRequired records a key/value pair and marks the key as required.
func (b *Builder) SetRequired(key, value string) {
	b.data[key] = value
	b.required = appendUnique(b.required, key)
}

// SetOptional records a key/value pair only when the value is non-empty.
func (b *Builder) SetOptional(key, value string) {
	if value == "" {
		return
	}
	b.data[key] = value
}

// RequiredKeys returns the canonical shared keys that every access ConfigMap must publish.
func RequiredKeys() []string {
	return slices.Clone(requiredKeys)
}

// ServiceFQDN returns the cluster-local FQDN for a Kubernetes Service.
func ServiceFQDN(serviceName, namespace string) (string, error) {
	if serviceName == "" {
		return "", fmt.Errorf("connectioninfo: service name is required")
	}
	if namespace == "" {
		return "", fmt.Errorf("connectioninfo: namespace is required")
	}
	return fmt.Sprintf("%s.%s.%s", serviceName, namespace, serviceDomainSuffix), nil
}

// Validate ensures the required endpoint set is complete.
func (e Endpoints) Validate() error {
	if e.RWHost == "" {
		return fmt.Errorf("connectioninfo: RWHost is required")
	}
	if e.ROHost == "" {
		return fmt.Errorf("connectioninfo: ROHost is required")
	}
	if e.RHost == "" {
		return fmt.Errorf("connectioninfo: RHost is required")
	}
	if (e.PoolerRWHost == "") != (e.PoolerROHost == "") {
		return fmt.Errorf("connectioninfo: pooler endpoints must both be set or both be empty")
	}
	return nil
}

// BuildConfigMapData builds the shared endpoint/port ConfigMap payload.
func BuildConfigMapData(endpoints Endpoints, opts ...Option) (map[string]string, []string, error) {
	if err := endpoints.Validate(); err != nil {
		return nil, nil, err
	}

	builder := &Builder{
		data: map[string]string{
			KeyClusterRWEndpoint:  endpoints.RWHost,
			KeyClusterROEndpoint:  endpoints.ROHost,
			KeyClusterREndpoint:   endpoints.RHost,
			KeyDefaultClusterPort: DefaultPort,
		},
		required: RequiredKeys(),
	}
	builder.SetOptional(KeyPoolerRWEndpoint, endpoints.PoolerRWHost)
	builder.SetOptional(KeyPoolerROEndpoint, endpoints.PoolerROHost)

	for _, opt := range opts {
		opt(builder)
	}

	for _, key := range builder.required {
		if builder.data[key] == "" {
			return nil, nil, fmt.Errorf("connectioninfo: required key %s is empty", key)
		}
	}

	return builder.data, slices.Clone(builder.required), nil
}

func appendUnique(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}
