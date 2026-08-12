# Collector delivery incident

Owner: Platform observability.

Use the `dashboard_url` in the alert for queue age, refusals, terminal drops, uncertainty, and Collector task identity.

## Diagnosis

Identify the affected signal, pipeline, cell, and Collector task.
Compare accepted items with sent, permanently dropped, and refused terminal outcomes.
Check the oldest pending item age, Collector memory, OTLP responses, CloudWatch EMF errors, and ECS unexpected-stop evidence.

## Mitigation

Stop promotion when delivery, accounting, or SLI state is unknown.
Remove the destination failure or capacity pressure before increasing traffic.
Replace a stuck Collector task only through the protected cell workflow so unexpected-stop evidence remains independent.

## Recovery

Require fresh task heartbeats, zero new uncertainty, zero queue refusal, and a bounded oldest pending age.
Re-run the exact seven-day loss gate and the managed proof before resuming promotion.

## Escalation

The Platform observability owner leads recovery.
Escalate persistent destination rejection to the Grafana Cloud owner and CloudWatch EMF rejection to the AWS account owner.
