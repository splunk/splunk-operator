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

/*
Package smartbus is the SmartBus splunkconfig.ConfFeature: the remote-queue .conf
settings a Splunk node needs to read from and write to a SmartBus queue.

Two orthogonal axes: Contribute maps role to conf files, identically for every
provider, while a provider implementation selected from QueueSpec.Provider decides
what goes inside each stanza.

Allowed imports from pkg/splunk/: common/, util/, splunkconfig/
*/
package smartbus
