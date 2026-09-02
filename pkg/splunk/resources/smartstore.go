// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package resources

import (
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	corev1 "k8s.io/api/core/v1"
)

// IsSmartstoreConfigured reports whether SmartStore has any configured entries.
func IsSmartstoreConfigured(smartstore *enterpriseApi.SmartStoreSpec) bool {
	return smartstore != nil && (smartstore.IndexList != nil || smartstore.VolList != nil || smartstore.Defaults.VolName != "")
}

// PrepareSmartstoreConfigMap prepares the ConfigMap containing SmartStore
// indexes.conf and server.conf entries.
func PrepareSmartstoreConfigMap(configMapName, namespace, defaultsConfIni, volumesConfIni, indexesConfIni, serverConfIni string) *corev1.ConfigMap {
	return prepareConfigMap(configMapName, namespace, map[string]string{
		"indexes.conf": fmt.Sprintf(`%s %s %s`, defaultsConfIni, volumesConfIni, indexesConfIni),
		"server.conf":  serverConfIni,
	})
}

// GetSmartstoreIndexesConfig returns the list of indexes configuration in INI format.
func GetSmartstoreIndexesConfig(indexes []enterpriseApi.IndexSpec) string {
	var indexesConf string

	defaultRemotePath := "$_index_name"
	for i := 0; i < len(indexes); i++ {
		// Write the index stanza name
		indexesConf = fmt.Sprintf(`%s
[%s]`, indexesConf, indexes[i].Name)
		if indexes[i].RemotePath != "" && indexes[i].VolName != "" {
			indexesConf = fmt.Sprintf(`%s
remotePath = volume:%s/%s`, indexesConf, indexes[i].VolName, indexes[i].RemotePath)
		} else if indexes[i].VolName != "" {
			indexesConf = fmt.Sprintf(`%s
remotePath = volume:%s/%s`, indexesConf, indexes[i].VolName, defaultRemotePath)
		}
		if indexes[i].HotlistBloomFilterRecencyHours != 0 {
			indexesConf = fmt.Sprintf(`%s
hotlist_bloom_filter_recency_hours = %d`, indexesConf, indexes[i].HotlistBloomFilterRecencyHours)
		}
		if indexes[i].HotlistRecencySecs != 0 {
			indexesConf = fmt.Sprintf(`%s
hotlist_recency_secs = %d`, indexesConf, indexes[i].HotlistRecencySecs)
		}
		if indexes[i].MaxGlobalDataSizeMB != 0 {
			indexesConf = fmt.Sprintf(`%s
maxGlobalDataSizeMB = %d`, indexesConf, indexes[i].MaxGlobalDataSizeMB)
		}
		if indexes[i].MaxGlobalRawDataSizeMB != 0 {
			indexesConf = fmt.Sprintf(`%s
maxGlobalRawDataSizeMB = %d`, indexesConf, indexes[i].MaxGlobalRawDataSizeMB)
		}

		// Add a new line in between index stanzas
		// Do not add config beyond here
		indexesConf = fmt.Sprintf(`%s
`, indexesConf)
	}
	return indexesConf
}

// GetServerConfigEntries prepares the server.conf entries, and returns as a string.
func GetServerConfigEntries(cacheManagerConf *enterpriseApi.CacheManagerSpec) string {
	if cacheManagerConf == nil {
		return ""
	}
	var serverConfIni string
	serverConfIni = `[cachemanager]`
	emptyStanza := serverConfIni
	if cacheManagerConf.EvictionPaddingSizeMB != 0 {
		serverConfIni = fmt.Sprintf(`%s
eviction_padding = %d`, serverConfIni, cacheManagerConf.EvictionPaddingSizeMB)
	}
	if cacheManagerConf.EvictionPolicy != "" {
		serverConfIni = fmt.Sprintf(`%s
eviction_policy = %s`, serverConfIni, cacheManagerConf.EvictionPolicy)
	}
	if cacheManagerConf.HotlistBloomFilterRecencyHours != 0 {
		serverConfIni = fmt.Sprintf(`%s
hotlist_bloom_filter_recency_hours = %d`, serverConfIni, cacheManagerConf.HotlistBloomFilterRecencyHours)
	}
	if cacheManagerConf.HotlistRecencySecs != 0 {
		serverConfIni = fmt.Sprintf(`%s
hotlist_recency_secs = %d`, serverConfIni, cacheManagerConf.HotlistRecencySecs)
	}
	if cacheManagerConf.MaxCacheSizeMB != 0 {
		serverConfIni = fmt.Sprintf(`%s
max_cache_size = %d`, serverConfIni, cacheManagerConf.MaxCacheSizeMB)
	}
	if cacheManagerConf.MaxConcurrentDownloads != 0 {
		serverConfIni = fmt.Sprintf(`%s
max_concurrent_downloads = %d`, serverConfIni, cacheManagerConf.MaxConcurrentDownloads)
	}
	if cacheManagerConf.MaxConcurrentUploads != 0 {
		serverConfIni = fmt.Sprintf(`%s
max_concurrent_uploads = %d`, serverConfIni, cacheManagerConf.MaxConcurrentUploads)
	}
	if emptyStanza == serverConfIni {
		return ""
	}
	serverConfIni = fmt.Sprintf(`%s
`, serverConfIni)
	return serverConfIni
}

// GetSmartstoreIndexesDefaults fills the indexes.conf default stanza in INI format.
func GetSmartstoreIndexesDefaults(defaults enterpriseApi.IndexConfDefaultsSpec) string {
	remotePath := "$_index_name"
	indexDefaults := fmt.Sprintf(`[default]
repFactor = auto
maxDataSize = auto
homePath = $SPLUNK_DB/%s/db
coldPath = $SPLUNK_DB/%s/colddb
thawedPath = $SPLUNK_DB/%s/thaweddb`, remotePath, remotePath, remotePath)
	// Do not change any of the following Sprintf formats(Intentionally indented)
	if defaults.VolName != "" {
		//if defaults.VolName != "" && defaults.RemotePath != "" {
		indexDefaults = fmt.Sprintf(`%s
remotePath = volume:%s/%s`, indexDefaults, defaults.VolName, remotePath)
	}
	if defaults.MaxGlobalDataSizeMB != 0 {
		indexDefaults = fmt.Sprintf(`%s
maxGlobalDataSizeMB = %d`, indexDefaults, defaults.MaxGlobalDataSizeMB)
	}
	if defaults.MaxGlobalRawDataSizeMB != 0 {
		indexDefaults = fmt.Sprintf(`%s
maxGlobalRawDataSizeMB = %d`, indexDefaults, defaults.MaxGlobalRawDataSizeMB)
	}
	return fmt.Sprintf(`%s
`, indexDefaults)
}
