// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
// Use of this source code is governed by an Apache 2 style
// license that can be found in the LICENSE file.

package resources

import (
	"fmt"
	"os"
	"time"
	"math/rand"

	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	SECRET_BYTES = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ01234567890%()*+,-./<=>?@[]^_{|}~"
)

func init() {
	// seed random number generator for splunk secret generation
	rand.Seed(time.Now().UnixNano())
}


// AsOwner returns an object to use for Kubernetes resource ownership references.
func AsOwner(cr *v1alpha1.SplunkEnterprise) metav1.OwnerReference {
	trueVar := true

	return metav1.OwnerReference{
		APIVersion: cr.TypeMeta.APIVersion,
		Kind:       cr.TypeMeta.Kind,
		Name:       cr.Name,
		UID:        cr.UID,
		Controller: &trueVar,
	}
}


// ParseResourceQuantity parses and returns a resource quantity from a string.
func ParseResourceQuantity(str string, useIfEmpty string) (resource.Quantity, error) {
	var result resource.Quantity

	if (str == "") {
		if (useIfEmpty != "") {
			result = resource.MustParse(useIfEmpty)
		}
	} else {
		var err error
		result, err = resource.ParseQuantity(str)
		if err != nil {
			return result, fmt.Errorf("Invalid resource quantity \"%s\": %s", str, err)
		}
	}

	return result, nil
}


// GetServiceFQDN returns the fully qualified domain name for a Kubernetes service.
func GetServiceFQDN(namespace string, name string) string {
	clusterDomain := os.Getenv("CLUSTER_DOMAIN")
	if clusterDomain == "" {
		clusterDomain = "cluster.local"
	}
	return fmt.Sprintf(
		"%s.%s.svc.%s",
		name, namespace, clusterDomain,
	)
}


// GenerateSecret returns a randomly generated sequence of text that is n bytes in length.
func GenerateSecret(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = SECRET_BYTES[rand.Int63() % int64(len(SECRET_BYTES))]
	}
	return b
}
