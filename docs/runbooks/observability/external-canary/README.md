# External observability canary incident

Owner: Platform observability.

Use the alert's `dashboard_url` to identify the first failed external query.

## Diagnosis

Check current logs, traces, metrics, CloudWatch access, dashboard availability, and alert-rule health from outside the observed stack.
Treat an empty, stale, unauthorized, or malformed response as a failure.

## Mitigation

Stop promotion while the canary is failing.
Restore the failed data source, permission, route, or query without weakening the required check.

## Recovery

Require two consecutive complete canary runs and a fresh authenticated proof.

## Escalation

The Platform observability owner leads recovery.
Escalate a managed Grafana outage to the Grafana Cloud owner and an AWS data-source failure to the AWS account owner.
