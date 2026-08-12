# Alert delivery incident

Owner: Platform incident response.

Use the alert's `dashboard_url` for rule-evaluation and notification history.

## Diagnosis

Check Grafana rule evaluation, the independent CloudWatch alarm, SNS delivery, translator Lambda errors, EventBridge delivery, and every configured dead-letter queue.
Confirm the notification document contains the exact environment, severity, signal, dashboard, runbook, identity, and recovery fields.

## Mitigation

Keep translator-failure delivery on its independent route.
Stop production promotion while a critical route cannot deliver and receive acknowledgement.
Replay only deduplicated dead-letter messages after the destination is healthy.

## Recovery

Send a controlled proof alert and require delivery, acknowledgement when required, resolution delivery, and empty dead-letter queues.

## Escalation

The Platform incident-response owner leads recovery.
Escalate email delivery to the mail owner and IRM delivery to the Grafana Cloud owner.
