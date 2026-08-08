terraform {
  required_version = ">= 1.10.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }

  backend "s3" {
    bucket              = "accountable-tofu-state-063280428550-eu-west-2"
    key                 = "foundations/account.tfstate"
    region              = "eu-west-2"
    encrypt             = true
    kms_key_id          = "arn:aws:kms:eu-west-2:063280428550:alias/accountable-security-backup-tofu-state"
    use_lockfile        = true
    allowed_account_ids = ["063280428550"]
  }
}

provider "aws" {
  region              = "eu-west-2"
  allowed_account_ids = ["063280428550"]

  default_tags {
    tags = {
      Environment = "security-backup"
      ManagedBy   = "opentofu"
      Project     = "accountable"
    }
  }
}

module "account" {
  source = "../../../modules/account-foundation"

  account_id  = "063280428550"
  environment = "security-backup"
}

data "aws_iam_policy_document" "image_recovery" {
  statement {
    sid     = "ReplicateDevelopmentImages"
    actions = ["ecr:CreateRepository", "ecr:ReplicateImage"]

    principals {
      type        = "AWS"
      identifiers = ["arn:aws:iam::453722413624:root"]
    }

    resources = ["arn:aws:ecr:eu-west-2:063280428550:repository/accountable"]
  }
}

resource "aws_ecr_registry_policy" "image_recovery" {
  policy = data.aws_iam_policy_document.image_recovery.json
}
