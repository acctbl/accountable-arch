variable "cloudwatch_role_arns" {
  description = "Environment name to CloudWatch read role ARN that each data source assumes"
  type        = map(string)

  validation {
    condition     = alltrue([for name in keys(var.cloudwatch_role_arns) : contains(["development", "staging", "production"], name)])
    error_message = "cloudwatch_role_arns keys must be registered environments."
  }
}

variable "region" {
  description = "AWS region that the CloudWatch data sources read"
  type        = string
}

variable "alert_topic_arns" {
  description = "Environment and route to the exact SNS output topic used for email and conditional IRM delivery"
  type        = map(map(string))

  validation {
    condition = alltrue([
      for environment, routes in var.alert_topic_arns :
      contains(["development", "staging", "production"], environment) &&
      length(routes) == 3 && alltrue([
        for route, arn in routes :
        contains(["critical", "proof", "warning"], route) &&
        can(regex("^arn:aws:sns:[a-z0-9-]+:[0-9]{12}:accountable-[a-z]+-observability-(critical|proof|warning)$", arn))
      ])
    ])
    error_message = "alert_topic_arns must map every registered route to an exact Accountable SNS topic ARN."
  }
}

variable "grafana_cloud_external_id" {
  description = "External ID issued by Grafana Cloud for assuming the environment alert roles"
  type        = string
}

variable "runbook_base_url" {
  description = "Public base URL for code-owned operator runbooks"
  type        = string
}

variable "dashboard_base_url" {
  description = "Base URL of the shared Grafana stack"
  type        = string
}
