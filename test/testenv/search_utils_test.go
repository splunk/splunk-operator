// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package testenv

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Search export result parsing", func() {
	It("returns a transforming-search result", func() {
		results, err := ParseSearchExportResults[map[string]string](
			`{"preview":false,"offset":0,"result":{"events":"3","peers":"3"}}`,
		)

		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(Equal([]map[string]string{{"events": "3", "peers": "3"}}))
	})

	It("ignores export records that do not contain a result", func() {
		response := "{\"messages\":[{\"type\":\"INFO\",\"text\":\"search started\"}]}\n" +
			"{\"result\":{\"count\":\"1\"}}\n"

		results, err := ParseSearchExportResults[map[string]string](response)

		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(Equal([]map[string]string{{"count": "1"}}))
	})

	It("returns every result row for a non-transforming search", func() {
		response := "{\"result\":{\"peer\":\"indexer-0\",\"events\":\"1\"}}\n" +
			"{\"result\":{\"peer\":\"indexer-1\",\"events\":\"1\"}}\n"

		results, err := ParseSearchExportResults[map[string]string](response)

		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(ConsistOf(
			map[string]string{"peer": "indexer-0", "events": "1"},
			map[string]string{"peer": "indexer-1", "events": "1"},
		))
	})

	It("decodes non-string fields into a typed result", func() {
		type typedResult struct {
			Count   int  `json:"count"`
			Preview bool `json:"preview"`
		}

		results, err := ParseSearchExportResults[typedResult](
			`{"result":{"count":2,"preview":true}}`,
		)

		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(Equal([]typedResult{{Count: 2, Preview: true}}))
	})

	DescribeTable("rejects an invalid export response",
		func(response string, expectedMessage string) {
			_, err := ParseSearchExportResults[map[string]string](response)
			Expect(err).To(MatchError(ContainSubstring(expectedMessage)))
		},
		Entry("blank output", "\n", "no result rows"),
		Entry("malformed JSON", `{not-json}`, "parse search export line 1"),
		Entry("only records without results", `{"messages":[]}`, "no result rows"),
		Entry("a non-string result field", `{"result":{"count":1}}`, "parse search export result"),
	)
})
