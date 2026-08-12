terraform {
  required_version = ">= 1.10.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }

  backend "s3" {
    bucket              = "accountable-tofu-state-576329195663-eu-west-2"
    key                 = "foundations/account.tfstate"
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

  default_tags {
    tags = {
      Environment = "production"
      ManagedBy   = "opentofu"
      Project     = "accountable"
    }
  }
}

data "aws_iam_policy_document" "development_replication" {
  statement {
    actions = ["ecr:ReplicateImage"]
    resources = [
      "arn:aws:ecr:eu-west-2:576329195663:repository/accountable",
      "arn:aws:ecr:eu-west-2:576329195663:repository/accountable-collector",
      "arn:aws:ecr:eu-west-2:576329195663:repository/accountable-k6",
    ]

    principals {
      type        = "AWS"
      identifiers = ["arn:aws:iam::453722413624:root"]
    }
  }
}

resource "aws_ecr_registry_policy" "development_replication" {
  policy = data.aws_iam_policy_document.development_replication.json
}

module "environment" {
  source = "../../../modules/environment-foundation"

  account_id                 = "576329195663"
  environment                = "production"
  github_oidc_subject_prefix = "repo:acctbl@309473689/accountable@1318144297"
  github_repository          = "acctbl/accountable"
  region                     = "eu-west-2"
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
