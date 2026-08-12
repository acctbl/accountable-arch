variable "configuration_revision" { type = string }
variable "api_desired_count" {
  type    = number
  default = 2
}
variable "image_uri" { type = string }
variable "collector_desired_count" {
  type    = number
  default = 2
}
variable "collector_image_uri" { type = string }
variable "collector_otlp_token_ref" {
  type    = string
  default = "grafana-otlp-token-active"

  validation {
    condition     = contains(["grafana-otlp-token-active", "grafana-otlp-token-standby"], var.collector_otlp_token_ref)
    error_message = "collector_otlp_token_ref must select the active or standby token."
  }
}
variable "grafana_otlp_endpoint" { type = string }
variable "runbook_base_url" { type = string }
variable "dashboard_base_url" { type = string }
variable "release_id" { type = string }
variable "release_manifest_digest" { type = string }
variable "web_asset_digest" { type = string }
variable "database_final_snapshot_identifier" { type = string }

variable "previous_web_identity" {
  type = object({
    release_id                   = string
    web_asset_digest             = string
    configuration_schema_version = number
    configuration_revision       = string
    configuration_fingerprint    = string
  })
  default  = null
  nullable = true
}
