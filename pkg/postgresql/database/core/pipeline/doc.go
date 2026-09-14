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

/*
Package pipeline contains the database-local reconcile runner prototype from
ADR-0006. It defines ordered steps, dependency contracts, and a use-case adapter
without moving production phases into the runner. Shared database reconciliation
outcomes live in database/core/types/reconciliation so future components and
use cases can report results without importing this orchestration package.
*/
package pipeline
