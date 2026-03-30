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
	R int       `json:"r"` // RTP * 100 (e.g., 96 for 96%)
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

// GenerateAllProfiles generates configs for all profiles
func (g *ConfigGenerator) GenerateAllProfiles(targetRTP, maxWin float64) *ConfigGeneratorResponse {
	profiles := []PlayerProfile{
		ProfileLowVol,
		ProfileMediumVol,
		ProfileHighVol,
	}

	response := &ConfigGeneratorResponse{
		Configs: make([]GeneratedConfig, 0, len(profiles)),
	}

	for _, profile := range profiles {
		config := g.GenerateConfig(targetRTP, maxWin, profile)
		response.Configs = append(response.Configs, *config)
	}

	return response
}

// GenerateConfig generates a config for a specific profile using Power Law
func (g *ConfigGenerator) GenerateConfig(targetRTP, maxWin float64, profile PlayerProfile) *GeneratedConfig {
	// 1. Determine Target Hit Rate based on Profile
	// User requirement: hit rate N cannot exceed 18 (P_win >= 1/18)
	var targetHitRate float64
	switch profile {
	case ProfileLowVol:
		targetHitRate = 5 // 1 in 5 (very frequent)
	case ProfileMediumVol:
		targetHitRate = 10 // 1 in 10
	case ProfileHighVol:
		targetHitRate = 18 // 1 in 18 (the maximum allowed N)
	default:
		targetHitRate = 10
	}

	// 2. Define bucket boundaries based on maxWin
	boundaries := g.calculateBucketBoundaries(maxWin)

	// 3. Generate Power Law distributed buckets
	buckets := g.generatePowerLawBuckets(boundaries, targetRTP, targetHitRate)

	// Calculate b64 config
	b64Config := g.toB64Config(targetRTP, buckets)

	// Calculate stats
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

// generatePowerLawBuckets creates bucket configs using a Bisection Search on the Power Law exponent
func (g *ConfigGenerator) generatePowerLawBuckets(boundaries []float64, targetRTP, targetHitRate float64) []BucketConfig {
	numBuckets := len(boundaries) - 1
	if numBuckets <= 0 {
		return []BucketConfig{}
	}

	x := make([]float64, numBuckets)
	for i := 0; i < numBuckets; i++ {
		// Use harmonic mean or arithmetic mean for the bucket center
		// Arithmetic mean works fine for defining the power law curve
		x[i] = (boundaries[i] + boundaries[i+1]) / 2.0
		if x[i] <= 0 {
			x[i] = 0.5
		}
	}

	// We want to find alpha such that:
	// sum(x_i^(1-alpha)) / sum(x_i^(-alpha)) = TargetRTP * TargetHitRate
	targetRatio := targetRTP * targetHitRate

	lowAlpha := -10.0
	highAlpha := 10.0

	calcRatio := func(alpha float64) float64 {
		sumNum := 0.0
		sumDen := 0.0
		for _, xi := range x {
			sumNum += math.Pow(xi, 1.0-alpha)
			sumDen += math.Pow(xi, -alpha)
		}
		if sumDen == 0 {
			return 0
		}
		return sumNum / sumDen
	}

	// Bisection search for alpha
	for i := 0; i < 60; i++ {
		mid := (lowAlpha + highAlpha) / 2.0
		if calcRatio(mid) > targetRatio {
			lowAlpha = mid // calcRatio is strictly decreasing w.r.t alpha
		} else {
			highAlpha = mid
		}
	}

	bestAlpha := (lowAlpha + highAlpha) / 2.0

	// Calculate Probabilities P_i
	sumDen := 0.0
	for _, xi := range x {
		sumDen += math.Pow(xi, -bestAlpha)
	}

	buckets := make([]BucketConfig, numBuckets)
	for i := 0; i < numBuckets; i++ {
		prob := (1.0 / targetHitRate) * (math.Pow(x[i], -bestAlpha) / sumDen)
		rtpContrib := prob * x[i]
		rtpPercent := (rtpContrib / targetRTP) * 100.0

		isMaxWin := i == numBuckets-1

		b := BucketConfig{
			MinPayout: boundaries[i],
			MaxPayout: boundaries[i+1],
		}

		if isMaxWin {
			b.Name = "maxwin"
			b.IsMaxWinBucket = true
			b.Type = ConstraintMaxWinFreq
			b.MaxWinFrequency = math.Round(1.0 / prob)
		} else {
			b.Name = fmt.Sprintf("bucket_%.2f_%.2f", boundaries[i], boundaries[i+1])
			b.Type = ConstraintRTPPercent
			b.RTPPercent = math.Round(rtpPercent*1000) / 1000 // Keep precision
		}

		buckets[i] = b
	}

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
		R: int(math.Round(targetRTP * 100)),
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
func (g *ConfigGenerator) GenerateAdaptiveConfig(mode string, targetRTP, maxWin float64, profile PlayerProfile) (*GeneratedConfig, error) {
	if g.analyzer == nil {
		// Fallback to legacy generation
		return g.GenerateConfig(targetRTP, maxWin, profile), nil
	}

	// Analyze the mode
	analysis, err := g.analyzer.AnalyzeMode(mode, targetRTP)
	if err != nil {
		// Fallback to legacy generation on analysis failure
		return g.GenerateConfig(targetRTP, maxWin, profile), nil
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

	// Determine Target Hit Rate based on Profile
	var targetHitRate float64
	switch profile {
	case ProfileLowVol:
		targetHitRate = 5 // 1 in 5 (very frequent)
	case ProfileMediumVol:
		targetHitRate = 10 // 1 in 10
	case ProfileHighVol:
		targetHitRate = 18 // 1 in 18 (the maximum allowed N)
	default:
		targetHitRate = 10
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

	// Generate Power Law distributed buckets
	buckets := g.generatePowerLawBuckets(boundaries, effectiveRTP, targetHitRate)

	// Fallback if no buckets generated
	if len(buckets) == 0 {
		return g.GenerateConfig(effectiveRTP, actualMaxWin, profile), nil
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
func (g *ConfigGenerator) GenerateAllAdaptiveProfiles(mode string, targetRTP float64) (*ConfigGeneratorResponse, error) {
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
		config, err := g.GenerateAdaptiveConfig(mode, targetRTP, maxWin, profile)
		if err != nil {
			// Use legacy fallback
			config = g.GenerateConfig(targetRTP, maxWin, profile)
		}
		response.Configs = append(response.Configs, *config)
	}

	return response, nil
}
