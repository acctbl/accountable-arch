terraform {
  required_version = ">= 1.10.0"

  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 6.0" }
  }

  backend "s3" {
    bucket              = "accountable-tofu-state-723225039926-eu-west-2"
    key                 = "cells/staging-01.tfstate"
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

  default_tags { tags = { Cell = "staging-01", Environment = "staging", ManagedBy = "opentofu", Project = "accountable" } }
}

provider "aws" {
  alias               = "global"
  region              = "us-east-1"
  allowed_account_ids = ["723225039926"]

  default_tags { tags = { Cell = "staging-01", Environment = "staging", ManagedBy = "opentofu", Project = "accountable" } }
}

module "cell" {
  source = "../../../../modules/cell"

  providers = { aws = aws, aws.global = aws.global }

  account_id                             = "723225039926"
  alarm_input_topic_arn                  = "arn:aws:sns:eu-west-2:723225039926:accountable-staging-observability-alarm-input"
  runbook_base_url                       = var.runbook_base_url
  dashboard_base_url                     = var.dashboard_base_url
  api_desired_count                      = var.api_desired_count
  availability_zones                     = ["eu-west-2a", "eu-west-2b"]
  cell_id                                = "staging-01"
  cell_lifecycle                         = "durable"
  collector_desired_count                = var.collector_desired_count
  collector_image_repository_arn         = "arn:aws:ecr:eu-west-2:723225039926:repository/accountable-collector"
  collector_image_uri                    = var.collector_image_uri
  collector_otlp_token_ref               = var.collector_otlp_token_ref
  configuration_revision                 = var.configuration_revision
  database_final_snapshot_identifier     = var.database_final_snapshot_identifier
  environment                            = "staging"
  grafana_instance_id                    = "1765140"
  grafana_otlp_private_dns               = var.grafana_otlp_private_dns
  grafana_otlp_private_link_service_name = var.grafana_otlp_private_link_service_name
  image_repository_arn                   = "arn:aws:ecr:eu-west-2:723225039926:repository/accountable"
  image_uri                              = var.image_uri
  synthetic_observation_enabled          = true
  synthetic_image_repository_arn         = "arn:aws:ecr:eu-west-2:723225039926:repository/accountable-k6"
  synthetic_image_uri                    = var.synthetic_image_uri
  previous_web_identity                  = var.previous_web_identity
  release_id                             = var.release_id
  release_manifest_digest                = var.release_manifest_digest
  web_asset_digest                       = var.web_asset_digest
  vpc_cidr                               = "10.40.0.0/16"
}
