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

package webhook

import (
	"context"
	"strings"
	"time"

	cmapi "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
	core "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
)

const (
	certManagerDefaultDuration    = 90 * 24 * time.Hour
	certManagerMinimumDuration    = time.Hour
	certManagerMinimumRenewBefore = 5 * time.Minute
)

var supportedCertificateUsages = map[cmapi.KeyUsage]struct{}{
	cmapi.UsageSigning: {}, cmapi.UsageDigitalSignature: {}, cmapi.UsageContentCommitment: {}, cmapi.UsageKeyEncipherment: {},
	cmapi.UsageKeyAgreement: {}, cmapi.UsageDataEncipherment: {}, cmapi.UsageCertSign: {}, cmapi.UsageCRLSign: {},
	cmapi.UsageEncipherOnly: {}, cmapi.UsageDecipherOnly: {}, cmapi.UsageAny: {}, cmapi.UsageServerAuth: {},
	cmapi.UsageClientAuth: {}, cmapi.UsageCodeSigning: {}, cmapi.UsageEmailProtection: {}, cmapi.UsageSMIME: {},
	cmapi.UsageIPsecEndSystem: {}, cmapi.UsageIPsecTunnel: {}, cmapi.UsageIPsecUser: {}, cmapi.UsageTimestamping: {},
	cmapi.UsageOCSPSigning: {}, cmapi.UsageMicrosoftSGC: {}, cmapi.UsageNetscapeSGC: {},
}

// ValidatePostgresClusterClassCreate validates a PostgresClusterClass on CREATE.
// It is retained for callers that do not have an admission reader.
func ValidatePostgresClusterClassCreate(obj *platformApi.PostgresClusterClass) field.ErrorList {
	return ValidatePostgresClusterClassCreateWithContext(context.Background(), obj, nil)
}

// ValidatePostgresClusterClassCreateWithContext validates a PostgresClusterClass
// on CREATE and verifies built-in issuer references when a reader is available.
func ValidatePostgresClusterClassCreateWithContext(ctx context.Context, obj *platformApi.PostgresClusterClass, reader client.Reader) field.ErrorList {
	var allErrs field.ErrorList

	if !config.DefaultMutableFeatureGate.Enabled(config.PostgresController) {
		allErrs = append(allErrs, field.Forbidden(
			field.NewPath("spec"),
			"the PostgresController feature is not enabled; set --feature-gates=PostgresController=true to activate"))

		return allErrs
	}

	if obj.Spec.Config != nil && len(obj.Spec.Config.PgHBA) > 0 {
		pgHBAPath := field.NewPath("spec").Child("config").Child("pgHBA")
		for _, re := range core.ValidateRules(obj.Spec.Config.PgHBA) {
			allErrs = append(allErrs, field.Invalid(
				pgHBAPath.Index(re.Index),
				obj.Spec.Config.PgHBA[re.Index],
				re.Message))
		}
	}
	if obj.Spec.Config != nil {
		allErrs = append(allErrs, toFieldErrors(core.ValidatePostgresImage(
			obj.Spec.Config.PostgresImage,
			obj.Spec.Config.PostgresVersion,
			"spec.config.postgresImage",
		))...)
	}
	allErrs = append(allErrs, validateCertificateConfig(ctx, obj, reader)...)

	return allErrs
}

// ValidatePostgresClusterClassUpdate validates a PostgresClusterClass on UPDATE.
// It is retained for callers that do not have an admission reader.
func ValidatePostgresClusterClassUpdate(obj, oldObj *platformApi.PostgresClusterClass) field.ErrorList {
	return ValidatePostgresClusterClassUpdateWithContext(context.Background(), obj, oldObj, nil)
}

// ValidatePostgresClusterClassUpdateWithContext validates a PostgresClusterClass
// on UPDATE and verifies built-in issuer references when a reader is available.
func ValidatePostgresClusterClassUpdateWithContext(ctx context.Context, obj, oldObj *platformApi.PostgresClusterClass, reader client.Reader) field.ErrorList {
	return ValidatePostgresClusterClassCreateWithContext(ctx, obj, reader)
}

func validateCertificateConfig(ctx context.Context, obj *platformApi.PostgresClusterClass, reader client.Reader) field.ErrorList {
	certificates := certificateConfig(obj)
	if certificates == nil || certificates.Mode == nil || *certificates.Mode == platformApi.PostgresCertificateModeCNPGDefault {
		return nil
	}

	path := field.NewPath("spec", "tls", "certificates")
	var errs field.ErrorList
	if *certificates.Mode != platformApi.PostgresCertificateModeCertManager {
		errs = append(errs, field.NotSupported(path.Child("mode"), *certificates.Mode, []string{string(platformApi.PostgresCertificateModeCNPGDefault), string(platformApi.PostgresCertificateModeCertManager)}))
		return errs
	}
	if certificates.IssuerRef == nil {
		return append(errs, field.Required(path.Child("issuerRef"), "issuerRef is required when mode is certManager"))
	}
	duration := certManagerDefaultDuration
	if certificates.Duration != nil {
		duration = certificates.Duration.Duration
		if duration < certManagerMinimumDuration {
			errs = append(errs, field.Invalid(path.Child("duration"), duration.String(), "must be at least 1h"))
		}
	}
	if certificates.RenewBefore != nil {
		renewBefore := certificates.RenewBefore.Duration
		if renewBefore < certManagerMinimumRenewBefore {
			errs = append(errs, field.Invalid(path.Child("renewBefore"), renewBefore.String(), "must be at least 5m"))
		}
		if renewBefore >= duration {
			message := "must be less than duration"
			if certificates.Duration == nil {
				message = "must be less than cert-manager's default duration of 90d"
			}
			errs = append(errs, field.Invalid(path.Child("renewBefore"), renewBefore.String(), message))
		}
	}
	for i, usage := range certificates.ServerUsages {
		usagePath := path.Child("serverUsages").Index(i)
		if strings.TrimSpace(usage) == "" {
			errs = append(errs, field.Invalid(usagePath, usage, "must not be empty"))
			continue
		}
		if _, supported := supportedCertificateUsages[cmapi.KeyUsage(usage)]; !supported {
			errs = append(errs, field.NotSupported(usagePath, usage, supportedCertificateUsageNames()))
		}
	}

	issuerRef := certificates.IssuerRef
	if issuerRef.Group != "" && issuerRef.Group != "cert-manager.io" {
		return append(errs, field.NotSupported(path.Child("issuerRef", "group"), issuerRef.Group, []string{"cert-manager.io"}))
	}
	// A PostgresClusterClass is cluster-scoped. Its admission request has no
	// namespace, so a namespaced Issuer cannot be resolved here. The per-cluster
	// cert-manager adapter resolves it in the consuming PostgresCluster namespace
	// and fails closed when it is absent or not Ready.
	if reader == nil || issuerRef.Kind != "ClusterIssuer" {
		return errs
	}
	issuer := &cmapi.ClusterIssuer{}
	if err := reader.Get(ctx, client.ObjectKey{Name: issuerRef.Name}, issuer); err != nil {
		return append(errs, field.Invalid(path.Child("issuerRef"), issuerRef.Name, "must reference a readable ClusterIssuer"))
	}
	if issuer.Spec.ACME != nil {
		return append(errs, field.Invalid(path.Child("issuerRef"), issuerRef.Name, "ACME issuers are not supported for PostgreSQL server certificates"))
	}
	return errs
}

func supportedCertificateUsageNames() []string {
	return []string{
		string(cmapi.UsageSigning), string(cmapi.UsageDigitalSignature), string(cmapi.UsageContentCommitment), string(cmapi.UsageKeyEncipherment),
		string(cmapi.UsageKeyAgreement), string(cmapi.UsageDataEncipherment), string(cmapi.UsageCertSign), string(cmapi.UsageCRLSign),
		string(cmapi.UsageEncipherOnly), string(cmapi.UsageDecipherOnly), string(cmapi.UsageAny), string(cmapi.UsageServerAuth),
		string(cmapi.UsageClientAuth), string(cmapi.UsageCodeSigning), string(cmapi.UsageEmailProtection), string(cmapi.UsageSMIME),
		string(cmapi.UsageIPsecEndSystem), string(cmapi.UsageIPsecTunnel), string(cmapi.UsageIPsecUser), string(cmapi.UsageTimestamping),
		string(cmapi.UsageOCSPSigning), string(cmapi.UsageMicrosoftSGC), string(cmapi.UsageNetscapeSGC),
	}
}

func certificateConfig(obj *platformApi.PostgresClusterClass) *platformApi.PostgresCertificateConfig {
	if obj.Spec.TLS == nil {
		return nil
	}
	return obj.Spec.TLS.Certificates
}

// GetPostgresClusterClassWarningsOnCreate returns warnings for PostgresClusterClass CREATE.
func GetPostgresClusterClassWarningsOnCreate(obj *platformApi.PostgresClusterClass) []string {
	return nil
}

// GetPostgresClusterClassWarningsOnUpdate returns warnings for PostgresClusterClass UPDATE.
func GetPostgresClusterClassWarningsOnUpdate(obj, oldObj *platformApi.PostgresClusterClass) []string {
	return nil
}
