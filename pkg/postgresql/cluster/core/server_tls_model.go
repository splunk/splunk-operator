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

package core

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"slices"
	"sort"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	tlsport "github.com/splunk/splunk-operator/pkg/postgresql/cluster/ports/tls"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type serverTLSModel struct {
	manager      tlsport.Manager
	events       eventEmitter
	updateStatus healthStatusUpdater
	cluster      *platformv1alpha1.PostgresCluster
	class        *platformv1alpha1.PostgresClusterClass
	mergedConfig *MergedConfig
	contracts    *reconcileContracts
}

// serverTLSPlan is immutable desired TLS intent. Its mode stays private: core
// consumers use only pure apply, observe, and certificate-validation behavior.
type serverTLSPlan struct {
	initialized        bool
	certManager        bool
	serverTLSSecret    string
	serverCASecret     string
	poolerEnabled      bool
	requiredPoolerSANs []string
}

// tlsBackendState is the observed CNPG result derived from a plan. It carries
// no Kubernetes client and performs no I/O.
type tlsBackendState struct {
	observed        bool
	Converged       bool
	ServerTLSSecret string
	ConnectionCARef *corev1.SecretKeySelector
}

func (p serverTLSPlan) ApplyToCNPG(spec *cnpgv1.ClusterSpec, clusterName, namespace string) {
	if p.certManager {
		if spec.Certificates == nil {
			spec.Certificates = &cnpgv1.CertificatesConfiguration{}
		}
		spec.Certificates.ServerTLSSecret = p.serverTLSSecret
		spec.Certificates.ServerCASecret = p.serverCASecret
		// cert-manager owns the complete SAN set; CNPG must not generate or merge it.
		spec.Certificates.ServerAltDNSNames = nil
		return
	}
	applyPoolerSANs(spec, p.poolerEnabled, clusterName, namespace)
}

func (p serverTLSPlan) ObserveCNPG(status cnpgv1.ClusterStatus) tlsBackendState {
	serverTLSSecret := status.Certificates.ServerTLSSecret
	serverCASecret := status.Certificates.ServerCASecret
	state := tlsBackendState{observed: true, ServerTLSSecret: serverTLSSecret}
	if serverCASecret != "" {
		state.ConnectionCARef = &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: serverCASecret},
			Key:                  defaultServerCACertKey,
		}
	}
	if p.certManager {
		state.Converged = serverTLSSecret == p.serverTLSSecret && serverCASecret == p.serverCASecret
		return state
	}
	// CNPG default TLS chooses both Secret names itself. ClusterReady owns the
	// adoption of its leaf Secret; ConfigMapsReady separately waits until the CA
	// selector is materialized and published.
	state.Converged = serverTLSSecret != ""
	return state
}

func (p serverTLSPlan) ValidatesPoolerLeaf(cert *x509.Certificate) bool {
	if cert == nil {
		return false
	}
	for _, dnsName := range p.requiredPoolerSANs {
		if !slices.Contains(cert.DNSNames, dnsName) {
			return false
		}
	}
	return true
}

func newServerTLSModel(manager tlsport.Manager, events eventEmitter, updateStatus healthStatusUpdater, cluster *platformv1alpha1.PostgresCluster, class *platformv1alpha1.PostgresClusterClass, mergedConfig *MergedConfig, contracts *reconcileContracts) *serverTLSModel {
	return &serverTLSModel{manager: manager, events: events, updateStatus: updateStatus, cluster: cluster, class: class, mergedConfig: mergedConfig, contracts: contracts}
}

func (p *serverTLSModel) Name() string { return "ServerTLS" }
func (p *serverTLSModel) Requires() []contractKey {
	return []contractKey{contractAuthority, contractEnvironmentNamer}
}
func (p *serverTLSModel) Provides() []contractKey { return []contractKey{contractServerTLS} }
func (p *serverTLSModel) CheckContracts() error {
	if !checkContractsFromRequirements(p.Requires(), p.contracts) {
		return errContractsNotReady
	}
	return nil
}

func (p *serverTLSModel) Reconcile(ctx context.Context) error {
	plan, err := p.reconcilePlan(ctx)
	if err != nil {
		return err
	}
	if !plan.initialized {
		return fmt.Errorf("%w: server TLS workflow did not return a ready plan", tlsport.ErrConfiguration)
	}
	p.contracts.ServerTLS = plan
	return nil
}

func (p *serverTLSModel) reconcilePlan(ctx context.Context) (serverTLSPlan, error) {
	certificates := certificateConfigForClass(p.class)
	environmentName := p.authoritativeEnvironmentName()
	if certificates == nil || certificates.Mode == nil || *certificates.Mode == platformv1alpha1.PostgresCertificateModeCNPGDefault {
		return newCNPGDefaultServerTLSPlan(p.cluster, p.mergedConfig, environmentName), nil
	}
	if p.manager == nil {
		return serverTLSPlan{}, fmt.Errorf("%w: server TLS manager is unavailable", tlsport.ErrConfiguration)
	}
	if certificates.IssuerRef == nil || certificates.IssuerRef.Name == "" {
		return serverTLSPlan{}, fmt.Errorf("%w: issuerRef is required", tlsport.ErrConfiguration)
	}
	identity := serverTLSIdentity(p.cluster)
	observedDNSNames, err := p.manager.ExistingCertificateDNSNames(ctx, identity)
	if err != nil {
		return serverTLSPlan{}, err
	}
	dnsNames, retainedPoolerDNSNames := completeServerTLSNames(serverTLSNames(p.cluster, p.mergedConfig, environmentName), observedDNSNames, environmentName, p.cluster.Namespace)
	request := tlsport.Request{
		Identity:               identity,
		IssuerRef:              tlsport.IssuerReference{Name: certificates.IssuerRef.Name, Kind: certificates.IssuerRef.Kind, Group: certificates.IssuerRef.Group},
		DNSNames:               dnsNames,
		RetainedPoolerDNSNames: retainedPoolerDNSNames,
		Usages:                 resolvedServerTLSUsages(certificates.ServerUsages),
		Duration:               certificates.Duration,
		RenewBefore:            certificates.RenewBefore,
	}
	if err := p.manager.Reconcile(ctx, request); err != nil {
		return serverTLSPlan{}, err
	}
	return newCertManagerServerTLSPlan(p.cluster, p.mergedConfig, identity, environmentName), nil
}

func (p *serverTLSModel) authoritativeEnvironmentName() string {
	if p.cluster == nil {
		return ""
	}
	if p.contracts == nil || p.contracts.EnvironmentNamer == nil {
		return p.cluster.Name
	}
	return p.contracts.EnvironmentNamer.AuthoritativeEnvironmentName(p.cluster.Name, p.contracts.Authority)
}

func (p *serverTLSModel) Observe(_ context.Context, reconcileErr error) (componentHealth, error) {
	before := p.cluster.Status.DeepCopy()
	var health componentHealth
	switch {
	case reconcileErr == nil:
		health = newReadyHealth(certificatesReady, "CertificateReady", "Server TLS certificate is ready")
	case errors.Is(reconcileErr, tlsport.ErrCertManagerNotInstalled):
		health = newFailedHealth(certificatesReady, reasonCertManagerNotInstalled, string(msgCertManagerNotInstalled))
	case errors.Is(reconcileErr, tlsport.ErrCertificatePending):
		health = newProvisioningHealth(certificatesReady, reasonCertificatePending, string(msgCertificatePending))
	case errors.Is(reconcileErr, tlsport.ErrInvalidMaterial):
		health = newFailedHealth(certificatesReady, reasonCertificateInvalid, string(msgCertificateInvalid))
	default:
		health = newFailedHealth(certificatesReady, reasonCertificateConfigError, "Server TLS certificate configuration is invalid")
	}
	statusErr := writeComponentStatus(p.updateStatus, before, health)
	if statusErr == nil && p.events != nil && p.certManagerMode() && certificateConditionChanged(before.Conditions, p.cluster.Status.Conditions) {
		if health.State == pgcConstants.Ready {
			p.events.emitNormal(p.cluster, EventCertificateReady, health.Message)
		} else {
			p.events.emitWarning(p.cluster, EventCertificateReconcileFailed, health.Message)
		}
	}
	if reconcileErr != nil && !isIntermediateState(health.State) {
		return health, errors.Join(reconcileErr, statusErr)
	}
	return health, statusErr
}

func (p *serverTLSModel) certManagerMode() bool {
	certificates := certificateConfigForClass(p.class)
	return certificates != nil && certificates.Mode != nil && *certificates.Mode == platformv1alpha1.PostgresCertificateModeCertManager
}

func certificateConditionChanged(before, after []metav1.Condition) bool {
	previous := meta.FindStatusCondition(before, string(certificatesReady))
	next := meta.FindStatusCondition(after, string(certificatesReady))
	if previous == nil || next == nil {
		return previous != next
	}
	return previous.Status != next.Status || previous.Reason != next.Reason || previous.Message != next.Message
}

func certificateConfigForClass(class *platformv1alpha1.PostgresClusterClass) *platformv1alpha1.PostgresCertificateConfig {
	if class == nil || class.Spec.TLS == nil {
		return nil
	}
	return class.Spec.TLS.Certificates
}

func serverTLSNames(cluster *platformv1alpha1.PostgresCluster, cfg *MergedConfig, environmentName string) []string {
	if cluster == nil {
		return nil
	}
	if environmentName == "" {
		environmentName = cluster.Name
	}
	names := []string{}
	for _, endpoint := range []string{"rw", "ro", "r"} {
		names = append(names, serviceDNSNames(environmentName+"-"+endpoint, cluster.Namespace)...)
	}
	if cfg != nil && cfg.Spec != nil && isPoolerEnabled(cfg.Spec.ConnectionPooler) {
		if poolerReadWriteWanted(cfg.Spec.ConnectionPooler) {
			names = append(names, serviceDNSNames(environmentName+defaultPoolerSuffix+readWriteEndpoint, cluster.Namespace)...)
		}
		if poolerReadOnlyWanted(cfg.Spec.ConnectionPooler) {
			names = append(names, serviceDNSNames(environmentName+defaultPoolerSuffix+readOnlyEndpoint, cluster.Namespace)...)
		}
	}
	if cluster.Spec.TLS != nil {
		names = append(names, cluster.Spec.TLS.ServerAltDNSNames...)
	}
	sort.Strings(names)
	return slices.Compact(names)
}

func serverTLSIdentity(cluster *platformv1alpha1.PostgresCluster) tlsport.Identity {
	if cluster == nil {
		return tlsport.Identity{}
	}
	controller := true
	blockOwnerDeletion := true
	return tlsport.Identity{
		ClusterName: cluster.Name,
		Namespace:   cluster.Namespace,
		ClusterUID:  cluster.UID,
		OwnerReference: metav1.OwnerReference{
			APIVersion:         platformv1alpha1.GroupVersion.String(),
			Kind:               "PostgresCluster",
			Name:               cluster.Name,
			UID:                cluster.UID,
			Controller:         &controller,
			BlockOwnerDeletion: &blockOwnerDeletion,
		},
		CertificateName: cluster.Name + "-server-tls",
		ServerTLSSecret: cluster.Name + "-server-tls",
		ServerCASecret:  cluster.Name + "-server-ca",
	}
}

func completeServerTLSNames(desired, observed []string, environmentName, namespace string) ([]string, []string) {
	all := append([]string(nil), desired...)
	retained := make([]string, 0)
	generatedPoolerNames := generatedPoolerDNSNames(environmentName, namespace)
	for _, dnsName := range observed {
		if slices.Contains(generatedPoolerNames, dnsName) && !slices.Contains(all, dnsName) {
			all = append(all, dnsName)
			retained = append(retained, dnsName)
		}
	}
	sort.Strings(all)
	sort.Strings(retained)
	return slices.Compact(all), slices.Compact(retained)
}

func generatedPoolerDNSNames(environmentName, namespace string) []string {
	return append(
		serviceDNSNames(environmentName+defaultPoolerSuffix+readWriteEndpoint, namespace),
		serviceDNSNames(environmentName+defaultPoolerSuffix+readOnlyEndpoint, namespace)...,
	)
}

func resolvedServerTLSUsages(usages []string) []string {
	if len(usages) == 0 {
		return []string{"digital signature", "key encipherment", "server auth"}
	}
	return append([]string(nil), usages...)
}

func resolvedServerTLSRetentionPolicy(certificates *platformv1alpha1.PostgresCertificateConfig) platformv1alpha1.PostgresCertificateRetentionPolicy {
	if certificates != nil && certificates.SecretRetentionPolicy != nil {
		return *certificates.SecretRetentionPolicy
	}
	return platformv1alpha1.PostgresCertificateRetentionPolicyRetain
}

func serviceDNSNames(service, namespace string) []string {
	return []string{service, service + "." + namespace, service + "." + namespace + ".svc", service + "." + namespace + ".svc.cluster.local"}
}

func newCNPGDefaultServerTLSPlan(cluster *platformv1alpha1.PostgresCluster, cfg *MergedConfig, environmentName string) serverTLSPlan {
	plan := serverTLSPlan{initialized: true}
	if cluster == nil || cfg == nil || cfg.Spec == nil || !isPoolerEnabled(cfg.Spec.ConnectionPooler) {
		return plan
	}
	if environmentName == "" {
		environmentName = cluster.Name
	}
	plan.poolerEnabled = true
	plan.requiredPoolerSANs = computeDesiredPoolerSANSet(true, nil, environmentName, cluster.Namespace)
	return plan
}

func newCertManagerServerTLSPlan(cluster *platformv1alpha1.PostgresCluster, cfg *MergedConfig, identity tlsport.Identity, environmentName string) serverTLSPlan {
	plan := serverTLSPlan{
		initialized:     true,
		certManager:     true,
		serverTLSSecret: identity.ServerTLSSecret,
		serverCASecret:  identity.ServerCASecret,
	}
	if cluster != nil && cfg != nil && cfg.Spec != nil && isPoolerEnabled(cfg.Spec.ConnectionPooler) {
		plan.poolerEnabled = true
		plan.requiredPoolerSANs = certManagerPoolerSANs(cluster, cfg, environmentName)
	}
	return plan
}

// certManagerPoolerSANs selects the pooler DNS names actually requested from
// cert-manager. Unlike CNPG default TLS, cert-manager can independently omit
// RW or RO pooler names, so the post-adoption leaf check must use the issuance
// intent rather than reconstructing a broader CNPG-default set.
func certManagerPoolerSANs(cluster *platformv1alpha1.PostgresCluster, cfg *MergedConfig, environmentName string) []string {
	if cluster == nil || cfg == nil || cfg.Spec == nil || !isPoolerEnabled(cfg.Spec.ConnectionPooler) {
		return nil
	}
	if environmentName == "" {
		environmentName = cluster.Name
	}
	poolerNames := make([]string, 0, 8)
	if poolerReadWriteWanted(cfg.Spec.ConnectionPooler) {
		poolerNames = append(poolerNames, serviceDNSNames(environmentName+defaultPoolerSuffix+readWriteEndpoint, cluster.Namespace)...)
	}
	if poolerReadOnlyWanted(cfg.Spec.ConnectionPooler) {
		poolerNames = append(poolerNames, serviceDNSNames(environmentName+defaultPoolerSuffix+readOnlyEndpoint, cluster.Namespace)...)
	}
	sort.Strings(poolerNames)
	return poolerNames
}

var _ component = (*serverTLSModel)(nil)
