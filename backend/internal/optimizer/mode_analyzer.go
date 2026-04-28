package optimizer

import (
	"fmt"
	"math"
	"sort"

	"lutexplorer/internal/lut"
	"stakergs"
)

// ModeType classifies the mode based on its RTP requirements and payout range
type ModeType string

const (
	ModeTypeStandard    ModeType = "standard"     // Standard slots ~96% RTP
	ModeTypeBonusNarrow ModeType = "bonus_narrow" // Bonus with narrow payout range
	ModeTypeBonusWide   ModeType = "bonus_wide"   // Bonus with wide payout range
	ModeTypeHighRTP     ModeType = "high_rtp"     // RTP > 200%
	ModeTypeExtreme     ModeType = "extreme"      // RTP > 1000%
)

// ModeAnalysis contains the analysis results for a mode
type ModeAnalysis struct {
	Mode string   `json:"mode"`
	Type ModeType `json:"mode_type"`

	// LUT statistics
	TotalOutcomes  int     `json:"total_outcomes"`
	MinPayout      float64 `json:"min_payout"`
	MaxPayout      float64 `json:"max_payout"`
	AvgPayout      float64 `json:"avg_payout"`
	PayoutVariance float64 `json:"payout_variance"`
	PayoutStdDev   float64 `json:"payout_std_dev"`

	// Payout distribution percentiles
	Percentiles map[string]float64 `json:"percentiles"`

	// RTP boundaries
	MinAchievableRTP float64 `json:"min_achievable_rtp"`
	MaxAchievableRTP float64 `json:"max_achievable_rtp"`

	// Mode info
	Cost        float64 `json:"cost"`
	IsBonusMode bool    `json:"is_bonus_mode"`

	// Recommendations
	RecommendedBuckets []BucketRecommendation `json:"recommended_buckets"`
	Feasible           bool                   `json:"feasible"`
	FeasibilityNote    string                 `json:"feasibility_note,omitempty"`
	SuggestedRTP       float64                `json:"suggested_rtp,omitempty"` // Suggested RTP if target is infeasible

	// WinPayouts is the full list of normalized non-zero payouts (cost-divided).
	// Used by the config generator to pick a feasible volatility exponent that
	// keeps the implied total win probability ≤ 1 (any p>0 counts as a hit,
	// including sub-1x outcomes — matches optimizer's totalWinWeight logic).
	WinPayouts []float64 `json:"-"`
}

// BucketRecommendation recommends bucket configuration based on LUT analysis
type BucketRecommendation struct {
	MinPayout    float64 `json:"min_payout"`
	MaxPayout    float64 `json:"max_payout"`
	OutcomeCount int     `json:"outcome_count"`   // Number of outcomes in this range
	RTPCapacity  float64 `json:"rtp_capacity"`    // Max RTP contribution of this bucket
	AvgPayout    float64 `json:"avg_payout"`      // Average payout in bucket
	SuggestedRTP float64 `json:"suggested_rtp"`   // Recommended % of target RTP
	Description  string  `json:"description"`     // Human-readable description
}

// ModeAnalyzer analyzes LUT data to generate adaptive configurations
type ModeAnalyzer struct {
	loader *lut.Loader
}

// NewModeAnalyzer creates a new mode analyzer
func NewModeAnalyzer(loader *lut.Loader) *ModeAnalyzer {
	return &ModeAnalyzer{loader: loader}
}

// AnalyzeMode performs comprehensive analysis of a mode's LUT
func (a *ModeAnalyzer) AnalyzeMode(mode string, targetRTP float64) (*ModeAnalysis, error) {
	table, err := a.loader.GetMode(mode)
	if err != nil {
		return nil, fmt.Errorf("failed to load mode %s: %w", mode, err)
	}

	return a.AnalyzeTable(table, mode, targetRTP)
}

// AnalyzeTable analyzes a lookup table directly
func (a *ModeAnalyzer) AnalyzeTable(table *stakergs.LookupTable, mode string, targetRTP float64) (*ModeAnalysis, error) {
	n := len(table.Outcomes)
	if n == 0 {
		return nil, fmt.Errorf("empty table")
	}

	cost := table.Cost
	if cost <= 0 {
		cost = 1.0
	}

	// Extract and normalize payouts
	payouts := make([]float64, 0, n)
	winPayouts := make([]float64, 0, n) // Only non-zero payouts
	var minPay, maxPay float64 = math.MaxFloat64, 0
	var sumPay, sumSq float64

	for _, outcome := range table.Outcomes {
		payout := float64(outcome.Payout) / 100.0 / cost
		payouts = append(payouts, payout)

		if payout > 0 {
			winPayouts = append(winPayouts, payout)
			if payout < minPay {
				minPay = payout
			}
			if payout > maxPay {
				maxPay = payout
			}
			sumPay += payout
			sumSq += payout * payout
		}
	}

	if len(winPayouts) == 0 {
		return nil, fmt.Errorf("no winning outcomes in table")
	}

	if minPay == math.MaxFloat64 {
		minPay = 0
	}

	avgPay := sumPay / float64(len(winPayouts))
	variance := (sumSq / float64(len(winPayouts))) - (avgPay * avgPay)
	if variance < 0 {
		variance = 0
	}
	stdDev := math.Sqrt(variance)

	// Calculate percentiles
	sort.Float64s(winPayouts)
	percentiles := calculatePercentiles(winPayouts)

	// Calculate RTP boundaries
	// Min RTP: All weight on min payout outcome
	// Max RTP: All weight on max payout outcome
	minRTP := minPay
	maxRTP := maxPay

	// Check feasibility
	feasible := targetRTP >= minRTP && targetRTP <= maxRTP
	var feasibilityNote string
	var suggestedRTP float64

	if !feasible {
		if targetRTP > maxRTP {
			feasibilityNote = fmt.Sprintf("Target RTP %.2f%% exceeds maximum achievable %.2f%% (max payout = %.2fx)",
				targetRTP*100, maxRTP*100, maxPay)
			suggestedRTP = maxRTP * 0.95 // Suggest 95% of max
		} else {
			feasibilityNote = fmt.Sprintf("Target RTP %.2f%% is below minimum achievable %.2f%% (min payout = %.2fx)",
				targetRTP*100, minRTP*100, minPay)
			suggestedRTP = minRTP * 1.05 // Suggest 105% of min
		}
	}

	// Classify mode type
	modeType := a.classifyMode(targetRTP, maxPay/minPay, cost)

	// Generate adaptive bucket recommendations
	buckets := a.generateAdaptiveBuckets(payouts, winPayouts, targetRTP, modeType)

	return &ModeAnalysis{
		Mode:             mode,
		Type:             modeType,
		TotalOutcomes:    n,
		MinPayout:        minPay,
		MaxPayout:        maxPay,
		AvgPayout:        avgPay,
		PayoutVariance:   variance,
		PayoutStdDev:     stdDev,
		Percentiles:      percentiles,
		MinAchievableRTP: minRTP,
		MaxAchievableRTP: maxRTP,
		Cost:             cost,
		IsBonusMode:      cost > 1.5,
		RecommendedBuckets: buckets,
		Feasible:         feasible,
		FeasibilityNote:  feasibilityNote,
		SuggestedRTP:     suggestedRTP,
		WinPayouts:       winPayouts,
	}, nil
}

// classifyMode determines the mode type based on characteristics
func (a *ModeAnalyzer) classifyMode(targetRTP, payoutRange, cost float64) ModeType {
	// Extreme RTP modes (1000%+)
	if targetRTP > 10.0 {
		return ModeTypeExtreme
	}

	// High RTP modes (200-1000%)
	if targetRTP > 2.0 {
		return ModeTypeHighRTP
	}

	// Bonus modes (high cost)
	if cost > 1.5 {
		if payoutRange < 10 {
			return ModeTypeBonusNarrow
		}
		return ModeTypeBonusWide
	}

	return ModeTypeStandard
}

// generateAdaptiveBuckets creates bucket recommendations based on actual payout distribution
func (a *ModeAnalyzer) generateAdaptiveBuckets(allPayouts, winPayouts []float64, targetRTP float64, modeType ModeType) []BucketRecommendation {
	if len(winPayouts) == 0 {
		return nil
	}

	// Sort win payouts for percentile-based bucketing
	sorted := make([]float64, len(winPayouts))
	copy(sorted, winPayouts)
	sort.Float64s(sorted)

	n := len(sorted)

	// Different bucketing strategies based on mode type
	var percentiles []float64
	var descriptions []string

	switch modeType {
	case ModeTypeExtreme, ModeTypeHighRTP:
		// For extreme modes, use fewer buckets with emphasis on high payouts
		percentiles = []float64{0, 0.5, 0.8, 0.95, 1.0}
		descriptions = []string{"low_payouts", "medium_payouts", "high_payouts", "jackpot"}

	case ModeTypeBonusNarrow:
		// For narrow bonus modes, use tight buckets around the average
		percentiles = []float64{0, 0.33, 0.67, 1.0}
		descriptions = []string{"below_avg", "around_avg", "above_avg"}

	case ModeTypeBonusWide:
		// For wide bonus modes, use more buckets
		percentiles = []float64{0, 0.25, 0.5, 0.75, 0.9, 1.0}
		descriptions = []string{"low", "low_medium", "medium", "high", "jackpot"}

	default: // ModeTypeStandard
		// Standard slots use traditional bucket structure
		percentiles = []float64{0, 0.25, 0.5, 0.75, 0.9, 0.95, 0.99, 1.0}
		descriptions = []string{"small", "low_medium", "medium", "medium_high", "large", "huge", "jackpot"}
	}

	buckets := make([]BucketRecommendation, 0)
	var totalCapacity float64

	for i := 0; i < len(percentiles)-1; i++ {
		startIdx := int(float64(n) * percentiles[i])
		endIdx := int(float64(n) * percentiles[i+1])
		if endIdx > n {
			endIdx = n
		}
		if startIdx >= endIdx {
			continue
		}

		minPay := sorted[startIdx]
		maxPay := sorted[endIdx-1]

		// Ensure no gaps between buckets
		if len(buckets) > 0 && minPay > buckets[len(buckets)-1].MaxPayout {
			// Extend previous bucket or adjust current
			minPay = buckets[len(buckets)-1].MaxPayout
		}

		// Extend max slightly to ensure coverage
		if i == len(percentiles)-2 {
			maxPay = sorted[n-1] * 1.01 // Ensure last bucket covers max
		}

		// Calculate RTP capacity
		var sumPay float64
		outcomeCount := 0
		for j := startIdx; j < endIdx; j++ {
			sumPay += sorted[j]
			outcomeCount++
		}
		avgPay := sumPay / float64(outcomeCount)
		rtpCapacity := avgPay // Max contribution when this bucket gets 100% probability

		desc := "bucket"
		if i < len(descriptions) {
			desc = descriptions[i]
		}

		buckets = append(buckets, BucketRecommendation{
			MinPayout:    minPay,
			MaxPayout:    maxPay,
			OutcomeCount: outcomeCount,
			RTPCapacity:  rtpCapacity,
			AvgPayout:    avgPay,
			Description:  desc,
		})

		totalCapacity += rtpCapacity * float64(outcomeCount)
	}

	// Distribute target RTP proportionally
	if totalCapacity > 0 {
		for i := range buckets {
			share := (buckets[i].RTPCapacity * float64(buckets[i].OutcomeCount)) / totalCapacity
			buckets[i].SuggestedRTP = share * 100 // As percentage
		}
	}

	return buckets
}

// calculatePercentiles calculates common percentiles for sorted payouts
func calculatePercentiles(sorted []float64) map[string]float64 {
	n := len(sorted)
	if n == 0 {
		return nil
	}

	getPercentile := func(p float64) float64 {
		idx := int(float64(n-1) * p)
		if idx < 0 {
			idx = 0
		}
		if idx >= n {
			idx = n - 1
		}
		return sorted[idx]
	}

	return map[string]float64{
		"p10": getPercentile(0.10),
		"p25": getPercentile(0.25),
		"p50": getPercentile(0.50),
		"p75": getPercentile(0.75),
		"p90": getPercentile(0.90),
		"p95": getPercentile(0.95),
		"p99": getPercentile(0.99),
	}
}

// GetVolatilityModifiers returns profile modifiers for bucket distribution
func (a *ModeAnalyzer) GetVolatilityModifiers(profile PlayerProfile, numBuckets int) []float64 {
	modifiers := make([]float64, numBuckets)

	switch profile {
	case ProfileLowVol:
		// More weight on lower payout buckets, decreasing toward high payouts
		for i := range modifiers {
			// Exponential decay: first bucket gets most, last gets least
			modifiers[i] = math.Pow(0.7, float64(i))
		}

	case ProfileHighVol:
		// More weight on higher payout buckets, increasing toward high payouts
		for i := range modifiers {
			// Exponential growth: first bucket gets least, last gets most
			modifiers[i] = math.Pow(1.3, float64(i))
		}

	default: // ProfileMediumVol
		// Balanced distribution
		for i := range modifiers {
			modifiers[i] = 1.0
		}
	}

	// Normalize so sum equals numBuckets
	var sum float64
	for _, m := range modifiers {
		sum += m
	}
	if sum > 0 {
		for i := range modifiers {
			modifiers[i] = modifiers[i] / sum * float64(numBuckets)
		}
	}

	return modifiers
}

// CreateBucketsFromAnalysis generates BucketConfig from analysis and profile
func (a *ModeAnalyzer) CreateBucketsFromAnalysis(analysis *ModeAnalysis, targetRTP float64, profile PlayerProfile) []BucketConfig {
	recs := analysis.RecommendedBuckets
	if len(recs) == 0 {
		return nil
	}

	// Get modifiers for this profile
	modifiers := a.GetVolatilityModifiers(profile, len(recs))

	buckets := make([]BucketConfig, len(recs))

	// Apply modifiers to recommended RTP shares
	var totalModified float64
	for i, rec := range recs {
		totalModified += rec.SuggestedRTP * modifiers[i]
	}

	for i, rec := range recs {
		adjustedShare := (rec.SuggestedRTP * modifiers[i]) / totalModified * 100

		buckets[i] = BucketConfig{
			Name:      rec.Description,
			MinPayout: rec.MinPayout,
			MaxPayout: rec.MaxPayout,
		}

		// Choose constraint type based on adjusted share and mode type
		switch analysis.Type {
		case ModeTypeExtreme, ModeTypeHighRTP:
			// For extreme modes, use AUTO to let algorithm distribute
			buckets[i].Type = ConstraintAuto
			buckets[i].AutoExponent = a.getExponentForProfile(profile)

		case ModeTypeBonusNarrow, ModeTypeBonusWide:
			// For bonus modes, use RTP percent
			buckets[i].Type = ConstraintRTPPercent
			buckets[i].RTPPercent = adjustedShare

		default:
			// For standard modes, use mix of frequency and RTP%
			// Lower buckets use frequency, higher use RTP%
			if i < len(recs)/2 {
				// Calculate frequency from RTP contribution
				avgPayout := rec.AvgPayout
				if avgPayout > 0 {
					rtpContrib := (adjustedShare / 100) * targetRTP
					prob := rtpContrib / avgPayout
					freq := 1.0 / prob
					if freq < 200 {
						buckets[i].Type = ConstraintFrequency
						buckets[i].Frequency = math.Round(freq*10) / 10
						if buckets[i].Frequency < 1 {
							buckets[i].Frequency = 1
						}
					} else {
						buckets[i].Type = ConstraintRTPPercent
						buckets[i].RTPPercent = adjustedShare
					}
				} else {
					buckets[i].Type = ConstraintRTPPercent
					buckets[i].RTPPercent = adjustedShare
				}
			} else {
				buckets[i].Type = ConstraintRTPPercent
				buckets[i].RTPPercent = adjustedShare
			}
		}
	}

	return buckets
}

// getExponentForProfile returns AUTO exponent based on profile
func (a *ModeAnalyzer) getExponentForProfile(profile PlayerProfile) float64 {
	switch profile {
	case ProfileLowVol:
		return 1.5 // Steeper = lower high payouts
	case ProfileHighVol:
		return 0.5 // Flatter = more high payouts
	default:
		return 1.0
	}
}

// FeasibilityInfo provides details about RTP feasibility
type FeasibilityInfo struct {
	Original    float64 `json:"original"`
	Effective   float64 `json:"effective"`
	WasAdjusted bool    `json:"was_adjusted"`
	MinPossible float64 `json:"min_possible"`
	MaxPossible float64 `json:"max_possible"`
}

// VoidSuggestion represents a bucket that can be voided to reach target RTP
type VoidSuggestion struct {
	Index           int     `json:"index"`            // Bucket index
	Name            string  `json:"name"`             // Bucket name
	MinPayout       float64 `json:"min_payout"`       // Min payout in bucket
	MaxPayout       float64 `json:"max_payout"`       // Max payout in bucket
	OutcomeCount    int     `json:"outcome_count"`    // Number of outcomes in bucket
	RtpContribution float64 `json:"rtp_contribution"` // RTP contribution of this bucket
	Priority        int     `json:"priority"`         // Priority for voiding (1=highest)
}

// GenerateConfigsAnalysis provides analysis for generate-configs endpoint
type GenerateConfigsAnalysis struct {
	ModeType             ModeType         `json:"mode_type"`
	Feasible             bool             `json:"feasible"`
	FeasibilityNote      string           `json:"feasibility_note,omitempty"`
	MinAchievableRTP     float64          `json:"min_achievable_rtp"`
	MaxAchievableRTP     float64          `json:"max_achievable_rtp"`
	SuggestedRTP         float64          `json:"suggested_rtp,omitempty"`
	IsBonusMode          bool             `json:"is_bonus_mode"`
	SuggestedVoidBuckets []VoidSuggestion `json:"suggested_void_buckets,omitempty"`
}

// CalculateVoidSuggestions calculates which buckets can be voided to reach target RTP
// Returns suggestions sorted by priority (highest payout buckets first - safer to void)
func CalculateVoidSuggestions(buckets []BucketConfig, payouts []float64, targetRTP, minAchievableRTP float64) []VoidSuggestion {
	if minAchievableRTP <= targetRTP {
		return nil // No voiding needed
	}

	rtpToRemove := minAchievableRTP - targetRTP

	// Calculate RTP contribution for each bucket
	type bucketInfo struct {
		config      BucketConfig
		index       int
		rtpContrib  float64
		avgPayout   float64
		count       int
	}

	var bucketInfos []bucketInfo
	for i, bucket := range buckets {
		// Find outcomes in this bucket and calculate RTP contribution
		var count int
		var sumPayout float64
		for _, payout := range payouts {
			if payout >= bucket.MinPayout && payout < bucket.MaxPayout {
				count++
				sumPayout += payout
			}
		}
		if count == 0 {
			continue
		}

		avgPayout := sumPayout / float64(count)
		// Estimate RTP contribution assuming uniform distribution
		// This is a simplified calculation; actual depends on weights
		rtpContrib := avgPayout / float64(len(payouts))

		bucketInfos = append(bucketInfos, bucketInfo{
			config:     bucket,
			index:      i,
			rtpContrib: rtpContrib,
			avgPayout:  avgPayout,
			count:      count,
		})
	}

	// Sort by average payout descending (high payouts first - safer to void)
	sort.Slice(bucketInfos, func(i, j int) bool {
		return bucketInfos[i].avgPayout > bucketInfos[j].avgPayout
	})

	var suggestions []VoidSuggestion
	removedRTP := 0.0
	priority := 1

	for _, info := range bucketInfos {
		if removedRTP >= rtpToRemove {
			break
		}
		suggestions = append(suggestions, VoidSuggestion{
			Index:           info.index,
			Name:            info.config.Name,
			MinPayout:       info.config.MinPayout,
			MaxPayout:       info.config.MaxPayout,
			OutcomeCount:    info.count,
			RtpContribution: info.rtpContrib * 100, // As percentage
			Priority:        priority,
		})
		removedRTP += info.rtpContrib
		priority++
	}

	return suggestions
}
