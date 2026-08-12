variable "account_id" {
  description = "AWS account that owns this application environment"
  type        = string
}

variable "environment" {
  description = "Application environment represented by the AWS account"
  type        = string

  validation {
    condition     = contains(["development", "staging", "production"], var.environment)
    error_message = "environment must be development, staging, or production."
  }
}

variable "github_oidc_subject_prefix" {
  description = "Immutable GitHub repository subject prefix trusted by deployment roles"
  type        = string
}

variable "github_repository" {
  description = "GitHub owner and repository used in exact workflow references"
  type        = string
}

variable "region" {
  description = "AWS region containing the environment state backend"
  type        = string
}
