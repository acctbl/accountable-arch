# Collector and Grafana capacity incident

Owner: Platform observability.

Use the alert's `dashboard_url` for Collector pressure and the latest shared-stack capacity sample.

## Diagnosis

Require one fresh successful capacity read with approved tier-policy compliance.
Inspect current and forecast use for metric series, logs, traces, Grafana users, IRM users, alert rules, and maximum instances in one rule.
Confirm development, staging, and production remain inside their 10, 15, and 25 percent allocations and aggregate shared use remains at or below 50 percent of the Free-tier limit.

## Mitigation

Stop apply and promotion when the sample is missing, stale, malformed, over its critical threshold, or reports an unapproved tier.
Reduce telemetry volume or identity cardinality at the source before raising infrastructure limits.

## Recovery

Require two fresh compliant samples below the applicable threshold and a passing Collector loss gate.

## Escalation

The Platform observability owner leads recovery.
Any paid-tier change requires explicit product-owner approval before apply.
