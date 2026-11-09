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
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CandidatePoolerExpectation is the frozen Pooler topology for the attempt.
// A disabled side must not silently become a published endpoint.
type CandidatePoolerExpectation struct {
	// Enabled preserves the existing two-key schema even if no Pooler side is
	// currently routable (for example an RO-only Pooler at one instance).
	Enabled   bool
	RWEnabled bool
	ROEnabled bool
}

// ResolveCandidateConnectionData validates candidate Services, CNPG health and
// Pooler Deployment readiness before deriving the routing data to be published.
// It proves endpoint preparation, not that green's write fence is open.
func ResolveCandidateConnectionData(
	ctx context.Context,
	reader client.Reader,
	green *cnpgv1.Cluster,
	candidate *platformv1alpha1.BlueGreenCandidateEndpointsStatus,
	expectedPoolers CandidatePoolerExpectation,
) (map[string]string, error) {
	if reader == nil || green == nil || green.UID == "" || candidate == nil || candidate.DirectService == nil {
		return nil, fmt.Errorf("candidate endpoints: reader, green identity and direct Service reference are required")
	}
	if err := validateCandidateDirectServices(ctx, reader, green, candidate); err != nil {
		return nil, err
	}

	pooler, err := resolveCandidatePoolers(ctx, reader, green, candidate, expectedPoolers)
	if err != nil {
		return nil, err
	}

	endpoints, err := ResolveConnectionEndpoints(
		green.Name, green.Namespace, green.Status.WriteService,
		green.Status.ReadService, green.Status.ReadyInstances, pooler,
	)
	if err != nil {
		return nil, err
	}
	return pgconninfo.RoutingData(endpoints)
}

func readCandidatePooler(ctx context.Context, reader client.Reader, namespace, name, clusterName string, expectedType cnpgv1.PoolerType) (*cnpgv1.Pooler, error) {
	pooler := &cnpgv1.Pooler{}
	if err := reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pooler); err != nil {
		return nil, fmt.Errorf("reading candidate Pooler %s: %w", name, err)
	}
	if pooler.Spec.Cluster.Name != clusterName || pooler.Spec.Type != expectedType {
		return nil, fmt.Errorf("candidate Pooler %s targets a different Cluster or service side", name)
	}
	return pooler, nil
}

type candidateServiceExpectation struct {
	selector   map[string]string
	portName   string
	port       int32
	targetPort intstr.IntOrString
}

func directServiceExpectation(clusterName, side string) candidateServiceExpectation {
	selector := map[string]string{"cnpg.io/cluster": clusterName}
	switch side {
	case "rw":
		selector["cnpg.io/instanceRole"] = "primary"
	case "ro":
		selector["cnpg.io/instanceRole"] = "replica"
	default:
		selector["cnpg.io/podRole"] = "instance"
	}
	return candidateServiceExpectation{selector: selector, portName: "postgres", port: 5432, targetPort: intstr.FromInt32(5432)}
}

func poolerServiceExpectation(name string) candidateServiceExpectation {
	return candidateServiceExpectation{selector: map[string]string{"cnpg.io/poolerName": name}, portName: "pgbouncer", port: 5432, targetPort: intstr.FromString("pgbouncer")}
}

func readCandidateService(ctx context.Context, reader client.Reader, namespace, name string, ref *corev1.ObjectReference, expectation candidateServiceExpectation) (*corev1.Service, error) {
	if name == "" {
		return nil, fmt.Errorf("Service name is required")
	}
	service := &corev1.Service{}
	if err := reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, service); err != nil {
		return nil, fmt.Errorf("reading Service %s: %w", name, err)
	}
	if err := validateCandidateServiceSpec(service, namespace, name, ref, expectation); err != nil {
		return nil, err
	}
	return service, nil
}

func validateCandidateDirectServices(ctx context.Context, reader client.Reader, green *cnpgv1.Cluster, candidate *platformv1alpha1.BlueGreenCandidateEndpointsStatus) error {
	if green.DeletionTimestamp != nil || green.Status.Phase != cnpgv1.PhaseHealthy || green.Status.ReadyInstances < 1 {
		return fmt.Errorf("candidate endpoints: green Cluster %s is not healthy", green.Name)
	}
	if candidate.DirectService.Name != green.Status.WriteService {
		return fmt.Errorf("candidate endpoints: direct Service does not match green write Service")
	}
	if green.Status.WriteService != green.GetServiceReadWriteName() {
		return fmt.Errorf("candidate endpoints: write Service does not match the CNPG primary Service")
	}
	if _, err := readCandidateService(ctx, reader, green.Namespace, green.Status.WriteService, candidate.DirectService, directServiceExpectation(green.Name, "rw")); err != nil {
		return fmt.Errorf("candidate direct Service: %w", err)
	}
	if _, err := readCandidateService(ctx, reader, green.Namespace, green.GetServiceReadName(), nil, directServiceExpectation(green.Name, "r")); err != nil {
		return fmt.Errorf("candidate read Service: %w", err)
	}
	if green.Status.ReadyInstances >= MinInstancesForReadOnly {
		if green.Status.ReadService != green.GetServiceReadName() {
			return fmt.Errorf("candidate endpoints: read Service does not match the CNPG read Service")
		}
		if _, err := readCandidateService(ctx, reader, green.Namespace, green.GetServiceReadOnlyName(), nil, directServiceExpectation(green.Name, "ro")); err != nil {
			return fmt.Errorf("candidate read-only Service: %w", err)
		}
	}

	return nil
}

func resolveCandidatePoolers(ctx context.Context, reader client.Reader, green *cnpgv1.Cluster, candidate *platformv1alpha1.BlueGreenCandidateEndpointsStatus, expectedPoolers CandidatePoolerExpectation) (PoolerAvailability, error) {
	pooler := PoolerAvailability{
		Enabled: expectedPoolers.Enabled || expectedPoolers.RWEnabled || expectedPoolers.ROEnabled,
	}
	if err := validateCandidatePoolerReference(green.Name, candidate.PoolerService, expectedPoolers); err != nil {
		return PoolerAvailability{}, err
	}
	if expectedPoolers.RWEnabled {
		rwName := fmt.Sprintf(poolerServiceNameTemplate, green.Name, "rw")

		if err := validateCandidatePoolerService(ctx, reader, green, rwName, candidate.PoolerService, cnpgv1.PoolerTypeRW); err != nil {
			return PoolerAvailability{}, err
		}
		pooler.RWReady = true
	}
	if expectedPoolers.ROEnabled && green.Status.ReadyInstances >= MinInstancesForReadOnly {
		roName := fmt.Sprintf(poolerServiceNameTemplate, green.Name, "ro")

		var ref *corev1.ObjectReference
		if !expectedPoolers.RWEnabled {
			ref = candidate.PoolerService
		}
		if err := validateCandidatePoolerService(ctx, reader, green, roName, ref, cnpgv1.PoolerTypeRO); err != nil {
			return PoolerAvailability{}, err
		}
		pooler.ROReady = true
	}

	return pooler, nil
}

func validateCandidatePoolerReference(clusterName string, ref *corev1.ObjectReference, expectedPoolers CandidatePoolerExpectation) error {
	candidatePoolerName := ""
	if expectedPoolers.RWEnabled {
		candidatePoolerName = fmt.Sprintf(poolerServiceNameTemplate, clusterName, "rw")
	}
	if expectedPoolers.ROEnabled && candidatePoolerName == "" {
		candidatePoolerName = fmt.Sprintf(poolerServiceNameTemplate, clusterName, "ro")
	}
	if candidatePoolerName == "" && ref != nil {
		return fmt.Errorf("candidate endpoints: unexpected Pooler Service reference")
	}
	if candidatePoolerName != "" && (ref == nil || ref.Name != candidatePoolerName) {
		return fmt.Errorf("candidate endpoints: Pooler Service reference does not match expected topology")
	}
	return nil
}

func validateCandidatePoolerService(ctx context.Context, reader client.Reader, green *cnpgv1.Cluster, name string, ref *corev1.ObjectReference, side cnpgv1.PoolerType) error {
	pooler, err := readCandidatePooler(ctx, reader, green.Namespace, name, green.Name, side)
	if err != nil {
		return err
	}
	service, err := readCandidateService(ctx, reader, green.Namespace, name, ref, poolerServiceExpectation(name))
	if err != nil {
		return fmt.Errorf("candidate %s Pooler Service: %w", strings.ToUpper(string(side)), err)
	}
	ready, err := PoolerDeploymentReady(ctx, reader, pooler, service)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("candidate Pooler %s Deployment is not ready", name)
	}
	return nil
}

func validateCandidateServiceSpec(service *corev1.Service, namespace, name string, ref *corev1.ObjectReference, expectation candidateServiceExpectation) error {
	if service.DeletionTimestamp != nil {
		return fmt.Errorf("Service %s is terminating", name)
	}
	if service.UID == "" {
		return fmt.Errorf("Service %s has no UID", name)
	}
	if ref != nil && (ref.Name != name || (ref.Namespace != "" && ref.Namespace != namespace) || ref.UID == "" || ref.UID != service.UID) {
		return fmt.Errorf("Service %s does not match candidate identity", name)
	}
	for key, value := range expectation.selector {
		if service.Spec.Selector[key] != value {
			return fmt.Errorf("Service %s selector %s does not target expected backend", name, key)
		}
	}
	portValid := false
	for _, port := range service.Spec.Ports {
		if port.Name == expectation.portName && port.Port == expectation.port && port.Protocol == corev1.ProtocolTCP && port.TargetPort == expectation.targetPort {
			portValid = true
			break
		}
	}
	if !portValid {
		return fmt.Errorf("Service %s does not expose expected PostgreSQL port", name)
	}
	return nil
}
