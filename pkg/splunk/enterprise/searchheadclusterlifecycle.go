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

package enterprise

import (
	"strings"
	"time"
)

// This file holds the pure decision logic behind searchHeadClusterPodManager's
// detention-timeout and upgrade-window bookkeeping — the part of the SHC
// rolling-update lifecycle that is genuinely stateful and easy to get subtly
// wrong (timer restarts, timeout boundaries, revision-change detection).
// Every function here takes plain values and returns plain values: no REST
// calls, no Kubernetes client, no *SearchHeadCluster. That is what makes them
// exhaustively unit-testable without mocking a REST client, and it is the
// first step toward the wider SHC lifecycle state machine (CSPL-5266): the
// imperative methods on searchHeadClusterPodManager become thin shells that
// gather observations, call these functions, and execute the I/O the result
// implies.

// DetentionWindow is the cross-reconcile bookkeeping searchHeadClusterPodManager
// persists in cr.Status while a member sits in ManualDetention. Its only
// purpose is measuring how long a member has been waiting, so
// Spec.DetentionTimeoutSeconds can eventually force a recycle even if active
// searches never drain to zero on their own.
type DetentionWindow struct {
	StartTimestamp int64
	MemberName     string
	PodRevision    string
}

// nextDetentionWindow decides whether the detention clock should restart for
// memberName, given its currently observed pod revision, or continue an
// already-running window.
//
// The clock restarts when detention was not already running, when a
// different member was being tracked, or when this member's own revision
// changed underneath us — that last case identifies a genuine replacement
// pod: a timeout-forced recycle can leave a stale window open (PrepareRecycle
// never clears it; only FinishRecycle does, after the pod is confirmed back),
// and if a new pod has since entered ManualDetention in its place, that is a
// new episode, not a continuation of the old one.
//
// An empty currentRevision (the controller-revision-hash label not yet
// observed) never counts as a change on its own, and never restarts an
// already-running window — it is simply recorded once observed.
func nextDetentionWindow(existing DetentionWindow, memberName, currentRevision string, now time.Time) DetentionWindow {
	revisionChanged := existing.PodRevision != "" &&
		currentRevision != "" &&
		existing.PodRevision != currentRevision
	startNew := existing.StartTimestamp == 0 ||
		existing.MemberName != memberName ||
		revisionChanged
	if startNew {
		return DetentionWindow{
			StartTimestamp: now.Unix(),
			MemberName:     memberName,
			PodRevision:    currentRevision,
		}
	}
	next := existing
	if currentRevision != "" {
		next.PodRevision = currentRevision
	}
	return next
}

// effectiveDetentionTimeoutSeconds returns the configured detention timeout,
// falling back to defaultSearchHeadDetentionTimeoutSeconds when unset (0) or
// invalid (negative).
func effectiveDetentionTimeoutSeconds(specTimeoutSeconds int32) int64 {
	if specTimeoutSeconds <= 0 {
		return defaultSearchHeadDetentionTimeoutSeconds
	}
	return int64(specTimeoutSeconds)
}

// detentionTimedOut reports whether a member that entered ManualDetention at
// window.StartTimestamp has been waiting at least timeoutSeconds, as of now.
// This is the escape hatch that guarantees a rolling update eventually makes
// progress even if a search on the drained member never finishes: past this
// point PrepareRecycle reports the member ready for deletion regardless of
// its active-search count, and the pod delete kills those searches outright.
func detentionTimedOut(window DetentionWindow, timeoutSeconds int64, now time.Time) bool {
	return now.Unix()-window.StartTimestamp >= timeoutSeconds
}

// activeSearchCount is Historical+Realtime, named so the "<=0 means drained"
// comparison at call sites reads as what it means rather than an anonymous sum.
func activeSearchCount(historical, realtime int) int {
	return historical + realtime
}

// isNewUpgradeWindow reports whether recycling a member should be treated as
// the start of a new cluster-wide upgrade window, given the previously
// recorded start/end timestamps. end >= start means the previous window (if
// any) already closed, so this begins a fresh one and its bookkeeping should
// be reset; end < start means a window is already open, so leave its
// timestamps alone. This mirrors the exact comparison the original inline
// code used — it does not gate whether InitiateUpgrade itself gets called,
// only whether the start-timestamp/phase bookkeeping resets.
func isNewUpgradeWindow(previousStart, previousEnd int64) bool {
	return previousEnd >= previousStart
}

// captainStabilizationSeconds is the minimum time the currently observed
// captain must have held continuously (same label, ready) before a rolling
// update is allowed to recycle another member. This closes a real gap: today
// nothing stops recycling the member after the captain the moment Splunk's
// own dynamic re-election reports a new captain ready, even though that
// election just happened and nothing has confirmed it actually held. It is
// deliberately a small, fixed constant rather than a Spec field for this
// first pass — the goal is visibility and a minimum pause, not a tunable knob.
const captainStabilizationSeconds = 30

// nextCaptainStableSince decides the timestamp from which the currently
// observed captain label has been continuously ready.
//
// The very first time a captain is ever observed — previousLabel empty AND
// no stable timestamp has ever been recorded (previousStableSince zero) — is
// treated as already stable, backdated by a full settle window: nothing has
// been recycled yet, so there is no "did the newly elected captain actually
// hold after a recycle" question to answer, and a brand new cluster must not
// sit blocked for captainStabilizationSeconds before its first rolling
// update can even begin.
//
// previousLabel is also empty on every reconcile where reading captain info
// failed outright (the caller unconditionally clears Status.Captain before
// attempting the read), which is not the same as "never observed": a
// previously nonzero previousStableSince proves a captain was tracked before
// this outage. Backdating here would let an outage right after a real
// captain change masquerade as an already-settled captain the moment the
// first post-outage read succeeds. Treat that case as a plain reset to now
// instead, same as any other not-continuously-ready transition.
//
// Past that first observation, the clock restarts whenever the previously
// observed state was not itself continuously ready under the same label:
// a different label (a captain that changed because the previous one was
// just recycled), a captain that is not currently ready, or a previous
// observation that was not ready (including one where reading captain info
// failed outright — the caller leaves CaptainReady false in that case).
// Any of these means the interval since the last reset cannot be counted as
// continuous readiness, so a later observation of "same label, now ready"
// must not inherit a timestamp that predates a not-ready gap. Only "same
// label, ready last time, ready now" leaves the existing timestamp
// unchanged.
func nextCaptainStableSince(previousLabel string, previousReady bool, previousStableSince int64, observedLabel string, captainReady bool, now time.Time) int64 {
	if previousLabel == "" && observedLabel != "" && captainReady {
		if previousStableSince != 0 {
			return now.Unix()
		}
		return now.Add(-captainStabilizationSeconds * time.Second).Unix()
	}
	if !captainReady || !previousReady || observedLabel == "" || observedLabel != previousLabel {
		return now.Unix()
	}
	return previousStableSince
}

// captainStable reports whether the captain has been continuously stable for
// at least captainStabilizationSeconds, as of now. An empty label reports
// stable (true): that only means no captain has ever been observed yet at
// all, a state callers already gate on elsewhere before ever reaching a
// recycle decision — treating it as "not stable" here would block on a
// precondition this function was never meant to enforce in the first place.
func captainStable(label string, stableSince int64, now time.Time) bool {
	if label == "" {
		return true
	}
	return now.Unix()-stableSince >= captainStabilizationSeconds
}

// isCaptainMember reports whether memberName identifies the same search head
// as captainLabel. captain/info reports the captain as a fully-qualified
// label (e.g. "splunk-c3-search-head-2.splunk-c3-search-head-headless...
// .svc.cluster.local"), while a member's own tracked Name is just its short
// pod name (e.g. "splunk-c3-search-head-2") — comparing them for exact
// equality would never match in real usage. A member owns the captain
// label either if it equals it outright (some builds may report a bare
// name) or if the label starts with the member name followed by a dot (the
// FQDN case).
func isCaptainMember(captainLabel, memberName string) bool {
	if memberName == "" {
		return false
	}
	return captainLabel == memberName || strings.HasPrefix(captainLabel, memberName+".")
}

// memberNeedsRecycle reports whether a member still counts as "not yet
// rolled" for the purpose of deferring the captain: either its pod has not
// been recreated with the desired revision at all yet, or it has but has
// not yet fully rejoined (status back to "Up"). The captain must not be
// touched while any other member is still anywhere in this process — a
// pod that was merely recreated but hasn't rejoined the cluster yet is not
// meaningfully safer to have next to a captain recycle than one that hasn't
// been touched at all.
func memberNeedsRecycle(podRevision, desiredRevision, status string) bool {
	if podRevision != desiredRevision {
		return true
	}
	return status != "Up"
}

// shouldDeferCaptainRecycle reports whether recycling the member at the
// ordinal currently holding the captain role should be deferred, given how
// many other members still need to be recycled. This is re-evaluated fresh
// every reconcile from live data (who currently holds captain, and every
// member's current revision/status) — if the captain changes mid rolling
// update, which is common in Kubernetes since any pod disruption can trigger
// a new election, the newly-current captain is simply the one deferred on
// the next reconcile. There is no "captain as of when the rollout started"
// bookkeeping to get wrong, because none is kept.
//
// alreadyStartedOwnRecycle (the member's own Status is "ManualDetention")
// always wins over deferring, even if otherMembersNeedingRecycle is nonzero.
// The captain can only ever reach ManualDetention once every other member
// was already fully recycled (otherMembersNeedingRecycle was 0 at that
// point), so seeing it nonzero again afterward means some already-recycled
// member's live status independently regressed away from "Up" — an
// unrelated event, not a reason to re-defer a recycle already in progress.
// Once started, finish it rather than leaving it paused indefinitely.
func shouldDeferCaptainRecycle(isCaptain bool, alreadyStartedOwnRecycle bool, otherMembersNeedingRecycle int) bool {
	if alreadyStartedOwnRecycle {
		return false
	}
	return isCaptain && otherMembersNeedingRecycle > 0
}
