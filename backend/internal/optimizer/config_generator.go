package optimizer

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"

	"lutexplorer/internal/common"
)

// PlayerProfile defines a volatility/playstyle preset
type PlayerProfile string

const (
	ProfileLowVol    PlayerProfile = "low_volatility"    // Frequent small wins
	ProfileMediumVol PlayerProfile = "medium_volatility" // Balanced
	ProfileHighVol   PlayerProfile = "high_volatility"   // Rare big wins
)

// ProfileDescription provides human-readable info about each profile
var ProfileDescriptions = map[PlayerProfile]string{
	ProfileLowVol:    "Frequent small wins, minimal risk. Ideal for casual players.",
	ProfileMediumVol: "Balanced distribution between small and large wins.",
	ProfileHighVol:   "Rare but large wins. For thrill seekers.",
}

// GeneratedConfig represents a generated bucket configuration
type GeneratedConfig struct {
	Profile     PlayerProfile   `json:"profile"`
	ProfileName string          `json:"profile_name"`
	Description string          `json:"description"`
	TargetRTP   float64         `json:"target_rtp"`
	MaxWin      float64         `json:"max_win"`
	Buckets     []BucketConfig  `json:"buckets"`
	B64Config   string          `json:"b64_config"`
	Stats       ConfigStats     `json:"stats"`
	Feasibility *FeasibilityInfo `json:"feasibility,omitempty"`
}

// ConfigStats provides statistical info about the generated config
type ConfigStats struct {
	TotalBuckets    int                `json:"total_buckets"`
	RTPDistribution map[string]float64 `json:"rtp_distribution"` // bucket range -> % of RTP
	AvgHitRate      float64            `json:"avg_hit_rate"`     // Average 1 in N for any win
	MaxWinFreq      float64            `json:"max_win_freq"`     // Frequency of max win bucket
}

// ConfigGeneratorRequest contains input for config generation
type ConfigGeneratorRequest struct {
	TargetRTP float64       `json:"target_rtp"` // e.g., 0.96
	MaxWin    float64       `json:"max_win"`    // e.g., 5000
	Profile   PlayerProfile `json:"profile"`    // Optional: specific profile
}

// ConfigGeneratorResponse contains generated configs
type ConfigGeneratorResponse struct {
	Configs []GeneratedConfig `json:"configs"`
}

// ShortConfig is the compact b64 format for frontend
type ShortConfig struct {
	R float64   `json:"r"` // RTP * 100 (e.g., 97.8 for 97.8%) — float to preserve precision
	B [][]any   `json:"b"` // [[min, max, type(0/1/2), value], ...]
}

// ConfigGenerator generates optimal bucket configurations using Power Law distribution
type ConfigGenerator struct {
	analyzer *ModeAnalyzer
}

// NewConfigGenerator creates a new config generator
func NewConfigGenerator() *ConfigGenerator {
	return &ConfigGenerator{}
}

// NewConfigGeneratorWithAnalyzer creates a config generator with mode analyzer
func NewConfigGeneratorWithAnalyzer(analyzer *ModeAnalyzer) *ConfigGenerator {
	return &ConfigGenerator{analyzer: analyzer}
}

// SetAnalyzer sets the mode analyzer for adaptive generation
func (g *ConfigGenerator) SetAnalyzer(analyzer *ModeAnalyzer) {
	g.analyzer = analyzer
}

// DefaultMaxWinFreq is used when callers don't supply a maxwin frequency.
// 1 in 10M is conservative — typical industry-grade max win hit rate.
const DefaultMaxWinFreq = 10_000_000.0

// profileAlpha returns the volatility shape exponent for a profile.
// Higher α  → steeper decay → mass on small payouts → low std dev, thin tail.
// Lower α   → fatter tail   → mass on big payouts   → high std dev, fat tail.
// Hit rate naturally follows from α; profiles differ in shape, not in target RTP.
func profileAlpha(profile PlayerProfile) float64 {
	// Mild values are mandatory at high target RTP. Steep α blows total prob > 1
	// (since per-outcome prob ∝ p^(-α) is dominated by sub-1x payouts), the
	// optimizer collapses loss to MinWeight and RTP severely undershoots.
	// These tuned values keep total prob ≤ 1 across typical slot LUTs while
	// preserving distinguishable shape across LOW/MED/HIGH.
	// Initial guess; the generator's bisection then nudges α into the feasible
	// hit-rate band [1/18, 0.95]. For typical slot LUTs (no extreme sub-1x mass)
	// these stay close to the requested values; for sub-1x-heavy LUTs they
	// converge toward each other (volatility shape gets bounded by physics).
	switch profile {
	case ProfileLowVol:
		return 1.5
	case ProfileMediumVol:
		return 0.7
	case ProfileHighVol:
		return -0.3
	default:
		return 0.7
	}
}

// GenerateAllProfiles generates configs for all profiles
func (g *ConfigGenerator) GenerateAllProfiles(targetRTP, maxWin, maxWinFreq float64) *ConfigGeneratorResponse {
	profiles := []PlayerProfile{
		ProfileLowVol,
		ProfileMediumVol,
		ProfileHighVol,
	}

	response := &ConfigGeneratorResponse{
		Configs: make([]GeneratedConfig, 0, len(profiles)),
	}

	for _, profile := range profiles {
		config := g.GenerateConfig(targetRTP, maxWin, maxWinFreq, profile)
		response.Configs = append(response.Configs, *config)
	}

	return response
}

// GenerateConfig generates a config for a specific profile using Power Law.
// Bucket RTPs (excluding the MAX bucket) sum exactly to targetRTP - maxWinRTP, so
// the math closes to targetRTP regardless of which profile / α is used.
func (g *ConfigGenerator) GenerateConfig(targetRTP, maxWin, maxWinFreq float64, profile PlayerProfile) *GeneratedConfig {
	if maxWinFreq <= 0 {
		maxWinFreq = DefaultMaxWinFreq
	}

	boundaries := g.calculateBucketBoundaries(maxWin)
	// Legacy path has no payout list — feasibility check falls back to boundary midpoints.
	buckets := g.generatePowerLawBuckets(boundaries, targetRTP, maxWin, maxWinFreq, profileAlpha(profile), nil)

	b64Config := g.toB64Config(targetRTP, buckets)
	stats := g.calculateStats(buckets, targetRTP)

	return &GeneratedConfig{
		Profile:     profile,
		ProfileName: g.getProfileName(profile),
		Description: ProfileDescriptions[profile],
		TargetRTP:   targetRTP,
		MaxWin:      maxWin,
		Buckets:     buckets,
		B64Config:   b64Config,
		Stats:       stats,
	}
}

// calculateBucketBoundaries determines bucket ranges based on max win
func (g *ConfigGenerator) calculateBucketBoundaries(maxWin float64) []float64 {
	baseBoundaries := []float64{0, 1, 2, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000}
	
	var valid []float64
	for _, b := range baseBoundaries {
		if b < maxWin {
			valid = append(valid, b)
		}
	}
	
	epsilon := maxWin * 0.001
	if epsilon < 0.01 {
		epsilon = 0.01
	}
	maxWinThreshold := maxWin - epsilon
	
	if len(valid) == 0 || valid[len(valid)-1] < maxWinThreshold {
		valid = append(valid, maxWinThreshold)
	}
	valid = append(valid, maxWin + 0.01) // Ensure maxWin is fully enclosed

	return valid
}

// densifyBoundaries inserts log-spaced midpoints between consecutive boundaries
// whose ratio exceeds maxRatio. Keeps the slice strictly increasing.
// Useful when analyzer-derived percentile boundaries leave a 5x+ gap at the top
// (e.g. 786 → 5050) that would otherwise become one fat bucket.
func densifyBoundaries(b []float64, maxRatio float64) []float64 {
	if len(b) < 2 || maxRatio <= 1 {
		return b
	}
	out := make([]float64, 0, len(b)*2)
	out = append(out, b[0])
	for i := 1; i < len(b); i++ {
		prev := out[len(out)-1]
		cur := b[i]
		// If lower edge is zero/sub-1, anchor on 1 before applying ratio test
		if prev <= 1 && cur > 1 {
			out = append(out, 1)
			prev = 1
		}
		for prev > 0 && cur/prev > maxRatio {
			mid := math.Sqrt(prev * cur)
			// Round to a sensible scale to keep numbers readable
			scale := math.Pow(10, math.Floor(math.Log10(mid))-1)
			if scale > 0 {
				mid = math.Round(mid/scale) * scale
			}
			if mid <= prev || mid >= cur {
				break
			}
			out = append(out, mid)
			prev = mid
		}
		out = append(out, cur)
	}
	return out
}

// generatePowerLawBuckets builds buckets so that math closes exactly to targetRTP:
//
//	maxWinRTP = maxWinPayout / maxWinFreq
//	distributableRTP = targetRTP - maxWinRTP
//	rtp_i ∝ x_i^(1-α)        (power-law shape; α = volatility exponent)
//	sum(rtp_i) == distributableRTP   →   sum(rtp_i) + maxWinRTP == targetRTP
//
// All non-MAX buckets are emitted as ConstraintRTPPercent (their RTPPercent values
// sum exactly to distributableRTP/targetRTP*100 with full float precision). The
// MAX bucket carries the supplied maxWinFreq verbatim. Inputs are in NORMALIZED
// payout units (cost-divided), so this works unchanged for bonus modes.
func (g *ConfigGenerator) generatePowerLawBuckets(boundaries []float64, targetRTP, maxWinPayout, maxWinFreq, alpha float64, winPayouts []float64) []BucketConfig {
	if len(boundaries) < 2 {
		return []BucketConfig{}
	}
	if maxWinFreq <= 0 {
		maxWinFreq = DefaultMaxWinFreq
	}

	// Always carve out a dedicated tight MAX bucket around the actual game maxPayout
	// so it captures only the absolute max and not the whole "jackpot" percentile band.
	// Densify regular boundaries where adjacent ratio > 4 so distribution doesn't
	// collapse into one fat top bucket.
	if maxWinPayout <= 0 {
		// Fall back: treat the last input boundary as the max.
		maxWinPayout = boundaries[len(boundaries)-1]
	}
	// Tight MAX range. Default ε is fixed-fraction of maxWin (works without
	// payout list), but when winPayouts are available, snap maxLower halfway
	// between maxWinPayout and the second-highest distinct payout. This ensures
	// the MAX bucket contains EXACTLY one outcome (the absolute max), so the
	// user-displayed bucket frequency equals the configured 1:N — no aggregate
	// from near-max outcomes inflating the apparent hit rate.
	eps := math.Max(maxWinPayout*1e-4, 0.001)
	maxLower := maxWinPayout - eps
	maxUpper := maxWinPayout + eps
	if len(winPayouts) > 0 {
		// Find the largest distinct payout strictly less than maxWinPayout.
		secondMax := 0.0
		for _, p := range winPayouts {
			if p < maxWinPayout && p > secondMax {
				secondMax = p
			}
		}
		if secondMax > 0 && secondMax < maxWinPayout {
			// Cut just above secondMax — by definition no payout lies strictly
			// between secondMax and maxWinPayout, so the MAX bucket then contains
			// outcomes only at p == maxWinPayout. ALWAYS apply (override default
			// ε-based maxLower), so user-displayed bucket frequency = configured 1:N.
			gap := maxWinPayout - secondMax
			snapLower := secondMax + gap*0.5
			maxLower = snapLower
			// Also ensure maxUpper sits strictly above maxWinPayout
			if maxUpper <= maxWinPayout {
				maxUpper = maxWinPayout + math.Max(gap*0.5, 0.001)
			}
		}
	}

	// Keep only original boundaries strictly below maxLower (with a tiny safety margin).
	regular := make([]float64, 0, len(boundaries)+4)
	for _, b := range boundaries {
		if b < maxLower*0.999 {
			regular = append(regular, b)
		}
	}
	if len(regular) == 0 {
		regular = append(regular, 0)
	}
	// Cap with maxLower so we have a clean handoff to the MAX bucket.
	if regular[len(regular)-1] < maxLower {
		regular = append(regular, maxLower)
	}
	// Densify any pair (a,b) with b/a > 4 by inserting log-spaced midpoints.
	regular = densifyBoundaries(regular, 4.0)

	// Final boundary list: regular + maxUpper. Last bucket is the MAX bucket.
	boundaries = append(regular, maxUpper)
	numBuckets := len(boundaries) - 1

	// Maxwin RTP claim. If it would consume more than 99% of target, clamp so the
	// power-law part still has a meaningful budget. Optimizer's tilt-rebalance
	// handles the residual rounding.
	maxWinRTP := 0.0
	if maxWinPayout > 0 {
		maxWinRTP = maxWinPayout / maxWinFreq
	}
	maxAllowedMaxwin := targetRTP * 0.99
	if maxWinRTP > maxAllowedMaxwin {
		maxWinRTP = maxAllowedMaxwin
	}
	distributableRTP := targetRTP - maxWinRTP

	// Last bucket is the dedicated MAX bucket; rest get the power-law allocation
	// via ConstraintAuto. Auto distributes per-OUTCOME prob ∝ payout^(-α) and
	// scales so that sum of bucket RTPs == remainingRTP (= targetRTP - maxWinRTP).
	// This closes math exactly using the optimizer's harmonic-mean-aware machinery
	// instead of the generator's arithmetic-midpoint approximation, which broke
	// closure for steep α (LOW vol) where implied per-bucket prob exceeded 1.
	// All non-MAX auto buckets share the same exponent — they're effectively a
	// single distribution partitioned for display.
	nonMaxCount := numBuckets - 1
	buckets := make([]BucketConfig, 0, numBuckets)

	// Estimate feasible α: with the requested α, per-outcome prob ∝ p^(-α).
	// Total prob = distributableRTP * Σp^(-α) / Σp^(1-α). If that exceeds the win
	// probability budget (≤ 1 - 1/lossMin; we use 0.7 as a safety margin), the
	// optimizer ends up with totalWinProb > 1, loss collapses to MinWeight, and
	// the actual achieved RTP ≈ remainingRTP / total_prob → undershoot.
	// Lower α (less steep) until feasible. The shape stays distinguishable across
	// profiles even after capping (LOW/MED/HIGH still rank in steepness).
	// Use REAL outcome payouts when available (every p > 0 counts as a hit per
	// optimizer's totalWinWeight check). Falls back to boundary midpoints in
	// the legacy non-adaptive path where we have no outcome list.
	probePayouts := winPayouts
	if len(probePayouts) == 0 {
		probePayouts = make([]float64, 0, nonMaxCount)
		for i := 0; i < nonMaxCount; i++ {
			mid := (boundaries[i] + boundaries[i+1]) / 2.0
			if mid <= 0 {
				mid = 0.5
			}
			probePayouts = append(probePayouts, mid)
		}
	}
	// Bisection on α to keep total win prob in [pMin, pMax]:
	//  - pMin = 1/18 + tiny margin (avoid the optimizer's hit-rate clamp, which
	//    distorts MaxWin frequency by inflating loss weight).
	//  - pMax = 0.95 (leave at least 5% of spins for losses).
	// total_prob is monotone increasing in α, so a binary search converges fast.
	const (
		pMin = 1.0/18.0 + 1.0/200.0
		pMax = 0.95
	)
	computeTotalProb := func(a float64) float64 {
		var sumPow, sumPowM1 float64
		for _, p := range probePayouts {
			if p <= 0 {
				continue
			}
			sumPow += math.Pow(p, -a)
			sumPowM1 += math.Pow(p, 1-a)
		}
		if sumPowM1 <= 0 {
			return 0
		}
		return distributableRTP * sumPow / sumPowM1
	}
	feasibleAlpha := alpha
	lo, hi := -3.0, 5.0
	if feasibleAlpha < lo {
		feasibleAlpha = lo
	}
	if feasibleAlpha > hi {
		feasibleAlpha = hi
	}
	for iter := 0; iter < 60; iter++ {
		tp := computeTotalProb(feasibleAlpha)
		if tp >= pMin && tp <= pMax {
			break
		}
		if tp > pMax {
			hi = feasibleAlpha
		} else { // tp < pMin
			lo = feasibleAlpha
		}
		next := (lo + hi) / 2
		if math.Abs(next-feasibleAlpha) < 1e-4 {
			feasibleAlpha = next
			break
		}
		feasibleAlpha = next
	}

	for i := 0; i < nonMaxCount; i++ {
		buckets = append(buckets, BucketConfig{
			Name:         fmt.Sprintf("bucket_%.2f_%.2f", boundaries[i], boundaries[i+1]),
			MinPayout:    boundaries[i],
			MaxPayout:    boundaries[i+1],
			Type:         ConstraintAuto,
			AutoExponent: feasibleAlpha,
		})
	}

	// MAX bucket — frequency is verbatim so that RTP math stays exact.
	buckets = append(buckets, BucketConfig{
		Name:            "maxwin",
		MinPayout:       boundaries[numBuckets-1],
		MaxPayout:       boundaries[numBuckets],
		Type:            ConstraintMaxWinFreq,
		MaxWinFrequency: maxWinFreq,
		IsMaxWinBucket:  true,
	})

	return buckets
}

// toB64Config converts buckets to base64 encoded short config
func (g *ConfigGenerator) toB64Config(targetRTP float64, buckets []BucketConfig) string {
	shortBuckets := make([][]any, len(buckets))

	for i, b := range buckets {
		var typeInt int
		var value float64

		switch b.Type {
		case ConstraintFrequency:
			typeInt = 0
			value = b.Frequency
		case ConstraintRTPPercent:
			typeInt = 1
			value = b.RTPPercent
		case ConstraintAuto:
			typeInt = 2
			value = b.AutoExponent
		case ConstraintMaxWinFreq:
			typeInt = 3
			value = b.MaxWinFrequency
		}

		shortBuckets[i] = []any{b.MinPayout, b.MaxPayout, typeInt, value}
	}

	short := ShortConfig{
		R: targetRTP * 100,
		B: shortBuckets,
	}

	jsonBytes, err := json.Marshal(short)
	if err != nil {
		return ""
	}

	return base64.StdEncoding.EncodeToString(jsonBytes)
}

// calculateStats computes statistics for the config
func (g *ConfigGenerator) calculateStats(buckets []BucketConfig, targetRTP float64) ConfigStats {
	rtpDistribution := make(map[string]float64)
	var totalWinProb float64
	var maxWinFreq float64

	for i, b := range buckets {
		rangeKey := fmt.Sprintf("%.2f-%.2f", b.MinPayout, b.MaxPayout)
		if b.Type == ConstraintRTPPercent {
			rtpDistribution[rangeKey] = math.Round(b.RTPPercent*100) / 100
		} else if b.Type == ConstraintMaxWinFreq {
			// approximate RTP
			avgPayout := (b.MinPayout + b.MaxPayout) / 2
			prob := 1.0 / b.MaxWinFrequency
			rtpContrib := prob * avgPayout
			rtpDistribution[rangeKey] = math.Round((rtpContrib/targetRTP)*10000) / 100
		}

		// Calculate win probability for this bucket
		avgPayout := (b.MinPayout + b.MaxPayout) / 2
		if avgPayout <= 0 {
			avgPayout = b.MaxPayout / 2
		}

		var prob float64
		switch b.Type {
		case ConstraintFrequency:
			prob = 1.0 / b.Frequency
		case ConstraintRTPPercent:
			rtpContrib := (b.RTPPercent / 100) * targetRTP
			prob = rtpContrib / avgPayout
		case ConstraintMaxWinFreq:
			prob = 1.0 / b.MaxWinFrequency
		case ConstraintAuto:
			// Estimate for AUTO - will be calculated properly during optimization
			rtpEstimate := 0.05 * targetRTP // Assume 5% of RTP
			prob = rtpEstimate / avgPayout
		}

		totalWinProb += prob

		// Track max win frequency
		if i == len(buckets)-1 {
			maxWinFreq = 1.0 / prob
		}
	}

	avgHitRate := 1.0 / totalWinProb
	if math.IsInf(avgHitRate, 1) || math.IsNaN(avgHitRate) {
		avgHitRate = 0
	}

	return ConfigStats{
		TotalBuckets:    len(buckets),
		RTPDistribution: rtpDistribution,
		AvgHitRate:      math.Round(avgHitRate*10) / 10,
		MaxWinFreq:      math.Round(maxWinFreq),
	}
}

// getProfileName returns human-readable profile name
func (g *ConfigGenerator) getProfileName(profile PlayerProfile) string {
	names := map[PlayerProfile]string{
		ProfileLowVol:    "Low Volatility",
		ProfileMediumVol: "Medium Volatility",
		ProfileHighVol:   "High Volatility",
	}

	if name, ok := names[profile]; ok {
		return name
	}
	return string(profile)
}

// ValidateGeneratedConfig validates a generated config is mathematically sound
func ValidateGeneratedConfig(config *GeneratedConfig) error {
	if config.TargetRTP <= 0 || config.TargetRTP > common.MaxOptimizerTargetRTP {
		return fmt.Errorf("invalid target RTP: %.4f (max %.2f)", config.TargetRTP, common.MaxOptimizerTargetRTP)
	}

	if config.MaxWin <= 0 {
		return fmt.Errorf("invalid max win: %.2f", config.MaxWin)
	}

	if len(config.Buckets) == 0 {
		return fmt.Errorf("no buckets generated")
	}

	// Calculate total RTP contribution to ensure it's valid
	var totalRTPContribution float64

	for _, b := range config.Buckets {
		avgPayout := (b.MinPayout + b.MaxPayout) / 2
		if avgPayout <= 0 {
			avgPayout = b.MaxPayout / 2
		}

		var contribution float64
		switch b.Type {
		case ConstraintFrequency:
			prob := 1.0 / b.Frequency
			contribution = prob * avgPayout
		case ConstraintRTPPercent:
			contribution = (b.RTPPercent / 100) * config.TargetRTP
		case ConstraintAuto:
			// AUTO buckets use remaining RTP
			continue
		}

		totalRTPContribution += contribution
	}

	// Total contribution should not exceed target (AUTO handles remainder)
	if totalRTPContribution > config.TargetRTP*1.1 { // 10% tolerance
		return fmt.Errorf("RTP overcommitment: %.4f > %.4f", totalRTPContribution, config.TargetRTP)
	}

	return nil
}

// GenerateAdaptiveConfig generates a config using LUT analysis for adaptive bucket generation
// This method uses mode analysis to handle extreme RTP values and non-standard payout ranges
func (g *ConfigGenerator) GenerateAdaptiveConfig(mode string, targetRTP, maxWin, maxWinFreq float64, profile PlayerProfile) (*GeneratedConfig, error) {
	if maxWinFreq <= 0 {
		maxWinFreq = DefaultMaxWinFreq
	}

	if g.analyzer == nil {
		// Fallback to legacy generation
		return g.GenerateConfig(targetRTP, maxWin, maxWinFreq, profile), nil
	}

	// Analyze the mode
	analysis, err := g.analyzer.AnalyzeMode(mode, targetRTP)
	if err != nil {
		// Fallback to legacy generation on analysis failure
		return g.GenerateConfig(targetRTP, maxWin, maxWinFreq, profile), nil
	}

	// Determine effective RTP (adjust if infeasible)
	effectiveRTP := targetRTP
	wasAdjusted := false

	if !analysis.Feasible {
		wasAdjusted = true
		if targetRTP > analysis.MaxAchievableRTP {
			effectiveRTP = analysis.MaxAchievableRTP * 0.95 // 95% of max
		} else {
			effectiveRTP = analysis.MinAchievableRTP * 1.05 // 105% of min
		}
	}

	// Use actual max payout from analysis
	actualMaxWin := analysis.MaxPayout
	if actualMaxWin <= 0 {
		actualMaxWin = maxWin
	}

	// Extract boundaries from analysis recommendations
	var boundaries []float64
	if len(analysis.RecommendedBuckets) > 0 {
		boundaries = append(boundaries, analysis.RecommendedBuckets[0].MinPayout)
		for _, rec := range analysis.RecommendedBuckets {
			boundaries = append(boundaries, rec.MaxPayout)
		}
	} else {
		boundaries = g.calculateBucketBoundaries(actualMaxWin)
	}

	// Generate Power Law distributed buckets. Pass real win payouts so the
	// generator picks an α that keeps total win prob ≤ 1 against the actual LUT
	// (every p > 0 counts as a hit, including sub-1x outcomes).
	buckets := g.generatePowerLawBuckets(boundaries, effectiveRTP, actualMaxWin, maxWinFreq, profileAlpha(profile), analysis.WinPayouts)

	// Fallback if no buckets generated
	if len(buckets) == 0 {
		return g.GenerateConfig(effectiveRTP, actualMaxWin, maxWinFreq, profile), nil
	}

	// Create b64 config
	b64Config := g.toB64Config(effectiveRTP, buckets)

	// Calculate stats from generated buckets
	stats := g.calculateStats(buckets, effectiveRTP)

	// Build feasibility info
	feasibility := &FeasibilityInfo{
		Original:    targetRTP,
		Effective:   effectiveRTP,
		WasAdjusted: wasAdjusted,
		MinPossible: analysis.MinAchievableRTP,
		MaxPossible: analysis.MaxAchievableRTP,
	}

	return &GeneratedConfig{
		Profile:     profile,
		ProfileName: g.getProfileName(profile),
		Description: ProfileDescriptions[profile],
		TargetRTP:   effectiveRTP,
		MaxWin:      actualMaxWin,
		Buckets:     buckets,
		B64Config:   b64Config,
		Stats:       stats,
		Feasibility: feasibility,
	}, nil
}

// GenerateAllAdaptiveProfiles generates adaptive configs for all profiles using mode analysis
func (g *ConfigGenerator) GenerateAllAdaptiveProfiles(mode string, targetRTP, maxWinFreq float64) (*ConfigGeneratorResponse, error) {
	if maxWinFreq <= 0 {
		maxWinFreq = DefaultMaxWinFreq
	}

	profiles := []PlayerProfile{
		ProfileLowVol,
		ProfileMediumVol,
		ProfileHighVol,
	}

	// Get max win from analysis if possible
	var maxWin float64 = 5000 // Default
	if g.analyzer != nil {
		analysis, err := g.analyzer.AnalyzeMode(mode, targetRTP)
		if err == nil {
			maxWin = analysis.MaxPayout
		}
	}

	response := &ConfigGeneratorResponse{
		Configs: make([]GeneratedConfig, 0, len(profiles)),
	}

	for _, profile := range profiles {
		config, err := g.GenerateAdaptiveConfig(mode, targetRTP, maxWin, maxWinFreq, profile)
		if err != nil {
			// Use legacy fallback
			config = g.GenerateConfig(targetRTP, maxWin, maxWinFreq, profile)
		}
		response.Configs = append(response.Configs, *config)
	}

	return response, nil
}
