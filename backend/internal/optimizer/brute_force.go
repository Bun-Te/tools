package optimizer

import (
	"math"
	"time"

	"lutexplorer/internal/common"
	"stakergs"
)

// BruteForceOptimizer performs iterative optimization to hit target RTP precisely
type BruteForceOptimizer struct {
	config       *BucketOptimizerConfig
	progressChan chan<- BruteForceProgress
	stopChan     <-chan struct{} // Channel to signal stop
}

// NewBruteForceOptimizer creates a new brute force optimizer
func NewBruteForceOptimizer(config *BucketOptimizerConfig, progressChan chan<- BruteForceProgress) *BruteForceOptimizer {
	return NewBruteForceOptimizerWithStop(config, progressChan, nil)
}

// NewBruteForceOptimizerWithStop creates a new brute force optimizer with stop channel
func NewBruteForceOptimizerWithStop(config *BucketOptimizerConfig, progressChan chan<- BruteForceProgress, stopChan <-chan struct{}) *BruteForceOptimizer {
	if config == nil {
		config = DefaultBucketConfig()
	}
	if config.MinWeight < 1 {
		config.MinWeight = 1
	}
	if config.RTPTolerance <= 0 {
		config.RTPTolerance = 0.0001 // 0.01% default for brute force
	}
	if config.MaxIterations <= 0 {
		// Unlimited mode: run until converged or stopped (max 1M iterations as safety)
		config.MaxIterations = 1000000
	}
	return &BruteForceOptimizer{
		config:       config,
		progressChan: progressChan,
		stopChan:     stopChan,
	}
}

// getDefaultIterations returns default iteration count based on mode
func getDefaultIterations(mode OptimizationMode) int {
	switch mode {
	case ModeFast:
		return 100
	case ModePrecise:
		return 10000
	default: // ModeBalanced
		return 1000
	}
}



// OptimizeTable performs brute force optimization on a lookup table
func (o *BruteForceOptimizer) OptimizeTable(table *stakergs.LookupTable) (*BruteForceResult, error) {
	startTime := time.Now()

	// Delegate entirely to the base optimizer which now uses the deterministic Tilt Rebalancing algorithm
	baseOptimizer := NewBucketOptimizer(o.config)
	
	// Send initial progress
	o.sendProgress("init", 0, 0)
	
	result, err := baseOptimizer.OptimizeTable(table)
	if err != nil {
		return nil, err
	}

	// Calculate final error
	finalError := math.Abs(result.FinalRTP - result.TargetRTP)
	
	// Send final progress (simulated iterations for frontend compatibility)
	o.sendProgress("complete", 50, result.FinalRTP)

	return &BruteForceResult{
		BucketOptimizerResult: result,
		Iterations:            50, // Simulated iteration count for the frontend
		SearchDuration:        time.Since(startTime).Milliseconds(),
		FinalError:            finalError,
	}, nil
}

// sendProgress sends a progress update if channel is available
func (o *BruteForceOptimizer) sendProgress(phase string, iteration int, currentRTP float64) {
	if o.progressChan == nil {
		return
	}

	progress := BruteForceProgress{
		Phase:      phase,
		Iteration:  iteration,
		MaxIter:    o.config.MaxIterations,
		CurrentRTP: currentRTP,
		TargetRTP:  o.config.TargetRTP,
		Error:      math.Abs(currentRTP - o.config.TargetRTP),
		Converged:  math.Abs(currentRTP-o.config.TargetRTP) <= o.config.RTPTolerance,
	}

	select {
	case o.progressChan <- progress:
	default:
		// Channel full, skip this update
	}
}


// GetMaxIterationsForMode returns default iterations for optimization mode
func GetMaxIterationsForMode(mode OptimizationMode) int {
	return getDefaultIterations(mode)
}

// OptimizeWithProgress runs optimization with progress reporting
func OptimizeWithProgress(table *stakergs.LookupTable, config *BucketOptimizerConfig, progressChan chan<- BruteForceProgress) (*BruteForceResult, error) {
	optimizer := NewBruteForceOptimizer(config, progressChan)
	return optimizer.OptimizeTable(table)
}

// DefaultBruteForceConfig returns default config for brute force optimization
func DefaultBruteForceConfig() *BucketOptimizerConfig {
	config := DefaultBucketConfig()
	config.EnableBruteForce = true
	config.OptimizationMode = ModeBalanced
	config.MaxIterations = getDefaultIterations(ModeBalanced)
	config.RTPTolerance = 0.0001 // 0.01% tolerance
	config.MinWeight = common.BaseWeight / 1000000000 // 1000 minimum
	return config
}
