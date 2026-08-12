# Browser telemetry incident

Owner: Web platform.

Use the `dashboard_url` in the alert for API outcomes, runtime failures, Web Vitals, and client delivery loss.

## Diagnosis

Separate public warning-only browser signals from server SLI failures.
Confirm the release, web asset digest, configuration fingerprint, route, browser family, and bounded correlation identity.
Check client admission, known-drop, and unexpected-stop outcomes before treating missing telemetry as healthy.

## Mitigation

Do not block promotion only because a public browser warning is active.
Block promotion if the same symptom is confirmed by the server SLI, privacy, identity, proof, or telemetry-delivery gate.
Use the protected promotion workflow for any rollback.

## Recovery

Require the alert's `recovery_signal`, a passing browser smoke, and no new client loss or privacy violation.

## Escalation

The Web platform owner leads browser remediation.
Escalate server-correlated failures to the Platform owner.
