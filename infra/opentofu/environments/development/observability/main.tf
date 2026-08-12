terraform {
  required_version = ">= 1.10.0"
  required_providers { aws = { source = "hashicorp/aws", version = "~> 6.0" } }

  backend "s3" {
    bucket              = "accountable-tofu-state-453722413624-eu-west-2"
    key                 = "observability/development.tfstate"
    region              = "eu-west-2"
    encrypt             = true
    kms_key_id          = "arn:aws:kms:eu-west-2:453722413624:alias/accountable-development-tofu-state"
    use_lockfile        = true
    allowed_account_ids = ["453722413624"]
  }
}

provider "aws" {
  region              = "eu-west-2"
  allowed_account_ids = ["453722413624"]
  default_tags { tags = { Environment = "development", ManagedBy = "opentofu", Project = "accountable" } }
}

module "observability" {
  source = "../../../modules/observability-root"

  account_id                = "453722413624"
  region                    = "eu-west-2"
  environment               = "development"
  alert_email_addresses     = var.alert_email_addresses
  translator_package_path   = var.translator_package_path
  canary_package_path       = var.canary_package_path
  capacity_package_path     = var.capacity_package_path
  enable_capacity_producer  = true
  runtime_log_group_name    = var.runtime_log_group_name
  cells                     = var.cells
  grafana_base_url          = var.grafana_base_url
  grafana_cloud_account_id  = var.grafana_cloud_account_id
  grafana_cloud_external_id = var.grafana_cloud_external_id
  runbook_base_url          = var.runbook_base_url
  dashboard_base_url        = var.dashboard_base_url
}
