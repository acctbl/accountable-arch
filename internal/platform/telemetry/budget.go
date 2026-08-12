package telemetry

import (
	"math/big"
	"slices"
	"time"
)

const (
	budgetPeriod        = 5 * time.Minute
	budgetWindow        = 30 * 24 * time.Hour
	budgetEvaluationLag = 10 * time.Minute
	stagingWindow       = 7 * 24 * time.Hour
	stagingDailyMinimum = int64(100)
)

type BudgetState string

const (
	BudgetUnknown     BudgetState = "unknown"
	BudgetNoData      BudgetState = "no_data"
	BudgetProvisional BudgetState = "provisional"
	BudgetComplete    BudgetState = "complete"
)

type BudgetPeriod struct {
	StartedAt       time.Time `json:"started_at"`
	Eligible        *int64    `json:"eligible,omitempty"`
	AvailabilityBad *int64    `json:"availability_bad,omitempty"`
	LatencySlow     *int64    `json:"latency_slow,omitempty"`
	RequiredCells   []string  `json:"required_cells"`
	HeartbeatCells  []string  `json:"heartbeat_cells"`
}

type BudgetInput struct {
	Now               time.Time      `json:"now"`
	WindowStart       time.Time      `json:"window_start"`
	WindowEnd         time.Time      `json:"window_end"`
	Periods           []BudgetPeriod `json:"periods"`
	DeliveryUncertain bool           `json:"delivery_uncertain"`
}

type BudgetMetricPoint struct {
	StartedAt time.Time `json:"started_at"`
	Value     int64     `json:"value"`
}

type BudgetSnapshot struct {
	Now               time.Time                      `json:"now"`
	WindowStart       time.Time                      `json:"window_start"`
	WindowEnd         time.Time                      `json:"window_end"`
	ExpectedCells     []string                       `json:"expected_cells"`
	Inventory         map[string][]InventoryRecord   `json:"inventory"`
	Eligible          []BudgetMetricPoint            `json:"eligible"`
	AvailabilityBad   []BudgetMetricPoint            `json:"availability_bad"`
	LatencySlow       []BudgetMetricPoint            `json:"latency_slow"`
	Heartbeats        map[string][]BudgetMetricPoint `json:"heartbeats"`
	DeliveryUncertain bool                           `json:"delivery_uncertain"`
}

type BudgetResult struct {
	State                            BudgetState `json:"state"`
	Eligible                         int64       `json:"eligible"`
	AvailabilityBad                  int64       `json:"availability_bad"`
	LatencySlow                      int64       `json:"latency_slow"`
	AvailabilityRemainingBasisPoints int64       `json:"availability_remaining_basis_points"`
	LatencyRemainingBasisPoints      int64       `json:"latency_remaining_basis_points"`
}

type StagingObservationDay struct {
	StartedAt string `json:"started_at"`
	Eligible  int64  `json:"eligible"`
}

type StagingObservationResult struct {
	State GateState               `json:"state"`
	Days  []StagingObservationDay `json:"days"`
}

func EvaluateBudget(input BudgetInput) BudgetResult {
	unknown := BudgetResult{State: BudgetUnknown}
	if input.DeliveryUncertain || !validBudgetWindow(input) {
		return unknown
	}
	var eligible, bad, slow int64
	for index, period := range input.Periods {
		if !period.StartedAt.Equal(input.WindowStart.Add(time.Duration(index)*budgetPeriod)) ||
			!sameStringSet(period.RequiredCells, period.HeartbeatCells) {
			return unknown
		}
		if len(period.RequiredCells) == 0 {
			if period.Eligible != nil || period.AvailabilityBad != nil || period.LatencySlow != nil {
				return unknown
			}
			continue
		}
		periodEligible := int64(0)
		if period.Eligible != nil {
			periodEligible = *period.Eligible
		}
		periodBad := int64(0)
		if period.AvailabilityBad != nil {
			periodBad = *period.AvailabilityBad
		}
		periodSlow := int64(0)
		if period.LatencySlow != nil {
			periodSlow = *period.LatencySlow
		}
		if periodEligible < 0 || periodBad < 0 || periodSlow < 0 || periodBad > periodEligible || periodSlow > periodEligible {
			return unknown
		}
		eligible += periodEligible
		bad += periodBad
		slow += periodSlow
	}
	result := BudgetResult{Eligible: eligible, AvailabilityBad: bad, LatencySlow: slow}
	if eligible == 0 {
		result.State = BudgetNoData
		return result
	}
	result.State = BudgetProvisional
	if input.WindowEnd.Sub(input.WindowStart) == budgetWindow {
		result.State = BudgetComplete
	}
	result.AvailabilityRemainingBasisPoints = remainingBudgetBasisPoints(bad, eligible, 1000)
	result.LatencyRemainingBasisPoints = remainingBudgetBasisPoints(slow, eligible, 100)
	return result
}

func EvaluateBudgetSnapshot(snapshot BudgetSnapshot) BudgetResult {
	input, valid := budgetInputFromSnapshot(snapshot)
	if !valid {
		return BudgetResult{State: BudgetUnknown}
	}
	return EvaluateBudget(input)
}

func EvaluateStagingObservation(input BudgetInput) StagingObservationResult {
	unknown := StagingObservationResult{State: GateUnknown}
	if input.WindowEnd.Sub(input.WindowStart) != stagingWindow || !utcDayBoundary(input.WindowStart) || !utcDayBoundary(input.WindowEnd) {
		return unknown
	}
	if result := EvaluateBudget(input); result.State == BudgetUnknown {
		return unknown
	}

	const periodsPerDay = 24 * time.Hour / budgetPeriod
	days := make([]StagingObservationDay, stagingWindow/(24*time.Hour))
	state := GatePass
	for index := range days {
		days[index].StartedAt = input.WindowStart.Add(time.Duration(index) * 24 * time.Hour).Format(time.RFC3339)
	}
	for index, period := range input.Periods {
		day := index / int(periodsPerDay)
		if len(period.RequiredCells) == 0 {
			state = GateFail
			continue
		}
		if period.Eligible != nil {
			days[day].Eligible += *period.Eligible
		}
	}
	for _, day := range days {
		if day.Eligible < stagingDailyMinimum {
			state = GateFail
		}
	}
	return StagingObservationResult{State: state, Days: days}
}

func EvaluateStagingObservationSnapshot(snapshot BudgetSnapshot) StagingObservationResult {
	input, valid := budgetInputFromSnapshot(snapshot)
	if !valid {
		return StagingObservationResult{State: GateUnknown}
	}
	return EvaluateStagingObservation(input)
}

func budgetInputFromSnapshot(snapshot BudgetSnapshot) (BudgetInput, bool) {
	if !snapshot.WindowStart.Before(snapshot.WindowEnd) {
		return BudgetInput{}, false
	}
	periodCount := int(snapshot.WindowEnd.Sub(snapshot.WindowStart) / budgetPeriod)
	input := BudgetInput{
		Now: snapshot.Now, WindowStart: snapshot.WindowStart, WindowEnd: snapshot.WindowEnd,
		DeliveryUncertain: snapshot.DeliveryUncertain, Periods: make([]BudgetPeriod, periodCount),
	}
	if !validBudgetWindow(input) {
		return BudgetInput{}, false
	}
	inventoryCells := make([]string, 0, len(snapshot.Inventory))
	for cellID := range snapshot.Inventory {
		inventoryCells = append(inventoryCells, cellID)
	}
	if len(snapshot.ExpectedCells) == 0 || !sameStringSet(snapshot.ExpectedCells, inventoryCells) {
		return BudgetInput{}, false
	}
	inventory := make(map[string][]InventoryRecord, len(snapshot.Inventory))
	for cellID, records := range snapshot.Inventory {
		if len(records) == 0 || records[0].CellID != cellID || ValidateInventoryChain(records) != nil {
			return BudgetInput{}, false
		}
		for _, record := range records {
			if record.CellID != cellID {
				return BudgetInput{}, false
			}
		}
		inventory[cellID] = records
	}
	eligible, ok := budgetPointIndex(snapshot.Eligible, snapshot.WindowStart, snapshot.WindowEnd)
	if !ok {
		return BudgetInput{}, false
	}
	bad, ok := budgetPointIndex(snapshot.AvailabilityBad, snapshot.WindowStart, snapshot.WindowEnd)
	if !ok {
		return BudgetInput{}, false
	}
	slow, ok := budgetPointIndex(snapshot.LatencySlow, snapshot.WindowStart, snapshot.WindowEnd)
	if !ok {
		return BudgetInput{}, false
	}
	heartbeats := make(map[string]map[int64]int64, len(snapshot.Heartbeats))
	for cellID, points := range snapshot.Heartbeats {
		if _, known := inventory[cellID]; !known {
			return BudgetInput{}, false
		}
		indexed, valid := budgetPointIndex(points, snapshot.WindowStart, snapshot.WindowEnd)
		if !valid {
			return BudgetInput{}, false
		}
		heartbeats[cellID] = indexed
	}
	input.Periods = make([]BudgetPeriod, 0, periodCount)
	for index := range periodCount {
		startedAt := snapshot.WindowStart.Add(time.Duration(index) * budgetPeriod)
		key := startedAt.Unix()
		period := BudgetPeriod{StartedAt: startedAt}
		for cellID, records := range inventory {
			if inventoryStateAt(records, startedAt) == "active" {
				period.RequiredCells = append(period.RequiredCells, cellID)
				if heartbeats[cellID][key] > 0 {
					period.HeartbeatCells = append(period.HeartbeatCells, cellID)
				}
			}
		}
		slices.Sort(period.RequiredCells)
		slices.Sort(period.HeartbeatCells)
		if len(period.RequiredCells) > 0 {
			period.Eligible = budgetPointPointer(eligible, key)
			period.AvailabilityBad = budgetPointPointer(bad, key)
			period.LatencySlow = budgetPointPointer(slow, key)
		} else if eligible[key] != 0 || bad[key] != 0 || slow[key] != 0 {
			return BudgetInput{}, false
		}
		input.Periods = append(input.Periods, period)
	}
	return input, true
}

func utcDayBoundary(instant time.Time) bool {
	return instant.Location() == time.UTC && instant.Hour() == 0 && instant.Minute() == 0 && instant.Second() == 0 && instant.Nanosecond() == 0
}

func budgetPointIndex(points []BudgetMetricPoint, start, end time.Time) (map[int64]int64, bool) {
	indexed := make(map[int64]int64, len(points))
	for _, point := range points {
		if point.Value < 0 || point.StartedAt.Before(start) || !point.StartedAt.Before(end) || point.StartedAt.Unix()%300 != 0 {
			return nil, false
		}
		key := point.StartedAt.Unix()
		if _, duplicate := indexed[key]; duplicate {
			return nil, false
		}
		indexed[key] = point.Value
	}
	return indexed, true
}

func budgetPointPointer(points map[int64]int64, key int64) *int64 {
	value, present := points[key]
	if !present {
		return nil
	}
	return &value
}

func inventoryStateAt(records []InventoryRecord, instant time.Time) string {
	state := ""
	for _, record := range records {
		effectiveAt, _ := time.Parse(time.RFC3339, record.EffectiveAt)
		if effectiveAt.After(instant) {
			break
		}
		state = record.State
	}
	return state
}

func validBudgetWindow(input BudgetInput) bool {
	if input.Now.IsZero() || input.WindowStart.IsZero() || input.WindowEnd.IsZero() ||
		!input.WindowStart.Before(input.WindowEnd) || input.WindowStart.Unix()%300 != 0 || input.WindowEnd.Unix()%300 != 0 ||
		input.WindowEnd.After(input.Now.Add(-budgetEvaluationLag)) || input.WindowEnd.Sub(input.WindowStart) > budgetWindow {
		return false
	}
	expected := int(input.WindowEnd.Sub(input.WindowStart) / budgetPeriod)
	return expected > 0 && len(input.Periods) == expected
}

func sameStringSet(left, right []string) bool {
	leftCopy := slices.Clone(left)
	rightCopy := slices.Clone(right)
	slices.Sort(leftCopy)
	slices.Sort(rightCopy)
	return slices.Equal(leftCopy, rightCopy) && len(leftCopy) == len(attributeSet(leftCopy...))
}

func remainingBudgetBasisPoints(bad, eligible, objectiveDenominator int64) int64 {
	used := new(big.Int).Mul(big.NewInt(bad), big.NewInt(objectiveDenominator*10000))
	used.Quo(used, big.NewInt(eligible))
	remaining := new(big.Int).Sub(big.NewInt(10000), used)
	if remaining.Sign() < 0 {
		return 0
	}
	if remaining.Cmp(big.NewInt(10000)) > 0 {
		return 10000
	}
	return remaining.Int64()
}
