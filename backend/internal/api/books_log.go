package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"lutexplorer/internal/common"
	"stakergs"
)

const (
	defaultAvgTarget = 2.0
	defaultTargetWin = 50.0
	defaultTolerance = 12.0
	minTolerancePct  = 10.0
	maxTolerancePct  = 15.0
)

type BooksLogCounts struct {
	MaxWin int `json:"maxwin"`
	Avg    int `json:"avg"`
	Target int `json:"target"`
}

type ModeBooksLogResult struct {
	Mode      string         `json:"mode"`
	MaxPayout float64        `json:"max_payout"`
	Counts    BooksLogCounts `json:"counts"`
	MaxWinIDs []int          `json:"maxwin_ids"`
	AvgIDs    []int          `json:"avg_ids"`
	TargetIDs []int          `json:"target_ids"`
}

type ModeBooksLogResponse struct {
	Timestamp    string             `json:"timestamp"`
	AvgTarget    float64            `json:"avg_target"`
	TargetWin    float64            `json:"target_win"`
	TolerancePct float64            `json:"tolerance_pct"`
	Result       ModeBooksLogResult `json:"result"`
	Log          string             `json:"log"`
}

type AllModesBooksLogResponse struct {
	Timestamp    string               `json:"timestamp"`
	AvgTarget    float64              `json:"avg_target"`
	TargetWin    float64              `json:"target_win"`
	TolerancePct float64              `json:"tolerance_pct"`
	ModeCount    int                  `json:"mode_count"`
	Results      []ModeBooksLogResult `json:"results"`
	Log          string               `json:"log"`
}

func parseBooksLogParams(r *http.Request) (float64, float64, float64, error) {
	query := r.URL.Query()

	avgTarget := defaultAvgTarget
	if raw := query.Get("avg_target"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("invalid avg_target: %w", err)
		}
		if parsed <= 0 {
			return 0, 0, 0, fmt.Errorf("avg_target must be > 0")
		}
		avgTarget = parsed
	}

	targetWin := defaultTargetWin
	if raw := query.Get("target_win"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("invalid target_win: %w", err)
		}
		if parsed <= 0 {
			return 0, 0, 0, fmt.Errorf("target_win must be > 0")
		}
		targetWin = parsed
	}

	tolerancePct := defaultTolerance
	if raw := query.Get("tolerance_pct"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("invalid tolerance_pct: %w", err)
		}
		tolerancePct = parsed
	}
	if tolerancePct < minTolerancePct {
		tolerancePct = minTolerancePct
	}
	if tolerancePct > maxTolerancePct {
		tolerancePct = maxTolerancePct
	}

	return avgTarget, targetWin, tolerancePct, nil
}

func payoutBand(center float64, tolerancePct float64) (float64, float64) {
	f := tolerancePct / 100.0
	return center * (1 - f), center * (1 + f)
}

// maxWinRepresentativeIDs returns at most one sim_id: the smallest sim_id among rows at the
// global max payout. Tolerance does not apply here—maxwin means the top multiplier in the LUT.
func maxWinRepresentativeIDs(table *stakergs.LookupTable) []int {
	if len(table.Outcomes) == 0 {
		return nil
	}
	maxP := table.MaxPayout()
	var bestID int
	found := false
	for i := range table.Outcomes {
		o := &table.Outcomes[i]
		if o.Payout != maxP {
			continue
		}
		if !found || o.SimID < bestID {
			found = true
			bestID = o.SimID
		}
	}
	if !found {
		return nil
	}
	return []int{bestID}
}

// closestSimIDInBand picks the single outcome in the payout tolerance band whose multiplier is
// closest to center; ties break on smaller sim_id. LUT rows often have unique payouts, so
// “one id per payout” still produced huge lists—this returns one representative per line.
func closestSimIDInBand(table *stakergs.LookupTable, center float64, tolerancePct float64) []int {
	minPayout, maxPayout := payoutBand(center, tolerancePct)
	var bestID int
	var bestDist float64
	found := false
	for i := range table.Outcomes {
		o := &table.Outcomes[i]
		payout := float64(o.Payout) / 100.0
		if payout < minPayout || payout > maxPayout {
			continue
		}
		dist := payout - center
		if dist < 0 {
			dist = -dist
		}
		if !found || dist < bestDist-1e-9 || (dist <= bestDist+1e-9 && o.SimID < bestID) {
			found = true
			bestDist = dist
			bestID = o.SimID
		}
	}
	if !found {
		return nil
	}
	return []int{bestID}
}

func formatMultiplier(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", value), "0"), ".")
}

func formatIDsLine(ids []int) string {
	if len(ids) == 0 {
		return "-"
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, ",")
}

func buildLogText(avgTarget float64, targetWin float64, rows []ModeBooksLogResult) string {
	var sb strings.Builder
	for i, row := range rows {
		sb.WriteString(fmt.Sprintf("mode=%s\n", row.Mode))
		sb.WriteString(fmt.Sprintf("maxwin: %s\n", formatIDsLine(row.MaxWinIDs)))
		sb.WriteString(fmt.Sprintf("avg(~%sx): %s\n", formatMultiplier(avgTarget), formatIDsLine(row.AvgIDs)))
		sb.WriteString(fmt.Sprintf("target(~%sx): %s", formatMultiplier(targetWin), formatIDsLine(row.TargetIDs)))
		if i < len(rows)-1 {
			sb.WriteString("\n\n")
		}
	}
	return sb.String()
}

func buildModeBooksLogResult(mode string, table *stakergs.LookupTable, avgTarget float64, targetWin float64, tolerancePct float64) ModeBooksLogResult {
	maxPayout := float64(table.MaxPayout()) / 100.0
	maxWinIDs := maxWinRepresentativeIDs(table)
	avgIDs := closestSimIDInBand(table, avgTarget, tolerancePct)
	targetIDs := closestSimIDInBand(table, targetWin, tolerancePct)

	return ModeBooksLogResult{
		Mode:      mode,
		MaxPayout: maxPayout,
		Counts: BooksLogCounts{
			MaxWin: len(maxWinIDs),
			Avg:    len(avgIDs),
			Target: len(targetIDs),
		},
		MaxWinIDs: maxWinIDs,
		AvgIDs:    avgIDs,
		TargetIDs: targetIDs,
	}
}

func (s *Server) handleModeBooksLog(w http.ResponseWriter, r *http.Request) {
	mode := r.PathValue("mode")
	if mode == "" {
		common.WriteError(w, http.StatusBadRequest, "mode parameter required")
		return
	}

	avgTarget, targetWin, tolerancePct, err := parseBooksLogParams(r)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	table, err := s.loader.GetMode(mode)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, err.Error())
		return
	}

	row := buildModeBooksLogResult(mode, table, avgTarget, targetWin, tolerancePct)
	timestamp := time.Now().UTC().Format(time.RFC3339)

	common.WriteSuccess(w, ModeBooksLogResponse{
		Timestamp:    timestamp,
		AvgTarget:    avgTarget,
		TargetWin:    targetWin,
		TolerancePct: tolerancePct,
		Result:       row,
		Log:          buildLogText(avgTarget, targetWin, []ModeBooksLogResult{row}),
	})
}

func (s *Server) handleAllModesBooksLog(w http.ResponseWriter, r *http.Request) {
	avgTarget, targetWin, tolerancePct, err := parseBooksLogParams(r)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	modeNames := s.loader.ListModes()
	rows := make([]ModeBooksLogResult, 0, len(modeNames))
	for _, mode := range modeNames {
		table, err := s.loader.GetMode(mode)
		if err != nil {
			continue
		}
		rows = append(rows, buildModeBooksLogResult(mode, table, avgTarget, targetWin, tolerancePct))
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	common.WriteSuccess(w, AllModesBooksLogResponse{
		Timestamp:    timestamp,
		AvgTarget:    avgTarget,
		TargetWin:    targetWin,
		TolerancePct: tolerancePct,
		ModeCount:    len(rows),
		Results:      rows,
		Log:          buildLogText(avgTarget, targetWin, rows),
	})
}
