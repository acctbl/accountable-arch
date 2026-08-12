variable "account_id" {
  description = "AWS account that owns the environment observability resources"
  type        = string

  validation {
    condition     = can(regex("^[0-9]{12}$", var.account_id))
    error_message = "account_id must be a 12-digit AWS account ID."
  }
}

variable "region" {
  description = "AWS region that owns the environment observability resources"
  type        = string
}

variable "environment" {
  description = "Application environment that owns these alert routes"
  type        = string

  validation {
    condition     = contains(["development", "staging", "production"], var.environment)
    error_message = "environment must be development, staging, or production."
  }
}

variable "kms_key_arn" {
  description = "Customer-managed key that encrypts alert topics and observability log groups"
  type        = string
}

variable "alert_email_addresses" {
  description = "Operator email addresses subscribed to every alert output topic"
  type        = list(string)

  validation {
    condition     = length(var.alert_email_addresses) > 0
    error_message = "at least one operator email address must receive alerts."
  }
}

variable "translator_package_path" {
  description = "Absolute path of the built alert translator deployment package"
  type        = string
}

variable "canary_package_path" {
  description = "Absolute path of the built canary deployment package, which carries the canary deployment document"
  type        = string
}

variable "capacity_package_path" {
  description = "Absolute path of the stack capacity deployment package"
  type        = string
  default     = null
  nullable    = true
}

variable "enable_capacity_producer" {
  description = "Whether this environment owns the one shared-stack capacity producer"
  type        = bool
  default     = false
}

variable "canary_active" {
  description = "Whether the external Grafana canary and its fail-closed alarm are active"
  type        = bool
}

variable "grafana_capacity_cloud_token_parameter" {
  description = "Exact Parameter Store name of the Grafana Cloud API capacity token"
  type        = string
  default     = null
  nullable    = true
}

variable "grafana_capacity_stack_token_parameter" {
  description = "Exact Parameter Store name of the Grafana stack capacity token"
  type        = string
  default     = null
  nullable    = true
}

variable "cells" {
  description = "Registered cell ID to its ECS cluster ARN, so uncertainty metrics carry the cell ID"
  type        = map(string)
}

variable "grafana_base_url" {
  description = "Grafana Cloud stack base URL that the canary queries"
  type        = string
}

variable "grafana_token_parameter" {
  description = "Parameter Store name of the environment Grafana health token"
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

  validation {
    condition     = can(regex("^[0-9]{12}$", var.grafana_cloud_account_id))
    error_message = "grafana_cloud_account_id must be the 12-digit account that Grafana Cloud reports."
  }
}

variable "grafana_cloud_external_id" {
  description = "External ID that Grafana Cloud issues for this CloudWatch data source"
  type        = string

  validation {
    condition     = length(var.grafana_cloud_external_id) > 0
    error_message = "grafana_cloud_external_id must be the value Grafana Cloud issues."
  }
}
