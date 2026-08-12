# SLO budget incident

Owner: Service reliability.

Use the alert's `dashboard_url` for availability, latency, heartbeats, and the evaluated 30-day budget.

## Diagnosis

Validate the immutable cell inventory chain before evaluating any period.
Confirm every active five-minute cell period has a heartbeat and no API-task or SLI-delivery uncertainty.
Compare eligible, availability-bad, and latency-slow totals with the canonical budget evidence.

## Mitigation

Stop promotion when the budget state is `unknown` or `fail`.
Reduce or stop the failing traffic source and restore the responsible service or dependency.

## Recovery

Require fresh complete periods, passing availability and latency budgets, and no uncertainty in the evaluated window.
Store the new canonical evaluation in immutable promotion evidence.

## Escalation

The Service reliability owner leads recovery.
The Platform owner resolves inventory, heartbeat, or delivery uncertainty.
