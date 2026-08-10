package telemetry

import "time"

type DeliveryCounters struct {
	AcceptedItemCount           int64 `json:"accepted_item_count"`
	RefusedItemCount            int64 `json:"refused_item_count"`
	SentItemCount               int64 `json:"sent_item_count"`
	PermanentlyDroppedItemCount int64 `json:"permanently_dropped_item_count"`
	PendingItemCount            int64 `json:"pending_item_count"`
	OldestPendingItemAgeSeconds int64 `json:"oldest_pending_item_age_seconds"`
}

type LossGateSeries struct {
	Environment     string           `json:"environment"`
	CellID          string           `json:"cell_id"`
	CollectorTaskID string           `json:"collector_task_id"`
	Pipeline        string           `json:"pipeline"`
	Signal          string           `json:"signal"`
	SampledAt       time.Time        `json:"sampled_at"`
	Counters        DeliveryCounters `json:"counters"`
}

type LossGateInput struct {
	Now                     time.Time        `json:"now"`
	DesiredTaskCount        int              `json:"desired_task_count"`
	RunningCollectorTaskIDs []string         `json:"running_collector_task_ids"`
	Series                  []LossGateSeries `json:"series"`
	DeliveryUncertain       bool             `json:"delivery_uncertain"`
	AccountingUncertain     bool             `json:"accounting_uncertain"`
}

type GateState string

const (
	GatePass    GateState = "pass"
	GateFail    GateState = "fail"
	GateUnknown GateState = "unknown"
)

func ParseDeliveryCounters(raw []byte) (DeliveryCounters, error) {
	var counters DeliveryCounters
	if err := decodeStrictJSON(raw, &counters); err != nil {
		return DeliveryCounters{}, err
	}
	if err := ValidateDeliveryCounters(counters); err != nil {
		return DeliveryCounters{}, err
	}
	return counters, nil
}

func ValidateDeliveryCounters(counters DeliveryCounters) error {
	if counters.AcceptedItemCount < 0 || counters.RefusedItemCount < 0 || counters.SentItemCount < 0 ||
		counters.PermanentlyDroppedItemCount < 0 || counters.PendingItemCount < 0 || counters.OldestPendingItemAgeSeconds < 0 {
		return contractError("delivery counter")
	}
	if counters.AcceptedItemCount != counters.SentItemCount+counters.PermanentlyDroppedItemCount+counters.PendingItemCount {
		return contractError("delivery conservation")
	}
	if counters.PendingItemCount == 0 && counters.OldestPendingItemAgeSeconds != 0 {
		return contractError("pending age")
	}
	return nil
}

func EvaluateLossGate(input LossGateInput) GateState {
	if input.DeliveryUncertain || input.AccountingUncertain ||
		len(input.RunningCollectorTaskIDs) != input.DesiredTaskCount {
		return GateUnknown
	}
	running := attributeSet(input.RunningCollectorTaskIDs...)
	seen := make(map[string]struct{})
	for _, series := range input.Series {
		if _, ok := running[series.CollectorTaskID]; !ok ||
			input.Now.Sub(series.SampledAt) > 180*time.Second || input.Now.Before(series.SampledAt) ||
			ValidateDeliveryCounters(series.Counters) != nil {
			return GateUnknown
		}
		key := series.CollectorTaskID + "\x00" + series.Pipeline + "\x00" + series.Signal
		if _, duplicate := seen[key]; duplicate {
			return GateUnknown
		}
		seen[key] = struct{}{}
		terminal := series.Counters.SentItemCount + series.Counters.PermanentlyDroppedItemCount
		if series.Counters.RefusedItemCount != 0 || terminal < 100 ||
			permanentDropRateAtLeastOnePercent(series.Counters.PermanentlyDroppedItemCount, terminal) ||
			series.Counters.OldestPendingItemAgeSeconds >= 600 {
			return GateFail
		}
	}
	if len(input.Series) == 0 {
		return GateUnknown
	}
	return GatePass
}

func permanentDropRateAtLeastOnePercent(dropped, terminal int64) bool {
	minimumDrops := terminal / 100
	if terminal%100 != 0 {
		minimumDrops++
	}
	return dropped >= minimumDrops
}
