data "aws_caller_identity" "current" {}

locals {
  contract_role_name = "accountable-${var.environment}-contract"
  github_oidc_subjects = [
    for workflow in ["ci.yml", "managed-contract.yml"] :
    "${var.github_oidc_subject_prefix}:environment:${var.github_environment}:workflow_ref:${var.github_repository}/.github/workflows/${workflow}@refs/heads/main"
  ]
  insecure_bucket_name = "accountable-${var.environment}-insecure-${var.account_id}-${var.region}"
  secure_bucket_name   = "accountable-${var.environment}-contract-${var.account_id}-${var.region}"
  contract_buckets = {
    secure   = local.secure_bucket_name
    insecure = local.insecure_bucket_name
  }

  # A fixture, not a credential: the contract proves the adapter returns these
  # exact bytes through a SecureString read, so the value is deliberately public.
  contract_parameter_path  = "/accountable/${var.environment}/contract"
  contract_parameter_name  = "managed-contract-proof"
  contract_parameter_value = "accountable-parameter-store-contract"
}

resource "aws_kms_key" "storage" {
  description             = "Encrypts Accountable ${var.environment} contract objects"
  deletion_window_in_days = 30
  enable_key_rotation     = true

  lifecycle {
    precondition {
      condition     = data.aws_caller_identity.current.account_id == var.account_id
      error_message = "Authenticated AWS account does not match the managed contract root."
    }
  }

  tags = {
    Purpose = "contract-storage"
  }
}

resource "aws_kms_alias" "storage" {
  name          = "alias/accountable-${var.environment}-storage"
  target_key_id = aws_kms_key.storage.key_id
}

resource "aws_kms_key" "crypto" {
  description             = "Proves Accountable application encryption against AWS KMS"
  deletion_window_in_days = 30
  enable_key_rotation     = true

  tags = {
    Purpose = "contract-crypto"
  }
}

resource "aws_kms_alias" "crypto" {
  name          = "alias/accountable-${var.environment}-crypto"
  target_key_id = aws_kms_key.crypto.key_id
}

resource "aws_s3_bucket" "contract" {
  for_each = local.contract_buckets
  bucket   = each.value
}

resource "aws_s3_bucket_ownership_controls" "contract" {
  for_each = local.contract_buckets
  bucket   = aws_s3_bucket.contract[each.key].id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

# The insecure bucket exists only to prove the adapter rejects any disabled public-access guard.
#trivy:ignore:AVD-AWS-0086:exp:2026-11-05 trivy:ignore:AVD-AWS-0087:exp:2026-11-05 trivy:ignore:AVD-AWS-0091:exp:2026-11-05 trivy:ignore:AVD-AWS-0093:exp:2026-11-05
resource "aws_s3_bucket_public_access_block" "contract" {
  for_each = local.contract_buckets
  bucket   = aws_s3_bucket.contract[each.key].id

  block_public_acls       = each.key == "secure"
  block_public_policy     = each.key == "secure"
  ignore_public_acls      = each.key == "secure"
  restrict_public_buckets = each.key == "secure"
}

resource "aws_s3_bucket_server_side_encryption_configuration" "contract" {
  for_each = local.contract_buckets
  bucket   = aws_s3_bucket.contract[each.key].id

  rule {
    apply_server_side_encryption_by_default {
      kms_master_key_id = aws_kms_key.storage.arn
      sse_algorithm     = "aws:kms"
    }

    bucket_key_enabled = true
  }
}

data "aws_iam_policy_document" "contract_bucket" {
  for_each = local.contract_buckets

  statement {
    sid     = "RequireTLS"
    effect  = "Deny"
    actions = ["s3:*"]
    resources = [
      aws_s3_bucket.contract[each.key].arn,
      "${aws_s3_bucket.contract[each.key].arn}/*",
    ]

    principals {
      type        = "AWS"
      identifiers = ["*"]
    }

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["false"]
    }
  }
}

resource "aws_s3_bucket_policy" "contract" {
  for_each = local.contract_buckets
  bucket   = aws_s3_bucket.contract[each.key].id
  policy   = data.aws_iam_policy_document.contract_bucket[each.key].json
}

data "aws_iam_openid_connect_provider" "github" {
  url = "https://token.actions.githubusercontent.com"
}

data "aws_iam_policy_document" "github_assume_role" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [data.aws_iam_openid_connect_provider.github.arn]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:sub"
      values   = local.github_oidc_subjects
    }
  }
}

resource "aws_iam_role" "contract" {
  name                 = local.contract_role_name
  assume_role_policy   = data.aws_iam_policy_document.github_assume_role.json
  max_session_duration = 3600
}

resource "aws_ssm_parameter" "contract" {
  name   = "${local.contract_parameter_path}/${local.contract_parameter_name}"
  type   = "SecureString"
  key_id = aws_kms_key.storage.arn
  value  = local.contract_parameter_value
  tier   = "Standard"

  tags = {
    Purpose = "contract-secret"
  }
}

data "aws_iam_policy_document" "contract" {
  statement {
    sid       = "ReadContractParameter"
    actions   = ["ssm:GetParameters"]
    resources = [aws_ssm_parameter.contract.arn]
  }

  statement {
    sid = "InspectContractBuckets"
    actions = [
      "s3:GetBucketPublicAccessBlock",
      "s3:GetEncryptionConfiguration",
    ]
    resources = [
      "arn:aws:s3:::${local.secure_bucket_name}",
      "arn:aws:s3:::${local.insecure_bucket_name}",
    ]
  }

  statement {
    sid = "RoundTripContractObjects"
    actions = [
      "s3:DeleteObject",
      "s3:GetObject",
      "s3:PutObject",
    ]
    resources = ["arn:aws:s3:::${local.secure_bucket_name}/contract/*"]
  }

  statement {
    sid = "UseContractKeys"
    actions = [
      "kms:Decrypt",
      "kms:DescribeKey",
      "kms:Encrypt",
      "kms:GenerateDataKey",
    ]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "aws:ResourceTag/Project"
      values   = ["accountable"]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:ResourceTag/Environment"
      values   = [var.environment]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:ResourceTag/Purpose"
      values   = ["contract-storage", "contract-crypto"]
    }
  }
}

resource "aws_iam_role_policy" "contract" {
  name   = local.contract_role_name
  role   = aws_iam_role.contract.id
  policy = data.aws_iam_policy_document.contract.json
}
