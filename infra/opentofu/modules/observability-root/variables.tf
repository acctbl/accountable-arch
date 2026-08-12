variable "account_id" { type = string }
variable "region" { type = string }
variable "environment" { type = string }
variable "alert_email_addresses" { type = list(string) }
variable "translator_package_path" { type = string }
variable "canary_package_path" { type = string }
variable "capacity_package_path" {
  type     = string
  default  = null
  nullable = true
}
variable "enable_capacity_producer" {
  type    = bool
  default = false
}
variable "canary_active" { type = bool }
variable "cells" { type = map(string) }
variable "grafana_base_url" { type = string }
variable "grafana_cloud_account_id" { type = string }
variable "grafana_cloud_external_id" { type = string }
variable "runbook_base_url" { type = string }
variable "dashboard_base_url" { type = string }
