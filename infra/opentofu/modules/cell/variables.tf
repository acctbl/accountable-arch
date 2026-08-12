variable "account_id" {
  description = "AWS account that owns the cell"
  type        = string

  validation {
    condition     = can(regex("^[0-9]{12}$", var.account_id))
    error_message = "account_id must be a 12-digit AWS account ID."
  }
}

variable "api_desired_count" {
  description = "API task count for the current deployment phase"
  type        = number

  validation {
    condition     = contains([0, 1, 2], var.api_desired_count)
    error_message = "api_desired_count must be zero before preflight, one in development, or two in staging and production."
  }
}

variable "alarm_input_topic_arn" {
  description = "Environment alert-translator input topic"
  type        = string

  validation {
    condition     = can(regex("^arn:aws:sns:[a-z0-9-]+:[0-9]{12}:accountable-[a-z]+-observability-alarm-input$", var.alarm_input_topic_arn))
    error_message = "alarm_input_topic_arn must be the exact environment alarm-input topic ARN."
  }
}

variable "runbook_base_url" {
  description = "Public base URL for code-owned operator runbooks"
  type        = string
}

variable "dashboard_base_url" {
  description = "Base URL of the shared Grafana stack"
  type        = string
}

variable "availability_zones" {
  description = "Two Availability Zones used by the cell"
  type        = list(string)

  validation {
    condition     = length(var.availability_zones) == 2 && length(distinct(var.availability_zones)) == 2
    error_message = "availability_zones must contain exactly two distinct Availability Zones."
  }
}

variable "cell_id" {
  description = "Stable identifier used in cell resource names and runtime identity"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{2,15}$", var.cell_id))
    error_message = "cell_id must be 3 to 16 lowercase letters, digits, or hyphens and start with a letter."
  }
}

variable "collector_desired_count" {
  description = "Collector task count for the current deployment phase"
  type        = number

  validation {
    condition     = contains([0, 1, 2], var.collector_desired_count)
    error_message = "collector_desired_count must be zero before token provisioning, one in development, or two in staging and production."
  }
}

variable "collector_image_repository_arn" {
  description = "ECR repository ARN containing the pinned Collector image"
  type        = string

  validation {
    condition     = can(regex("^arn:aws:ecr:[a-z0-9-]+:[0-9]{12}:repository/[A-Za-z0-9._/-]+$", var.collector_image_repository_arn))
    error_message = "collector_image_repository_arn must be an ECR repository ARN."
  }
}

variable "collector_image_uri" {
  description = "Collector image URI pinned to a sha256 manifest digest"
  type        = string

  validation {
    condition     = can(regex("^[^[:space:]@]+@sha256:[a-f0-9]{64}$", var.collector_image_uri))
    error_message = "collector_image_uri must be pinned to a sha256 manifest digest."
  }
}

variable "collector_otlp_token_ref" {
  description = "Parameter Store reference for the active Grafana OTLP token during overlap-first replacement"
  type        = string
  default     = "grafana-otlp-token-active"

  validation {
    condition     = contains(["grafana-otlp-token-active", "grafana-otlp-token-standby"], var.collector_otlp_token_ref)
    error_message = "collector_otlp_token_ref must be the active or standby Grafana OTLP token."
  }
}

variable "grafana_instance_id" {
  description = "Grafana Cloud instance identity, used as the OTLP Basic authentication username"
  type        = string

  validation {
    condition     = can(regex("^[0-9]{1,20}$", var.grafana_instance_id))
    error_message = "grafana_instance_id must be the numeric Grafana Cloud instance identity."
  }
}

variable "grafana_otlp_private_dns" {
  description = "Grafana Cloud OTLP PrivateLink DNS name for the cell"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9.-]+\\.grafana\\.net$", var.grafana_otlp_private_dns))
    error_message = "grafana_otlp_private_dns must be a Grafana Cloud DNS name."
  }
}

variable "grafana_otlp_private_link_service_name" {
  description = "Grafana Cloud OTLP endpoint service name in the cell region"
  type        = string

  validation {
    condition     = can(regex("^com\\.amazonaws\\.vpce\\.[a-z0-9-]+\\.vpce-svc-[a-f0-9]+$", var.grafana_otlp_private_link_service_name))
    error_message = "grafana_otlp_private_link_service_name must be an AWS PrivateLink endpoint service name."
  }
}

variable "configuration_revision" {
  description = "Immutable source revision embedded in runtime configuration"
  type        = string

  validation {
    condition     = can(regex("^[-A-Za-z0-9._]{1,128}$", var.configuration_revision))
    error_message = "configuration_revision must match the runtime revision contract."
  }
}

variable "cell_lifecycle" {
  description = "Cell recovery and deletion posture: ephemeral for create-prove-destroy development cells, durable for lasting environments"
  type        = string

  validation {
    condition     = contains(["ephemeral", "durable"], var.cell_lifecycle)
    error_message = "cell_lifecycle must be ephemeral or durable."
  }
}

variable "database_final_snapshot_identifier" {
  description = "Unique final RDS snapshot identifier required when cell_lifecycle is durable"
  type        = string
  default     = null

  validation {
    condition = (
      var.database_final_snapshot_identifier == null ||
      can(regex("^[a-z][a-z0-9-]{0,253}[a-z0-9]$", var.database_final_snapshot_identifier))
    )
    error_message = "database_final_snapshot_identifier must be a valid lowercase RDS snapshot identifier."
  }
}

variable "environment" {
  description = "Application environment hosted by the cell"
  type        = string

  validation {
    condition     = contains(["development", "staging", "production"], var.environment)
    error_message = "environment must be development, staging, or production."
  }
}

variable "image_repository_arn" {
  description = "ECR repository ARN containing the production image"
  type        = string

  validation {
    condition     = can(regex("^arn:aws:ecr:[a-z0-9-]+:[0-9]{12}:repository/[A-Za-z0-9._/-]+$", var.image_repository_arn))
    error_message = "image_repository_arn must be an ECR repository ARN."
  }
}

variable "image_uri" {
  description = "Production image URI pinned to a sha256 manifest digest"
  type        = string

  validation {
    condition     = can(regex("^[^[:space:]@]+@sha256:[a-f0-9]{64}$", var.image_uri))
    error_message = "image_uri must be pinned to a sha256 manifest digest."
  }
}

variable "synthetic_observation_enabled" {
  description = "Whether this cell runs the continuous production-equivalent synthetic workload"
  type        = bool
  default     = false

  validation {
    condition     = !var.synthetic_observation_enabled || var.environment == "staging"
    error_message = "synthetic_observation_enabled may be true only for staging."
  }
}

variable "synthetic_image_repository_arn" {
  description = "ECR repository ARN containing the signed synthetic workload image"
  type        = string
  default     = null
  nullable    = true

  validation {
    condition = (
      (!var.synthetic_observation_enabled && var.synthetic_image_repository_arn == null) ||
      (var.synthetic_observation_enabled && can(regex("^arn:aws:ecr:[a-z0-9-]+:[0-9]{12}:repository/accountable-k6$", var.synthetic_image_repository_arn)))
    )
    error_message = "synthetic_image_repository_arn must be null outside staging and the accountable-k6 ECR ARN in staging."
  }
}

variable "synthetic_image_uri" {
  description = "Signed synthetic workload image URI pinned to a sha256 manifest digest"
  type        = string
  default     = null
  nullable    = true

  validation {
    condition = (
      (!var.synthetic_observation_enabled && var.synthetic_image_uri == null) ||
      (var.synthetic_observation_enabled && can(regex("^[^[:space:]@]+/accountable-k6@sha256:[a-f0-9]{64}$", var.synthetic_image_uri)))
    )
    error_message = "synthetic_image_uri must be null outside staging and an accountable-k6 sha256 digest URI in staging."
  }
}

variable "release_id" {
  description = "UUIDv7 identity of the verified signed release"
  type        = string

  validation {
    condition     = can(regex("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$", var.release_id))
    error_message = "release_id must be the lowercase UUIDv7 from the verified signed release."
  }
}

variable "release_manifest_digest" {
  description = "Digest of the canonical release manifest that produced this deployment"
  type        = string

  validation {
    condition     = can(regex("^sha256:[0-9a-f]{64}$", var.release_manifest_digest))
    error_message = "release_manifest_digest must be a sha256 digest."
  }
}

variable "web_asset_digest" {
  description = "Verified web asset file-set digest in the signed release"
  type        = string

  validation {
    condition     = can(regex("^sha256:[0-9a-f]{64}$", var.web_asset_digest))
    error_message = "web_asset_digest must be a sha256 digest."
  }
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

  validation {
    condition = var.previous_web_identity == null || (
      can(regex("^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$", var.previous_web_identity.release_id)) &&
      can(regex("^sha256:[0-9a-f]{64}$", var.previous_web_identity.web_asset_digest)) &&
      var.previous_web_identity.configuration_schema_version == 1 &&
      can(regex("^[-A-Za-z0-9._]{1,128}$", var.previous_web_identity.configuration_revision)) &&
      can(regex("^[0-9a-f]{64}$", var.previous_web_identity.configuration_fingerprint))
    )
    error_message = "previous_web_identity must be null or one complete valid web identity tuple."
  }
}

variable "vpc_cidr" {
  description = "Private IPv4 CIDR assigned to the cell"
  type        = string

  validation {
    condition     = can(cidrsubnet(var.vpc_cidr, 4, 0)) && !can(regex("^0\\.", var.vpc_cidr))
    error_message = "vpc_cidr must be a valid non-default IPv4 CIDR with room for cell subnets."
  }
}
