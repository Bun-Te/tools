// Package lut provides compliance checking for LUT tables.
package lut

import (
	"fmt"

	"stakergs"
)

// ComplianceCheckID identifies a specific compliance check.
type ComplianceCheckID string

const (
	CheckRTPRange          ComplianceCheckID = "rtp_range"
	CheckRTPVariation      ComplianceCheckID = "rtp_variation"
	CheckMaxWinAchievable  ComplianceCheckID = "max_win_achievable"
	CheckHitRateReasonable ComplianceCheckID = "hit_rate_reasonable"
	CheckPayoutGaps        ComplianceCheckID = "payout_gaps"
	CheckUniquePayouts  ComplianceCheckID = "unique_payouts"
	CheckZeroPayoutRate ComplianceCheckID = "zero_payout_rate"
	CheckVolatility        ComplianceCheckID = "volatility"
	// Stake Engine star-tier rubric
	CheckStarTier            ComplianceCheckID = "star_tier"
	CheckMaxPayoutMultiplier ComplianceCheckID = "max_payout_multiplier"
	CheckBaseStdDev          ComplianceCheckID = "base_std_dev"
	CheckCVaR                ComplianceCheckID = "cvar_001"
	CheckETL                 ComplianceCheckID = "etl_40x"
	CheckProbWin5K           ComplianceCheckID = "prob_win_5k"
	CheckProbWin10K          ComplianceCheckID = "prob_win_10k"
	CheckMinOutcomeCount     ComplianceCheckID = "min_outcome_count"
)

// MinOutcomeCount is the minimum number of outcomes (rows) a LUT must contain
// to be considered statistically credible for the Stake Engine rubric.
const MinOutcomeCount = 100_000

// StarTier is the Stake Engine bet-level eligibility tier assigned to a mode.
// Tiers map to operator-side risk controls (exposure and bet-wager caps).
type StarTier int

const (
	StarTierIneligible StarTier = 0
	StarTier1          StarTier = 1
	StarTier2          StarTier = 2
	StarTier3          StarTier = 3
)

// TierLimits are the Stake Engine rubric ceilings for each star tier.
// Values may be tweaked by operator ops pending audit of active games.
type TierLimits struct {
	Tier                StarTier `json:"tier"`
	MaxExposureUSD      float64  `json:"max_exposure_usd"`
	MaxSingleBetUSD     float64  `json:"max_single_bet_usd"`
	MaxPayoutMultiplier float64  `json:"max_payout_multiplier"`
	StdDevMin           float64  `json:"std_dev_min"`
	StdDevMax           float64  `json:"std_dev_max"`
}

// StarTierTable lists the rubric ceilings in ascending-tier order.
var StarTierTable = []TierLimits{
	{Tier: StarTier1, MaxExposureUSD: 100_000, MaxSingleBetUSD: 15_000, MaxPayoutMultiplier: 15_000, StdDevMin: 1, StdDevMax: 35},
	{Tier: StarTier2, MaxExposureUSD: 5_000_000, MaxSingleBetUSD: 50_000, MaxPayoutMultiplier: 25_000, StdDevMin: 1, StdDevMax: 40},
	{Tier: StarTier3, MaxExposureUSD: 10_000_000, MaxSingleBetUSD: 500_000, MaxPayoutMultiplier: 100_000, StdDevMin: 1, StdDevMax: 50},
}

// Global safety-test limits for the pass/fail rubric rows. These are tentative
// defaults; the operator calibrates them against the empirical distribution of
// all currently published game modes.
const (
	CVaRMaxMultiplier = 50_000.0 // worst 0.1% tail must average <= 50,000x
	ETL40xMaxPerBet   = 2.0      // RTP contribution (per bet) from payouts >= 40x
	ProbWin5KMax      = 0.005    // P(>=5,000x) must be <= 0.5%
	ProbWin10KMax     = 0.001    // P(>=10,000x) must be <= 0.1%
)

// ComplianceCheck represents a single compliance check result.
type ComplianceCheck struct {
	ID             ComplianceCheckID `json:"id"`
	NameKey        string            `json:"nameKey"`
	DescriptionKey string            `json:"descriptionKey"`
	Passed         bool              `json:"passed"`
	Value          string            `json:"value"`
	Expected       string            `json:"expected"`
	ReasonKey      string            `json:"reasonKey,omitempty"`
	Severity       string            `json:"severity"` // "error", "warning", "info"
	Details        interface{}       `json:"details,omitempty"`
}

// ComplianceResult contains all compliance check results for a mode.
type ComplianceResult struct {
	Mode         string             `json:"mode"`
	Passed       bool               `json:"passed"`
	PassedCount  int                `json:"passed_count"`
	FailedCount  int                `json:"failed_count"`
	WarningCount int                `json:"warning_count"`
	Checks       []ComplianceCheck  `json:"checks"`
	Summary      ComplianceSummary  `json:"summary"`
	// Stake Engine bet-level eligibility tier assigned by the rubric.
	StarTier     StarTier           `json:"star_tier"`
	TierLimits   *TierLimits        `json:"tier_limits,omitempty"`
}

// ComplianceSummary contains summary statistics used for compliance checks.
type ComplianceSummary struct {
	RTP               float64 `json:"rtp"`
	HitRate           float64 `json:"hit_rate"`
	MaxPayout         float64 `json:"max_payout"`
	MaxPayoutHitRate  float64 `json:"max_payout_hit_rate"`
	TotalOutcomes     int     `json:"total_outcomes"`
	UniquePayouts     int     `json:"unique_payouts"`
	ZeroPayoutRate float64 `json:"zero_payout_rate"`
	Volatility     float64 `json:"volatility"`
	// Tail-risk metrics (Stake Engine rubric)
	StdDev     float64 `json:"std_dev"`
	CVaR001    float64 `json:"cvar_001"`
	ETL40x     float64 `json:"etl_40x"`
	ETL10kx    float64 `json:"etl_10kx"`
	ProbWin5K  float64 `json:"prob_win_5k"`
	ProbWin10K float64 `json:"prob_win_10k"`
}

// AllModesComplianceResult contains compliance results for all modes.
type AllModesComplianceResult struct {
	AllPassed   bool                        `json:"all_passed"`
	ModeResults map[string]*ComplianceResult `json:"mode_results"`
	GlobalChecks []ComplianceCheck           `json:"global_checks"`
}

// ComplianceChecker performs compliance checks on LUT tables.
type ComplianceChecker struct {
	analyzer *Analyzer
}

// NewComplianceChecker creates a new compliance checker.
func NewComplianceChecker() *ComplianceChecker {
	return &ComplianceChecker{
		analyzer: NewAnalyzer(),
	}
}

// CheckMode performs all compliance checks on a single mode.
func (c *ComplianceChecker) CheckMode(lut *stakergs.LookupTable) *ComplianceResult {
	stats := c.analyzer.Analyze(lut)
	totalWeight := lut.TotalWeight()

	result := &ComplianceResult{
		Mode:   lut.Mode,
		Checks: make([]ComplianceCheck, 0),
		Summary: ComplianceSummary{
			RTP:            stats.RTP,
			HitRate:        stats.HitRate,
			MaxPayout:      stats.MaxPayout,
			TotalOutcomes:  stats.TotalOutcomes,
			ZeroPayoutRate: stats.ZeroPayoutRate,
			Volatility:     stats.Volatility,
			StdDev:         stats.StdDev,
			CVaR001:        stats.CVaR001,
			ETL40x:         stats.ETL40x,
			ETL10kx:        stats.ETL10kx,
			ProbWin5K:      stats.ProbWin5K,
			ProbWin10K:     stats.ProbWin10K,
		},
	}

	// Calculate additional summary values
	result.Summary.UniquePayouts = c.countUniquePayouts(lut)
	result.Summary.MaxPayoutHitRate = c.calculateMaxPayoutHitRate(lut, totalWeight)

	// Run all checks
	result.Checks = append(result.Checks, c.checkRTPRange(stats))
	result.Checks = append(result.Checks, c.checkMaxWinAchievable(lut, totalWeight, stats))
	result.Checks = append(result.Checks, c.checkHitRateReasonable(lut, stats))
	result.Checks = append(result.Checks, c.checkPayoutGaps(lut, stats))
	result.Checks = append(result.Checks, c.checkUniquePayouts(lut))
	result.Checks = append(result.Checks, c.checkZeroPayoutRate(stats))
	result.Checks = append(result.Checks, c.checkVolatility(stats))

	// Stake Engine star-tier rubric. Tier is derived from the max-payout
	// multiplier and base-game std-dev range; we then apply safety-test rows
	// against the rubric-wide CVaR/ETL/probability limits.
	tier, tierLimits := c.classifyStarTier(stats)
	result.StarTier = tier
	if tierLimits != nil {
		limitsCopy := *tierLimits
		result.TierLimits = &limitsCopy
	}
	result.Checks = append(result.Checks, c.checkStarTier(tier, tierLimits, stats))
	result.Checks = append(result.Checks, c.checkMaxPayoutMultiplier(stats, tierLimits))
	result.Checks = append(result.Checks, c.checkBaseStdDev(lut, stats, tierLimits))
	result.Checks = append(result.Checks, c.checkCVaR(stats))
	result.Checks = append(result.Checks, c.checkETL(stats))
	result.Checks = append(result.Checks, c.checkProbWin5K(stats))
	result.Checks = append(result.Checks, c.checkProbWin10K(stats))
	result.Checks = append(result.Checks, c.checkMinOutcomeCount(stats))

	// Calculate totals
	for _, check := range result.Checks {
		if check.Passed {
			result.PassedCount++
		} else if check.Severity == "warning" {
			result.WarningCount++
		} else {
			result.FailedCount++
		}
	}

	result.Passed = result.FailedCount == 0

	return result
}

// CheckAllModes performs compliance checks on all modes and cross-mode checks.
func (c *ComplianceChecker) CheckAllModes(tables map[string]*stakergs.LookupTable) *AllModesComplianceResult {
	result := &AllModesComplianceResult{
		ModeResults:  make(map[string]*ComplianceResult),
		GlobalChecks: make([]ComplianceCheck, 0),
		AllPassed:    true,
	}

	// Compute base RTP first (needed for per-mode checks)
	baseRTP, baseModeName := c.findBaseRTP(tables)

	// Check each mode individually
	for mode, lut := range tables {
		modeResult := c.CheckMode(lut)

		// Add per-mode RTP variation check if we have multiple modes
		if len(tables) > 1 {
			rtpCheck := c.checkModeRTPVariation(lut, baseRTP, baseModeName)
			modeResult.Checks = append(modeResult.Checks, rtpCheck)

			// Update counts
			if rtpCheck.Passed {
				modeResult.PassedCount++
			} else if rtpCheck.Severity == "warning" {
				modeResult.WarningCount++
			} else {
				modeResult.FailedCount++
				modeResult.Passed = false
			}
		}

		result.ModeResults[mode] = modeResult
		if !modeResult.Passed {
			result.AllPassed = false
		}
	}

	// Global cross-mode RTP variation check (summary)
	if len(tables) > 1 {
		rtpCheck := c.checkRTPVariationGlobal(tables, baseRTP, baseModeName)
		result.GlobalChecks = append(result.GlobalChecks, rtpCheck)
		if !rtpCheck.Passed && rtpCheck.Severity == "error" {
			result.AllPassed = false
		}
	}

	return result
}

// findBaseRTP finds the base RTP for cross-mode comparison.
// Prefers mode named "base", otherwise uses mode with highest RTP.
func (c *ComplianceChecker) findBaseRTP(tables map[string]*stakergs.LookupTable) (float64, string) {
	// First, try to find a mode named "base"
	if lut, ok := tables["base"]; ok {
		return lut.RTP(), "base"
	}

	// No "base" mode found, use mode with highest RTP
	var baseRTP float64
	var baseModeName string
	for mode, lut := range tables {
		rtp := lut.RTP()
		if rtp > baseRTP || baseModeName == "" {
			baseRTP = rtp
			baseModeName = mode
		}
	}

	return baseRTP, baseModeName
}

// checkModeRTPVariation checks if a single mode's RTP is within allowed range of base RTP.
func (c *ComplianceChecker) checkModeRTPVariation(lut *stakergs.LookupTable, baseRTP float64, baseModeName string) ComplianceCheck {
	maxVariation := 0.005 // 0.5%
	minAllowed := baseRTP - maxVariation
	maxAllowed := baseRTP + maxVariation

	modeRTP := lut.RTP()
	deviation := modeRTP - baseRTP
	if deviation < 0 {
		deviation = -deviation
	}

	isInRange := modeRTP >= minAllowed && modeRTP <= maxAllowed

	check := ComplianceCheck{
		ID:             CheckRTPVariation,
		NameKey:        "compliance.checks.rtpVariation.name",
		DescriptionKey: "compliance.checks.rtpVariation.description",
		Expected:       fmt.Sprintf("%.2f%% - %.2f%%", minAllowed*100, maxAllowed*100),
		Value:          fmt.Sprintf("%.2f%% (deviation: %.2f%%)", modeRTP*100, deviation*100),
		Severity:       "error",
		Details: map[string]interface{}{
			"base_mode":   baseModeName,
			"base_rtp":    baseRTP,
			"mode_rtp":    modeRTP,
			"deviation":   deviation,
			"min_allowed": minAllowed,
			"max_allowed": maxAllowed,
		},
	}

	if isInRange {
		check.Passed = true
	} else {
		check.Passed = false
		if modeRTP < minAllowed {
			check.ReasonKey = "compliance.checks.rtpVariation.reasonLow"
		} else {
			check.ReasonKey = "compliance.checks.rtpVariation.reasonHigh"
		}
	}

	return check
}

func (c *ComplianceChecker) checkRTPRange(stats *Statistics) ComplianceCheck {
	minRTP := 0.90
	maxRTP := 0.98

	check := ComplianceCheck{
		ID:             CheckRTPRange,
		NameKey:        "compliance.checks.rtpRange.name",
		DescriptionKey: "compliance.checks.rtpRange.description",
		Expected:       fmt.Sprintf("%.1f%% - %.1f%%", minRTP*100, maxRTP*100),
		Value:          fmt.Sprintf("%.2f%%", stats.RTP*100),
		Severity:       "error",
	}

	if stats.RTP >= minRTP && stats.RTP <= maxRTP {
		check.Passed = true
	} else {
		check.Passed = false
		if stats.RTP < minRTP {
			check.ReasonKey = "compliance.checks.rtpRange.reasonLow"
		} else {
			check.ReasonKey = "compliance.checks.rtpRange.reasonHigh"
		}
	}

	return check
}

// checkRTPVariationGlobal creates a global summary of RTP variation across all modes.
func (c *ComplianceChecker) checkRTPVariationGlobal(tables map[string]*stakergs.LookupTable, baseRTP float64, baseModeName string) ComplianceCheck {
	maxVariation := 0.005 // 0.5%
	minAllowed := baseRTP - maxVariation
	maxAllowed := baseRTP + maxVariation

	modeRTPs := make(map[string]float64)
	var outOfRangeModes []string
	var maxDeviation float64

	for mode, lut := range tables {
		rtp := lut.RTP()
		modeRTPs[mode] = rtp

		deviation := rtp - baseRTP
		if deviation < 0 {
			deviation = -deviation
		}
		if deviation > maxDeviation {
			maxDeviation = deviation
		}
		if rtp < minAllowed || rtp > maxAllowed {
			outOfRangeModes = append(outOfRangeModes, mode)
		}
	}

	totalModes := len(tables)
	passedModes := totalModes - len(outOfRangeModes)

	check := ComplianceCheck{
		ID:             CheckRTPVariation,
		NameKey:        "compliance.checks.rtpVariationGlobal.name",
		DescriptionKey: "compliance.checks.rtpVariationGlobal.description",
		Expected:       fmt.Sprintf("%.2f%% - %.2f%%", minAllowed*100, maxAllowed*100),
		Value:          fmt.Sprintf("%d/%d modes passed", passedModes, totalModes),
		Severity:       "error",
		Details: map[string]interface{}{
			"base_mode":     baseModeName,
			"base_rtp":      baseRTP,
			"min_allowed":   minAllowed,
			"max_allowed":   maxAllowed,
			"mode_rtps":     modeRTPs,
			"out_of_range":  outOfRangeModes,
			"max_deviation": maxDeviation,
			"passed_count":  passedModes,
			"failed_count":  len(outOfRangeModes),
		},
	}

	if len(outOfRangeModes) == 0 {
		check.Passed = true
	} else {
		check.Passed = false
		check.ReasonKey = "compliance.checks.rtpVariationGlobal.reason"
	}

	return check
}

func (c *ComplianceChecker) checkMaxWinAchievable(lut *stakergs.LookupTable, totalWeight uint64, stats *Statistics) ComplianceCheck {
	// Max win must not be rarer than 1 in 20,000,000 (same threshold for all modes; no adjustment by cost).
	maxOdds := 20_000_000.0

	var maxPayoutWeight uint64
	maxPayout := lut.MaxPayout()
	for _, o := range lut.Outcomes {
		if o.Payout == maxPayout {
			maxPayoutWeight += o.Weight
		}
	}

	actualOdds := float64(totalWeight) / float64(maxPayoutWeight)

	check := ComplianceCheck{
		ID:             CheckMaxWinAchievable,
		NameKey:        "compliance.checks.maxWinAchievable.name",
		DescriptionKey: "compliance.checks.maxWinAchievable.description",
		Expected:       fmt.Sprintf("Odds ≤ 1 in %s", formatLargeNumber(maxOdds)),
		Value:          fmt.Sprintf("1 in %s", formatLargeNumber(actualOdds)),
		Severity:       "error",
		Details: map[string]interface{}{
			"max_payout":         stats.MaxPayout,
			"max_payout_weight":  maxPayoutWeight,
			"total_weight":       totalWeight,
			"actual_odds":        actualOdds,
			"max_allowed_odds":   maxOdds,
		},
	}

	if actualOdds <= maxOdds {
		check.Passed = true
	} else {
		check.Passed = false
		check.ReasonKey = "compliance.checks.maxWinAchievable.reason"
	}

	return check
}

func (c *ComplianceChecker) checkHitRateReasonable(lut *stakergs.LookupTable, stats *Statistics) ComplianceCheck {
	// Hit rate check only applies to base modes (cost <= 2x)
	// Bonus modes with higher cost naturally have higher hit rates (often 100%)
	cost := lut.Cost
	if cost <= 0 {
		cost = 1.0
	}

	// Skip check for bonus modes (cost > 2)
	if cost > 2 {
		return ComplianceCheck{
			ID:             CheckHitRateReasonable,
			NameKey:        "compliance.checks.hitRate.name",
			DescriptionKey: "compliance.checks.hitRate.descriptionSkipped",
			Expected:       "N/A (bonus mode)",
			Value:          fmt.Sprintf("%.2f%% (1 in %.2f)", stats.HitRate*100, 1.0/stats.HitRate),
			Severity:       "info",
			Passed:         true,
		}
	}

	// For base modes: hit rate should be between 1 in 3 and 1 in 20
	minHitRate := 0.05 // 1 in 20
	maxHitRate := 0.33 // 1 in 3

	odds := 1.0 / stats.HitRate

	check := ComplianceCheck{
		ID:             CheckHitRateReasonable,
		NameKey:        "compliance.checks.hitRate.name",
		DescriptionKey: "compliance.checks.hitRate.description",
		Expected:       fmt.Sprintf("%.0f%% - %.0f%% (1 in %.0f - 1 in %.0f)", minHitRate*100, maxHitRate*100, 1/maxHitRate, 1/minHitRate),
		Value:          fmt.Sprintf("%.2f%% (1 in %.2f)", stats.HitRate*100, odds),
		Severity:       "warning",
	}

	if stats.HitRate >= minHitRate && stats.HitRate <= maxHitRate {
		check.Passed = true
	} else {
		check.Passed = false
		if stats.HitRate < minHitRate {
			check.ReasonKey = "compliance.checks.hitRate.reasonLow"
		} else {
			check.ReasonKey = "compliance.checks.hitRate.reasonHigh"
		}
	}

	return check
}

func (c *ComplianceChecker) checkPayoutGaps(lut *stakergs.LookupTable, stats *Statistics) ComplianceCheck {
	// Check for significant gaps in payout distribution
	maxPayout := stats.MaxPayout

	// Create buckets for payout ranges
	buckets := []struct {
		start, end float64
		hasPayouts bool
	}{
		{0, 1, false},
		{1, 2, false},
		{2, 5, false},
		{5, 10, false},
		{10, 25, false},
		{25, 50, false},
		{50, 100, false},
		{100, 250, false},
		{250, 500, false},
		{500, 1000, false},
		{1000, 2500, false},
		{2500, 5000, false},
		{5000, maxPayout + 1, false},
	}

	for _, o := range lut.Outcomes {
		payout := float64(o.Payout) / 100.0
		if payout <= 0 {
			continue
		}
		for i := range buckets {
			if payout >= buckets[i].start && payout < buckets[i].end {
				buckets[i].hasPayouts = true
				break
			}
		}
	}

	// Find gaps in populated ranges
	var gaps []string
	inRange := false
	for i, b := range buckets {
		if b.end > maxPayout {
			break
		}
		if b.hasPayouts {
			inRange = true
		} else if inRange && i < len(buckets)-1 && buckets[i+1].hasPayouts {
			gaps = append(gaps, fmt.Sprintf("%.0fx-%.0fx", b.start, b.end))
		}
	}

	check := ComplianceCheck{
		ID:             CheckPayoutGaps,
		NameKey:        "compliance.checks.payoutGaps.name",
		DescriptionKey: "compliance.checks.payoutGaps.description",
		Expected:       "No significant gaps in payout ranges",
		Severity:       "warning",
	}

	if len(gaps) == 0 {
		check.Passed = true
		check.Value = "No gaps detected"
	} else {
		check.Passed = false
		check.Value = fmt.Sprintf("%d gap(s) found", len(gaps))
		check.ReasonKey = "compliance.checks.payoutGaps.reason"
		check.Details = gaps
	}

	return check
}

func (c *ComplianceChecker) checkUniquePayouts(lut *stakergs.LookupTable) ComplianceCheck {
	// For slot-type games, should have reasonable number of unique payout values
	minUnique := 10

	uniquePayouts := c.countUniquePayouts(lut)

	check := ComplianceCheck{
		ID:             CheckUniquePayouts,
		NameKey:        "compliance.checks.uniquePayouts.name",
		DescriptionKey: "compliance.checks.uniquePayouts.description",
		Expected:       fmt.Sprintf("≥ %d unique values", minUnique),
		Value:          fmt.Sprintf("%d unique values", uniquePayouts),
		Severity:       "warning",
	}

	if uniquePayouts >= minUnique {
		check.Passed = true
	} else {
		check.Passed = false
		check.ReasonKey = "compliance.checks.uniquePayouts.reason"
	}

	return check
}

func (c *ComplianceChecker) checkZeroPayoutRate(stats *Statistics) ComplianceCheck {
	// Non-paying results shouldn't exceed 90%
	maxZeroRate := 0.90

	check := ComplianceCheck{
		ID:             CheckZeroPayoutRate,
		NameKey:        "compliance.checks.zeroPayoutRate.name",
		DescriptionKey: "compliance.checks.zeroPayoutRate.description",
		Expected:       fmt.Sprintf("Non-paying ≤ %.0f%%", maxZeroRate*100),
		Value:          fmt.Sprintf("%.2f%% non-paying", stats.ZeroPayoutRate*100),
		Severity:       "error",
	}

	if stats.ZeroPayoutRate <= maxZeroRate {
		check.Passed = true
	} else {
		check.Passed = false
		check.ReasonKey = "compliance.checks.zeroPayoutRate.reason"
	}

	return check
}

func (c *ComplianceChecker) checkVolatility(stats *Statistics) ComplianceCheck {
	// Volatility check - standard deviation should be within industry norms
	// This is more informational
	maxVolatility := 50.0 // Very high volatility threshold

	check := ComplianceCheck{
		ID:             CheckVolatility,
		NameKey:        "compliance.checks.volatility.name",
		DescriptionKey: "compliance.checks.volatility.description",
		Expected:       fmt.Sprintf("Volatility < %.0f", maxVolatility),
		Value:          fmt.Sprintf("%.2f", stats.Volatility),
		Severity:       "info",
	}

	if stats.Volatility < maxVolatility {
		check.Passed = true
	} else {
		check.Passed = false
		check.ReasonKey = "compliance.checks.volatility.reason"
	}

	return check
}

// Stake Engine star-tier checks
// -----------------------------------------------------------------------------

// classifyStarTier assigns a bet-level eligibility tier to the mode by picking
// the lowest tier whose max-payout-multiplier AND std-dev ceilings both
// accommodate the mode's actual stats. Modes that exceed even the top tier are
// classified as ineligible. Std-dev is skipped for bonus modes (cost > 2), as
// the rubric rows target base-game spin variance.
func (c *ComplianceChecker) classifyStarTier(stats *Statistics) (StarTier, *TierLimits) {
	isBaseMode := stats.Cost <= 2

	for i := range StarTierTable {
		limits := &StarTierTable[i]
		if stats.MaxPayout > limits.MaxPayoutMultiplier {
			continue
		}
		if isBaseMode {
			if stats.StdDev < limits.StdDevMin || stats.StdDev > limits.StdDevMax {
				continue
			}
		}
		return limits.Tier, limits
	}
	return StarTierIneligible, nil
}

func (c *ComplianceChecker) checkStarTier(tier StarTier, limits *TierLimits, stats *Statistics) ComplianceCheck {
	check := ComplianceCheck{
		ID:             CheckStarTier,
		NameKey:        "compliance.checks.starTier.name",
		DescriptionKey: "compliance.checks.starTier.description",
		Severity:       "info",
		Details: map[string]any{
			"tier":           int(tier),
			"max_payout":     stats.MaxPayout,
			"std_dev":        stats.StdDev,
			"cost":           stats.Cost,
			"rubric":         StarTierTable,
			"applied_limits": limits,
		},
	}

	if tier == StarTierIneligible {
		check.Passed = false
		check.Severity = "error"
		check.Expected = "1-Star, 2-Star or 3-Star"
		check.Value = "Ineligible"
		check.ReasonKey = "compliance.checks.starTier.reasonIneligible"
		return check
	}

	check.Passed = true
	check.Expected = "1-Star, 2-Star or 3-Star"
	check.Value = fmt.Sprintf("%d-Star", int(tier))
	return check
}

func (c *ComplianceChecker) checkMaxPayoutMultiplier(stats *Statistics, limits *TierLimits) ComplianceCheck {
	// Rubric ceiling: even the top tier caps max payout at 100,000x.
	maxAllowed := StarTierTable[len(StarTierTable)-1].MaxPayoutMultiplier

	check := ComplianceCheck{
		ID:             CheckMaxPayoutMultiplier,
		NameKey:        "compliance.checks.maxPayoutMultiplier.name",
		DescriptionKey: "compliance.checks.maxPayoutMultiplier.description",
		Expected:       fmt.Sprintf("≤ %sx (rubric top tier)", formatLargeNumber(maxAllowed)),
		Value:          fmt.Sprintf("%sx", formatLargeNumber(stats.MaxPayout)),
		Severity:       "error",
		Details: map[string]any{
			"max_payout":        stats.MaxPayout,
			"rubric_top":        maxAllowed,
			"assigned_tier":     tierValue(limits),
			"assigned_ceiling":  tierCeiling(limits),
		},
	}

	if stats.MaxPayout <= maxAllowed {
		check.Passed = true
	} else {
		check.Passed = false
		check.ReasonKey = "compliance.checks.maxPayoutMultiplier.reason"
	}
	return check
}

func (c *ComplianceChecker) checkBaseStdDev(lut *stakergs.LookupTable, stats *Statistics, limits *TierLimits) ComplianceCheck {
	// Std-dev range only applies to base modes; bonus modes naturally have
	// much higher spin variance.
	cost := lut.Cost
	if cost <= 0 {
		cost = 1.0
	}

	check := ComplianceCheck{
		ID:             CheckBaseStdDev,
		NameKey:        "compliance.checks.baseStdDev.name",
		DescriptionKey: "compliance.checks.baseStdDev.description",
		Severity:       "warning",
		Details: map[string]any{
			"std_dev": stats.StdDev,
			"cost":    cost,
		},
	}

	if cost > 2 {
		check.Passed = true
		check.Severity = "info"
		check.DescriptionKey = "compliance.checks.baseStdDev.descriptionSkipped"
		check.Expected = "N/A (bonus mode)"
		check.Value = fmt.Sprintf("%.2f", stats.StdDev)
		return check
	}

	rubricMax := StarTierTable[len(StarTierTable)-1].StdDevMax
	rubricMin := StarTierTable[0].StdDevMin
	check.Expected = fmt.Sprintf("%.0f - %.0f (rubric bounds)", rubricMin, rubricMax)
	check.Value = fmt.Sprintf("%.2f", stats.StdDev)

	if stats.StdDev >= rubricMin && stats.StdDev <= rubricMax {
		check.Passed = true
	} else {
		check.Passed = false
		check.Severity = "error"
		if stats.StdDev < rubricMin {
			check.ReasonKey = "compliance.checks.baseStdDev.reasonLow"
		} else {
			check.ReasonKey = "compliance.checks.baseStdDev.reasonHigh"
		}
	}

	if limits != nil {
		check.Details.(map[string]any)["tier_min"] = limits.StdDevMin
		check.Details.(map[string]any)["tier_max"] = limits.StdDevMax
	}
	return check
}

func (c *ComplianceChecker) checkCVaR(stats *Statistics) ComplianceCheck {
	check := ComplianceCheck{
		ID:             CheckCVaR,
		NameKey:        "compliance.checks.cvar.name",
		DescriptionKey: "compliance.checks.cvar.description",
		Expected:       fmt.Sprintf("≤ %sx", formatLargeNumber(CVaRMaxMultiplier)),
		Value:          fmt.Sprintf("%sx", formatLargeNumber(stats.CVaR001)),
		Severity:       "error",
		Details: map[string]any{
			"cvar_001":   stats.CVaR001,
			"max_allowed": CVaRMaxMultiplier,
			"alpha":      0.001,
		},
	}

	if stats.CVaR001 <= CVaRMaxMultiplier {
		check.Passed = true
	} else {
		check.Passed = false
		check.ReasonKey = "compliance.checks.cvar.reason"
	}
	return check
}

func (c *ComplianceChecker) checkETL(stats *Statistics) ComplianceCheck {
	check := ComplianceCheck{
		ID:             CheckETL,
		NameKey:        "compliance.checks.etl.name",
		DescriptionKey: "compliance.checks.etl.description",
		Expected:       fmt.Sprintf("≤ %.2f (RTP contrib from ≥40x or ≥10,000x)", ETL40xMaxPerBet),
		Value:          fmt.Sprintf("%.4f (40x+) / %.4f (10kx+)", stats.ETL40x, stats.ETL10kx),
		Severity:       "error",
		Details: map[string]any{
			"etl_40x":     stats.ETL40x,
			"etl_10kx":    stats.ETL10kx,
			"max_allowed": ETL40xMaxPerBet,
		},
	}

	if stats.ETL40x <= ETL40xMaxPerBet {
		check.Passed = true
	} else {
		check.Passed = false
		check.ReasonKey = "compliance.checks.etl.reason"
	}
	return check
}

func (c *ComplianceChecker) checkProbWin5K(stats *Statistics) ComplianceCheck {
	check := ComplianceCheck{
		ID:             CheckProbWin5K,
		NameKey:        "compliance.checks.probWin5K.name",
		DescriptionKey: "compliance.checks.probWin5K.description",
		Expected:       fmt.Sprintf("≤ %.3f%%", ProbWin5KMax*100),
		Value:          fmt.Sprintf("%.4f%%", stats.ProbWin5K*100),
		Severity:       "warning",
		Details: map[string]any{
			"prob_win_5k": stats.ProbWin5K,
			"max_allowed": ProbWin5KMax,
		},
	}

	if stats.ProbWin5K <= ProbWin5KMax {
		check.Passed = true
	} else {
		check.Passed = false
		check.ReasonKey = "compliance.checks.probWin5K.reason"
	}
	return check
}

func (c *ComplianceChecker) checkProbWin10K(stats *Statistics) ComplianceCheck {
	check := ComplianceCheck{
		ID:             CheckProbWin10K,
		NameKey:        "compliance.checks.probWin10K.name",
		DescriptionKey: "compliance.checks.probWin10K.description",
		Expected:       fmt.Sprintf("≤ %.3f%%", ProbWin10KMax*100),
		Value:          fmt.Sprintf("%.4f%%", stats.ProbWin10K*100),
		Severity:       "warning",
		Details: map[string]any{
			"prob_win_10k": stats.ProbWin10K,
			"max_allowed":  ProbWin10KMax,
		},
	}

	if stats.ProbWin10K <= ProbWin10KMax {
		check.Passed = true
	} else {
		check.Passed = false
		check.ReasonKey = "compliance.checks.probWin10K.reason"
	}
	return check
}

func (c *ComplianceChecker) checkMinOutcomeCount(stats *Statistics) ComplianceCheck {
	check := ComplianceCheck{
		ID:             CheckMinOutcomeCount,
		NameKey:        "compliance.checks.minOutcomeCount.name",
		DescriptionKey: "compliance.checks.minOutcomeCount.description",
		Expected:       fmt.Sprintf("> %s outcomes", formatLargeNumber(float64(MinOutcomeCount))),
		Value:          fmt.Sprintf("%s outcomes", formatLargeNumber(float64(stats.TotalOutcomes))),
		Severity:       "error",
		Details: map[string]any{
			"total_outcomes": stats.TotalOutcomes,
			"min_required":   MinOutcomeCount,
		},
	}

	if stats.TotalOutcomes > MinOutcomeCount {
		check.Passed = true
	} else {
		check.Passed = false
		check.ReasonKey = "compliance.checks.minOutcomeCount.reason"
	}
	return check
}

func tierValue(limits *TierLimits) int {
	if limits == nil {
		return 0
	}
	return int(limits.Tier)
}

func tierCeiling(limits *TierLimits) float64 {
	if limits == nil {
		return 0
	}
	return limits.MaxPayoutMultiplier
}

// Helper functions

func (c *ComplianceChecker) countUniquePayouts(lut *stakergs.LookupTable) int {
	payouts := make(map[uint]struct{})
	for _, o := range lut.Outcomes {
		payouts[o.Payout] = struct{}{}
	}
	return len(payouts)
}

func (c *ComplianceChecker) calculateMaxPayoutHitRate(lut *stakergs.LookupTable, totalWeight uint64) float64 {
	maxPayout := lut.MaxPayout()
	var maxWeight uint64
	for _, o := range lut.Outcomes {
		if o.Payout == maxPayout {
			maxWeight += o.Weight
		}
	}
	if totalWeight == 0 {
		return 0
	}
	return round4(float64(maxWeight) / float64(totalWeight))
}

func formatLargeNumber(n float64) string {
	if n >= 1_000_000_000 {
		return fmt.Sprintf("%.2fB", n/1_000_000_000)
	}
	if n >= 1_000_000 {
		return fmt.Sprintf("%.2fM", n/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.2fK", n/1_000)
	}
	return fmt.Sprintf("%.0f", n)
}

// PayoutGapDetail contains details about payout distribution gaps.
type PayoutGapDetail struct {
	Range       string  `json:"range"`
	HasPayouts  bool    `json:"has_payouts"`
	TotalWeight uint64  `json:"total_weight"`
	Probability float64 `json:"probability"`
}

// GetPayoutRangeAnalysis returns detailed analysis of payout ranges.
func (c *ComplianceChecker) GetPayoutRangeAnalysis(lut *stakergs.LookupTable) []PayoutGapDetail {
	totalWeight := lut.TotalWeight()
	maxPayout := float64(lut.MaxPayout()) / 100.0

	ranges := []struct {
		start, end float64
		label      string
	}{
		{0, 0.01, "0 (No win)"},
		{0.01, 1, "0.01x - 1x"},
		{1, 2, "1x - 2x"},
		{2, 5, "2x - 5x"},
		{5, 10, "5x - 10x"},
		{10, 25, "10x - 25x"},
		{25, 50, "25x - 50x"},
		{50, 100, "50x - 100x"},
		{100, 250, "100x - 250x"},
		{250, 500, "250x - 500x"},
		{500, 1000, "500x - 1000x"},
		{1000, 2500, "1000x - 2500x"},
		{2500, 5000, "2500x - 5000x"},
		{5000, 10000, "5000x - 10000x"},
		{10000, maxPayout + 1, fmt.Sprintf("10000x - %.0fx", maxPayout)},
	}

	result := make([]PayoutGapDetail, 0)

	for _, r := range ranges {
		if r.start > maxPayout {
			break
		}

		var weight uint64
		for _, o := range lut.Outcomes {
			payout := float64(o.Payout) / 100.0
			if payout >= r.start && payout < r.end {
				weight += o.Weight
			}
		}

		prob := 0.0
		if totalWeight > 0 {
			prob = float64(weight) / float64(totalWeight)
		}

		result = append(result, PayoutGapDetail{
			Range:       r.label,
			HasPayouts:  weight > 0,
			TotalWeight: weight,
			Probability: prob,
		})
	}

	// Sort by range start (already sorted by definition)
	return result
}
