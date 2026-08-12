terraform {
  required_version = ">= 1.10.0"
  required_providers { aws = { source = "hashicorp/aws", version = "~> 6.0" } }

  backend "s3" {
    bucket              = "accountable-tofu-state-723225039926-eu-west-2"
    key                 = "observability/staging.tfstate"
    region              = "eu-west-2"
    encrypt             = true
    kms_key_id          = "arn:aws:kms:eu-west-2:723225039926:alias/accountable-staging-tofu-state"
    use_lockfile        = true
    allowed_account_ids = ["723225039926"]
  }
}

provider "aws" {
  region              = "eu-west-2"
  allowed_account_ids = ["723225039926"]
  default_tags { tags = { Environment = "staging", ManagedBy = "opentofu", Project = "accountable" } }
}

module "observability" {
  source = "../../../modules/observability-root"

  account_id              = "723225039926"
  region                  = "eu-west-2"
  environment             = "staging"
  alert_email_addresses   = var.alert_email_addresses
  translator_package_path = var.translator_package_path
  canary_package_path     = var.canary_package_path
  canary_active           = var.canary_active
  cells = {
    staging-01 = "arn:aws:ecs:eu-west-2:723225039926:cluster/accountable-staging-01"
  }
  grafana_base_url          = var.grafana_base_url
  grafana_cloud_account_id  = var.grafana_cloud_account_id
  grafana_cloud_external_id = var.grafana_cloud_external_id
  runbook_base_url          = var.runbook_base_url
  dashboard_base_url        = var.dashboard_base_url
}
