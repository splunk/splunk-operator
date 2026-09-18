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

package shc

import (
	"testing"
	"time"
)

func TestNextDetentionWindow_FirstEntryStartsWindow(t *testing.T) {
	now := time.Now()
	window := nextDetentionWindow(DetentionWindow{}, "member-0", "rev-1", now)

	if window.StartTimestamp != now.Unix() {
		t.Errorf("StartTimestamp = %d, want %d", window.StartTimestamp, now.Unix())
	}
	if window.MemberName != "member-0" {
		t.Errorf("MemberName = %q, want %q", window.MemberName, "member-0")
	}
	if window.PodRevision != "rev-1" {
		t.Errorf("PodRevision = %q, want %q", window.PodRevision, "rev-1")
	}
}

func TestNextDetentionWindow_DifferentMemberRestartsWindow(t *testing.T) {
	existing := DetentionWindow{StartTimestamp: 100, MemberName: "member-0", PodRevision: "rev-1"}
	now := time.Unix(1000, 0)

	window := nextDetentionWindow(existing, "member-1", "rev-1", now)

	if window.StartTimestamp != now.Unix() {
		t.Error("expected the window to restart for a different member")
	}
	if window.MemberName != "member-1" {
		t.Errorf("MemberName = %q, want %q", window.MemberName, "member-1")
	}
}

func TestNextDetentionWindow_RevisionChangeRestartsWindow(t *testing.T) {
	existing := DetentionWindow{StartTimestamp: 100, MemberName: "member-0", PodRevision: "rev-1"}
	now := time.Unix(1000, 0)

	window := nextDetentionWindow(existing, "member-0", "rev-2", now)

	if window.StartTimestamp != now.Unix() {
		t.Error("expected the window to restart when the observed pod revision changed — a genuine replacement pod")
	}
	if window.PodRevision != "rev-2" {
		t.Errorf("PodRevision = %q, want %q", window.PodRevision, "rev-2")
	}
}

func TestNextDetentionWindow_UnchangedRevisionContinuesWindow(t *testing.T) {
	existing := DetentionWindow{StartTimestamp: 100, MemberName: "member-0", PodRevision: "rev-1"}
	now := time.Unix(1000, 0)

	window := nextDetentionWindow(existing, "member-0", "rev-1", now)

	if window.StartTimestamp != 100 {
		t.Errorf("StartTimestamp = %d, want the original 100 (window must not restart)", window.StartTimestamp)
	}
}

func TestNextDetentionWindow_FirstObservedRevisionDoesNotRestartWindow(t *testing.T) {
	// DetainedPodRevision starts empty (label not yet observed on a prior
	// reconcile); the label finally showing up must not look like a change.
	existing := DetentionWindow{StartTimestamp: 100, MemberName: "member-0", PodRevision: ""}
	now := time.Unix(1000, 0)

	window := nextDetentionWindow(existing, "member-0", "rev-1", now)

	if window.StartTimestamp != 100 {
		t.Error("expected the window to continue when the pod revision is observed for the first time")
	}
	if window.PodRevision != "rev-1" {
		t.Errorf("PodRevision = %q, want the newly observed %q recorded", window.PodRevision, "rev-1")
	}
}

func TestNextDetentionWindow_UnobservedRevisionDoesNotRestartOrOverwrite(t *testing.T) {
	// A transient failure to read the controller-revision-hash label (empty
	// currentRevision) must neither restart the window nor blank out an
	// already-recorded revision.
	existing := DetentionWindow{StartTimestamp: 100, MemberName: "member-0", PodRevision: "rev-1"}
	now := time.Unix(1000, 0)

	window := nextDetentionWindow(existing, "member-0", "", now)

	if window.StartTimestamp != 100 {
		t.Error("expected the window to continue when the current revision is transiently unobservable")
	}
	if window.PodRevision != "rev-1" {
		t.Errorf("PodRevision = %q, want the previously recorded %q preserved", window.PodRevision, "rev-1")
	}
}

func TestEffectiveDetentionTimeoutSeconds(t *testing.T) {
	cases := []struct {
		name string
		in   int32
		want int64
	}{
		{"unset defaults", 0, defaultSearchHeadDetentionTimeoutSeconds},
		{"negative defaults", -1, defaultSearchHeadDetentionTimeoutSeconds},
		{"positive is honored", 60, 60},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveDetentionTimeoutSeconds(tc.in); got != tc.want {
				t.Errorf("effectiveDetentionTimeoutSeconds(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestDetentionTimedOut(t *testing.T) {
	window := DetentionWindow{StartTimestamp: 1000}
	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"before timeout", time.Unix(1000+59, 0), false},
		{"exactly at timeout boundary", time.Unix(1000+60, 0), true},
		{"past timeout", time.Unix(1000+61, 0), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detentionTimedOut(window, 60, tc.now); got != tc.want {
				t.Errorf("detentionTimedOut(...) = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestActiveSearchCount(t *testing.T) {
	if got := activeSearchCount(3, 2); got != 5 {
		t.Errorf("activeSearchCount(3, 2) = %d, want 5", got)
	}
	if got := activeSearchCount(0, 0); got != 0 {
		t.Errorf("activeSearchCount(0, 0) = %d, want 0", got)
	}
}

func TestIsNewUpgradeWindow(t *testing.T) {
	cases := []struct {
		name  string
		start int64
		end   int64
		want  bool
	}{
		{"no prior window (both zero)", 0, 0, true},
		{"previous window closed (end after start)", 100, 200, true},
		{"previous window closed (end equals start)", 100, 100, true},
		{"previous window still open (end before start)", 200, 100, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNewUpgradeWindow(tc.start, tc.end); got != tc.want {
				t.Errorf("isNewUpgradeWindow(%d, %d) = %v, want %v", tc.start, tc.end, got, tc.want)
			}
		})
	}
}

func TestNextCaptainStableSince_FirstObservationIsImmediatelyStable(t *testing.T) {
	// A brand new cluster's very first captain observation must not sit
	// blocked for captainStabilizationSeconds before its first rolling
	// update can even begin — nothing has been recycled yet, so there is no
	// "did the newly elected captain hold" question to answer.
	now := time.Unix(1000, 0)
	got := nextCaptainStableSince("", false, 0, "captain-0", true, now)
	if !captainStable("captain-0", got, now) {
		t.Error("expected a cluster's first-ever captain observation to be immediately stable")
	}
}

func TestNextCaptainStableSince_PostOutageReadDoesNotBackdate(t *testing.T) {
	// previousLabel is empty not just for a truly fresh cluster but also for
	// any reconcile where captain/info failed outright — the caller
	// unconditionally clears Status.Captain before attempting the read. A
	// nonzero previousStableSince proves a captain was tracked before this
	// outage, so the first post-outage success must restart the clock to
	// now, not backdate it as if this were the cluster's first-ever
	// observation. Backdating here would let a captain that was recycled
	// right before the outage appear already-settled the instant the first
	// post-outage read succeeds.
	now := time.Unix(2000, 0)
	got := nextCaptainStableSince("", false, 900, "captain-0", true, now)
	if got != now.Unix() {
		t.Errorf("nextCaptainStableSince(...) = %d, want %d (post-outage read must reset, not backdate)", got, now.Unix())
	}
}

func TestNextCaptainStableSince_SameLabelStillReadyContinuesClock(t *testing.T) {
	now := time.Unix(2000, 0)
	got := nextCaptainStableSince("captain-0", true, 500, "captain-0", true, now)
	if got != 500 {
		t.Errorf("nextCaptainStableSince(...) = %d, want the original 500 (clock must not restart)", got)
	}
}

func TestNextCaptainStableSince_DifferentLabelRestartsClock(t *testing.T) {
	now := time.Unix(2000, 0)
	got := nextCaptainStableSince("captain-0", true, 500, "captain-1", true, now)
	if got != now.Unix() {
		t.Errorf("nextCaptainStableSince(...) = %d, want %d (new captain must restart the clock)", got, now.Unix())
	}
}

func TestNextCaptainStableSince_NotReadyRestartsClockEvenUnderSameLabel(t *testing.T) {
	now := time.Unix(2000, 0)
	got := nextCaptainStableSince("captain-0", true, 500, "captain-0", false, now)
	if got != now.Unix() {
		t.Errorf("nextCaptainStableSince(...) = %d, want %d (not-ready must restart the clock)", got, now.Unix())
	}
}

func TestNextCaptainStableSince_EmptyObservedLabelRestartsClock(t *testing.T) {
	now := time.Unix(2000, 0)
	got := nextCaptainStableSince("captain-0", true, 500, "", true, now)
	if got != now.Unix() {
		t.Errorf("nextCaptainStableSince(...) = %d, want %d (an unobserved captain must not appear stable)", got, now.Unix())
	}
}

func TestNextCaptainStableSince_PreviouslyNotReadyRestartsClockOnceReadyAgain(t *testing.T) {
	// Regression for a real gap: if the previous observation left the captain
	// not ready under this same label — whether because captain/info reported
	// not-ready, or because the captain/info call itself failed and the caller
	// left CaptainReady false — then observing the same label ready now must
	// not inherit a stableSince timestamp that predates the not-ready gap.
	// Otherwise the not-ready interval gets silently counted as stable time.
	now := time.Unix(2000, 0)
	got := nextCaptainStableSince("captain-0", false, 500, "captain-0", true, now)
	if got != now.Unix() {
		t.Errorf("nextCaptainStableSince(...) = %d, want %d (previously-not-ready must restart the clock, not reuse the stale timestamp)", got, now.Unix())
	}
}

func TestCaptainStable_EmptyLabelReportsStable(t *testing.T) {
	// No captain has ever been tracked at all — callers already gate on this
	// precondition elsewhere before ever reaching a recycle decision.
	if !captainStable("", 0, time.Now()) {
		t.Error("expected an empty (never observed) captain label to report stable")
	}
}

func TestCaptainStable_BeforeSettleWindowIsNotStable(t *testing.T) {
	now := time.Unix(1000, 0)
	stableSince := now.Add(-(captainStabilizationSeconds - 1) * time.Second).Unix()
	if captainStable("captain-0", stableSince, now) {
		t.Error("expected captain to not yet be reported stable before the settle window elapses")
	}
}

func TestCaptainStable_AtSettleWindowBoundaryIsStable(t *testing.T) {
	now := time.Unix(1000, 0)
	stableSince := now.Add(-captainStabilizationSeconds * time.Second).Unix()
	if !captainStable("captain-0", stableSince, now) {
		t.Error("expected captain to be reported stable exactly at the settle window boundary")
	}
}

func TestCaptainStable_WellPastSettleWindowIsStable(t *testing.T) {
	now := time.Unix(1000, 0)
	stableSince := now.Add(-10 * time.Minute).Unix()
	if !captainStable("captain-0", stableSince, now) {
		t.Error("expected a long-stable captain to be reported stable")
	}
}

func TestMemberNeedsRecycle(t *testing.T) {
	cases := []struct {
		name       string
		podRev     string
		desiredRev string
		status     string
		want       bool
	}{
		{"stale revision", "v0", "v1", "Up", true},
		{"current revision but not yet rejoined (empty status)", "v1", "v1", "", true},
		{"current revision but still in detention", "v1", "v1", "ManualDetention", true},
		{"current revision and fully rejoined", "v1", "v1", "Up", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := memberNeedsRecycle(tc.podRev, tc.desiredRev, tc.status); got != tc.want {
				t.Errorf("memberNeedsRecycle(%q, %q, %q) = %v, want %v", tc.podRev, tc.desiredRev, tc.status, got, tc.want)
			}
		})
	}
}

func TestShouldDeferCaptainRecycle(t *testing.T) {
	cases := []struct {
		name           string
		isCaptain      bool
		alreadyStarted bool
		others         int
		want           bool
	}{
		{"not the captain, others pending", false, false, 2, false},
		{"captain, no others pending", true, false, 0, false},
		{"captain, others pending", true, false, 1, true},
		{"captain already in ManualDetention, others pending", true, true, 1, false},
		{"captain already in ManualDetention, no others pending", true, true, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldDeferCaptainRecycle(tc.isCaptain, tc.alreadyStarted, tc.others); got != tc.want {
				t.Errorf("shouldDeferCaptainRecycle(%v, %v, %d) = %v, want %v", tc.isCaptain, tc.alreadyStarted, tc.others, got, tc.want)
			}
		})
	}
}

func TestIsCaptainMember(t *testing.T) {
	cases := []struct {
		name         string
		captainLabel string
		memberName   string
		want         bool
	}{
		{
			"real captain/info FQDN format",
			"splunk-c3-search-head-2.splunk-c3-search-head-headless.splunk-operator.svc.cluster.local",
			"splunk-c3-search-head-2",
			true,
		},
		{
			"FQDN for a different member",
			"splunk-c3-search-head-2.splunk-c3-search-head-headless.splunk-operator.svc.cluster.local",
			"splunk-c3-search-head-0",
			false,
		},
		{"bare short label still matches (defensive)", "splunk-c3-search-head-2", "splunk-c3-search-head-2", true},
		{
			"ordinal 1 must not match ordinal 10 as a prefix",
			"splunk-c3-search-head-10.splunk-c3-search-head-headless.splunk-operator.svc.cluster.local",
			"splunk-c3-search-head-1",
			false,
		},
		{"empty member name never matches", "splunk-c3-search-head-2.splunk-c3-search-head-headless", "", false},
		{"empty captain label never matches a real member", "", "splunk-c3-search-head-2", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCaptainMember(tc.captainLabel, tc.memberName); got != tc.want {
				t.Errorf("isCaptainMember(%q, %q) = %v, want %v", tc.captainLabel, tc.memberName, got, tc.want)
			}
		})
	}
}
