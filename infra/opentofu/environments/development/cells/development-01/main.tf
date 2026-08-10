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

  account_id             = "453722413624"
  api_desired_count      = var.api_desired_count
  availability_zones     = ["eu-west-2a", "eu-west-2b"]
  cell_id                = "development-01"
  configuration_revision = var.configuration_revision
  cell_lifecycle         = "ephemeral"
  environment            = "development"
  image_repository_arn   = "arn:aws:ecr:eu-west-2:453722413624:repository/accountable"
  image_uri              = var.image_uri
  vpc_cidr               = "10.20.0.0/16"
}
