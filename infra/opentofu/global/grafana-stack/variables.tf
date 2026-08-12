variable "stack_url" {
  description = "Base URL of the existing shared Grafana Cloud stack"
  type        = string
}

variable "cloudwatch_role_arns" {
  description = "Environment name to CloudWatch read role ARN that each data source assumes"
  type        = map(string)
}

variable "alert_topic_arns" {
  description = "Environment and route to the exact SNS output topic"
  type        = map(map(string))
}

variable "grafana_cloud_external_id" {
  description = "External ID issued by Grafana Cloud for assuming the alert roles"
  type        = string
}

variable "runbook_base_url" {
  description = "Public base URL for code-owned operator runbooks"
  type        = string
}
