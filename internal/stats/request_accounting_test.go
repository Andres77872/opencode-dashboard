package stats

import "testing"

func TestNewRequestAccountingCoverage(t *testing.T) {
	tests := []struct {
		name                             string
		recorded, recovered, unavailable int64
		observed, inferred, unknown      int64
		want                             TraceCoverage
		wantNil                          bool
	}{
		{name: "unsupported source stays omitted", unknown: 3, wantNil: true},
		{name: "all request events observed", recorded: 2, unavailable: 1, observed: 3, want: TraceCoverageComplete},
		{name: "legacy successes inferred", recorded: 2, inferred: 2, want: TraceCoverageSuccessfulOnly},
		{name: "modern and legacy mixed", recorded: 2, observed: 1, inferred: 1, want: TraceCoverageMixed},
		{name: "partly unclassified trace", recovered: 2, observed: 1, unknown: 1, want: TraceCoverageMixed},
		{name: "usage known but trace unknown", recovered: 1, unknown: 1, want: TraceCoverageUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewRequestAccounting(tt.recorded, tt.recovered, tt.unavailable, tt.observed, tt.inferred, tt.unknown)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("NewRequestAccounting() = %#v, want nil", got)
				}
				return
			}
			if got == nil || got.TraceCoverage != tt.want {
				t.Fatalf("NewRequestAccounting() = %#v, want coverage %q", got, tt.want)
			}
		})
	}
}

func TestMergeRequestAccountingPreservesCountsAndCoverage(t *testing.T) {
	got := MergeRequestAccounting(
		&RequestAccounting{UsageRecorded: 2, TraceCoverage: TraceCoverageSuccessfulOnly},
		nil,
		&RequestAccounting{UsageRecovered: 1, UsageUnavailable: 1, TraceCoverage: TraceCoverageComplete},
	)
	if got == nil || got.UsageRecorded != 2 || got.UsageRecovered != 1 || got.UsageUnavailable != 1 || got.TraceCoverage != TraceCoverageMixed {
		t.Fatalf("MergeRequestAccounting() = %#v, want 2/1/1 mixed", got)
	}
}
