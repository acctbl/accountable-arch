# Identity and proof incident

Owner: Platform security.

Use the alert's `identity_url` to inspect complete release, artifact, configuration, service, environment, and cell identity.

## Diagnosis

Reject mixed current and previous web identity tuples.
Verify the release manifest, signature bundle, API image, Collector image, web archive, and extracted file-set digests.
For an authenticated proof alert, match the proof type, key identifier, timestamp, body digest, and HMAC verification result.

## Mitigation

Stop promotion when identity or proof evidence is missing, stale, conflicting, or incomplete.
Retire an invalid artifact or key only through its protected workflow.

## Recovery

Require one complete active identity tuple, at most one complete previous web tuple, and a fresh authenticated end-to-end proof.
Re-run release verification before resuming promotion.

## Escalation

The Platform security owner leads recovery and approves any key or signing-identity response.
