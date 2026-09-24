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

package common

// DefaultYML contains the splunk-ansible default.yml fields managed by SOK.
// It is not a full representation of the default.yml schema
type DefaultYML struct {
	Splunk SplunkDefault `yaml:"splunk"`
}

// SplunkDefault holds the splunk-ansible configuration entries.
type SplunkDefault struct {
	Conf []ConfFileEntry `yaml:"conf"`
}

// ConfFileEntry represents a single .conf file entry in the splunk-ansible default.yml.
type ConfFileEntry struct {
	// ConfFileName is the name of the .conf file without path or extension, e.g. "outputs" or "inputs".
	ConfFileName string        `yaml:"key"`
	Value        ConfFileValue `yaml:"value"`
}

// StanzaFields holds the field key-value pairs for a single .conf stanza.
// Keys are dot-separated field names (e.g. "remote_queue.sqs_smartbus.auth_region").
type StanzaFields map[string]string

// ConfFileStanzas maps stanza headers to their fields within a single .conf file.
// Keys are stanza names as they appear in brackets in a .conf file,
// e.g. "remote_queue://smartbus" from "[remote_queue://smartbus]".
type ConfFileStanzas map[string]StanzaFields

// ConfFileValue represents the value of a .conf file entry, including the target
// directory and the stanza content to write.
type ConfFileValue struct {
	// Directory is the app-local directory where the .conf file is written.
	// Defaults to $SPLUNK_HOME/etc/system/local when omitted.
	// SOK sets this to $SPLUNK_HOME/etc/apps/100-sok/local to isolate managed config.
	Directory string `yaml:"directory"`
	// Stanzas holds the stanza names and their field values. Keys are stanza
	// names (e.g. "remote_queue://smartbus").
	Stanzas ConfFileStanzas `yaml:"content"`
}
