variable "api_desired_count" {
  description = "Zero before preflight and one for the deployed proof window"
  type        = number
}

variable "configuration_revision" {
  description = "Immutable source revision embedded in runtime configuration"
  type        = string
}

variable "image_uri" {
  description = "Production image URI pinned to a sha256 manifest digest"
  type        = string
}

variable "collector_desired_count" {
  description = "Collector task count: one for development, two for the highly available proof"
  type        = number
  default     = 1
}

variable "collector_image_uri" {
  description = "Collector image URI pinned to a sha256 manifest digest"
  type        = string
}

variable "collector_otlp_token_ref" {
  type    = string
  default = "grafana-otlp-token-active"

  validation {
    condition     = contains(["grafana-otlp-token-active", "grafana-otlp-token-standby"], var.collector_otlp_token_ref)
    error_message = "collector_otlp_token_ref must select the active or standby token."
  }
}

variable "grafana_otlp_private_dns" {
  description = "Grafana Cloud OTLP PrivateLink DNS name"
  type        = string
}

variable "grafana_otlp_private_link_service_name" {
  description = "Grafana Cloud OTLP PrivateLink endpoint service name"
  type        = string
}

variable "runbook_base_url" { type = string }
variable "dashboard_base_url" { type = string }

variable "release_id" {
  description = "UUIDv7 identity of the verified signed release"
  type        = string
}

variable "release_manifest_digest" {
  description = "Digest of the canonical release manifest that produced this deployment"
  type        = string
}

variable "web_asset_digest" {
  description = "Verified web asset file-set digest"
  type        = string
}

variable "previous_web_identity" {
  description = "Immediately previous complete web identity accepted during deployment overlap"
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
