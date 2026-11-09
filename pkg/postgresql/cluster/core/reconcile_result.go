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

package core

import ctrl "sigs.k8s.io/controller-runtime"

func soonerRequeue(a, b ctrl.Result) ctrl.Result {
	if a.Requeue || b.Requeue {
		return ctrl.Result{Requeue: true}
	}
	if a.RequeueAfter == 0 || b.RequeueAfter > 0 && b.RequeueAfter < a.RequeueAfter {
		return b
	}
	return a
}
