variable "account_id" {
  description = "AWS account that owns the protected release artifacts"
  type        = string

  validation {
    condition     = can(regex("^[0-9]{12}$", var.account_id))
    error_message = "account_id must be a 12-digit AWS account ID."
  }
}

variable "region" {
  description = "AWS region that stores the protected release artifacts"
  type        = string
}

variable "writer_role_name" {
  description = "IAM role name of the protected workflow that writes releases and cell-inventory records"
  type        = string
}

variable "reader_role_names" {
  description = "IAM role names that verify releases and read the cell-inventory chain"
  type        = list(string)
  default     = []
}

variable "external_reader_role_arns" {
  description = "Cross-account protected deployment roles that verify promoted release evidence"
  type        = list(string)
  default     = []

  validation {
    condition     = alltrue([for arn in var.external_reader_role_arns : can(regex("^arn:aws:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_/-]+$", arn))])
    error_message = "external_reader_role_arns must contain exact IAM role ARNs."
  }
}

variable "external_writer_role_arns" {
  description = "Cross-account protected deployment roles that write inventory and promotion evidence"
  type        = list(string)
  default     = []

  validation {
    condition     = alltrue([for arn in var.external_writer_role_arns : can(regex("^arn:aws:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_/-]+$", arn))])
    error_message = "external_writer_role_arns must contain exact IAM role ARNs."
  }
}
