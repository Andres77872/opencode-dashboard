package cache

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

func TestSourceFingerprintChangesWithPricingSnapshot(t *testing.T) {
	info := source.SourceInfo{
		ID:   source.SourceID("synthetic"),
		Kind: "fixture",
		CostPolicy: source.CostPolicy{
			PricingSnapshotID: "pricing-v1",
		},
	}
	first, err := sourceFingerprint(context.Background(), info)
	if err != nil {
		t.Fatalf("sourceFingerprint(v1): %v", err)
	}
	info.CostPolicy.PricingSnapshotID = "pricing-v2"
	second, err := sourceFingerprint(context.Background(), info)
	if err != nil {
		t.Fatalf("sourceFingerprint(v2): %v", err)
	}
	if first == second {
		t.Fatal("source fingerprint did not change when the pricing snapshot changed")
	}
	firstPricing, firstTagged := fingerprintPricingIdentity(first)
	secondPricing, secondTagged := fingerprintPricingIdentity(second)
	if !firstTagged || !secondTagged || firstPricing == secondPricing {
		t.Fatalf("pricing tags = (%q, %v) / (%q, %v), want two distinct tagged identities", firstPricing, firstTagged, secondPricing, secondTagged)
	}
	if fallbackFingerprint(info) == fallbackFingerprint(source.SourceInfo{ID: info.ID, Kind: info.Kind, CostPolicy: source.CostPolicy{PricingSnapshotID: "pricing-v1"}}) {
		t.Fatal("fallback fingerprint did not change when the pricing snapshot changed")
	}
}

func TestPricingSnapshotChangeRebuildsHistoricalCosts(t *testing.T) {
	for _, readTriggered := range []bool{false, true} {
		name := "explicit"
		if readTriggered {
			name = "read-triggered"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			base := time.Date(2026, 7, 17, 18, 0, 0, 0, time.UTC)
			cutoff := base.Add(-6 * time.Hour)
			src := &syncFakeSource{
				pricingSnapshotID: "pricing-v1",
				messages:          []stats.MessageEntry{testMessage("historical", base.Add(-12*time.Hour), 0.10)},
			}
			store := newTestStore(t)

			if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Mode: SyncModeIncremental, Cutoff: cutoff}); err != nil {
				t.Fatalf("initial sync: %v", err)
			}
			assertCachedMessageCost(t, store, syncFakeSourceID, "historical", 0.10)

			// The raw transcript is unchanged and the message is older than the
			// incremental cutoff. Only a full rebuild can replace its stale cost.
			src.pricingSnapshotID = "pricing-v2"
			src.messages[0].Cost = 0.42

			need, err := store.NeedsSync(ctx, src)
			if err != nil {
				t.Fatalf("NeedsSync: %v", err)
			}
			if !need.Needed || !strings.Contains(need.Reason, "full historical repricing") {
				t.Fatalf("NeedsSync = %#v, want pricing-triggered historical rebuild", need)
			}

			report, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{
				Mode:          SyncModeIncremental,
				Cutoff:        cutoff,
				ReadTriggered: readTriggered,
			})
			if err != nil {
				t.Fatalf("repricing sync: %v", err)
			}
			if report.Mode != SyncModeRebuild || !report.Since.IsZero() {
				t.Fatalf("repricing report = %#v, want rebuild from the beginning", report)
			}
			if report.Messages != 1 {
				t.Fatalf("repricing collected %d messages, want the historical row", report.Messages)
			}
			assertCachedMessageCost(t, store, syncFakeSourceID, "historical", 0.42)
		})
	}
}

func TestPricingRebuildFailureKeepsOldIdentityForRetry(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 7, 17, 18, 0, 0, 0, time.UTC)
	cutoff := base.Add(-6 * time.Hour)
	src := &syncFakeSource{
		pricingSnapshotID: "pricing-v1",
		messages:          []stats.MessageEntry{testMessage("historical", base.Add(-12*time.Hour), 0.10)},
	}
	store := newTestStore(t)
	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: cutoff}); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	before, _, err := store.SourceStatus(ctx, syncFakeSourceID)
	if err != nil {
		t.Fatalf("initial SourceStatus: %v", err)
	}

	src.pricingSnapshotID = "pricing-v2"
	src.messages[0].Cost = 0.42
	src.messagesErr = errors.New("temporary collect failure")
	report, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Mode: SyncModeIncremental, Cutoff: cutoff})
	if err == nil {
		t.Fatal("repricing sync unexpectedly succeeded")
	}
	if report.Mode != SyncModeRebuild {
		t.Fatalf("failed repricing mode = %q, want rebuild", report.Mode)
	}
	afterFailure, _, statusErr := store.SourceStatus(ctx, syncFakeSourceID)
	if statusErr != nil {
		t.Fatalf("failed SourceStatus: %v", statusErr)
	}
	if afterFailure.Fingerprint != before.Fingerprint {
		t.Fatalf("failed repricing advanced fingerprint from %q to %q", before.Fingerprint, afterFailure.Fingerprint)
	}
	assertCachedMessageCost(t, store, syncFakeSourceID, "historical", 0.10)

	src.messagesErr = nil
	retry, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Mode: SyncModeIncremental, Cutoff: cutoff})
	if err != nil {
		t.Fatalf("repricing retry: %v", err)
	}
	if retry.Mode != SyncModeRebuild || !retry.Since.IsZero() {
		t.Fatalf("repricing retry = %#v, want rebuild from the beginning", retry)
	}
	assertCachedMessageCost(t, store, syncFakeSourceID, "historical", 0.42)
}

func TestTranscriptChangeWithStablePricingRemainsIncremental(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 7, 17, 18, 0, 0, 0, time.UTC)
	firstCutoff := base.Add(-6 * time.Hour)
	src := &syncFakeSource{
		pricingSnapshotID: "pricing-v1",
		messages:          []stats.MessageEntry{testMessage("old", base.Add(-12*time.Hour), 0.10)},
	}
	store := newTestStore(t)
	if _, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{Cutoff: firstCutoff}); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	src.messages = append(src.messages, testMessage("new", base.Add(-5*time.Hour), 0.20))
	src.scannedFiles++
	report, err := store.SyncSourceWithOptions(ctx, src, SyncOptions{
		Mode:   SyncModeIncremental,
		Cutoff: base.Add(-4 * time.Hour),
	})
	if err != nil {
		t.Fatalf("incremental sync: %v", err)
	}
	if report.Mode != SyncModeIncremental || !report.Since.Equal(firstCutoff) {
		t.Fatalf("ordinary transcript report = %#v, want incremental since %s", report, firstCutoff)
	}
	if report.Messages != 1 {
		t.Fatalf("incremental sync collected %d messages, want only the new window", report.Messages)
	}
	assertCachedMessageCost(t, store, syncFakeSourceID, "old", 0.10)
	assertCachedMessageCost(t, store, syncFakeSourceID, "new", 0.20)
}

func assertCachedMessageCost(t *testing.T, store *Store, sourceID, messageID string, want float64) {
	t.Helper()
	var got float64
	if err := store.db.QueryRowContext(context.Background(), `
		SELECT cost FROM message_index WHERE source_id = ? AND message_id = ?
	`, sourceID, messageID).Scan(&got); err != nil {
		t.Fatalf("cached cost for %s: %v", messageID, err)
	}
	if got != want {
		t.Fatalf("cached cost for %s = %f, want %f", messageID, got, want)
	}
}
