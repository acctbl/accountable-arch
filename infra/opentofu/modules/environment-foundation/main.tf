module "account" {
  source = "../account-foundation"

  account_id  = var.account_id
  environment = var.environment
}

module "github" {
  source = "../github-deployment"

  account_id                 = var.account_id
  environment                = var.environment
  github_oidc_subject_prefix = var.github_oidc_subject_prefix
  github_repository          = var.github_repository
  region                     = var.region
}

resource "aws_kms_key" "observability" {
  description             = "Accountable ${var.environment} observability credentials, alerts, and logs"
  deletion_window_in_days = 30
  enable_key_rotation     = true
  policy                  = data.aws_iam_policy_document.observability_key.json

  lifecycle { prevent_destroy = true }
}

resource "aws_kms_alias" "observability" {
  name          = "alias/accountable-${var.environment}-observability"
  target_key_id = aws_kms_key.observability.key_id
}

data "aws_iam_policy_document" "observability_key" {
  statement {
    actions   = ["kms:*"]
    resources = ["*"]

    principals {
      type        = "AWS"
      identifiers = ["arn:aws:iam::${var.account_id}:root"]
    }
  }

  statement {
    actions   = ["kms:Decrypt", "kms:Describe*", "kms:Encrypt", "kms:GenerateDataKey*", "kms:ReEncrypt*"]
    resources = ["*"]

    principals {
      type        = "Service"
      identifiers = ["cloudwatch.amazonaws.com", "events.amazonaws.com", "logs.${var.region}.amazonaws.com", "sns.amazonaws.com"]
    }
  }
}

resource "aws_ecr_repository" "application" {
  name                 = "accountable"
  image_tag_mutability = "IMMUTABLE"

  encryption_configuration {
    encryption_type = "AES256"
  }

  image_scanning_configuration {
    scan_on_push = true
  }
}

resource "aws_ecr_lifecycle_policy" "application" {
  repository = aws_ecr_repository.application.name
  policy = jsonencode({
    rules = [{
      action       = { type = "expire" }
      description  = "Retain the newest twenty production images"
      rulePriority = 1
      selection = {
        countNumber = 20
        countType   = "imageCountMoreThan"
        tagStatus   = "any"
      }
    }]
  })
}

resource "aws_ecr_repository" "synthetic" {
  name                 = "accountable-k6"
  image_tag_mutability = "IMMUTABLE"

  encryption_configuration {
    encryption_type = "AES256"
  }

  image_scanning_configuration {
    scan_on_push = true
  }
}

resource "aws_ecr_lifecycle_policy" "synthetic" {
  repository = aws_ecr_repository.synthetic.name
  policy = jsonencode({
    rules = [{
      action       = { type = "expire" }
      description  = "Retain the newest twenty signed synthetic workload images"
      rulePriority = 1
      selection = {
        countNumber = 20
        countType   = "imageCountMoreThan"
        tagStatus   = "any"
      }
    }]
  })
}
