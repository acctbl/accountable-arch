terraform {
  required_version = ">= 1.10.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }

  backend "s3" {
    bucket              = "accountable-tofu-state-453722413624-eu-west-2"
    key                 = "cells/development-01.tfstate"
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

  default_tags {
    tags = {
      Cell        = "development-01"
      Environment = "development"
      ManagedBy   = "opentofu"
      Project     = "accountable"
    }
  }
}

provider "aws" {
  alias               = "global"
  region              = "us-east-1"
  allowed_account_ids = ["453722413624"]

  default_tags {
    tags = {
      Cell        = "development-01"
      Environment = "development"
      ManagedBy   = "opentofu"
      Project     = "accountable"
    }
  }
}

module "cell" {
  source = "../../../../modules/cell"

  providers = {
    aws        = aws
    aws.global = aws.global
  }

  account_id                             = "453722413624"
  alarm_input_topic_arn                  = "arn:aws:sns:eu-west-2:453722413624:accountable-development-observability-alarm-input"
  runbook_base_url                       = var.runbook_base_url
  dashboard_base_url                     = var.dashboard_base_url
  api_desired_count                      = var.api_desired_count
  availability_zones                     = ["eu-west-2a", "eu-west-2b"]
  cell_id                                = "development-01"
  cell_lifecycle                         = "ephemeral"
  collector_desired_count                = var.collector_desired_count
  collector_image_repository_arn         = "arn:aws:ecr:eu-west-2:453722413624:repository/accountable-collector"
  collector_image_uri                    = var.collector_image_uri
  collector_otlp_token_ref               = var.collector_otlp_token_ref
  configuration_revision                 = var.configuration_revision
  environment                            = "development"
  grafana_instance_id                    = "1765140"
  grafana_otlp_private_dns               = var.grafana_otlp_private_dns
  grafana_otlp_private_link_service_name = var.grafana_otlp_private_link_service_name
  image_repository_arn                   = "arn:aws:ecr:eu-west-2:453722413624:repository/accountable"
  image_uri                              = var.image_uri
  release_id                             = var.release_id
  release_manifest_digest                = var.release_manifest_digest
  previous_web_identity                  = var.previous_web_identity
  web_asset_digest                       = var.web_asset_digest
  vpc_cidr                               = "10.20.0.0/16"
}
