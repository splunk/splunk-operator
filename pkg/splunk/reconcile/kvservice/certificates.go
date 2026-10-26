// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kvservice

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strconv"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	reconcileutil "github.com/splunk/splunk-operator/pkg/splunk/reconcile"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func reconcileKVServiceCertificates(ctx context.Context, c client.Client, cr *enterpriseApi.KVService) (*certs.CertMountConfig, []corev1.EnvVar, error) {
	if len(cr.Spec.Certs) == 0 {
		return nil, nil, nil
	}
	entries := reconcileutil.ToCertEntries(
		cr.Spec.Certs,
		certs.AutoDNSNames(splcommon.SplunkKVService, cr.Name, cr.Namespace, cr.Spec.Replicas),
	)
	mounts, err := certs.ReconcileCerts(ctx, c, cr, entries)
	if err != nil {
		return nil, nil, fmt.Errorf("reconciling KVService certificates: %w", err)
	}
	servingCert := getKVServiceServingCertSpec(cr.Spec.Certs)
	secret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: servingCert.SecretRef.Name}, secret); err != nil {
		return nil, nil, fmt.Errorf("loading KVService serving certificate: %w", err)
	}
	for _, key := range []string{certs.CertTLSCRTKey, certs.CertTLSKeyKey} {
		if len(secret.Data[key]) == 0 {
			return nil, nil, fmt.Errorf("KVService serving certificate Secret %q must contain non-empty key %q", secret.Name, key)
		}
	}
	mountPath, ok := mounts.MountPathForSecret(servingCert.SecretRef.Name)
	if !ok {
		return nil, nil, fmt.Errorf("KVService serving certificate Secret %q has no resolved mount path", servingCert.SecretRef.Name)
	}
	env := []corev1.EnvVar{
		{Name: "TLS_CERTIFICATE", Value: path.Join(mountPath, certs.CertTLSCRTKey)},
		{Name: "TLS_PRIVATE_KEY", Value: path.Join(mountPath, certs.CertTLSKeyKey)},
		{Name: "TLS_HOST", Value: "0.0.0.0"},
		{Name: "TLS_PORT", Value: strconv.Itoa(splcommon.KVServicePort)},
	}
	return mounts, env, nil
}

func getKVServiceServingCertSpec(specs []enterpriseApi.CertSpec) enterpriseApi.CertSpec {
	if i := slices.IndexFunc(specs, func(spec enterpriseApi.CertSpec) bool {
		return spec.Role == enterpriseApi.CertRoleServer
	}); i >= 0 {
		return specs[i]
	}
	return specs[0]
}
