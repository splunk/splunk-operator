// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

package config

import (
	"os"
	"strings"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
)

const (
	// WatchNamespaceEnvVar is the constant for env variable WATCH_NAMESPACE
	// which specifies the Namespace to watch.
	// An empty value means the operator is running with cluster scope.
	WatchNamespaceEnvVar = "WATCH_NAMESPACE"
)

// GetWatchNamespaces returns the Namespaces the operator should be watching for changes.
func GetWatchNamespaces() []string {
	ns, found := os.LookupEnv(WatchNamespaceEnvVar)
	if !found {
		return nil
	}

	// Add support for MultiNamespace set in WATCH_NAMESPACE (e.g ns1,ns2).
	if strings.Contains(ns, ",") {
		return strings.Split(ns, ",")
	}

	return []string{ns}
}

// ManagerOptionsWithNamespaces returns an updated Options with namespaces information.
func ManagerOptionsWithNamespaces(logger logr.Logger, opt ctrl.Options) ctrl.Options {
	namespaces := GetWatchNamespaces()
	switch {
	case len(namespaces) == 0:
		logger.Info("Manager will watch and manage resources in all namespaces")
	case len(namespaces) == 1:
		logger.Info("Manager will be watching namespace", "namespace", namespaces[0])
		opt.Namespace = namespaces[0]
	case len(namespaces) > 1:
		// configure cluster-scoped with MultiNamespacedCacheBuilder
		logger.Info("Manager will be watching multiple namespaces", "namespaces", namespaces)
		opt.Namespace = ""
		opt.NewCache = cache.MultiNamespacedCacheBuilder(namespaces)
	}

	return opt
}
