package optimizer

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"lutexplorer/internal/common"
	"lutexplorer/internal/lut"
	"lutexplorer/internal/ws"

	"github.com/gorilla/websocket"
)

// Handlers provides HTTP handlers for the optimizer API
type Handlers struct {
	loader   *lut.Loader
	wsHub    *ws.Hub
	analyzer *ModeAnalyzer
}

// NewHandlers creates new optimizer HTTP handlers
func NewHandlers(loader *lut.Loader, wsHub *ws.Hub) *Handlers {
	return &Handlers{
		loader:   loader,
		wsHub:    wsHub,
		analyzer: NewModeAnalyzer(loader),
	}
}

// ============================================================================
// Apply Endpoint
// ============================================================================

// HandleApply applies weights to the LUT file
// POST /api/optimizer/{mode}/apply
func (h *Handlers) HandleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		common.WriteError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	mode := extractMode(r.URL.Path, "apply")
	if mode == "" {
		common.WriteError(w, http.StatusBadRequest, "mode required")
		return
	}

	var req struct {
		Weights      []uint64 `json:"weights"`
		CreateBackup bool     `json:"create_backup"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.Weights) == 0 {
		common.WriteError(w, http.StatusBadRequest, "weights required")
		return
	}

	var backupPath string
	var err error

	if req.CreateBackup {
		backupPath, err = h.loader.SaveWeightsWithBackup(mode, req.Weights)
	} else {
		err = h.loader.SaveWeights(mode, req.Weights)
	}

	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.notifyLutChanged(mode)

	response := map[string]interface{}{
		"saved":   true,
		"message": "Weights applied successfully",
	}
	if backupPath != "" {
		response["backup_path"] = backupPath
	}

	common.WriteSuccess(w, response)
}

// ============================================================================
// Backup Endpoints
// ============================================================================

// HandleBackups lists available backups for a mode
// GET /api/optimizer/{mode}/backups
func (h *Handlers) HandleBackups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		common.WriteError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}

	mode := extractMode(r.URL.Path, "backups")
	if mode == "" {
		common.WriteError(w, http.StatusBadRequest, "mode required")
		return
	}

	config, err := h.loader.GetModeConfig(mode)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, fmt.Sprintf("mode not found: %s", mode))
		return
	}

	baseDir := h.loader.BaseDir()
	pattern := config.Weights + ".*.bak"

	matches, err := filepath.Glob(filepath.Join(baseDir, pattern))
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type BackupInfo struct {
		Filename  string `json:"filename"`
		Timestamp string `json:"timestamp"`
		Path      string `json:"path"`
	}

	backups := make([]BackupInfo, 0, len(matches))
	for _, match := range matches {
		filename := filepath.Base(match)
		parts := strings.Split(filename, ".")
		timestamp := ""
		if len(parts) >= 3 {
			timestamp = parts[len(parts)-2]
		}

		backups = append(backups, BackupInfo{
			Filename:  filename,
			Timestamp: timestamp,
			Path:      match,
		})
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Timestamp > backups[j].Timestamp
	})

	common.WriteSuccess(w, backups)
}

// HandleRestore restores weights from a backup file
// POST /api/optimizer/{mode}/restore
func (h *Handlers) HandleRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		common.WriteError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	mode := extractMode(r.URL.Path, "restore")
	if mode == "" {
		common.WriteError(w, http.StatusBadRequest, "mode required")
		return
	}

	var req struct {
		BackupFile   string `json:"backup_file"`
		CreateBackup bool   `json:"create_backup"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.BackupFile == "" {
		common.WriteError(w, http.StatusBadRequest, "backup_file required")
		return
	}

	backupPath := req.BackupFile
	if !filepath.IsAbs(backupPath) {
		backupPath = filepath.Join(h.loader.BaseDir(), req.BackupFile)
	}

	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, fmt.Sprintf("backup file not found: %s", err.Error()))
		return
	}

	weights, err := parseWeightsFromCSV(backupData)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("failed to parse backup: %s", err.Error()))
		return
	}

	var preRestoreBackup string
	if req.CreateBackup {
		preRestoreBackup, err = h.loader.SaveWeightsWithBackup(mode, weights)
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create pre-restore backup: %s", err.Error()))
			return
		}
	} else {
		if err := h.loader.SaveWeights(mode, weights); err != nil {
			common.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	h.notifyLutChanged(mode)

	response := map[string]interface{}{
		"restored":      true,
		"restored_from": req.BackupFile,
		"message":       "Weights restored successfully",
	}
	if preRestoreBackup != "" {
		response["pre_restore_backup"] = preRestoreBackup
	}

	common.WriteSuccess(w, response)
}

// ============================================================================
// Utilities
// ============================================================================

func (h *Handlers) notifyLutChanged(mode string) {
	if h.wsHub == nil || mode == "" {
		return
	}
	h.wsHub.Broadcast(ws.Message{
		Type: ws.MsgLUTChangedOnDisk,
		Mode: mode,
		Payload: map[string]string{
			"mode":    mode,
			"message": "Lookup table updated",
		},
	})
}

func parseWeightsFromCSV(data []byte) ([]uint64, error) {
	var weights []uint64
	lines := strings.Split(string(data), "\n")

	for lineNum, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, ",")
		if len(parts) != 3 {
			return nil, fmt.Errorf("line %d: expected 3 fields, got %d", lineNum+1, len(parts))
		}

		weight, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid weight: %w", lineNum+1, err)
		}

		weights = append(weights, weight)
	}

	return weights, nil
}

func extractMode(path, action string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")

	optimizerIdx := -1
	for i, p := range parts {
		if p == "optimizer" {
			optimizerIdx = i
			break
		}
	}

	if optimizerIdx < 0 || optimizerIdx+1 >= len(parts) {
		return ""
	}

	mode := parts[optimizerIdx+1]

	if mode == action || mode == "bucket-presets" || mode == "profiles" || mode == "generate-configs" || mode == "generate-config" {
		return ""
	}

	return mode
}

// getModeNote returns a helpful note about the mode type
func getModeNote(cost float64) string {
	if cost > 1.5 {
		return fmt.Sprintf("Bonus mode (cost=%.0fx). Payouts are normalized: a %.0fx absolute payout = 1.0x normalized.", cost, cost)
	}
	return "Standard mode. Payouts are shown as bet multipliers."
}

// ============================================================================
// Bucket Optimizer Endpoints
// ============================================================================

// BucketOptimizeRequest is the API request for bucket-based optimization
type BucketOptimizeRequest struct {
	TargetRTP           float64          `json:"target_rtp"`                      // Target RTP (e.g., 0.97)
	RTPTolerance        float64          `json:"rtp_tolerance"`                   // Tolerance (e.g., 0.001)
	Buckets             []BucketConfig   `json:"buckets"`                         // Payout range configurations
	SaveToFile          bool             `json:"save_to_file"`                    // Save optimized weights to LUT file
	CreateBackup        bool             `json:"create_backup"`                   // Create backup before saving
	EnableBruteForce    bool             `json:"enable_brute_force,omitempty"`    // Enable iterative brute force search
	MaxIterations       int              `json:"max_iterations,omitempty"`        // Max iterations for brute force
	OptimizationMode    OptimizationMode `json:"optimization_mode,omitempty"`     // "fast"/"balanced"/"precise"
	EnableVoiding       bool             `json:"enable_voiding,omitempty"`        // DEPRECATED: Enable bucket voiding
	VoidedBucketIndices []int            `json:"voided_bucket_indices,omitempty"` // DEPRECATED: Indices of buckets to void
	EnableAutoVoiding   bool             `json:"enable_auto_voiding,omitempty"`   // Enable automatic outcome voiding to reach target RTP
}

// HandleBucketOptimize runs bucket-based optimization on a mode
// POST /api/optimizer/{mode}/bucket-optimize
func (h *Handlers) HandleBucketOptimize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		common.WriteError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	mode := extractMode(r.URL.Path, "bucket-optimize")
	if mode == "" {
		common.WriteError(w, http.StatusBadRequest, "mode required")
		return
	}

	// Parse request
	var req BucketOptimizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %s", err.Error()))
		return
	}

	// Apply defaults
	if req.TargetRTP <= 0 {
		req.TargetRTP = 0.97
	}
	if req.TargetRTP > common.MaxOptimizerTargetRTP {
		req.TargetRTP = common.MaxOptimizerTargetRTP
	}
	if req.RTPTolerance <= 0 {
		req.RTPTolerance = 0.001
	}

	// Validate buckets if provided
	if len(req.Buckets) > 0 {
		if err := ValidateBuckets(req.Buckets); err != nil {
			common.WriteError(w, http.StatusBadRequest, fmt.Sprintf("invalid buckets: %s", err.Error()))
			return
		}
	}

	// Load table
	table, err := h.loader.GetMode(mode)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, fmt.Sprintf("mode not found: %s", mode))
		return
	}

	// If no buckets provided, suggest them based on table
	buckets := req.Buckets
	if len(buckets) == 0 {
		buckets = SuggestBuckets(table, req.TargetRTP)
	}

	// Create optimizer config
	config := &BucketOptimizerConfig{
		TargetRTP:           req.TargetRTP,
		RTPTolerance:        req.RTPTolerance,
		Buckets:             buckets,
		MinWeight:           1,
		EnableBruteForce:    req.EnableBruteForce,
		MaxIterations:       req.MaxIterations,
		OptimizationMode:    req.OptimizationMode,
		EnableVoiding:       req.EnableVoiding,
		VoidedBucketIndices: req.VoidedBucketIndices,
		EnableAutoVoiding:   req.EnableAutoVoiding,
	}

	var result *BucketOptimizerResult
	var bruteForceResult *BruteForceResult

	// Run optimization - use brute force if enabled
	if req.EnableBruteForce {
		// Validate brute force config
		if err := ValidateBruteForceConfig(config); err != nil {
			common.WriteError(w, http.StatusBadRequest, fmt.Sprintf("invalid brute force config: %s", err.Error()))
			return
		}

		bruteForceOpt := NewBruteForceOptimizer(config, nil) // No progress channel for HTTP
		bruteForceResult, err = bruteForceOpt.OptimizeTable(table)
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		result = bruteForceResult.BucketOptimizerResult
	} else {
		optimizer := NewBucketOptimizer(config)
		result, err = optimizer.OptimizeTable(table)
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Save if requested
	var saveInfo map[string]interface{}
	if req.SaveToFile && result.NewWeights != nil {
		if req.CreateBackup {
			backupPath, err := h.loader.SaveWeightsWithBackup(mode, result.NewWeights)
			if err != nil {
				common.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("save failed: %s", err.Error()))
				return
			}
			saveInfo = map[string]interface{}{
				"saved":       true,
				"backup_path": backupPath,
			}
		} else {
			if err := h.loader.SaveWeights(mode, result.NewWeights); err != nil {
				common.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("save failed: %s", err.Error()))
				return
			}
			saveInfo = map[string]interface{}{"saved": true}
		}
		h.notifyLutChanged(mode)
	}

	// Get mode cost and max payout for context
	cost := table.Cost
	if cost <= 0 {
		cost = 1.0
	}
	isBonusMode := cost > 1.5

	// Find max payout (normalized by cost)
	var maxPayout float64
	for _, outcome := range table.Outcomes {
		payout := float64(outcome.Payout) / 100.0 / cost
		if payout > maxPayout {
			maxPayout = payout
		}
	}

	// Build response
	response := map[string]interface{}{
		"original_rtp":    result.OriginalRTP,
		"final_rtp":       result.FinalRTP,
		"target_rtp":      result.TargetRTP,
		"converged":       result.Converged,
		"total_weight":    result.TotalWeight,
		"bucket_results":  result.BucketResults,
		"loss_result":     result.LossResult,
		"warnings":        result.Warnings,
		"outcome_details": result.OutcomeDetails,
		"mode_info": map[string]interface{}{
			"cost":          cost,
			"is_bonus_mode": isBonusMode,
			"note":          getModeNote(cost),
			"max_payout":    maxPayout,
		},
		"config": map[string]interface{}{
			"target_rtp":         req.TargetRTP,
			"buckets":            buckets,
			"enable_brute_force": req.EnableBruteForce,
			"optimization_mode":  req.OptimizationMode,
			"enable_voiding":     req.EnableVoiding,
		},
	}

	// Add voided buckets info if any
	if len(result.VoidedBuckets) > 0 {
		response["voided_buckets"] = result.VoidedBuckets
	}

	// Add brute force specific info if used
	if bruteForceResult != nil {
		response["brute_force_info"] = map[string]interface{}{
			"iterations":      bruteForceResult.Iterations,
			"search_duration": bruteForceResult.SearchDuration,
			"final_error":     bruteForceResult.FinalError,
		}
	}

	if saveInfo != nil {
		response["save_result"] = saveInfo
	}

	common.WriteSuccess(w, response)
}

// HandleBucketPresets returns available bucket presets
// GET /api/optimizer/bucket-presets
func (h *Handlers) HandleBucketPresets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		common.WriteError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}

	// Return several preset configurations
	presets := map[string]interface{}{
		"default": DefaultBucketConfig().Buckets,
		"conservative": []BucketConfig{
			{Name: "sub_1x", MinPayout: 0.01, MaxPayout: 1, Type: ConstraintFrequency, Frequency: 2.5},
			{Name: "small", MinPayout: 1, MaxPayout: 5, Type: ConstraintFrequency, Frequency: 4},
			{Name: "medium", MinPayout: 5, MaxPayout: 20, Type: ConstraintFrequency, Frequency: 15},
			{Name: "large", MinPayout: 20, MaxPayout: 100, Type: ConstraintFrequency, Frequency: 80},
			{Name: "huge", MinPayout: 100, MaxPayout: 1000, Type: ConstraintRTPPercent, RTPPercent: 8},
			{Name: "jackpot", MinPayout: 1000, MaxPayout: 100000, Type: ConstraintRTPPercent, RTPPercent: 1},
		},
		"aggressive": []BucketConfig{
			{Name: "sub_1x", MinPayout: 0.01, MaxPayout: 1, Type: ConstraintFrequency, Frequency: 5},
			{Name: "small", MinPayout: 1, MaxPayout: 5, Type: ConstraintFrequency, Frequency: 10},
			{Name: "medium", MinPayout: 5, MaxPayout: 20, Type: ConstraintFrequency, Frequency: 50},
			{Name: "large", MinPayout: 20, MaxPayout: 100, Type: ConstraintFrequency, Frequency: 200},
			{Name: "huge", MinPayout: 100, MaxPayout: 1000, Type: ConstraintRTPPercent, RTPPercent: 3},
			{Name: "jackpot", MinPayout: 1000, MaxPayout: 100000, Type: ConstraintRTPPercent, RTPPercent: 0.3},
		},
	}

	common.WriteSuccess(w, presets)
}

// HandleSuggestBuckets analyzes a mode and suggests bucket configuration
// GET /api/optimizer/{mode}/suggest-buckets
func (h *Handlers) HandleSuggestBuckets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		common.WriteError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}

	mode := extractMode(r.URL.Path, "suggest-buckets")
	if mode == "" {
		common.WriteError(w, http.StatusBadRequest, "mode required")
		return
	}

	// Load table
	table, err := h.loader.GetMode(mode)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, fmt.Sprintf("mode not found: %s", mode))
		return
	}

	// Get target RTP from query param or use default
	targetRTP := 0.97
	if rtpStr := r.URL.Query().Get("target_rtp"); rtpStr != "" {
		if parsed, err := strconv.ParseFloat(rtpStr, 64); err == nil && parsed > 0 && parsed < 1 {
			targetRTP = parsed
			if targetRTP > common.MaxOptimizerTargetRTP {
				targetRTP = common.MaxOptimizerTargetRTP
			}
		}
	}

	// Suggest buckets
	buckets := SuggestBuckets(table, targetRTP)

	// Also return some table stats
	cost := table.Cost
	if cost <= 0 {
		cost = 1.0
	}
	var maxPayout, minPayout float64
	minPayout = 999999
	payoutCounts := make(map[string]int)

	for _, outcome := range table.Outcomes {
		payout := float64(outcome.Payout) / 100.0 / cost
		if payout > maxPayout {
			maxPayout = payout
		}
		if payout > 0 && payout < minPayout {
			minPayout = payout
		}
		// Categorize
		switch {
		case payout <= 0:
			payoutCounts["loss"]++
		case payout < 1:
			payoutCounts["sub_1x"]++
		case payout < 5:
			payoutCounts["1x-5x"]++
		case payout < 20:
			payoutCounts["5x-20x"]++
		case payout < 100:
			payoutCounts["20x-100x"]++
		case payout < 1000:
			payoutCounts["100x-1000x"]++
		default:
			payoutCounts["1000x+"]++
		}
	}

	isBonusMode := cost > 1.5

	common.WriteSuccess(w, map[string]interface{}{
		"suggested_buckets": buckets,
		"table_stats": map[string]interface{}{
			"outcome_count":  len(table.Outcomes),
			"max_payout":     maxPayout,
			"min_payout":     minPayout,
			"payout_counts":  payoutCounts,
			"current_rtp":    table.RTP(),
		},
		"mode_info": map[string]interface{}{
			"cost":          cost,
			"is_bonus_mode": isBonusMode,
			"note":          getModeNote(cost),
			"max_payout":    maxPayout,
		},
	})
}

// ============================================================================
// Mode Analysis Endpoints
// ============================================================================

// HandleAnalyzeMode analyzes a mode's LUT and returns RTP boundaries and recommendations
// GET /api/optimizer/{mode}/analyze?target_rtp=0.96
func (h *Handlers) HandleAnalyzeMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		common.WriteError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}

	mode := extractMode(r.URL.Path, "analyze")
	if mode == "" {
		common.WriteError(w, http.StatusBadRequest, "mode required")
		return
	}

	// Parse target RTP from query (default 0.96, but allow higher values for extreme modes)
	targetRTP := 0.96
	if rtpStr := r.URL.Query().Get("target_rtp"); rtpStr != "" {
		if parsed, err := strconv.ParseFloat(rtpStr, 64); err == nil && parsed > 0 {
			targetRTP = parsed
			if targetRTP <= 1 && targetRTP > common.MaxOptimizerTargetRTP {
				targetRTP = common.MaxOptimizerTargetRTP
			}
		}
	}

	analysis, err := h.analyzer.AnalyzeMode(mode, targetRTP)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, fmt.Sprintf("failed to analyze mode: %s", err.Error()))
		return
	}

	common.WriteSuccess(w, analysis)
}

// ============================================================================
// Config Generator Endpoints
// ============================================================================

// GenerateConfigRequest is the API request for config generation
type GenerateConfigRequest struct {
	TargetRTP float64       `json:"target_rtp"` // e.g., 0.96
	MaxWin    float64       `json:"max_win"`    // e.g., 5000
	Profile   PlayerProfile `json:"profile"`    // Optional: specific profile
}

// HandleGenerateConfigs generates bucket configs for all profiles
// GET /api/optimizer/generate-configs?target_rtp=0.96&max_win=5000
func (h *Handlers) HandleGenerateConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		common.WriteError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}

	// Parse query params
	targetRTP := 0.96
	if rtpStr := r.URL.Query().Get("target_rtp"); rtpStr != "" {
		if parsed, err := strconv.ParseFloat(rtpStr, 64); err == nil && parsed > 0 && parsed <= 1 {
			targetRTP = parsed
			if targetRTP > common.MaxOptimizerTargetRTP {
				targetRTP = common.MaxOptimizerTargetRTP
			}
		}
	}

	maxWin := 5000.0
	if maxWinStr := r.URL.Query().Get("max_win"); maxWinStr != "" {
		if parsed, err := strconv.ParseFloat(maxWinStr, 64); err == nil && parsed > 0 {
			maxWin = parsed
		}
	}

	maxWinFreq := DefaultMaxWinFreq
	if s := r.URL.Query().Get("max_win_freq"); s != "" {
		if parsed, err := strconv.ParseFloat(s, 64); err == nil && parsed > 0 {
			maxWinFreq = parsed
		}
	}

	generator := NewConfigGenerator()
	response := generator.GenerateAllProfiles(targetRTP, maxWin, maxWinFreq)

	common.WriteSuccess(w, response)
}

// HandleGenerateConfig generates bucket config for a specific profile
// POST /api/optimizer/generate-config
func (h *Handlers) HandleGenerateConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		common.WriteError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	var req GenerateConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %s", err.Error()))
		return
	}

	// Apply defaults
	if req.TargetRTP <= 0 {
		req.TargetRTP = 0.96
	} else if req.TargetRTP > 1 {
		req.TargetRTP = 0.96
	} else if req.TargetRTP > common.MaxOptimizerTargetRTP {
		req.TargetRTP = common.MaxOptimizerTargetRTP
	}
	if req.MaxWin <= 0 {
		req.MaxWin = 5000
	}
	if req.Profile == "" {
		req.Profile = ProfileMediumVol
	}

	generator := NewConfigGenerator()
	config := generator.GenerateConfig(req.TargetRTP, req.MaxWin, DefaultMaxWinFreq, req.Profile)

	// Validate the generated config
	if err := ValidateGeneratedConfig(config); err != nil {
		common.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("generated config invalid: %s", err.Error()))
		return
	}

	common.WriteSuccess(w, config)
}

// HandleGenerateConfigsForMode generates configs based on a mode's actual max payout
// Uses adaptive generation with LUT analysis for extreme RTP modes
// GET /api/optimizer/{mode}/generate-configs?target_rtp=0.96
func (h *Handlers) HandleGenerateConfigsForMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		common.WriteError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}

	mode := extractMode(r.URL.Path, "generate-configs")
	if mode == "" {
		common.WriteError(w, http.StatusBadRequest, "mode required")
		return
	}

	// Load table to get actual max payout and current RTP
	table, err := h.loader.GetMode(mode)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, fmt.Sprintf("mode not found: %s", mode))
		return
	}

	// Calculate max payout from table
	cost := table.Cost
	if cost <= 0 {
		cost = 1.0
	}
	var maxPayout float64
	for _, outcome := range table.Outcomes {
		payout := float64(outcome.Payout) / 100.0 / cost
		if payout > maxPayout {
			maxPayout = payout
		}
	}

	// Parse target RTP from query (allow values > 1 for extreme modes)
	targetRTP := 0.96
	if rtpStr := r.URL.Query().Get("target_rtp"); rtpStr != "" {
		if parsed, err := strconv.ParseFloat(rtpStr, 64); err == nil && parsed > 0 {
			targetRTP = parsed
			if targetRTP <= 1 && targetRTP > common.MaxOptimizerTargetRTP {
				targetRTP = common.MaxOptimizerTargetRTP
			}
		}
	}

	// Parse desired max win hit rate (1:N). Drives RTP budget for the MAX bucket
	// so the rest of the buckets sum exactly to targetRTP - maxWinRTP.
	maxWinFreq := DefaultMaxWinFreq
	if s := r.URL.Query().Get("max_win_freq"); s != "" {
		if parsed, err := strconv.ParseFloat(s, 64); err == nil && parsed > 0 {
			maxWinFreq = parsed
		}
	}

	// Use adaptive generation with analyzer
	generator := NewConfigGeneratorWithAnalyzer(h.analyzer)
	response, genErr := generator.GenerateAllAdaptiveProfiles(mode, targetRTP, maxWinFreq)

	// Fallback to legacy generation on error
	if genErr != nil || response == nil {
		generator := NewConfigGenerator()
		legacyResponse := generator.GenerateAllProfiles(targetRTP, maxPayout, maxWinFreq)
		response = legacyResponse
	}

	// Get analysis for additional info
	analysis, _ := h.analyzer.AnalyzeMode(mode, targetRTP)

	// Build response with mode-specific info
	responseData := map[string]interface{}{
		"mode":        mode,
		"max_payout":  maxPayout,
		"target_rtp":  targetRTP,
		"current_rtp": table.RTP(),
		"configs":     response.Configs,
	}

	// Include analysis info if available
	if analysis != nil {
		analysisData := map[string]interface{}{
			"mode_type":          analysis.Type,
			"feasible":           analysis.Feasible,
			"feasibility_note":   analysis.FeasibilityNote,
			"min_achievable_rtp": analysis.MinAchievableRTP,
			"max_achievable_rtp": analysis.MaxAchievableRTP,
			"suggested_rtp":      analysis.SuggestedRTP,
			"is_bonus_mode":      analysis.IsBonusMode,
		}

		// Calculate void suggestions if RTP is not feasible
		if !analysis.Feasible && analysis.MinAchievableRTP > targetRTP {
			// Get payouts from table
			cost := table.Cost
			if cost <= 0 {
				cost = 1.0
			}
			payouts := make([]float64, len(table.Outcomes))
			for i, outcome := range table.Outcomes {
				payouts[i] = float64(outcome.Payout) / 100.0 / cost
			}

			// Get buckets from response if available
			var buckets []BucketConfig
			if len(response.Configs) > 0 {
				buckets = response.Configs[0].Buckets
			}

			// Calculate suggestions
			voidSuggestions := CalculateVoidSuggestions(buckets, payouts, targetRTP, analysis.MinAchievableRTP)
			if len(voidSuggestions) > 0 {
				analysisData["suggested_void_buckets"] = voidSuggestions
			}
		}

		responseData["analysis"] = analysisData
	}

	common.WriteSuccess(w, responseData)
}

// HandleProfiles returns available player profiles
// GET /api/optimizer/profiles
func (h *Handlers) HandleProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		common.WriteError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}

	profiles := []map[string]interface{}{
		{
			"id":          ProfileLowVol,
			"name":        "Low Volatility",
			"description": ProfileDescriptions[ProfileLowVol],
		},
		{
			"id":          ProfileMediumVol,
			"name":        "Medium Volatility",
			"description": ProfileDescriptions[ProfileMediumVol],
		},
		{
			"id":          ProfileHighVol,
			"name":        "High Volatility",
			"description": ProfileDescriptions[ProfileHighVol],
		},
	}

	common.WriteSuccess(w, profiles)
}

// WebSocket upgrader for optimizer streaming
var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// WSProgressMessage is the WebSocket message format for optimization progress
type WSProgressMessage struct {
	Type       string  `json:"type"`        // "progress" | "result" | "error"
	Phase      string  `json:"phase"`       // "init" | "search" | "refine" | "complete"
	Iteration  int     `json:"iteration"`   // Current iteration
	MaxIter    int     `json:"max_iter"`    // Maximum iterations
	CurrentRTP float64 `json:"current_rtp"` // Current RTP
	TargetRTP  float64 `json:"target_rtp"`  // Target RTP
	Error      float64 `json:"error"`       // Current error
	Converged  bool    `json:"converged"`   // Whether converged
	ElapsedMs  int64   `json:"elapsed_ms"`  // Elapsed time
}

// WSResultMessage is the WebSocket message for final result
type WSResultMessage struct {
	Type   string      `json:"type"` // "result"
	Result interface{} `json:"result"`
}

// WSErrorMessage is the WebSocket message for errors
type WSErrorMessage struct {
	Type    string `json:"type"` // "error"
	Message string `json:"message"`
}

// HandleBruteForceOptimizeWS handles WebSocket connection for brute force optimization with streaming progress
// WS /api/optimizer/{mode}/optimize-stream
func (h *Handlers) HandleBruteForceOptimizeWS(w http.ResponseWriter, r *http.Request) {
	mode := extractMode(r.URL.Path, "optimize-stream")
	if mode == "" {
		common.WriteError(w, http.StatusBadRequest, "mode required")
		return
	}

	// Upgrade to WebSocket
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Read config from first message
	_, message, err := conn.ReadMessage()
	if err != nil {
		return
	}

	var req BucketOptimizeRequest
	if err := json.Unmarshal(message, &req); err != nil {
		conn.WriteJSON(WSErrorMessage{Type: "error", Message: "invalid request: " + err.Error()})
		return
	}

	// Apply defaults
	if req.TargetRTP <= 0 {
		req.TargetRTP = 0.97
	}
	if req.TargetRTP > common.MaxOptimizerTargetRTP {
		req.TargetRTP = common.MaxOptimizerTargetRTP
	}
	if req.RTPTolerance <= 0 {
		req.RTPTolerance = 0.0001 // Higher precision for brute force
	}

	// Load table
	table, err := h.loader.GetMode(mode)
	if err != nil {
		conn.WriteJSON(WSErrorMessage{Type: "error", Message: "mode not found: " + mode})
		return
	}

	// Validate and prepare buckets
	buckets := req.Buckets
	if len(buckets) > 0 {
		if err := ValidateBuckets(buckets); err != nil {
			conn.WriteJSON(WSErrorMessage{Type: "error", Message: "invalid buckets: " + err.Error()})
			return
		}
	} else {
		buckets = SuggestBuckets(table, req.TargetRTP)
	}

	// Create optimizer config
	config := &BucketOptimizerConfig{
		TargetRTP:           req.TargetRTP,
		RTPTolerance:        req.RTPTolerance,
		Buckets:             buckets,
		MinWeight:           1,
		EnableBruteForce:    true, // Always true for WS endpoint
		MaxIterations:       req.MaxIterations,
		OptimizationMode:    req.OptimizationMode,
		EnableVoiding:       req.EnableVoiding,
		VoidedBucketIndices: req.VoidedBucketIndices,
	}

	// Validate config
	if err := ValidateBruteForceConfig(config); err != nil {
		conn.WriteJSON(WSErrorMessage{Type: "error", Message: "invalid config: " + err.Error()})
		return
	}

	// Create channels
	progressChan := make(chan BruteForceProgress, 100)
	stopChan := make(chan struct{})
	defer close(progressChan)

	// Start goroutine to listen for stop messages from client
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return // Connection closed
			}
			// Check if it's a stop command
			var cmd struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(msg, &cmd) == nil && cmd.Type == "stop" {
				close(stopChan)
				return
			}
		}
	}()

	// Start optimization in goroutine
	resultChan := make(chan *BruteForceResult, 1)
	errChan := make(chan error, 1)
	startTime := time.Now()

	go func() {
		optimizer := NewBruteForceOptimizerWithStop(config, progressChan, stopChan)
		result, err := optimizer.OptimizeTable(table)
		if err != nil {
			errChan <- err
			return
		}
		resultChan <- result
	}()

	// Stream progress updates
	for {
		select {
		case progress := <-progressChan:
			msg := WSProgressMessage{
				Type:       "progress",
				Phase:      progress.Phase,
				Iteration:  progress.Iteration,
				MaxIter:    progress.MaxIter,
				CurrentRTP: progress.CurrentRTP,
				TargetRTP:  progress.TargetRTP,
				Error:      progress.Error,
				Converged:  progress.Converged,
				ElapsedMs:  time.Since(startTime).Milliseconds(),
			}
			if err := conn.WriteJSON(msg); err != nil {
				return
			}

			// Broadcast to all WebSocket clients via hub
			if h.wsHub != nil {
				h.wsHub.Broadcast(ws.Message{
					Type: ws.MsgOptimizerProgress,
					Mode: mode,
					Payload: map[string]interface{}{
						"phase":       progress.Phase,
						"iteration":   progress.Iteration,
						"max_iter":    progress.MaxIter,
						"current_rtp": progress.CurrentRTP,
						"target_rtp":  progress.TargetRTP,
						"error":       progress.Error,
						"converged":   progress.Converged,
					},
				})
			}

		case result := <-resultChan:
			// Save if requested
			var saveInfo map[string]interface{}
			if req.SaveToFile && result.NewWeights != nil {
				if req.CreateBackup {
					backupPath, err := h.loader.SaveWeightsWithBackup(mode, result.NewWeights)
					if err != nil {
						conn.WriteJSON(WSErrorMessage{Type: "error", Message: "save failed: " + err.Error()})
						return
					}
					saveInfo = map[string]interface{}{
						"saved":       true,
						"backup_path": backupPath,
					}
				} else {
					if err := h.loader.SaveWeights(mode, result.NewWeights); err != nil {
						conn.WriteJSON(WSErrorMessage{Type: "error", Message: "save failed: " + err.Error()})
						return
					}
					saveInfo = map[string]interface{}{"saved": true}
				}
				h.notifyLutChanged(mode)
			}

			// Get mode cost and max payout for context
			cost := table.Cost
			if cost <= 0 {
				cost = 1.0
			}
			isBonusMode := cost > 1.5

			// Find max payout (normalized by cost)
			var maxPayout float64
			for _, outcome := range table.Outcomes {
				payout := float64(outcome.Payout) / 100.0 / cost
				if payout > maxPayout {
					maxPayout = payout
				}
			}

			// Build response
			response := map[string]interface{}{
				"original_rtp":   result.OriginalRTP,
				"final_rtp":      result.FinalRTP,
				"target_rtp":     result.TargetRTP,
				"converged":      result.Converged,
				"total_weight":   result.TotalWeight,
				"bucket_results": result.BucketResults,
				"loss_result":    result.LossResult,
				"warnings":       result.Warnings,
				"mode_info": map[string]interface{}{
					"cost":          cost,
					"is_bonus_mode": isBonusMode,
					"note":          getModeNote(cost),
					"max_payout":    maxPayout,
				},
				"brute_force_info": map[string]interface{}{
					"iterations":      result.Iterations,
					"search_duration": result.SearchDuration,
					"final_error":     result.FinalError,
				},
			}
			if saveInfo != nil {
				response["save_result"] = saveInfo
			}
			// Add voided buckets info if any
			if len(result.VoidedBuckets) > 0 {
				response["voided_buckets"] = result.VoidedBuckets
			}

			conn.WriteJSON(WSResultMessage{Type: "result", Result: response})

			// Broadcast completion
			if h.wsHub != nil {
				h.wsHub.Broadcast(ws.Message{
					Type: ws.MsgOptimizerComplete,
					Mode: mode,
					Payload: map[string]interface{}{
						"final_rtp":  result.FinalRTP,
						"target_rtp": result.TargetRTP,
						"converged":  result.Converged,
						"iterations": result.Iterations,
					},
				})
			}
			return

		case err := <-errChan:
			conn.WriteJSON(WSErrorMessage{Type: "error", Message: err.Error()})
			if h.wsHub != nil {
				h.wsHub.Broadcast(ws.Message{
					Type: ws.MsgOptimizerError,
					Mode: mode,
					Payload: map[string]interface{}{
						"error": err.Error(),
					},
				})
			}
			return
		}
	}
}

// RegisterRoutes registers all optimizer routes
func (h *Handlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/optimizer/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		switch {
		// General endpoints
		case strings.HasSuffix(path, "/apply"):
			h.HandleApply(w, r)
		case strings.HasSuffix(path, "/backups"):
			h.HandleBackups(w, r)
		case strings.HasSuffix(path, "/restore"):
			h.HandleRestore(w, r)

		// Mode analysis endpoint
		case strings.HasSuffix(path, "/analyze"):
			h.HandleAnalyzeMode(w, r)

		// Bucket optimizer endpoints
		case strings.HasSuffix(path, "/bucket-optimize"):
			h.HandleBucketOptimize(w, r)
		case strings.HasSuffix(path, "/optimize-stream"):
			h.HandleBruteForceOptimizeWS(w, r)
		case strings.HasSuffix(path, "/suggest-buckets"):
			h.HandleSuggestBuckets(w, r)
		case path == "/api/optimizer/bucket-presets":
			h.HandleBucketPresets(w, r)

		// Config generator endpoints
		case path == "/api/optimizer/generate-configs":
			h.HandleGenerateConfigs(w, r)
		case path == "/api/optimizer/generate-config":
			h.HandleGenerateConfig(w, r)
		case path == "/api/optimizer/profiles":
			h.HandleProfiles(w, r)
		case strings.HasSuffix(path, "/generate-configs"):
			h.HandleGenerateConfigsForMode(w, r)

		default:
			common.WriteError(w, http.StatusNotFound, "endpoint not found")
		}
	})
}
