/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package majorupgradeadapter

import "strings"

// hbaRule is the single PostgreSQL representation used to render the CNPG
// overlay and the expected pg_hba_file_rules proof, preserving rule order.
type hbaRule struct{ kind, database, user, address, method string }

const hbaTLSConnection = "hostssl"

func cutoverFenceRules(control, replication string) []hbaRule {
	return []hbaRule{
		{hbaTLSConnection, "all", control, "all", scramSHA256},
		{hbaTLSConnection, "all", replication, "all", scramSHA256},
		{hbaTLSConnection, "replication", replication, "all", scramSHA256},
		{"host", "all", "all", "all", "reject"},
	}
}

func (r hbaRule) configLine() string {
	return strings.Join([]string{r.kind, r.database, r.user, r.address, r.method}, " ")
}

func (r hbaRule) loadedLine() string {
	return strings.Join([]string{r.kind, r.database, r.user, r.address, r.method}, ":")
}

func fenceRuleProofSQL(rules []hbaRule) string {
	values := make([]string, 0, len(rules))
	for _, rule := range rules {
		values = append(values, quoteSQLStringLiteral(rule.loadedLine()))
	}
	return "ARRAY[" + strings.Join(values, ",") + "]"
}
