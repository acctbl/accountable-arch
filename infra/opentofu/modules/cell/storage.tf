resource "aws_kms_key" "cell" {
  description             = "Encrypts ${local.name} data, database, and runtime secrets"
  deletion_window_in_days = 7
  enable_key_rotation     = true
  policy                  = data.aws_iam_policy_document.cell_key_bootstrap.json
  tags                    = { CellId = var.cell_id }

  lifecycle {
    precondition {
      condition     = data.aws_caller_identity.current.account_id == var.account_id
      error_message = "Authenticated AWS account does not match the cell account."
    }
  }
}

data "aws_iam_policy_document" "cell_key_bootstrap" {
  statement {
    sid       = "AccountRoot"
    actions   = ["kms:*"]
    resources = ["*"]

    principals {
      type        = "AWS"
      identifiers = ["arn:aws:iam::${var.account_id}:root"]
    }
  }

  statement {
    sid = "CellKeyAdministration"
    actions = [
      "kms:CreateAlias",
      "kms:CreateGrant",
      "kms:Decrypt",
      "kms:DeleteAlias",
      "kms:DescribeKey",
      "kms:EnableKeyRotation",
      "kms:GenerateDataKey",
      "kms:GetKeyPolicy",
      "kms:GetKeyRotationStatus",
      "kms:ListResourceTags",
      "kms:PutKeyPolicy",
      "kms:ScheduleKeyDeletion",
      "kms:TagResource",
      "kms:UntagResource",
      "kms:UpdateAlias",
    ]
    resources = ["*"]

    principals {
      type        = "AWS"
      identifiers = [local.cell_apply_role_arn]
    }
  }
}

resource "aws_kms_alias" "cell" {
  name          = "alias/${local.name}"
  target_key_id = aws_kms_key.cell.key_id
}

data "aws_iam_policy_document" "cell_key" {
  source_policy_documents = [data.aws_iam_policy_document.cell_key_bootstrap.json]

  statement {
    sid       = "CloudFrontWebRead"
    actions   = ["kms:Decrypt"]
    resources = ["*"]

    principals {
      type        = "Service"
      identifiers = ["cloudfront.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "AWS:SourceArn"
      values   = [aws_cloudfront_distribution.cell.arn]
    }
  }
}

resource "aws_kms_key_policy" "cell" {
  key_id = aws_kms_key.cell.id
  policy = data.aws_iam_policy_document.cell_key.json
}

resource "aws_s3_bucket" "data" {
  bucket        = local.data_bucket
  force_destroy = var.cell_lifecycle == "ephemeral"

  lifecycle {
    postcondition {
      condition     = self.force_destroy == (var.cell_lifecycle == "ephemeral")
      error_message = "The data bucket may erase objects during destroy only when the cell is ephemeral."
    }
  }
}

resource "aws_s3_bucket_ownership_controls" "data" {
  bucket = aws_s3_bucket.data.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_public_access_block" "data" {
  bucket = aws_s3_bucket.data.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true

  lifecycle {
    postcondition {
      condition     = self.block_public_acls && self.block_public_policy && self.ignore_public_acls && self.restrict_public_buckets
      error_message = "The data bucket must keep every S3 public-access control enabled."
    }
  }
}

resource "aws_s3_bucket_versioning" "data" {
  bucket = aws_s3_bucket.data.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "data" {
  bucket = aws_s3_bucket.data.id

  rule {
    apply_server_side_encryption_by_default {
      kms_master_key_id = aws_kms_key.cell.arn
      sse_algorithm     = "aws:kms"
    }

    bucket_key_enabled = true
  }
}

data "aws_iam_policy_document" "data_bucket" {
  statement {
    sid     = "RequireTLS"
    effect  = "Deny"
    actions = ["s3:*"]
    resources = [
      aws_s3_bucket.data.arn,
      "${aws_s3_bucket.data.arn}/*",
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

resource "aws_s3_bucket_policy" "data" {
  bucket = aws_s3_bucket.data.id
  policy = data.aws_iam_policy_document.data_bucket.json

  depends_on = [aws_s3_bucket_public_access_block.data]
}

resource "aws_s3_bucket" "web" {
  bucket        = local.web_bucket
  force_destroy = var.cell_lifecycle == "ephemeral"

  lifecycle {
    postcondition {
      condition     = self.force_destroy == (var.cell_lifecycle == "ephemeral")
      error_message = "The web bucket may erase objects during destroy only when the cell is ephemeral."
    }
  }
}

resource "aws_s3_bucket_ownership_controls" "web" {
  bucket = aws_s3_bucket.web.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_public_access_block" "web" {
  bucket = aws_s3_bucket.web.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true

  lifecycle {
    postcondition {
      condition     = self.block_public_acls && self.block_public_policy && self.ignore_public_acls && self.restrict_public_buckets
      error_message = "The web bucket must keep every S3 public-access control enabled."
    }
  }
}

resource "aws_s3_bucket_versioning" "web" {
  bucket = aws_s3_bucket.web.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "web" {
  bucket = aws_s3_bucket.web.id

  rule {
    apply_server_side_encryption_by_default {
      kms_master_key_id = aws_kms_key.cell.arn
      sse_algorithm     = "aws:kms"
    }

    bucket_key_enabled = true
  }
}

data "aws_iam_policy_document" "web_bucket" {
  statement {
    sid       = "CloudFrontRead"
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.web.arn}/*"]

    principals {
      type        = "Service"
      identifiers = ["cloudfront.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "AWS:SourceArn"
      values   = [aws_cloudfront_distribution.cell.arn]
    }
  }

  statement {
    sid     = "RequireTLS"
    effect  = "Deny"
    actions = ["s3:*"]
    resources = [
      aws_s3_bucket.web.arn,
      "${aws_s3_bucket.web.arn}/*",
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

resource "aws_s3_bucket_policy" "web" {
  bucket = aws_s3_bucket.web.id
  policy = data.aws_iam_policy_document.web_bucket.json

  depends_on = [aws_s3_bucket_public_access_block.web]
}
