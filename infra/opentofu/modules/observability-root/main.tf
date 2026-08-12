data "aws_kms_alias" "observability" {
  name = "alias/accountable-${var.environment}-observability"
}

module "observability" {
  source = "../observability-environment"

  account_id                             = var.account_id
  region                                 = var.region
  environment                            = var.environment
  kms_key_arn                            = data.aws_kms_alias.observability.target_key_arn
  alert_email_addresses                  = var.alert_email_addresses
  translator_package_path                = var.translator_package_path
  canary_package_path                    = var.canary_package_path
  capacity_package_path                  = var.capacity_package_path
  enable_capacity_producer               = var.enable_capacity_producer
  canary_active                          = var.canary_active
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
