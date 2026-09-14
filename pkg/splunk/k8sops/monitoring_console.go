// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package k8sops

import (
	"context"
	"reflect"
	"sort"
	"strings"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func ValidateMonitoringConsoleRef(ctx context.Context, c splcommon.ControllerClient, revised *appsv1.StatefulSet, serviceURLs []corev1.EnvVar) error {
	var err error
	namespacedName := types.NamespacedName{Namespace: revised.GetNamespace(), Name: revised.GetName()}
	var current appsv1.StatefulSet

	err = c.Get(context.TODO(), namespacedName, &current)
	if err == nil {
		currEnv := current.Spec.Template.Spec.Containers[0].Env
		revEnv := revised.Spec.Template.Spec.Containers[0].Env

		var cEnv, rEnv corev1.EnvVar

		for _, cEnvTemp := range currEnv {
			if cEnvTemp.Name == "SPLUNK_MONITORING_CONSOLE_REF" {
				cEnv.Value = cEnvTemp.Value
			}
		}

		for _, rEnvTemp := range revEnv {
			if rEnvTemp.Name == "SPLUNK_MONITORING_CONSOLE_REF" {
				rEnv.Value = rEnvTemp.Value
			}
		}

		if cEnv.Value != "" && rEnv.Value != "" && cEnv.Value != rEnv.Value {
			//1. if revised Spec has different mcRef defined
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, c, current.ObjectMeta.GetNamespace(), current.ObjectMeta.GetName(), cEnv.Value, serviceURLs, false)
			if err != nil {
				return err
			}
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, c, current.ObjectMeta.GetNamespace(), current.ObjectMeta.GetName(), rEnv.Value, serviceURLs, true)
			if err != nil {
				return err
			}
		} else if cEnv.Value != "" && rEnv.Value == "" {
			//2. if revised Spec doesn't have mcRef defined
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, c, current.ObjectMeta.GetNamespace(), current.ObjectMeta.GetName(), cEnv.Value, serviceURLs, false)
			if err != nil {
				return err
			}
		}
	}
	//if the sts doesn't exists no need for any change
	return nil
}

func ApplyMonitoringConsoleEnvConfigMap(ctx context.Context, client splcommon.ControllerClient, namespace string, crName string, monitoringConsoleRef string, newURLs []corev1.EnvVar, addNewURLs bool) (*corev1.ConfigMap, error) {

	var current corev1.ConfigMap

	configMap := splutil.GetSplunkMonitoringconsoleConfigMapName(monitoringConsoleRef, splcommon.SplunkMonitoringConsole)
	namespacedName := types.NamespacedName{Namespace: namespace, Name: configMap}
	err := client.Get(ctx, namespacedName, &current)

	if err == nil {
		revised := current.DeepCopy()
		if revised.Data == nil {
			revised.Data = make(map[string]string)
		}
		if addNewURLs {
			AddMonitoringConsoleURLs(revised, crName, newURLs)
		} else {
			DeleteMonitoringConsoleURLs(revised, crName, newURLs, true)
		}
		if !reflect.DeepEqual(revised.Data, current.Data) {
			current.Data = revised.Data
			err = splutil.UpdateResource(ctx, client, &current)
			if err != nil {
				return nil, err
			}
		}
		return &current, nil
	}

	// if err is not resource not found then return the err
	if !k8serrors.IsNotFound(err) {
		return nil, err
	}

	// case when resource not found
	//If no configMap and deletion of CR is requested then create a empty configMap
	current = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMap,
			Namespace: namespace,
		},
		Data: make(map[string]string),
	}
	if addNewURLs {

		//else create a new configMap with new entries
		for _, url := range newURLs {
			current.Data[url.Name] = url.Value
		}
	}

	current.ObjectMeta = metav1.ObjectMeta{
		Name:      configMap,
		Namespace: namespace,
	}

	err = splutil.CreateResource(ctx, client, &current)
	if err != nil {
		return nil, err
	}

	return &current, nil
}

// crPodNamePrefix derives the per-CR resource-name prefix "splunk-<id>-<kind>-"
// from the first entry of a comma-separated MC URL value. Supports statefulset
// pod URLs (suffix "-<digits>") and service URLs (suffix "-service"/"-headless").
// Returns "" when the prefix cannot be derived; callers then fall back to crName.
func crPodNamePrefix(value string) string {
	if value == "" {
		return ""
	}
	// Pod/service name has no '.', strip any DNS suffix and trailing entries.
	name := strings.SplitN(strings.SplitN(value, ",", 2)[0], ".", 2)[0]
	idx := strings.LastIndex(name, "-")
	if idx <= 0 || idx == len(name)-1 {
		return ""
	}
	suffix := name[idx+1:]
	if suffix != "service" && suffix != "headless" {
		for _, r := range suffix {
			if r < '0' || r > '9' {
				return ""
			}
		}
	}
	return name[:idx+1]
}

// crOwnsURL reports whether `curr` belongs to the CR identified by crPrefix.
// Ownership requires the derived prefix of `curr` to equal crPrefix: a plain
// substring check is unsafe when one CR's name (or kind segment) is contained
// in another's (e.g. "search-head" vs "search-head-adhoc", or "cm" vs
// "cm-cluster-manager-extra"). Falls back to a crName substring match when no
// prefix can be derived.
func crOwnsURL(curr, crPrefix, crName string) bool {
	if crPrefix == "" {
		return strings.Contains(curr, crName)
	}
	if currPrefix := crPodNamePrefix(curr); currPrefix != "" {
		return currPrefix == crPrefix
	}
	return strings.Contains(curr, crPrefix)
}

// AddMonitoringConsoleURLs adds server peers to a Monitoring Console ConfigMap.
func AddMonitoringConsoleURLs(revised *corev1.ConfigMap, crName string, newURLs []corev1.EnvVar) {
	for _, url := range newURLs {
		if _, ok := revised.Data[url.Name]; !ok {
			revised.Data[url.Name] = url.Value
			continue
		}

		newInstanceURLs := strings.Split(url.Value, ",")
		crPrefix := crPodNamePrefix(url.Value)
		currentURLs := strings.Split(revised.Data[url.Name], ",")
		currentCRCount := 0
		// 1. Count CR-owned URLs currently present in the configmap for this key.
		//    We compare counts (not string lengths) because string-length comparison
		//    is unreliable: it depends on whether new entries are a subset of current,
		//    and could never detect scale-down (where current has MORE CR URLs than new).
		for _, currentURL := range currentURLs {
			if crOwnsURL(currentURL, crPrefix, crName) {
				currentCRCount++
			}
		}

		if currentCRCount == len(newInstanceURLs) {
			// 2. Same count: ensure all new entries are present (otherwise it's a rename/no-op),
			//    nothing to add or remove.
			allPresent := true
			for _, newEntry := range newInstanceURLs {
				if !strings.Contains(revised.Data[url.Name], newEntry) {
					allPresent = false
					break
				}
			}
			if allPresent {
				continue
			}
		}

		if currentCRCount < len(newInstanceURLs) {
			// 3. scaling UP
			for _, newEntry := range newInstanceURLs {
				if !strings.Contains(revised.Data[url.Name], newEntry) {
					revised.Data[url.Name] = strings.Join([]string{revised.Data[url.Name], newEntry}, ",")
				}
			}
			continue
		}

		// 4. scaling DOWN (currentCRCount > newCount)
		DeleteMonitoringConsoleURLs(revised, crName, newURLs, false)
	}
}

// AddURLsConfigMap is retained for compatibility with callers of the legacy helper name.
// Deprecated: use AddMonitoringConsoleURLs.
func AddURLsConfigMap(revised *corev1.ConfigMap, crName string, newURLs []corev1.EnvVar) {
	AddMonitoringConsoleURLs(revised, crName, newURLs)
}

// DeleteMonitoringConsoleURLs removes server peers from a Monitoring Console ConfigMap.
func DeleteMonitoringConsoleURLs(revised *corev1.ConfigMap, crName string, newURLs []corev1.EnvVar, deleteCR bool) {
	for _, url := range newURLs {
		crPrefix := crPodNamePrefix(url.Value)
		currentURLs := strings.Split(revised.Data[url.Name], ",")
		sort.Strings(currentURLs)
		for _, currentURL := range currentURLs {
			// scale DOWN
			if crOwnsURL(currentURL, crPrefix, crName) && !strings.Contains(url.Value, currentURL) && !deleteCR {
				revised.Data[url.Name] = strings.ReplaceAll(revised.Data[url.Name], currentURL, "")
			} else if crOwnsURL(currentURL, crPrefix, crName) && deleteCR {
				revised.Data[url.Name] = strings.ReplaceAll(revised.Data[url.Name], url.Value, "")
			}
			// if deleting "SPLUNK_MULTISITE_MASTER" delete "SPLUNK_SITE"
			if url.Name == "SPLUNK_SITE" && deleteCR {
				delete(revised.Data, "SPLUNK_SITE")
			}
			if strings.HasPrefix(revised.Data[url.Name], ",") {
				revised.Data[url.Name] = strings.TrimPrefix(revised.Data[url.Name], ",")
			}
			if strings.HasSuffix(revised.Data[url.Name], ",") {
				revised.Data[url.Name] = strings.TrimSuffix(revised.Data[url.Name], ",")
			}
			if strings.Contains(revised.Data[url.Name], ",,") {
				revised.Data[url.Name] = strings.ReplaceAll(revised.Data[url.Name], ",,", ",")
			}
			if revised.Data[url.Name] == "" {
				delete(revised.Data, url.Name)
			}
		}
	}
}

// DeleteURLsConfigMap is retained for compatibility with callers of the legacy helper name.
// Deprecated: use DeleteMonitoringConsoleURLs.
func DeleteURLsConfigMap(revised *corev1.ConfigMap, crName string, newURLs []corev1.EnvVar, deleteCR bool) {
	DeleteMonitoringConsoleURLs(revised, crName, newURLs, deleteCR)
}
