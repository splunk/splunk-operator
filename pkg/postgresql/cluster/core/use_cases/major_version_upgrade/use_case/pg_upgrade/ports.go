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

package pgupgradeflow

import "context"

type PgUpgrade interface {
	ApplyTargetImage(context.Context) error
	UpgradeComplete(context.Context) (bool, error)
	VerifyUpgrade(context.Context) error
}

type Notifier interface {
	Inform(reason, message string)
	Warn(reason, message string)
}

type noopNotifier struct{}

func (noopNotifier) Inform(_, _ string) { /* no-op */ }
func (noopNotifier) Warn(_, _ string)   { /* no-op */ }

// NoopNotifier returns a Notifier that silently discards all events.
func NoopNotifier() Notifier { return noopNotifier{} }
