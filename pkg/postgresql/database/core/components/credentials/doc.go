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

// Package credentials contains the dormant PostgresDatabase policy for
// generated and externally managed role credentials. Kubernetes Secret objects
// and password material remain behind the consumer-owned port. CPI-2165 will
// link this component into the production facade after the remaining component
// slices are complete.
package credentials
