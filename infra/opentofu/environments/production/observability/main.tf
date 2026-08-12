terraform {
  required_version = ">= 1.10.0"
  required_providers { aws = { source = "hashicorp/aws", version = "~> 6.0" } }

  backend "s3" {
    bucket              = "accountable-tofu-state-576329195663-eu-west-2"
    key                 = "observability/production.tfstate"
    region              = "eu-west-2"
    encrypt             = true
    kms_key_id          = "arn:aws:kms:eu-west-2:576329195663:alias/accountable-production-tofu-state"
    use_lockfile        = true
    allowed_account_ids = ["576329195663"]
  }
}

provider "aws" {
  region              = "eu-west-2"
  allowed_account_ids = ["576329195663"]
  default_tags { tags = { Environment = "production", ManagedBy = "opentofu", Project = "accountable" } }
}

module "observability" {
  source = "../../../modules/observability-root"

  account_id              = "576329195663"
  region                  = "eu-west-2"
  environment             = "production"
  alert_email_addresses   = var.alert_email_addresses
  translator_package_path = var.translator_package_path
  canary_package_path     = var.canary_package_path
  canary_active           = var.canary_active
  cells = {
    production-01 = "arn:aws:ecs:eu-west-2:576329195663:cluster/accountable-production-01"
  }
  grafana_base_url          = var.grafana_base_url
  grafana_cloud_account_id  = var.grafana_cloud_account_id
  grafana_cloud_external_id = var.grafana_cloud_external_id
  runbook_base_url          = var.runbook_base_url
  dashboard_base_url        = var.dashboard_base_url
}
