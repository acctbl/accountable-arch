# Service dependency incident

Owner: Platform.

Use the `dashboard_url` in the alert for the affected environment and the `identity_url` to confirm the deployed release, configuration, and cell.

## Diagnosis

Confirm the alert dimensions identify one expected cell.
Check ECS desired and running task counts, target health, Route 53 health, CloudFront status, RDS status, backup jobs, and NAT errors.
Use the deployment history dashboard to correlate the first failing period with a release or infrastructure apply.

## Mitigation

Stop promotion while any production dependency alert is active.
Restore the failed dependency before replacing healthy tasks.
For a release-correlated failure, promote the last verified release by digest through the protected workflow.

## Recovery

Wait for the alert's `recovery_signal` and confirm the service identity dashboard shows only the intended complete identity tuple.
Run the protected production smoke for production or edge smoke for development and staging.

## Escalation

The Platform owner leads recovery.
Page the database owner for RDS or backup failures and the edge owner for Route 53 or CloudFront failures.
