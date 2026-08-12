terraform {
  required_version = ">= 1.10.0"

  required_providers {
    grafana = {
      source  = "grafana/grafana"
      version = "4.45.0"
    }
  }

  backend "s3" {
    bucket              = "accountable-tofu-state-453722413624-eu-west-2"
    key                 = "global/grafana-stack.tfstate"
    region              = "eu-west-2"
    encrypt             = true
    kms_key_id          = "arn:aws:kms:eu-west-2:453722413624:alias/accountable-development-tofu-state"
    use_lockfile        = true
    allowed_account_ids = ["453722413624"]
  }
}

provider "grafana" {
  url = var.stack_url
}

module "stack" {
  source = "../../modules/grafana-stack"

  region                    = "eu-west-2"
  cloudwatch_role_arns      = var.cloudwatch_role_arns
  alert_topic_arns          = var.alert_topic_arns
  grafana_cloud_external_id = var.grafana_cloud_external_id
  runbook_base_url          = var.runbook_base_url
  dashboard_base_url        = var.stack_url
}
