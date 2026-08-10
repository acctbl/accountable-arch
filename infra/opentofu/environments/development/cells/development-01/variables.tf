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

