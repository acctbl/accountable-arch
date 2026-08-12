variable "alert_email_addresses" {
  description = "Operator email addresses subscribed to every alert output topic"
  type        = list(string)
}

variable "translator_package_path" {
  description = "Absolute path of the built alert translator deployment package"
  type        = string
}

variable "canary_package_path" {
  description = "Absolute path of the built canary deployment package"
  type        = string
}

variable "capacity_package_path" {
  description = "Absolute path of the built shared-stack capacity deployment package"
  type        = string
}

variable "runtime_log_group_name" {
  description = "Cell runtime log group that carries the safe stdout records"
  type        = string
}

variable "cells" {
  description = "Registered cell ID to its ECS cluster ARN"
  type        = map(string)
}

variable "grafana_base_url" {
  description = "Grafana Cloud stack base URL that the canary queries"
  type        = string
}

variable "runbook_base_url" {
  description = "Base URL of the operator runbooks"
  type        = string
}

variable "dashboard_base_url" {
  description = "Base URL of the environment dashboards"
  type        = string
}

variable "grafana_cloud_account_id" {
  description = "AWS account that Grafana Cloud uses to assume the CloudWatch read role"
  type        = string
}

variable "grafana_cloud_external_id" {
  description = "External ID that Grafana Cloud issues for this CloudWatch data source"
  type        = string
}
