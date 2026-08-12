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
  github_repository          = "acctbl/accountable"
  region                     = "eu-west-2"
}

# Release identity storage is global to the system: a cell destroy never removes
# it, so it lives in the environment foundation rather than in a cell.
module "release_storage" {
  source = "../../../modules/release-storage"

  account_id       = "453722413624"
  region           = "eu-west-2"
  writer_role_name = "accountable-development-apply"
  reader_role_names = [
    "accountable-development-plan",
  ]
  external_reader_role_arns = [
    "arn:aws:iam::723225039926:role/accountable-staging-plan",
    "arn:aws:iam::723225039926:role/accountable-staging-apply",
    "arn:aws:iam::576329195663:role/accountable-production-plan",
    "arn:aws:iam::576329195663:role/accountable-production-apply",
  ]
  external_writer_role_arns = [
    "arn:aws:iam::723225039926:role/accountable-staging-apply",
    "arn:aws:iam::576329195663:role/accountable-production-apply",
  ]
}

resource "aws_ecr_repository" "collector" {
  name                 = "accountable-collector"
  image_tag_mutability = "IMMUTABLE"

  encryption_configuration {
    encryption_type = "AES256"
  }

  image_scanning_configuration {
    scan_on_push = true
  }
}

resource "aws_ecr_replication_configuration" "recovery" {
  replication_configuration {
    rule {
      dynamic "destination" {
        for_each = toset(["063280428550", "723225039926", "576329195663"])

        content {
          region      = "eu-west-2"
          registry_id = destination.value
        }
      }

      repository_filter {
        filter      = module.environment.image_repository_name
        filter_type = "PREFIX_MATCH"
      }

      repository_filter {
        filter      = aws_ecr_repository.collector.name
        filter_type = "PREFIX_MATCH"
      }

      repository_filter {
        filter      = module.environment.synthetic_image_repository_name
        filter_type = "PREFIX_MATCH"
      }
    }
  }
}
