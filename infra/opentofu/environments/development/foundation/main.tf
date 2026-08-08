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
    key                 = "foundations/account.tfstate"
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
      Environment = "development"
      ManagedBy   = "opentofu"
      Project     = "accountable"
    }
  }
}

module "environment" {
  source = "../../../modules/environment-foundation"

  account_id                 = "453722413624"
  environment                = "development"
  github_oidc_subject_prefix = "repo:acctbl@309473689/accountable@1318144297"
  region                     = "eu-west-2"
}

resource "aws_ecr_replication_configuration" "recovery" {
  replication_configuration {
    rule {
      destination {
        region      = "eu-west-2"
        registry_id = "063280428550"
      }

      repository_filter {
        filter      = module.environment.image_repository_name
        filter_type = "PREFIX_MATCH"
      }
    }
  }
}
