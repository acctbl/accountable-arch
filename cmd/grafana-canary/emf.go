package main

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/acctbl/accountable/internal/platform/clock"
)

// emfMetric is one integer metric in one embedded-metric-format document.
type emfMetric struct {
	Name  string
	Unit  string
	Value int64
}

// publishEMF writes one embedded-metric-format document to stdout. CloudWatch
// extracts the metrics from the Lambda log group, so the publisher needs no
// PutMetricData permission and no binary floating point.
func publishEMF(now clock.Clock, namespace string, dimensions map[string]string, metrics []emfMetric) error {
	if len(metrics) == 0 {
		return nil
	}
	names := make([]string, 0, len(dimensions))
	for name := range dimensions {
		names = append(names, name)
	}
	sort.Strings(names)

	definitions := make([]map[string]string, 0, len(metrics))
	document := make(map[string]any, len(dimensions)+len(metrics)+1)
	for _, metric := range metrics {
		definitions = append(definitions, map[string]string{"Name": metric.Name, "Unit": metric.Unit})
		document[metric.Name] = metric.Value
	}
	for name, value := range dimensions {
		document[name] = value
	}
	document["_aws"] = map[string]any{
		"Timestamp": now.Now().UnixMilli(),
		"CloudWatchMetrics": []map[string]any{{
			"Namespace":  namespace,
			"Dimensions": [][]string{names},
			"Metrics":    definitions,
		}},
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}
