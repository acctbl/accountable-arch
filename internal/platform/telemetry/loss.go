package telemetry

import "time"

const (
	lossGateMinimumTerminalItems  = 100
	lossGatePendingFreshnessLimit = 180 * time.Second
	lossGateMaximumPendingAge     = 600
)

type DeliveryCounters struct {
	AcceptedItemCount           int64 `json:"accepted_item_count"`
	RefusedItemCount            int64 `json:"refused_item_count"`
	SentItemCount               int64 `json:"sent_item_count"`
	PermanentlyDroppedItemCount int64 `json:"permanently_dropped_item_count"`
	PendingItemCount            int64 `json:"pending_item_count"`
	OldestPendingItemAgeSeconds int64 `json:"oldest_pending_item_age_seconds"`
}

type GrafanaDeliverySignal struct {
	Pipeline string `json:"pipeline"`
	Signal   string `json:"signal"`
}

type TerminalDelivery struct {
	GrafanaDeliverySignal
	SentItemCount               int64 `json:"sent_item_count"`
	PermanentlyDroppedItemCount int64 `json:"permanently_dropped_item_count"`
	RefusedItemCount            int64 `json:"refused_item_count"`
}

type PendingDelivery struct {
	GrafanaDeliverySignal
	CollectorTaskID             string    `json:"collector_task_id"`
	PendingItemCount            int64     `json:"pending_item_count"`
	OldestPendingItemAgeSeconds int64     `json:"oldest_pending_item_age_seconds"`
	SampledAt                   time.Time `json:"sampled_at"`
}

type LossGateInput struct {
	Now                     time.Time          `json:"now"`
	DesiredTaskCount        int                `json:"desired_task_count"`
	RunningCollectorTaskIDs []string           `json:"running_collector_task_ids"`
	TaskListFailed          bool               `json:"task_list_failed"`
	Terminal                []TerminalDelivery `json:"terminal"`
	Pending                 []PendingDelivery  `json:"pending"`
	DeliveryUncertain       bool               `json:"delivery_uncertain"`
	AccountingUncertain     bool               `json:"accounting_uncertain"`
}

type GateState string

const (
	GatePass    GateState = "pass"
	GateFail    GateState = "fail"
	GateUnknown GateState = "unknown"
)

func GrafanaDeliverySignals() []GrafanaDeliverySignal {
	return []GrafanaDeliverySignal{
		{Pipeline: "traces/app", Signal: "traces"},
		{Pipeline: "metrics/app", Signal: "metrics"},
		{Pipeline: "logs/app", Signal: "logs"},
	}
}

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

// EvaluateLossGate reports the seven-day Collector loss gate. Terminal counters are
// window increases for each pipeline and signal. Pending gauges are the freshest
// per-task samples for the same pipelines and signals.
func EvaluateLossGate(input LossGateInput) GateState {
	if input.TaskListFailed || input.DeliveryUncertain || input.AccountingUncertain ||
		input.DesiredTaskCount < 1 || len(input.RunningCollectorTaskIDs) != input.DesiredTaskCount {
		return GateUnknown
	}
	runningTasks := attributeSet(input.RunningCollectorTaskIDs...)
	if len(runningTasks) != input.DesiredTaskCount {
		return GateUnknown
	}
	expected := GrafanaDeliverySignals()
	terminal, ok := indexTerminalDelivery(input.Terminal, expected)
	if !ok {
		return GateUnknown
	}
	pending, ok := indexPendingDelivery(input, runningTasks, expected)
	if !ok {
		return GateUnknown
	}
	for _, signal := range expected {
		if failsTerminalLoss(terminal[signal]) {
			return GateFail
		}
		if pending[signal] >= lossGateMaximumPendingAge {
			return GateFail
		}
	}
	return GatePass
}

func indexTerminalDelivery(
	series []TerminalDelivery,
	expected []GrafanaDeliverySignal,
) (map[GrafanaDeliverySignal]TerminalDelivery, bool) {
	indexed := make(map[GrafanaDeliverySignal]TerminalDelivery, len(expected))
	for _, entry := range series {
		if _, duplicate := indexed[entry.GrafanaDeliverySignal]; duplicate {
			return nil, false
		}
		if entry.SentItemCount < 0 || entry.PermanentlyDroppedItemCount < 0 || entry.RefusedItemCount < 0 {
			return nil, false
		}
		indexed[entry.GrafanaDeliverySignal] = entry
	}
	for _, signal := range expected {
		if _, present := indexed[signal]; !present {
			return nil, false
		}
	}
	return indexed, len(indexed) == len(expected)
}

func indexPendingDelivery(
	input LossGateInput,
	runningTasks map[string]struct{},
	expected []GrafanaDeliverySignal,
) (map[GrafanaDeliverySignal]int64, bool) {
	type taskSignal struct {
		task   string
		signal GrafanaDeliverySignal
	}
	seen := make(map[taskSignal]struct{}, len(input.Pending))
	oldest := make(map[GrafanaDeliverySignal]int64, len(expected))
	for _, entry := range input.Pending {
		key := taskSignal{task: entry.CollectorTaskID, signal: entry.GrafanaDeliverySignal}
		if _, duplicate := seen[key]; duplicate {
			return nil, false
		}
		if !usablePendingSample(entry, input.Now, runningTasks) {
			return nil, false
		}
		seen[key] = struct{}{}
		if entry.OldestPendingItemAgeSeconds > oldest[entry.GrafanaDeliverySignal] {
			oldest[entry.GrafanaDeliverySignal] = entry.OldestPendingItemAgeSeconds
		}
	}
	for task := range runningTasks {
		for _, signal := range expected {
			if _, present := seen[taskSignal{task: task, signal: signal}]; !present {
				return nil, false
			}
		}
	}
	return oldest, true
}

func usablePendingSample(entry PendingDelivery, now time.Time, runningTasks map[string]struct{}) bool {
	if _, running := runningTasks[entry.CollectorTaskID]; !running {
		return false
	}
	age := now.Sub(entry.SampledAt)
	return age >= 0 && age <= lossGatePendingFreshnessLimit &&
		entry.PendingItemCount >= 0 && entry.OldestPendingItemAgeSeconds >= 0
}

func failsTerminalLoss(series TerminalDelivery) bool {
	terminal := series.SentItemCount + series.PermanentlyDroppedItemCount
	return series.RefusedItemCount != 0 ||
		terminal < lossGateMinimumTerminalItems ||
		permanentDropRateAtLeastOnePercent(series.PermanentlyDroppedItemCount, terminal)
}

func permanentDropRateAtLeastOnePercent(dropped, terminal int64) bool {
	minimumDrops := terminal / 100
	if terminal%100 != 0 {
		minimumDrops++
	}
	return dropped >= minimumDrops
}
