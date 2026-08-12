output "alarm_input_topic_arn" {
  value = aws_sns_topic.alarm_input.arn
}

output "critical_topic_arn" {
  value = aws_sns_topic.output["critical"].arn
}

output "warning_topic_arn" {
  value = aws_sns_topic.output["warning"].arn
}

output "proof_topic_arn" {
  value = aws_sns_topic.output["proof"].arn
}

output "translator_failure_topic_arn" {
  value = aws_sns_topic.translator_failure.arn
}

output "task_stop_log_group_name" {
  value = aws_cloudwatch_log_group.task_stops.name
}

# Every deployment document must match these exact values.
output "expected_topic_arns" {
  value = {
    alarm_input        = "${local.topic_arn_prefix}-alarm-input"
    critical           = "${local.topic_arn_prefix}-critical"
    warning            = "${local.topic_arn_prefix}-warning"
    proof              = "${local.topic_arn_prefix}-proof"
    translator_failure = "${local.topic_arn_prefix}-translator-failure"
  }
}

output "grafana_cloudwatch_role_arn" {
  value = aws_iam_role.grafana_cloudwatch.arn
}
