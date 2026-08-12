resource "aws_kms_key" "observability" {
  description             = "Accountable ${var.environment} observability alerts and log groups"
  deletion_window_in_days = 30
  enable_key_rotation     = true
  policy                  = data.aws_iam_policy_document.observability_key.json

  lifecycle { prevent_destroy = true }
}

resource "aws_kms_alias" "observability" {
  name          = "alias/accountable-${var.environment}-observability"
  target_key_id = aws_kms_key.observability.key_id
}

data "aws_iam_policy_document" "observability_key" {
  statement {
    actions   = ["kms:*"]
    resources = ["*"]

    principals {
      type        = "AWS"
      identifiers = ["arn:aws:iam::${var.account_id}:root"]
    }
  }

  statement {
    actions   = ["kms:Decrypt", "kms:Describe*", "kms:Encrypt", "kms:GenerateDataKey*", "kms:ReEncrypt*"]
    resources = ["*"]

    principals {
      type        = "Service"
      identifiers = ["cloudwatch.amazonaws.com", "events.amazonaws.com", "logs.${var.region}.amazonaws.com", "sns.amazonaws.com"]
    }
  }
}

module "observability" {
  source = "../observability-environment"

  account_id                             = var.account_id
  region                                 = var.region
  environment                            = var.environment
  kms_key_arn                            = aws_kms_key.observability.arn
  alert_email_addresses                  = var.alert_email_addresses
  translator_package_path                = var.translator_package_path
  canary_package_path                    = var.canary_package_path
  capacity_package_path                  = var.capacity_package_path
  enable_capacity_producer               = var.enable_capacity_producer
  runtime_log_group_name                 = var.runtime_log_group_name
  cells                                  = var.cells
  grafana_base_url                       = var.grafana_base_url
  grafana_cloud_account_id               = var.grafana_cloud_account_id
  grafana_cloud_external_id              = var.grafana_cloud_external_id
  grafana_token_parameter                = "/accountable/observability/${var.environment}/grafana-health-token"
  grafana_capacity_cloud_token_parameter = var.enable_capacity_producer ? "/accountable/observability/development/grafana-capacity-cloud-token" : null
  grafana_capacity_stack_token_parameter = var.enable_capacity_producer ? "/accountable/observability/development/grafana-capacity-stack-token" : null
  runbook_base_url                       = var.runbook_base_url
  dashboard_base_url                     = var.dashboard_base_url
}
