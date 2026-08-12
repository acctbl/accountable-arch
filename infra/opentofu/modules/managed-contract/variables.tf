variable "account_id" {
  type = string

  validation {
    condition     = can(regex("^[0-9]{12}$", var.account_id))
    error_message = "account_id must be a 12-digit AWS account ID."
  }
}

variable "environment" {
  type = string
}

variable "github_environment" {
  type = string
}

variable "github_oidc_subject_prefix" {
  type = string
}

variable "github_repository" {
  type = string
}

variable "region" {
  type = string
}
