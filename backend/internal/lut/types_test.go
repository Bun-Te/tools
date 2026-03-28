package lut

import (
	"math"
	"testing"

	"stakergs"
)

func TestPayoutBucketsRTPContributionSumsToRTP(t *testing.T) {
	a := NewAnalyzer()
	lut := &stakergs.LookupTable{
		Mode: "test",
		Cost: 2.0,
		Outcomes: []stakergs.Outcome{
			{SimID: 0, Weight: 100, Payout: 0},
			{SimID: 1, Weight: 300, Payout: 200},
			{SimID: 2, Weight: 600, Payout: 450}, // 4.50x fits [2,5) bucket; 5.00x would miss [2,5)
		},
	}
	tw := lut.TotalWeight()
	want := lut.RTP()
	buckets := a.BuildPayoutBuckets(lut, tw)
	var sum float64
	for _, b := range buckets {
		sum += b.RTPContribution
	}
	if math.Abs(sum-want) > 1e-9 {
		t.Fatalf("sum of bucket rtp_contribution=%g, lut.RTP()=%g", sum, want)
	}
}
