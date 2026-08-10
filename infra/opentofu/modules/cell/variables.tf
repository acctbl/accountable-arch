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
    condition     = contains([0, 1], var.api_desired_count)
    error_message = "api_desired_count must be zero before preflight or one after deployment."
  }
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

variable "vpc_cidr" {
  description = "Private IPv4 CIDR assigned to the cell"
  type        = string

  validation {
    condition     = can(cidrsubnet(var.vpc_cidr, 4, 0)) && !can(regex("^0\\.", var.vpc_cidr))
    error_message = "vpc_cidr must be a valid non-default IPv4 CIDR with room for cell subnets."
  }
}
