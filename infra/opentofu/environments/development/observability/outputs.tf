output "alarm_input_topic_arn" {
  value = module.observability.alarm_input_topic_arn
}

output "critical_topic_arn" {
  value = module.observability.critical_topic_arn
}

output "warning_topic_arn" {
  value = module.observability.warning_topic_arn
}

output "proof_topic_arn" {
  value = module.observability.proof_topic_arn
}

output "translator_failure_topic_arn" {
  value = module.observability.translator_failure_topic_arn
}

output "kms_key_arn" {
  value = module.observability.kms_key_arn
}

# The Grafana stack root consumes this ARN for its CloudWatch data source.
output "grafana_cloudwatch_role_arn" {
  value = module.observability.grafana_cloudwatch_role_arn
}

output "expected_topic_arns" {
  value = module.observability.expected_topic_arns
}
