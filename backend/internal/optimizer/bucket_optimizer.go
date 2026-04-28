package optimizer

import (
	"fmt"
	"math"
	"sort"

	"lutexplorer/internal/common"
	"lutexplorer/internal/lut"
	"stakergs"
)

// BucketConstraintType defines how a bucket's probability is specified
type BucketConstraintType string

const (
	// ConstraintFrequency specifies probability as "1 in N spins"
	ConstraintFrequency BucketConstraintType = "frequency"
	// ConstraintRTPPercent specifies probability via RTP contribution percentage
	ConstraintRTPPercent BucketConstraintType = "rtp_percent"
	// ConstraintAuto automatically uses remaining RTP after other buckets
	// Distributes weights inversely proportional to payout (higher payout = lower weight)
	ConstraintAuto BucketConstraintType = "auto"
	// ConstraintMaxWinFreq specifies the frequency of the maximum win outcome in the bucket
	ConstraintMaxWinFreq BucketConstraintType = "max_win_freq"
	// ConstraintOutcomeFreq specifies per-outcome frequency constraints
	ConstraintOutcomeFreq BucketConstraintType = "outcome_freq"
)

// ConstraintPriority defines whether a constraint is hard or soft
type ConstraintPriority int

const (
	// PriorityHard means the constraint must be satisfied (within tolerance)
	PriorityHard ConstraintPriority = 1
	// PrioritySoft means the constraint is best-effort
	PrioritySoft ConstraintPriority = 2
)

// OptimizationMode defines the search intensity
type OptimizationMode string

const (
	// ModeFast uses quick mathematical optimization (~100 iterations)
	ModeFast OptimizationMode = "fast"
	// ModeBalanced uses moderate search (~1000 iterations, default)
	ModeBalanced OptimizationMode = "balanced"
	// ModePrecise uses thorough search (~10000 iterations)
	ModePrecise OptimizationMode = "precise"
)

// BucketConfig defines a payout range and its probability constraint
type BucketConfig struct {
	Name             string               `json:"name"`                        // Human-readable name (e.g., "small_wins")
	MinPayout        float64              `json:"min_payout"`                  // Minimum payout in range (inclusive)
	MaxPayout        float64              `json:"max_payout"`                  // Maximum payout in range (exclusive, except for last bucket)
	Type             BucketConstraintType `json:"type"`                        // "frequency", "rtp_percent", "auto", "max_win_freq", "outcome_freq"
	Frequency        float64              `json:"frequency,omitempty"`         // 1 in N spins (e.g., 20 = 1 in 20 spins)
	RTPPercent       float64              `json:"rtp_percent,omitempty"`       // % of total RTP (e.g., 0.5 = 0.5% of RTP)
	AutoExponent     float64              `json:"auto_exponent,omitempty"`     // For auto: weight ∝ 1/payout^exponent (default 1.0, higher = steeper)
	MaxWinFrequency  float64              `json:"max_win_frequency,omitempty"` // For max_win_freq: frequency of the max payout in this bucket (1 in N)
	Priority         ConstraintPriority   `json:"priority,omitempty"`          // 1=hard, 2=soft constraint (default: hard)
	IsMaxWinBucket   bool                 `json:"is_maxwin_bucket,omitempty"`  // True if this bucket contains the max payout outcome
}

// BucketOptimizerConfig contains full configuration for bucket-based optimization
type BucketOptimizerConfig struct {
	TargetRTP           float64          `json:"target_rtp"`                      // Target RTP (e.g., 0.97)
	RTPTolerance        float64          `json:"rtp_tolerance"`                   // Acceptable deviation (e.g., 0.001)
	Buckets             []BucketConfig   `json:"buckets"`                         // Payout range configurations
	MinWeight           uint64           `json:"min_weight"`                      // Minimum weight for any outcome (default 1)
	MaxIterations       int              `json:"max_iterations,omitempty"`        // Max iterations for brute force (default: 1000)
	OptimizationMode    OptimizationMode `json:"optimization_mode,omitempty"`     // "fast"/"balanced"/"precise" (default: balanced)
	EnableBruteForce    bool             `json:"enable_brute_force,omitempty"`    // Enable iterative search (default: false)
	EnableVoiding       bool             `json:"enable_voiding,omitempty"`        // Enable bucket voiding (default: false) - DEPRECATED, use EnableAutoVoiding
	VoidedBucketIndices []int            `json:"voided_bucket_indices,omitempty"` // Indices of buckets to void - DEPRECATED
	EnableAutoVoiding   bool             `json:"enable_auto_voiding,omitempty"`   // Enable automatic outcome voiding to reach target RTP
}

// SearchState holds the current state during iterative optimization
type SearchState struct {
	Weights          []uint64           `json:"weights"`
	CurrentRTP       float64            `json:"current_rtp"`
	ConstraintErrors map[string]float64 `json:"constraint_errors"`
	Iteration        int                `json:"iteration"`
	Phase            string             `json:"phase"` // "init", "search", "refine"
}

// BruteForceProgress contains progress information for brute force optimization
type BruteForceProgress struct {
	Phase       string  `json:"phase"`        // "init", "search", "refine", "complete"
	Iteration   int     `json:"iteration"`    // Current iteration
	MaxIter     int     `json:"max_iter"`     // Maximum iterations
	CurrentRTP  float64 `json:"current_rtp"`  // Current RTP
	TargetRTP   float64 `json:"target_rtp"`   // Target RTP
	Error       float64 `json:"error"`        // Current error (|current - target|)
	Converged   bool    `json:"converged"`    // Whether optimization has converged
	ElapsedMs   int64   `json:"elapsed_ms"`   // Elapsed time in milliseconds
}

// BruteForceResult extends BucketOptimizerResult with additional search info
type BruteForceResult struct {
	*BucketOptimizerResult
	Iterations     int     `json:"iterations"`      // Total iterations performed
	SearchDuration int64   `json:"search_duration"` // Search duration in ms
	FinalError     float64 `json:"final_error"`     // Final RTP error
}

// DefaultBucketConfig returns a sensible default bucket configuration
func DefaultBucketConfig() *BucketOptimizerConfig {
	return &BucketOptimizerConfig{
		TargetRTP:    0.97,
		RTPTolerance: 0.001,
		MinWeight:    1,
		Buckets: []BucketConfig{
			{Name: "sub_1x", MinPayout: 0, MaxPayout: 1, Type: ConstraintFrequency, Frequency: 3},
			{Name: "small", MinPayout: 1, MaxPayout: 5, Type: ConstraintFrequency, Frequency: 5},
			{Name: "medium", MinPayout: 5, MaxPayout: 20, Type: ConstraintFrequency, Frequency: 25},
			{Name: "large", MinPayout: 20, MaxPayout: 100, Type: ConstraintFrequency, Frequency: 100},
			{Name: "huge", MinPayout: 100, MaxPayout: 1000, Type: ConstraintRTPPercent, RTPPercent: 5},
			{Name: "jackpot", MinPayout: 1000, MaxPayout: 100000, Type: ConstraintRTPPercent, RTPPercent: 0.5},
		},
	}
}

// BucketOptimizer optimizes using user-defined payout buckets
type BucketOptimizer struct {
	config *BucketOptimizerConfig
}

// NewBucketOptimizer creates a new bucket optimizer
func NewBucketOptimizer(config *BucketOptimizerConfig) *BucketOptimizer {
	if config == nil {
		config = DefaultBucketConfig()
	}
	if config.MinWeight < 1 {
		config.MinWeight = 1
	}
	if config.RTPTolerance <= 0 {
		config.RTPTolerance = 0.001
	}
	return &BucketOptimizer{config: config}
}

// BucketResult contains details about a single bucket's optimization
type BucketResult struct {
	Name              string  `json:"name"`
	MinPayout         float64 `json:"min_payout"`
	MaxPayout         float64 `json:"max_payout"`
	OutcomeCount      int     `json:"outcome_count"`
	TargetProbability float64 `json:"target_probability"` // Target probability for bucket
	ActualProbability float64 `json:"actual_probability"` // Achieved probability
	TargetFrequency   float64 `json:"target_frequency"`   // 1 in N (derived)
	ActualFrequency   float64 `json:"actual_frequency"`   // 1 in N (achieved)
	RTPContribution   float64 `json:"rtp_contribution"`   // % of RTP this bucket contributes
	TotalWeight       uint64  `json:"total_weight"`       // Sum of weights in bucket
	AvgPayout         float64 `json:"avg_payout"`         // Average payout in bucket
}

// VoidedBucketInfo contains information about a voided bucket (DEPRECATED - use VoidedOutcomeInfo)
type VoidedBucketInfo struct {
	Index           int     `json:"index"`            // Bucket index
	Name            string  `json:"name"`             // Bucket name
	OutcomeCount    int     `json:"outcome_count"`    // Number of outcomes excluded
	RtpContribution float64 `json:"rtp_contribution"` // Estimated RTP contribution that was removed
}

// VoidedOutcomeInfo contains information about an auto-voided outcome
type VoidedOutcomeInfo struct {
	SimID   int     `json:"sim_id"`   // Simulation ID of the voided outcome
	Payout  float64 `json:"payout"`   // Payout value of the voided outcome
	Reason  string  `json:"reason"`   // Why it was voided: "duplicate" or "high_payout"
	RTPLoss float64 `json:"rtp_loss"` // RTP contribution lost by voiding this outcome
}

// BucketOptimizerResult contains the full optimization result
type BucketOptimizerResult struct {
	OriginalRTP    float64             `json:"original_rtp"`
	FinalRTP       float64             `json:"final_rtp"`
	TargetRTP      float64             `json:"target_rtp"`
	Converged      bool                `json:"converged"`
	NewWeights     []uint64            `json:"new_weights"`
	BucketResults  []BucketResult      `json:"bucket_results"`
	LossResult     *BucketResult       `json:"loss_result"`
	TotalWeight    uint64              `json:"total_weight"`
	Warnings       []string            `json:"warnings,omitempty"`
	OutcomeDetails []OutcomeDetail     `json:"outcome_details,omitempty"`
	VoidedBuckets  []VoidedBucketInfo  `json:"voided_buckets,omitempty"`  // DEPRECATED - Buckets that were voided
	VoidedOutcomes []VoidedOutcomeInfo `json:"voided_outcomes,omitempty"` // Auto-voided outcomes
	TotalVoided    int                 `json:"total_voided,omitempty"`    // Total count of voided outcomes
	VoidedRTP      float64             `json:"voided_rtp,omitempty"`      // Total RTP removed by voiding
}

// OutcomeDetail shows how each outcome was assigned
type OutcomeDetail struct {
	SimID       int     `json:"sim_id"`
	Payout      float64 `json:"payout"`
	OldWeight   uint64  `json:"old_weight"`
	NewWeight   uint64  `json:"new_weight"`
	BucketName  string  `json:"bucket_name"`
	Probability float64 `json:"probability"`
}

// bucketAssignment holds outcomes assigned to a bucket during optimization
type bucketAssignment struct {
	config            BucketConfig
	outcomeIndices    []int
	payouts           []float64
	targetProb        float64   // Total probability for bucket (sum of outcomeProbs for auto)
	outcomeProbs      []float64 // Per-outcome probabilities (for auto buckets with varying probs)
	avgPayout         float64
	rtpContribution   float64
	isAuto            bool // True if this is an auto bucket
	isVoided          bool // True if this bucket is voided (excluded from optimization)
}

// payoutGroup groups outcomes by their payout value for auto-voiding analysis
type payoutGroup struct {
	payout        float64
	indices       []int   // Outcome indices with this payout
	simIDs        []int   // SimIDs for these outcomes
	rtpPerOutcome float64 // RTP contribution per outcome (with uniform distribution)
}

// autoSelectOutcomesToVoid automatically selects which outcomes to void to reach target RTP
// Strategy:
// 1. First void duplicate high payouts (same payout appearing multiple times)
// 2. Then void unique high payouts starting from the highest
func autoSelectOutcomesToVoid(
	payouts []float64,
	simIDs []int,
	targetRTP float64,
	currentMinRTP float64,
) ([]int, []VoidedOutcomeInfo) {
	if currentMinRTP <= targetRTP {
		return nil, nil // No voiding needed
	}

	rtpToRemove := currentMinRTP - targetRTP
	n := len(payouts)
	if n == 0 {
		return nil, nil
	}

	// Group outcomes by payout
	payoutMap := make(map[float64]*payoutGroup)
	for i, p := range payouts {
		if p <= 0 {
			continue // Skip loss outcomes
		}
		if group, exists := payoutMap[p]; exists {
			group.indices = append(group.indices, i)
			group.simIDs = append(group.simIDs, simIDs[i])
		} else {
			payoutMap[p] = &payoutGroup{
				payout:  p,
				indices: []int{i},
				simIDs:  []int{simIDs[i]},
			}
		}
	}

	// Calculate RTP per outcome for each payout (assuming uniform distribution)
	// RTP contribution = payout * (1/n) where n is total outcomes
	for _, group := range payoutMap {
		group.rtpPerOutcome = group.payout / float64(n)
	}

	// Sort payouts descending (highest first)
	var sortedPayouts []float64
	for p := range payoutMap {
		sortedPayouts = append(sortedPayouts, p)
	}
	sort.Slice(sortedPayouts, func(i, j int) bool {
		return sortedPayouts[i] > sortedPayouts[j]
	})

	var voidedIndices []int
	var voidedOutcomes []VoidedOutcomeInfo
	removedRTP := 0.0

	// Phase 1: Void duplicate high payouts (keep one of each)
	for _, payout := range sortedPayouts {
		if removedRTP >= rtpToRemove {
			break
		}
		group := payoutMap[payout]
		if len(group.indices) > 1 {
			// Void all but one (keep the first)
			for i := 1; i < len(group.indices); i++ {
				if removedRTP >= rtpToRemove {
					break
				}
				voidedIndices = append(voidedIndices, group.indices[i])
				voidedOutcomes = append(voidedOutcomes, VoidedOutcomeInfo{
					SimID:   group.simIDs[i],
					Payout:  group.payout,
					Reason:  "duplicate",
					RTPLoss: group.rtpPerOutcome,
				})
				removedRTP += group.rtpPerOutcome
			}
		}
	}

	// Phase 2: If still need to remove RTP, void unique high payouts
	for _, payout := range sortedPayouts {
		if removedRTP >= rtpToRemove {
			break
		}
		group := payoutMap[payout]
		// Check if all of this payout were already voided (or if it was kept as the single remaining)
		alreadyVoided := 0
		for _, idx := range voidedIndices {
			for _, gidx := range group.indices {
				if idx == gidx {
					alreadyVoided++
					break
				}
			}
		}
		// If we have at least one remaining (not voided), void it
		if alreadyVoided < len(group.indices) {
			// Find first non-voided index
			for i, idx := range group.indices {
				isAlreadyVoided := false
				for _, vidx := range voidedIndices {
					if vidx == idx {
						isAlreadyVoided = true
						break
					}
				}
				if !isAlreadyVoided {
					voidedIndices = append(voidedIndices, idx)
					voidedOutcomes = append(voidedOutcomes, VoidedOutcomeInfo{
						SimID:   group.simIDs[i],
						Payout:  group.payout,
						Reason:  "high_payout",
						RTPLoss: group.rtpPerOutcome,
					})
					removedRTP += group.rtpPerOutcome
					break
				}
			}
		}
	}

	return voidedIndices, voidedOutcomes
}

// calculateMinAchievableRTP calculates the minimum RTP possible with min weights
// This assumes each win outcome has minimum weight, and loss has maximum possible weight
func calculateMinAchievableRTP(payouts []float64, minWeight uint64) float64 {
	if len(payouts) == 0 {
		return 0
	}
	
	var winPayoutSum float64
	var winCount int
	for _, p := range payouts {
		if p > 0 {
			winPayoutSum += p
			winCount++
		}
	}

	if winCount == 0 {
		return 0
	}

	// Using a large realistic limit for loss weight to avoid uint64 overflow
	const maxLossWeight float64 = 1e15 
	winWeightSum := float64(winCount) * float64(minWeight)
	
	return (winPayoutSum * float64(minWeight)) / (winWeightSum + maxLossWeight)
}

// OptimizeTable optimizes a lookup table using bucket constraints
func (o *BucketOptimizer) OptimizeTable(table *stakergs.LookupTable) (*BucketOptimizerResult, error) {
	n := len(table.Outcomes)
	if n == 0 {
		return nil, fmt.Errorf("empty table")
	}

	cost := table.Cost
	if cost <= 0 {
		cost = 1.0
	}

	// Extract payouts (normalized by cost)
	payouts := make([]float64, n)
	originalWeights := make([]uint64, n)
	for i, outcome := range table.Outcomes {
		payouts[i] = float64(outcome.Payout) / 100.0 / cost
		originalWeights[i] = outcome.Weight
	}

	// Diagnostics: log max and top 5 normalized payouts to help debug unit mismatches
	if len(payouts) > 0 {
		maxP := payouts[0]
		for _, p := range payouts {
			if p > maxP {
				maxP = p
			}
		}
		tmp := make([]float64, len(payouts))
		copy(tmp, payouts)
		sort.Slice(tmp, func(i, j int) bool { return tmp[i] > tmp[j] })
		topN := 5
		if len(tmp) < topN {
			topN = len(tmp)
		}
		fmt.Printf("[OPTIMIZER] mode=%s cost=%.2f max_normalized=%.2f top%d=%v\n", table.Mode, cost, maxP, topN, tmp[:topN])
	}

	originalRTP := calculateRTPFromWeights(originalWeights, payouts)

	// Extract simIDs for auto-voiding
	simIDs := make([]int, n)
	for i, outcome := range table.Outcomes {
		simIDs[i] = outcome.SimID
	}

	// NEW: Auto-voiding - automatically select outcomes to void
	var autoVoidedIndices []int
	var autoVoidedOutcomes []VoidedOutcomeInfo
	var autoVoidedRTP float64

	if o.config.EnableAutoVoiding {
		minRTP := calculateMinAchievableRTP(payouts, o.config.MinWeight)
		autoVoidedIndices, autoVoidedOutcomes = autoSelectOutcomesToVoid(payouts, simIDs, o.config.TargetRTP, minRTP)
		// Calculate total voided RTP
		for _, vo := range autoVoidedOutcomes {
			autoVoidedRTP += vo.RTPLoss
		}
	}

	// LEGACY: Create a set of voided bucket indices for fast lookup (deprecated)
	voidedBucketSet := make(map[int]bool)
	if o.config.EnableVoiding && len(o.config.VoidedBucketIndices) > 0 {
		for _, idx := range o.config.VoidedBucketIndices {
			voidedBucketSet[idx] = true
		}
	}

	// Assign outcomes to buckets
	assignments, lossIndices, warnings := o.assignOutcomesToBuckets(payouts)

	// LEGACY: Mark voided buckets and collect voided outcomes info (deprecated)
	var voidedBuckets []VoidedBucketInfo
	var voidedOutcomeIndices []int
	for i := range assignments {
		if voidedBucketSet[i] {
			assignments[i].isVoided = true
			// Calculate estimated RTP contribution for this bucket
			var sumPayout float64
			for _, p := range assignments[i].payouts {
				sumPayout += p
			}
			avgPayout := 0.0
			if len(assignments[i].payouts) > 0 {
				avgPayout = sumPayout / float64(len(assignments[i].payouts))
			}
			// Estimate RTP contribution (rough estimate based on uniform distribution)
			rtpContrib := avgPayout * float64(len(assignments[i].outcomeIndices)) / float64(n) * 100

			voidedBuckets = append(voidedBuckets, VoidedBucketInfo{
				Index:           i,
				Name:            assignments[i].config.Name,
				OutcomeCount:    len(assignments[i].outcomeIndices),
				RtpContribution: rtpContrib,
			})
			// Collect voided outcome indices
			voidedOutcomeIndices = append(voidedOutcomeIndices, assignments[i].outcomeIndices...)
		}
	}

	// Merge auto-voided indices with legacy voided indices
	if len(autoVoidedIndices) > 0 {
		voidedOutcomeIndices = append(voidedOutcomeIndices, autoVoidedIndices...)
	}

	// Calculate target probabilities for each bucket (excluding voided)
	probWarnings := o.calculateTargetProbabilities(assignments)
	warnings = append(warnings, probWarnings...)

	// Calculate weights (voided outcomes will have weight 0)
	newWeights, bucketResults, lossResult, weightWarnings := o.calculateWeightsWithVoiding(payouts, assignments, lossIndices, voidedOutcomeIndices)
	if len(weightWarnings) > 0 {
		warnings = append(warnings, weightWarnings...)
	}

	// Calculate final RTP
	finalRTP := calculateRTPFromWeights(newWeights, payouts)
	converged := math.Abs(finalRTP-o.config.TargetRTP) <= o.config.RTPTolerance

	// Fine-tune if not converged
	if !converged {
		newWeights = o.rebalanceWinsToTargetRTP(newWeights, payouts, o.config.TargetRTP, o.config.MinWeight)
		finalRTP = calculateRTPFromWeights(newWeights, payouts)
		converged = math.Abs(finalRTP-o.config.TargetRTP) <= o.config.RTPTolerance

		// Recalculate bucket results and loss result since actual weights changed.
		// bucketResults is filtered (empty/voided buckets skipped), so map by name
		// to avoid index drift between assignments[] and bucketResults[].
		assignmentByName := make(map[string]*bucketAssignment, len(assignments))
		for j := range assignments {
			assignmentByName[assignments[j].config.Name] = &assignments[j]
		}
		totalWeight := sumUint64(newWeights)
		for i := range bucketResults {
			a, ok := assignmentByName[bucketResults[i].Name]
			if !ok {
				continue
			}
			var bucketWeight uint64
			var bucketRTP float64
			for _, idx := range a.outcomeIndices {
				bucketWeight += newWeights[idx]
				if totalWeight > 0 {
					prob := float64(newWeights[idx]) / float64(totalWeight)
					bucketRTP += prob * payouts[idx]
				}
			}

			bucketResults[i].TotalWeight = bucketWeight
			if bucketWeight > 0 && totalWeight > 0 {
				bucketResults[i].ActualProbability = float64(bucketWeight) / float64(totalWeight)
				bucketResults[i].ActualFrequency = 1.0 / bucketResults[i].ActualProbability
			} else {
				bucketResults[i].ActualProbability = 0
				bucketResults[i].ActualFrequency = 0
			}
			bucketResults[i].RTPContribution = bucketRTP * 100
		}

		if len(lossIndices) > 0 {
			lossResult = o.calculateLossResult(newWeights, payouts, lossIndices)
		}
	}

	// Add warning if final RTP is way off target
	if !converged {
		diff := (finalRTP - o.config.TargetRTP) * 100
		if diff > 10 {
			warnings = append(warnings, fmt.Sprintf(
				"Final RTP %.1f%% is %.0f%% above target. High-payout outcomes (with min weight=1) contribute too much RTP. Try removing high-payout buckets or using fewer frequency constraints.",
				finalRTP*100, diff))
		} else if diff < -10 {
			warnings = append(warnings, fmt.Sprintf(
				"Final RTP %.1f%% is %.0f%% below target. Not enough high-value outcomes to reach target RTP.",
				finalRTP*100, -diff))
		}
	}

	// Add info about legacy bucket voiding if applied (deprecated)
	if len(voidedBuckets) > 0 {
		var voidedNames []string
		for _, vb := range voidedBuckets {
			voidedNames = append(voidedNames, vb.Name)
		}
		warnings = append(warnings, fmt.Sprintf(
			"Voided %d bucket(s) to reach target RTP: %v",
			len(voidedBuckets), voidedNames))
	}

	// Add info about auto-voiding if applied
	if len(autoVoidedOutcomes) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"Auto-voided %d outcome(s), removed %.2f%% RTP",
			len(autoVoidedOutcomes), autoVoidedRTP*100))
	}

	// Build outcome details
	outcomeDetails := o.buildOutcomeDetailsWithVoiding(table, payouts, originalWeights, newWeights, assignments, lossIndices, voidedOutcomeIndices)

	return &BucketOptimizerResult{
		OriginalRTP:    originalRTP,
		FinalRTP:       finalRTP,
		TargetRTP:      o.config.TargetRTP,
		Converged:      converged,
		NewWeights:     newWeights,
		BucketResults:  bucketResults,
		LossResult:     lossResult,
		TotalWeight:    sumUint64(newWeights),
		Warnings:       warnings,
		OutcomeDetails: outcomeDetails,
		VoidedBuckets:  voidedBuckets,
		VoidedOutcomes: autoVoidedOutcomes,
		TotalVoided:    len(autoVoidedOutcomes),
		VoidedRTP:      autoVoidedRTP,
	}, nil
}

// assignOutcomesToBuckets assigns each outcome to appropriate bucket
func (o *BucketOptimizer) assignOutcomesToBuckets(payouts []float64) ([]bucketAssignment, []int, []string) {
	var warnings []string
	var lossIndices []int

	// Create assignments for each bucket
	assignments := make([]bucketAssignment, len(o.config.Buckets))
	for i, bucket := range o.config.Buckets {
		assignments[i] = bucketAssignment{
			config:         bucket,
			outcomeIndices: []int{},
			payouts:        []float64{},
		}
	}

	// Assign each outcome
	for i, payout := range payouts {
		if payout <= 0 {
			lossIndices = append(lossIndices, i)
			continue
		}

		assigned := false
		for j := range assignments {
			bucket := &assignments[j]
			// Check if payout falls within this bucket's range
			// Last bucket includes max (>=), others exclude it (<)
			inRange := payout >= bucket.config.MinPayout
			if j < len(assignments)-1 {
				inRange = inRange && payout < bucket.config.MaxPayout
			} else {
				inRange = inRange && payout <= bucket.config.MaxPayout
			}

			if inRange {
				bucket.outcomeIndices = append(bucket.outcomeIndices, i)
				bucket.payouts = append(bucket.payouts, payout)
				assigned = true
				break
			}
		}

		if !assigned {
			// Outcome doesn't fit any bucket - silently assign to closest
			// Find closest bucket
			closestIdx := 0
			closestDist := math.MaxFloat64
			for j, bucket := range assignments {
				dist := math.Min(
					math.Abs(payout-bucket.config.MinPayout),
					math.Abs(payout-bucket.config.MaxPayout),
				)
				if dist < closestDist {
					closestDist = dist
					closestIdx = j
				}
			}
			assignments[closestIdx].outcomeIndices = append(assignments[closestIdx].outcomeIndices, i)
			assignments[closestIdx].payouts = append(assignments[closestIdx].payouts, payout)
		}
	}

	// Calculate average payout for each bucket
	for i := range assignments {
		if len(assignments[i].payouts) > 0 {
			sum := 0.0
			for _, p := range assignments[i].payouts {
				sum += p
			}
			assignments[i].avgPayout = sum / float64(len(assignments[i].payouts))
		}
	}

	return assignments, lossIndices, warnings
}

// calculateTargetProbabilities calculates target probability for each bucket
// For auto buckets, it first calculates non-auto buckets, then distributes remaining RTP
// Returns warnings if constraints are impossible to satisfy
func (o *BucketOptimizer) calculateTargetProbabilities(assignments []bucketAssignment) []string {
	var warnings []string
	// First pass: calculate probabilities for frequency and rtp_percent buckets
	var usedRTP float64

	for i := range assignments {
		bucket := &assignments[i]
		if len(bucket.outcomeIndices) == 0 {
			continue
		}

		// Calculate harmonic mean payout since we distribute weights inversely proportional to payout
		var invSum float64
		var validCount int
		for _, p := range bucket.payouts {
			if p > 0 {
				invSum += 1.0 / p
				validCount++
			}
		}
		
		effectiveAvgPayout := bucket.avgPayout
		if invSum > 0 && validCount > 0 {
			effectiveAvgPayout = float64(validCount) / invSum
		}

		switch bucket.config.Type {
		case ConstraintFrequency:
			// Frequency: 1 in N spins = probability of 1/N
			if bucket.config.Frequency > 0 {
				bucket.targetProb = 1.0 / bucket.config.Frequency
			}
			// Calculate implied RTP contribution
			bucket.rtpContribution = bucket.targetProb * effectiveAvgPayout
			usedRTP += bucket.rtpContribution

		case ConstraintRTPPercent:
			// RTP%: X% of target RTP
			if effectiveAvgPayout > 0 && bucket.config.RTPPercent > 0 {
				bucket.rtpContribution = (bucket.config.RTPPercent / 100.0) * o.config.TargetRTP
				bucket.targetProb = bucket.rtpContribution / effectiveAvgPayout
				usedRTP += bucket.rtpContribution
			}

		case ConstraintMaxWinFreq:
			// MaxWinFreq: only the absolute max payout in the bucket carries weight,
			// at probability 1/freq. Account for its RTP so AUTO buckets don't over-allocate.
			if bucket.config.MaxWinFrequency > 0 {
				maxP := 0.0
				for _, p := range bucket.payouts {
					if p > maxP {
						maxP = p
					}
				}
				bucket.targetProb = 1.0 / bucket.config.MaxWinFrequency
				bucket.rtpContribution = bucket.targetProb * maxP
				usedRTP += bucket.rtpContribution
			}

		case ConstraintAuto:
			bucket.isAuto = true
			// Will be calculated in second pass
		}
	}

	// Second pass: distribute remaining RTP to auto buckets
	remainingRTP := o.config.TargetRTP - usedRTP
	if remainingRTP < 0 {
		remainingRTP = 0
	}

	// Track if frequency buckets already exceed target RTP
	if usedRTP > o.config.TargetRTP {
		warnings = append(warnings, fmt.Sprintf(
			"Frequency/RTP%% constraints already use %.1f%% RTP (target: %.1f%%). Cannot reach target RTP. Reduce frequencies or use AUTO type.",
			usedRTP*100, o.config.TargetRTP*100))
	}

	// Collect all auto bucket outcomes
	var autoBucketIndices []int
	var totalAutoOutcomes int
	for i := range assignments {
		if assignments[i].isAuto && len(assignments[i].outcomeIndices) > 0 {
			autoBucketIndices = append(autoBucketIndices, i)
			totalAutoOutcomes += len(assignments[i].outcomeIndices)
		}
	}

	if len(autoBucketIndices) > 0 && remainingRTP > 0 {
		// For auto buckets, use inverse-proportional distribution:
		// prob_i = remainingRTP * (1/payout_i^exp) / Σ(payout_j^(1-exp))
		//
		// This ensures:
		// 1. Higher payouts get lower weights
		// 2. Total RTP contribution equals remainingRTP
		// 3. Each outcome contributes equally to RTP (with exp=1)

		// Calculate sum of payout^(1-exp) across all auto outcomes
		var sumPayout1MinusExp float64
		for _, bucketIdx := range autoBucketIndices {
			bucket := &assignments[bucketIdx]
			exp := bucket.config.AutoExponent
			if exp == 0 && bucket.config.AutoExponent != 0 {
				// no-op; keep zero as-is below
			}
			for _, p := range bucket.payouts {
				if p > 0 {
					sumPayout1MinusExp += math.Pow(p, 1-exp)
				}
			}
		}

		// Distribute to each auto bucket
		for _, bucketIdx := range autoBucketIndices {
			bucket := &assignments[bucketIdx]
			exp := bucket.config.AutoExponent
			// Negative and zero exponents are valid:
			//   exp > 0  → mass on small payouts (low vol)
			//   exp = 0  → uniform RTP per outcome
			//   exp < 0  → mass on big payouts (high vol)

			bucket.outcomeProbs = make([]float64, len(bucket.payouts))
			var bucketTotalProb float64
			var bucketRTP float64

			for j, p := range bucket.payouts {
				if p > 0 && sumPayout1MinusExp > 0 {
					// prob_i = remainingRTP * (1/p^exp) / Σ(p^(1-exp))
					prob := remainingRTP * math.Pow(p, -exp) / sumPayout1MinusExp
					bucket.outcomeProbs[j] = prob
					bucketTotalProb += prob
					bucketRTP += prob * p
				}
			}

			bucket.targetProb = bucketTotalProb
			bucket.rtpContribution = bucketRTP
		}
	}

	return warnings
}

// calculateWeights converts probabilities to weights
func (o *BucketOptimizer) calculateWeights(payouts []float64, assignments []bucketAssignment, lossIndices []int) ([]uint64, []BucketResult, *BucketResult, []string) {
	n := len(payouts)
	weights := make([]uint64, n)

	// Use large base for precision
	baseWeight := common.BaseWeight

	// Calculate total win probability and RTP contribution
	var totalWinProb float64
	var totalWinRTP float64

	bucketResults := make([]BucketResult, 0, len(assignments))
	var warnings []string

	for _, bucket := range assignments {
		if len(bucket.outcomeIndices) == 0 {
			// Include empty bucket result so UI can show unused buckets
			bucketResults = append(bucketResults, BucketResult{
				Name:              bucket.config.Name,
				MinPayout:         bucket.config.MinPayout,
				MaxPayout:         bucket.config.MaxPayout,
				OutcomeCount:      0,
				TargetProbability: bucket.targetProb,
				TargetFrequency:   0,
				ActualProbability: 0,
				ActualFrequency:   0,
				RTPContribution:   0,
				TotalWeight:       0,
				AvgPayout:         bucket.avgPayout,
			})
			warnings = append(warnings, fmt.Sprintf("bucket '%s' has no matching outcomes", bucket.config.Name))
			continue
		}

		var actualTotalWeight uint64

		if bucket.isAuto && len(bucket.outcomeProbs) == len(bucket.outcomeIndices) {
			// Auto bucket: use per-outcome probabilities
			for j, idx := range bucket.outcomeIndices {
				prob := bucket.outcomeProbs[j]
				w := uint64(prob * float64(baseWeight))
				if w < o.config.MinWeight {
					w = o.config.MinWeight
				}
				weights[idx] = w
				actualTotalWeight += w
			}
		} else {
			// Non-auto bucket: distribute inversely proportional to payout
			bucketTotalWeight := uint64(bucket.targetProb * float64(baseWeight))
			
			var invSum float64
			for _, idx := range bucket.outcomeIndices {
				if payouts[idx] > 0 {
					invSum += 1.0 / payouts[idx]
				}
			}

			if invSum > 0 {
				for _, idx := range bucket.outcomeIndices {
					if payouts[idx] <= 0 {
						continue
					}
					fraction := (1.0 / payouts[idx]) / invSum
					w := uint64(float64(bucketTotalWeight) * fraction)
					
					if w < o.config.MinWeight {
						w = o.config.MinWeight
					}
					weights[idx] = w
					actualTotalWeight += w
				}
			} else {
				weightPerOutcome := bucketTotalWeight / uint64(len(bucket.outcomeIndices))
				if weightPerOutcome < o.config.MinWeight {
					weightPerOutcome = o.config.MinWeight
				}
				for _, idx := range bucket.outcomeIndices {
					weights[idx] = weightPerOutcome
					actualTotalWeight += weightPerOutcome
				}
			}
		}

		totalWinProb += bucket.targetProb
		totalWinRTP += bucket.rtpContribution

		// Record bucket result
		targetFreq := 0.0
		if bucket.targetProb > 0 {
			targetFreq = 1.0 / bucket.targetProb
		}

		bucketResults = append(bucketResults, BucketResult{
			Name:              bucket.config.Name,
			MinPayout:         bucket.config.MinPayout,
			MaxPayout:         bucket.config.MaxPayout,
			OutcomeCount:      len(bucket.outcomeIndices),
			TargetProbability: bucket.targetProb,
			TargetFrequency:   targetFreq,
			RTPContribution:   bucket.rtpContribution * 100, // As absolute % RTP
			TotalWeight:       actualTotalWeight,
			AvgPayout:         bucket.avgPayout,
		})
	}

	// Calculate loss weight
	// RTP = totalWinRTP + 0 (loss contributes 0)
	// We need: totalWinRTP = targetRTP
	// Loss probability = 1 - totalWinProb
	//
	// Actually, we need to adjust. Let's calculate:
	// Current win RTP = totalWinRTP
	// If totalWinRTP > targetRTP, we need more loss
	// If totalWinRTP < targetRTP, we need less loss (or can't achieve target)

	// The relationship is:
	// RTP = Σ(p_i * payout_i) where Σp_i = 1
	// Let p_loss = 1 - totalWinProb
	// RTP = totalWinRTP (since loss * 0 = 0)
	//
	// But we distributed based on target probs, not actual probs.
	// The actual prob depends on total weight.
	//
	// Let's work backwards:
	// totalWinWeight = sum of bucket weights
	// We want: totalWinRTP = targetRTP
	// actualRTP = Σ(weight_i * payout_i) / totalWeight
	//
	// Set loss weight such that:
	// Σ(winWeight * payout) / (winWeight + lossWeight) = targetRTP
	// weightedPayoutSum / (totalWinWeight + lossWeight) = targetRTP
	// lossWeight = weightedPayoutSum / targetRTP - totalWinWeight

	// Calculate loss weight strictly based on Hit Rate (totalWinProb)
	var totalWinWeight uint64
	for i, w := range weights {
		if payouts[i] > 0 {
			totalWinWeight += w
		}
	}

	// ENFORCE MINIMUM HIT RATE (1 in 18)
	// The hit rate N cannot be greater than 18 (i.e. probability cannot be less than 1/18).
	minWinProb := 1.0 / 18.0
	if totalWinProb < minWinProb-1e-9 {
		warnings = append(warnings, fmt.Sprintf("Target hit rate (1:%.2f) is less frequent than the allowed minimum (1:18). Hit rate adjusted to 1:18.", 1.0/totalWinProb))
		totalWinProb = minWinProb
	}

	var requiredLossWeight float64
	if totalWinProb < 1.0 && totalWinProb > 0 {
		lossProb := 1.0 - totalWinProb
		requiredLossWeight = float64(totalWinWeight) * (lossProb / totalWinProb)
	} else {
		requiredLossWeight = float64(o.config.MinWeight)
	}

	if requiredLossWeight < float64(o.config.MinWeight) {
		requiredLossWeight = float64(o.config.MinWeight)
	}

	// Distribute loss weight among loss outcomes
	var lossResult *BucketResult
	if len(lossIndices) > 0 {
		lossWeightPerOutcome := uint64(math.Round(requiredLossWeight / float64(len(lossIndices))))
		if lossWeightPerOutcome < o.config.MinWeight {
			lossWeightPerOutcome = o.config.MinWeight
		}

		var totalLossWeight uint64
		for _, idx := range lossIndices {
			weights[idx] = lossWeightPerOutcome
			totalLossWeight += lossWeightPerOutcome
		}

		totalWeight := totalWinWeight + totalLossWeight
		lossProb := float64(totalLossWeight) / float64(totalWeight)

		lossResult = &BucketResult{
			Name:              "loss",
			MinPayout:         0,
			MaxPayout:         0,
			OutcomeCount:      len(lossIndices),
			TargetProbability: 1 - totalWinProb,
			ActualProbability: lossProb,
			TargetFrequency:   1.0 / (1 - totalWinProb),
			ActualFrequency:   1.0 / lossProb,
			RTPContribution:   0,
			TotalWeight:       totalLossWeight,
			AvgPayout:         0,
		}
	}

	// Update bucket results with actual probabilities and RTP contributions
	totalWeight := sumUint64(weights)
	for i := range bucketResults {
		if bucketResults[i].TotalWeight > 0 && totalWeight > 0 {
			bucketResults[i].ActualProbability = float64(bucketResults[i].TotalWeight) / float64(totalWeight)
			bucketResults[i].ActualFrequency = 1.0 / bucketResults[i].ActualProbability
			// Recalculate RTP contribution based on actual probability
			bucketResults[i].RTPContribution = bucketResults[i].ActualProbability * bucketResults[i].AvgPayout * 100
		} else {
			bucketResults[i].ActualProbability = 0
			bucketResults[i].ActualFrequency = 0
			bucketResults[i].RTPContribution = 0
		}
	}

	return weights, bucketResults, lossResult, warnings
}

// rebalanceWinsToTargetRTP uses exponential tilt and bisection search to smoothly
// rebalance win weights to hit the exact target RTP while keeping loss weight constant.
func (o *BucketOptimizer) rebalanceWinsToTargetRTP(weights []uint64, payouts []float64, targetRTP float64, minWeight uint64) []uint64 {
	result := make([]uint64, len(weights))
	copy(result, weights)

	// Identify fixed indices that shouldn't be tilted (e.g. Max Win freq targets).
	// Mark EVERY outcome at max payout in each MaxWinFreq bucket — if multiple
	// outcomes share the max payout, the override block split weight between
	// them; tilting any of them would break that split.
	fixedIndices := make(map[int]bool)

	for _, bucket := range o.config.Buckets {
		if bucket.Type == ConstraintMaxWinFreq && bucket.MaxWinFrequency > 0 {
			// First pass: find the max payout in this range.
			maxP := -1.0
			for _, p := range payouts {
				inRange := p >= bucket.MinPayout && (p < bucket.MaxPayout || (p <= bucket.MaxPayout && bucket.MaxPayout >= payouts[len(payouts)-1]))
				if inRange && p > maxP {
					maxP = p
				}
			}
			if maxP <= 0 {
				continue
			}
			// Second pass: pin every outcome at exactly maxP.
			for i, p := range payouts {
				if p == maxP {
					fixedIndices[i] = true
				}
			}
		}
	}

	var winIndices []int
	var originalTotalWinWeight uint64

	for i, p := range payouts {
		if p > 0 && !fixedIndices[i] {
			if result[i] > 0 { // ignore voided
				winIndices = append(winIndices, i)
				originalTotalWinWeight += result[i]
			}
		}
	}

	if len(winIndices) == 0 {
		return result
	}

	calculateRTPWithAlpha := func(alpha float64) (float64, []uint64) {
		tempWeights := make([]uint64, len(weights))
		copy(tempWeights, weights)
		
		var tiltedSum float64
		tilted := make([]float64, len(payouts))
		for _, i := range winIndices {
			t := float64(weights[i]) * math.Exp(alpha*payouts[i])
			tilted[i] = t
			tiltedSum += t
		}
		
		if tiltedSum == 0 || math.IsNaN(tiltedSum) || math.IsInf(tiltedSum, 0) {
			return calculateRTPFromWeights(tempWeights, payouts), tempWeights
		}
		
		for _, i := range winIndices {
			w := uint64(math.Round(tilted[i] * float64(originalTotalWinWeight) / tiltedSum))
			if w < minWeight {
				w = minWeight
			}
			tempWeights[i] = w
		}
		
		return calculateRTPFromWeights(tempWeights, payouts), tempWeights
	}

	lowAlpha := -1.0
	highAlpha := 1.0
	
	rtpLow, _ := calculateRTPWithAlpha(lowAlpha)
	rtpHigh, _ := calculateRTPWithAlpha(highAlpha)
	
	for i := 0; i < 10; i++ {
		if rtpLow > targetRTP {
			lowAlpha *= 2.0
			rtpLow, _ = calculateRTPWithAlpha(lowAlpha)
		} else {
			break
		}
	}
	
	for i := 0; i < 10; i++ {
		if rtpHigh < targetRTP {
			highAlpha *= 2.0
			rtpHigh, _ = calculateRTPWithAlpha(highAlpha)
		} else {
			break
		}
	}
	
	var bestWeights []uint64
	bestError := math.MaxFloat64
	
	for iter := 0; iter < 50; iter++ {
		midAlpha := (lowAlpha + highAlpha) / 2.0
		rtp, w := calculateRTPWithAlpha(midAlpha)
		err := math.Abs(rtp - targetRTP)
		
		if err < bestError {
			bestError = err
			bestWeights = w
		}
		
		if err <= o.config.RTPTolerance {
			break
		}
		
		if rtp > targetRTP {
			highAlpha = midAlpha
		} else {
			lowAlpha = midAlpha
		}
	}
	
	if bestWeights != nil {
		return bestWeights
	}
	
	return result
}

// calculateLossResult recalculates loss bucket result after fine-tuning
func (o *BucketOptimizer) calculateLossResult(weights []uint64, payouts []float64, lossIndices []int) *BucketResult {
	var totalLossWeight uint64
	for _, idx := range lossIndices {
		totalLossWeight += weights[idx]
	}

	totalWeight := sumUint64(weights)
	lossProb := float64(totalLossWeight) / float64(totalWeight)

	return &BucketResult{
		Name:              "loss",
		MinPayout:         0,
		MaxPayout:         0,
		OutcomeCount:      len(lossIndices),
		ActualProbability: lossProb,
		ActualFrequency:   1.0 / lossProb,
		RTPContribution:   0,
		TotalWeight:       totalLossWeight,
		AvgPayout:         0,
	}
}

// buildOutcomeDetails creates detailed info for each outcome
func (o *BucketOptimizer) buildOutcomeDetails(table *stakergs.LookupTable, payouts []float64, oldWeights, newWeights []uint64, assignments []bucketAssignment, lossIndices []int) []OutcomeDetail {
	totalWeight := sumUint64(newWeights)
	details := make([]OutcomeDetail, len(payouts))

	// Create index to bucket name mapping
	bucketNames := make(map[int]string)
	for _, bucket := range assignments {
		for _, idx := range bucket.outcomeIndices {
			bucketNames[idx] = bucket.config.Name
		}
	}
	for _, idx := range lossIndices {
		bucketNames[idx] = "loss"
	}

	for i := range payouts {
		details[i] = OutcomeDetail{
			SimID:       table.Outcomes[i].SimID,
			Payout:      payouts[i] * table.Cost, // De-normalize
			OldWeight:   oldWeights[i],
			NewWeight:   newWeights[i],
			BucketName:  bucketNames[i],
			Probability: float64(newWeights[i]) / float64(totalWeight),
		}
	}

	return details
}

// calculateWeightsWithVoiding converts probabilities to weights, setting voided outcomes to weight 0
func (o *BucketOptimizer) calculateWeightsWithVoiding(payouts []float64, assignments []bucketAssignment, lossIndices []int, voidedOutcomeIndices []int) ([]uint64, []BucketResult, *BucketResult, []string) {
	n := len(payouts)
	weights := make([]uint64, n)
	var warnings []string

	// Create set of voided outcome indices
	voidedSet := make(map[int]bool)
	for _, idx := range voidedOutcomeIndices {
		voidedSet[idx] = true
	}

	// Use large base for precision
	baseWeight := common.BaseWeight

	// Calculate total win probability and RTP contribution (excluding voided)
	var totalWinProb float64
	var totalWinRTP float64

	bucketResults := make([]BucketResult, 0, len(assignments))

	for _, bucket := range assignments {
		if len(bucket.outcomeIndices) == 0 || bucket.isVoided {
			continue
		}

		var actualTotalWeight uint64

		if bucket.isAuto && len(bucket.outcomeProbs) == len(bucket.outcomeIndices) {
			// Auto bucket: use per-outcome probabilities
			for j, idx := range bucket.outcomeIndices {
				if voidedSet[idx] {
					weights[idx] = 0
					continue
				}
				prob := bucket.outcomeProbs[j]
				w := uint64(prob * float64(baseWeight))
				if w < o.config.MinWeight {
					w = o.config.MinWeight
				}
				weights[idx] = w
				actualTotalWeight += w
			}
		} else {
			// Non-auto bucket: distribute inversely proportional to payout (excluding voided)
			bucketTotalWeight := uint64(bucket.targetProb * float64(baseWeight))
			
			var invSum float64
			nonVoidedCount := 0
			for _, idx := range bucket.outcomeIndices {
				if !voidedSet[idx] {
					nonVoidedCount++
					if payouts[idx] > 0 {
						invSum += 1.0 / payouts[idx]
					}
				}
			}

			if nonVoidedCount > 0 {
				if invSum > 0 {
					for _, idx := range bucket.outcomeIndices {
						if voidedSet[idx] {
							weights[idx] = 0
							continue
						}
						if payouts[idx] <= 0 {
							continue
						}
						fraction := (1.0 / payouts[idx]) / invSum
						w := uint64(float64(bucketTotalWeight) * fraction)
						
						if w < o.config.MinWeight {
							w = o.config.MinWeight
						}
						weights[idx] = w
						actualTotalWeight += w
					}
				} else {
					weightPerOutcome := bucketTotalWeight / uint64(nonVoidedCount)
					if weightPerOutcome < o.config.MinWeight {
						weightPerOutcome = o.config.MinWeight
					}
					for _, idx := range bucket.outcomeIndices {
						if voidedSet[idx] {
							weights[idx] = 0
							continue
						}
						weights[idx] = weightPerOutcome
						actualTotalWeight += weightPerOutcome
					}
				}
			}
		}

		totalWinProb += bucket.targetProb
		totalWinRTP += bucket.rtpContribution

		// Record bucket result
		targetFreq := 0.0
		if bucket.targetProb > 0 {
			targetFreq = 1.0 / bucket.targetProb
		}

		bucketResults = append(bucketResults, BucketResult{
			Name:              bucket.config.Name,
			MinPayout:         bucket.config.MinPayout,
			MaxPayout:         bucket.config.MaxPayout,
			OutcomeCount:      len(bucket.outcomeIndices),
			TargetProbability: bucket.targetProb,
			TargetFrequency:   targetFreq,
			RTPContribution:   bucket.rtpContribution * 100,
			TotalWeight:       actualTotalWeight,
			AvgPayout:         bucket.avgPayout,
		})
	}

	// Apply MaxWinFrequency overrides.
	// Bucket frequency 1:N must reflect the rate of EVERY outcome in the bucket
	// landing combined. The earlier inverse-proportional distribution (the else
	// branch above) seeded weight on every outcome in the bucket, so just
	// overriding the single max-payout outcome would leave residual weight on the
	// rest, inflating the bucket's actual probability (the cause of the
	// "configured 1:10M → observed 1:5.3M" symptom). Reset everything in the
	// bucket to zero, then split the target weight equally among outcomes at the
	// maximum payout (handles duplicates: same payout, distinct simIDs).
	resultIdxByName := make(map[string]int, len(bucketResults))
	for i := range bucketResults {
		resultIdxByName[bucketResults[i].Name] = i
	}
	for _, bucket := range assignments {
		if bucket.config.Type != ConstraintMaxWinFreq || bucket.config.MaxWinFrequency <= 0 || len(bucket.outcomeIndices) == 0 {
			continue
		}

		// Find max payout in this bucket.
		maxPayout := payouts[bucket.outcomeIndices[0]]
		for _, idx := range bucket.outcomeIndices {
			if payouts[idx] > maxPayout {
				maxPayout = payouts[idx]
			}
		}

		// Collect every outcome at exactly maxPayout (duplicates allowed).
		var maxIndices []int
		for _, idx := range bucket.outcomeIndices {
			if payouts[idx] == maxPayout {
				maxIndices = append(maxIndices, idx)
			}
		}
		if len(maxIndices) == 0 {
			continue
		}

		targetProb := 1.0 / bucket.config.MaxWinFrequency
		totalRequiredWeight := targetProb * float64(baseWeight)
		perOutcomeWeight := uint64(math.Round(totalRequiredWeight / float64(len(maxIndices))))
		if perOutcomeWeight < o.config.MinWeight {
			perOutcomeWeight = o.config.MinWeight
		}

		// Zero every outcome in this bucket, then assign weight to max-payout ones.
		// Voided outcomes were already at 0 — keep them that way.
		for _, idx := range bucket.outcomeIndices {
			weights[idx] = 0
		}
		var bucketTotalNew uint64
		for _, idx := range maxIndices {
			if voidedSet[idx] {
				continue
			}
			weights[idx] = perOutcomeWeight
			bucketTotalNew += perOutcomeWeight
		}

		if ri, ok := resultIdxByName[bucket.config.Name]; ok {
			bucketResults[ri].TotalWeight = bucketTotalNew
		}
	}

	// Set voided outcomes to weight 0
	for _, idx := range voidedOutcomeIndices {
		weights[idx] = 0
	}

	// Calculate loss weight strictly based on Hit Rate (totalWinProb)
	var totalWinWeight uint64
	for i, w := range weights {
		if payouts[i] > 0 && w > 0 {
			totalWinWeight += w
		}
	}

	// ENFORCE MINIMUM HIT RATE (1 in 18)
	// The hit rate N cannot be greater than 18 (i.e. probability cannot be less than 1/18).
	minWinProb := 1.0 / 18.0
	if totalWinProb < minWinProb-1e-9 {
		warnings = append(warnings, fmt.Sprintf("Target hit rate (1:%.2f) is less frequent than the allowed minimum (1:18). Hit rate adjusted to 1:18.", 1.0/totalWinProb))
		totalWinProb = minWinProb
	}

	var requiredLossWeight float64
	if totalWinProb < 1.0 && totalWinProb > 0 {
		lossProb := 1.0 - totalWinProb
		requiredLossWeight = float64(totalWinWeight) * (lossProb / totalWinProb)
	} else {
		requiredLossWeight = float64(o.config.MinWeight)
	}

	if requiredLossWeight < float64(o.config.MinWeight) {
		requiredLossWeight = float64(o.config.MinWeight)
	}

	// Distribute loss weight among loss outcomes
	var lossResult *BucketResult
	if len(lossIndices) > 0 {
		lossWeightPerOutcome := uint64(math.Round(requiredLossWeight / float64(len(lossIndices))))
		if lossWeightPerOutcome < o.config.MinWeight {
			lossWeightPerOutcome = o.config.MinWeight
		}

		var totalLossWeight uint64
		for _, idx := range lossIndices {
			weights[idx] = lossWeightPerOutcome
			totalLossWeight += lossWeightPerOutcome
		}

		totalWeight := totalWinWeight + totalLossWeight
		lossProb := float64(totalLossWeight) / float64(totalWeight)

		lossResult = &BucketResult{
			Name:              "loss",
			MinPayout:         0,
			MaxPayout:         0,
			OutcomeCount:      len(lossIndices),
			TargetProbability: 1 - totalWinProb,
			ActualProbability: lossProb,
			TargetFrequency:   1.0 / (1 - totalWinProb),
			ActualFrequency:   1.0 / lossProb,
			RTPContribution:   0,
			TotalWeight:       totalLossWeight,
			AvgPayout:         0,
		}
	}

	// Update bucket results with actual probabilities
	totalWeight := sumUint64(weights)
	for i := range bucketResults {
		if bucketResults[i].TotalWeight > 0 && totalWeight > 0 {
			bucketResults[i].ActualProbability = float64(bucketResults[i].TotalWeight) / float64(totalWeight)
			bucketResults[i].ActualFrequency = 1.0 / bucketResults[i].ActualProbability
			bucketResults[i].RTPContribution = bucketResults[i].ActualProbability * bucketResults[i].AvgPayout * 100
		}
	}

	return weights, bucketResults, lossResult, warnings
}


// buildOutcomeDetailsWithVoiding creates detailed info including voided outcomes
func (o *BucketOptimizer) buildOutcomeDetailsWithVoiding(table *stakergs.LookupTable, payouts []float64, oldWeights, newWeights []uint64, assignments []bucketAssignment, lossIndices []int, voidedOutcomeIndices []int) []OutcomeDetail {
	totalWeight := sumUint64(newWeights)
	details := make([]OutcomeDetail, len(payouts))

	// Create set of voided outcome indices
	voidedSet := make(map[int]bool)
	for _, idx := range voidedOutcomeIndices {
		voidedSet[idx] = true
	}

	// Create index to bucket name mapping
	bucketNames := make(map[int]string)
	for _, bucket := range assignments {
		for _, idx := range bucket.outcomeIndices {
			if bucket.isVoided || voidedSet[idx] {
				bucketNames[idx] = bucket.config.Name + " (voided)"
			} else {
				bucketNames[idx] = bucket.config.Name
			}
		}
	}
	for _, idx := range lossIndices {
		bucketNames[idx] = "loss"
	}

	for i := range payouts {
		prob := 0.0
		if totalWeight > 0 {
			prob = float64(newWeights[i]) / float64(totalWeight)
		}
		details[i] = OutcomeDetail{
			SimID:       table.Outcomes[i].SimID,
			Payout:      payouts[i] * table.Cost,
			OldWeight:   oldWeights[i],
			NewWeight:   newWeights[i],
			BucketName:  bucketNames[i],
			Probability: prob,
		}
	}

	return details
}

// OptimizeFromLoader loads a mode and optimizes it
func (o *BucketOptimizer) OptimizeFromLoader(loader *lut.Loader, mode string) (*BucketOptimizerResult, error) {
	table, err := loader.GetMode(mode)
	if err != nil {
		return nil, fmt.Errorf("failed to load mode %s: %w", mode, err)
	}
	return o.OptimizeTable(table)
}

// ValidateBuckets checks if bucket configuration is valid
func ValidateBuckets(buckets []BucketConfig) error {
	if len(buckets) == 0 {
		return fmt.Errorf("at least one bucket required")
	}

	// Sort by MinPayout
	sorted := make([]BucketConfig, len(buckets))
	copy(sorted, buckets)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].MinPayout < sorted[j].MinPayout
	})

	// Check for gaps and overlaps
	for i := 0; i < len(sorted)-1; i++ {
		if sorted[i].MaxPayout < sorted[i+1].MinPayout {
			return fmt.Errorf("gap between buckets: %.2f-%.2f and %.2f-%.2f",
				sorted[i].MinPayout, sorted[i].MaxPayout,
				sorted[i+1].MinPayout, sorted[i+1].MaxPayout)
		}
		if sorted[i].MaxPayout > sorted[i+1].MinPayout {
			return fmt.Errorf("overlap between buckets: %.2f-%.2f and %.2f-%.2f",
				sorted[i].MinPayout, sorted[i].MaxPayout,
				sorted[i+1].MinPayout, sorted[i+1].MaxPayout)
		}
	}

	// Validate each bucket
	autoCount := 0
	for _, bucket := range buckets {
		if bucket.MinPayout < 0 {
			return fmt.Errorf("bucket %s: min_payout cannot be negative", bucket.Name)
		}
		if bucket.MaxPayout < bucket.MinPayout {
			return fmt.Errorf("bucket %s: max_payout must be >= min_payout", bucket.Name)
		}

		switch bucket.Type {
		case ConstraintFrequency:
			if bucket.Frequency <= 0 {
				return fmt.Errorf("bucket %s: frequency must be > 0", bucket.Name)
			}
		case ConstraintRTPPercent:
			if bucket.RTPPercent <= 0 || bucket.RTPPercent > 100 {
				return fmt.Errorf("bucket %s: rtp_percent must be between 0 and 100", bucket.Name)
			}
		case ConstraintAuto:
			autoCount++
			// AutoExponent can be any real number — sign controls volatility
			// direction (positive → low vol, negative → high vol).
		case ConstraintMaxWinFreq:
			if bucket.MaxWinFrequency <= 0 {
				return fmt.Errorf("bucket %s: max_win_frequency must be > 0", bucket.Name)
			}
		case ConstraintOutcomeFreq:
			// Outcome frequency uses per-outcome constraints, validated separately
		default:
			return fmt.Errorf("bucket %s: unknown constraint type %s", bucket.Name, bucket.Type)
		}

		// Validate priority if set
		if bucket.Priority != 0 && bucket.Priority != PriorityHard && bucket.Priority != PrioritySoft {
			return fmt.Errorf("bucket %s: invalid priority %d (must be 1=hard or 2=soft)", bucket.Name, bucket.Priority)
		}
	}

	// Warn if multiple auto buckets (allowed but unusual)
	if autoCount > 1 {
		// This is fine - multiple auto buckets will share remaining RTP
	}

	return nil
}

// ValidateBruteForceConfig validates brute force optimization config
func ValidateBruteForceConfig(config *BucketOptimizerConfig) error {
	if config.TargetRTP <= 0 || config.TargetRTP > common.MaxOptimizerTargetRTP {
		return fmt.Errorf("target_rtp must be between 0 and %.2f", common.MaxOptimizerTargetRTP)
	}
	if config.RTPTolerance < 0 {
		return fmt.Errorf("rtp_tolerance cannot be negative")
	}
	if config.MaxIterations < 0 {
		return fmt.Errorf("max_iterations cannot be negative")
	}
	// OptimizationMode is no longer validated - runs until converged or stopped
	return nil
}

// SuggestBuckets analyzes a table and suggests bucket configuration
// For high-cost modes (bonus), generates buckets adapted to normalized payouts
// Always creates a separate maxwin bucket for precise control
func SuggestBuckets(table *stakergs.LookupTable, targetRTP float64) []BucketConfig {
	cost := table.Cost
	if cost <= 0 {
		cost = 1.0
	}

	// Find payout ranges (normalized by cost)
	var maxPayout, minPayout float64
	minPayout = math.MaxFloat64
	for _, outcome := range table.Outcomes {
		payout := float64(outcome.Payout) / 100.0 / cost
		if payout > maxPayout {
			maxPayout = payout
		}
		if payout > 0 && payout < minPayout {
			minPayout = payout
		}
	}

	if maxPayout <= 0 {
		return []BucketConfig{}
	}

	var buckets []BucketConfig

	// For bonus modes (cost > 1), all normalized payouts are typically < 2x
	// Generate buckets based on actual payout distribution
	if cost > 1.5 {
		buckets = suggestBonusBuckets(minPayout, maxPayout, targetRTP)
	} else {
		// Standard mode buckets
		buckets = suggestStandardBuckets(maxPayout)
	}

	// Ensure maxwin is always a separate bucket
	buckets = ensureMaxWinBucket(buckets, maxPayout)

	return buckets
}

// ensureMaxWinBucket ensures the max payout has its own dedicated bucket
// Creates a precise bucket that contains ONLY the maximum payout outcome
func ensureMaxWinBucket(buckets []BucketConfig, maxPayout float64) []BucketConfig {
	if len(buckets) == 0 || maxPayout <= 0 {
		return buckets
	}

	// Find the bucket that contains maxPayout
	var containingIdx int = -1
	for i, b := range buckets {
		if maxPayout >= b.MinPayout && maxPayout <= b.MaxPayout {
			containingIdx = i
			break
		}
	}

	// If maxPayout is in the last bucket with very narrow range, just mark it
	if containingIdx >= 0 && buckets[containingIdx].IsMaxWinBucket {
		return buckets
	}

	// Create a precise threshold just below maxPayout
	// Use 0.1% below maxPayout or 0.01 whichever is larger
	epsilon := maxPayout * 0.001
	if epsilon < 0.01 {
		epsilon = 0.01
	}
	maxWinThreshold := maxPayout - epsilon

	// Find which bucket contains the maxPayout and split it
	if containingIdx >= 0 {
		bucket := buckets[containingIdx]

		// If the bucket only contains maxPayout range already, just mark it
		if bucket.MinPayout >= maxWinThreshold {
			buckets[containingIdx].IsMaxWinBucket = true
			buckets[containingIdx].Name = "maxwin"
			return buckets
		}

		// Split the bucket: adjust its max to exclude maxwin
		buckets[containingIdx].MaxPayout = maxWinThreshold
	}

	// Add dedicated maxwin bucket with precise range
	maxwinBucket := BucketConfig{
		Name:           "maxwin",
		MinPayout:      maxWinThreshold,
		MaxPayout:      maxPayout + 0.01, // Tiny margin to ensure inclusion
		Type:           ConstraintMaxWinFreq,
		MaxWinFrequency: 50000, // Default 1:50000 frequency
		IsMaxWinBucket: true,
	}

	buckets = append(buckets, maxwinBucket)
	return buckets
}

// suggestBonusBuckets generates buckets for high-cost modes (bonus)
// where normalized payouts are typically clustered around targetRTP
func suggestBonusBuckets(minPayout, maxPayout, targetRTP float64) []BucketConfig {
	buckets := []BucketConfig{}

	// For bonus modes, payouts are typically 0.5x - 1.5x normalized
	// The distribution is usually tight around the target RTP

	// Split into 3-4 buckets based on actual range
	payoutRange := maxPayout - minPayout
	if payoutRange <= 0 {
		payoutRange = 1.0
	}

	// Low payouts: below target RTP
	// Always start from 0 to catch all positive payouts
	lowThreshold := targetRTP * 0.8
	buckets = append(buckets, BucketConfig{
		Name:         "below_avg",
		MinPayout:    0, // Start from 0 to catch all payouts
		MaxPayout:    lowThreshold,
		Type:         ConstraintAuto,
		AutoExponent: 1.0,
	})

	// Around target RTP (most common)
	midLow := lowThreshold
	midHigh := targetRTP * 1.2
	if midHigh > maxPayout {
		midHigh = maxPayout * 0.9
	}
	buckets = append(buckets, BucketConfig{
		Name:         "around_avg",
		MinPayout:    midLow,
		MaxPayout:    midHigh,
		Type:         ConstraintAuto,
		AutoExponent: 1.0,
	})

	// Above average (good bonus outcomes)
	if maxPayout > midHigh {
		highThreshold := targetRTP * 1.5
		if highThreshold < midHigh {
			highThreshold = midHigh * 1.2
		}
		if highThreshold > maxPayout {
			highThreshold = maxPayout + 0.01
		}

		buckets = append(buckets, BucketConfig{
			Name:       "above_avg",
			MinPayout:  midHigh,
			MaxPayout:  highThreshold,
			Type:       ConstraintRTPPercent,
			RTPPercent: 15, // 15% of RTP for good outcomes
		})

		// Jackpot tier (if exists)
		if maxPayout > highThreshold {
			buckets = append(buckets, BucketConfig{
				Name:       "jackpot",
				MinPayout:  highThreshold,
				MaxPayout:  maxPayout + 0.01,
				Type:       ConstraintRTPPercent,
				RTPPercent: 5, // 5% of RTP for jackpots
			})
		}
	}

	return buckets
}

// suggestStandardBuckets generates buckets for normal modes (cost = 1)
// Updated to generate 10-12 buckets with finer granularity
func suggestStandardBuckets(maxPayout float64) []BucketConfig {
	buckets := []BucketConfig{}

	// Sub-1x wins (0.01-1x) - partial returns
	buckets = append(buckets, BucketConfig{
		Name:      "sub_1x",
		MinPayout: 0.01,
		MaxPayout: 1,
		Type:      ConstraintFrequency,
		Frequency: 3, // 1 in 3
	})

	// Break-even zone: 1x-2x
	if maxPayout >= 2 {
		buckets = append(buckets, BucketConfig{
			Name:      "breakeven",
			MinPayout: 1,
			MaxPayout: 2,
			Type:      ConstraintFrequency,
			Frequency: 5, // 1 in 5
		})
	}

	// Small wins: 2x-5x
	if maxPayout >= 5 {
		buckets = append(buckets, BucketConfig{
			Name:      "small",
			MinPayout: 2,
			MaxPayout: 5,
			Type:      ConstraintFrequency,
			Frequency: 8, // 1 in 8
		})
	}

	// Low-medium wins: 5x-10x
	if maxPayout >= 10 {
		buckets = append(buckets, BucketConfig{
			Name:      "low_med",
			MinPayout: 5,
			MaxPayout: 10,
			Type:      ConstraintFrequency,
			Frequency: 15, // 1 in 15
		})
	}

	// Medium wins: 10x-25x
	if maxPayout >= 25 {
		buckets = append(buckets, BucketConfig{
			Name:      "medium",
			MinPayout: 10,
			MaxPayout: 25,
			Type:      ConstraintFrequency,
			Frequency: 30, // 1 in 30
		})
	}

	// Medium-high wins: 25x-50x
	if maxPayout >= 50 {
		buckets = append(buckets, BucketConfig{
			Name:      "med_high",
			MinPayout: 25,
			MaxPayout: 50,
			Type:      ConstraintFrequency,
			Frequency: 60, // 1 in 60
		})
	}

	// High wins: 50x-100x
	if maxPayout >= 100 {
		buckets = append(buckets, BucketConfig{
			Name:      "high",
			MinPayout: 50,
			MaxPayout: 100,
			Type:      ConstraintFrequency,
			Frequency: 100, // 1 in 100
		})
	}

	// Very high wins: 100x-250x (RTP-based)
	if maxPayout >= 250 {
		buckets = append(buckets, BucketConfig{
			Name:       "very_high",
			MinPayout:  100,
			MaxPayout:  250,
			Type:       ConstraintRTPPercent,
			RTPPercent: 3, // 3% of RTP
		})
	}

	// Huge wins: 250x-500x (RTP-based)
	if maxPayout >= 500 {
		buckets = append(buckets, BucketConfig{
			Name:       "huge",
			MinPayout:  250,
			MaxPayout:  500,
			Type:       ConstraintRTPPercent,
			RTPPercent: 2, // 2% of RTP
		})
	}

	// Massive wins: 500x-1000x (RTP-based)
	if maxPayout >= 1000 {
		buckets = append(buckets, BucketConfig{
			Name:       "massive",
			MinPayout:  500,
			MaxPayout:  1000,
			Type:       ConstraintRTPPercent,
			RTPPercent: 1, // 1% of RTP
		})
	}

	// Epic wins: 1000x-2500x (RTP-based)
	if maxPayout >= 2500 {
		buckets = append(buckets, BucketConfig{
			Name:       "epic",
			MinPayout:  1000,
			MaxPayout:  2500,
			Type:       ConstraintRTPPercent,
			RTPPercent: 0.5, // 0.5% of RTP
		})
	}

	// Jackpot: 2500x+ (RTP-based) - will be split by ensureMaxWinBucket
	if maxPayout >= 2500 {
		buckets = append(buckets, BucketConfig{
			Name:       "jackpot",
			MinPayout:  2500,
			MaxPayout:  maxPayout + 1,
			Type:       ConstraintRTPPercent,
			RTPPercent: 0.3, // 0.3% of RTP
		})
	} else if maxPayout >= 1000 {
		// For smaller max payouts, jackpot starts at 1000x
		buckets = append(buckets, BucketConfig{
			Name:       "jackpot",
			MinPayout:  1000,
			MaxPayout:  maxPayout + 1,
			Type:       ConstraintRTPPercent,
			RTPPercent: 0.5, // 0.5% of RTP
		})
	}

	return buckets
}
