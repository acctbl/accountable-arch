locals {
  bucket_name        = "accountable-releases-${var.account_id}-${var.region}"
  release_prefix     = "releases/v1"
  inventory_prefix   = "observability/v1/cell-inventory"
  promotion_prefix   = "observability/v1/promotion-evidence"
  protected_prefixes = ["${local.release_prefix}/*", "${local.inventory_prefix}/*", "${local.promotion_prefix}/*"]
}

data "aws_iam_policy_document" "release_key" {
  statement {
    actions   = ["kms:*"]
    resources = ["*"]

    principals {
      type        = "AWS"
      identifiers = ["arn:aws:iam::${var.account_id}:root"]
    }
  }

  dynamic "statement" {
    for_each = length(var.external_reader_role_arns) == 0 ? [] : [1]

    content {
      actions   = ["kms:Decrypt", "kms:DescribeKey"]
      resources = ["*"]

      principals {
        type        = "AWS"
        identifiers = var.external_reader_role_arns
      }
    }
  }

  dynamic "statement" {
    for_each = length(var.external_writer_role_arns) == 0 ? [] : [1]

    content {
      actions   = ["kms:Decrypt", "kms:DescribeKey", "kms:Encrypt", "kms:GenerateDataKey"]
      resources = ["*"]

      principals {
        type        = "AWS"
        identifiers = var.external_writer_role_arns
      }
    }
  }

  statement {
    effect = "Deny"
    actions = [
      "kms:DisableKey",
      "kms:ScheduleKeyDeletion",
    ]
    resources = ["*"]

    principals {
      type        = "*"
      identifiers = ["*"]
    }
  }
}

resource "aws_kms_key" "releases" {
  description             = "Accountable protected release and cell-inventory artifacts"
  deletion_window_in_days = 30
  enable_key_rotation     = true
  policy                  = data.aws_iam_policy_document.release_key.json

  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_kms_alias" "releases" {
  name          = "alias/accountable-releases"
  target_key_id = aws_kms_key.releases.key_id
}

resource "aws_s3_bucket" "releases" {
  bucket              = local.bucket_name
  object_lock_enabled = true

  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_s3_bucket_object_lock_configuration" "releases" {
  bucket = aws_s3_bucket.releases.id

  rule {
    default_retention {
      mode  = "COMPLIANCE"
      years = 7
    }
  }

  depends_on = [aws_s3_bucket_versioning.releases]
}

# Versioning is the immutability foundation: the bucket policy denies every delete
# and every versioning change, so a later write can never remove earlier bytes.
resource "aws_s3_bucket_versioning" "releases" {
  bucket = aws_s3_bucket.releases.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "releases" {
  bucket = aws_s3_bucket.releases.id

  rule {
    bucket_key_enabled = true

    apply_server_side_encryption_by_default {
      kms_master_key_id = aws_kms_key.releases.arn
      sse_algorithm     = "aws:kms"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "releases" {
  bucket                  = aws_s3_bucket.releases.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "releases" {
  bucket = aws_s3_bucket.releases.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

data "aws_iam_policy_document" "releases" {
  statement {
    sid       = "DenyInsecureTransport"
    effect    = "Deny"
    actions   = ["s3:*"]
    resources = [aws_s3_bucket.releases.arn, "${aws_s3_bucket.releases.arn}/*"]

    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["false"]
    }
  }

  dynamic "statement" {
    for_each = length(var.external_reader_role_arns) == 0 ? [] : [1]

    content {
      sid = "AllowProtectedCrossAccountReaders"
      actions = [
        "s3:GetObject",
        "s3:GetObjectVersion",
        "s3:ListBucket",
      ]
      resources = [aws_s3_bucket.releases.arn, "${aws_s3_bucket.releases.arn}/*"]

      principals {
        type        = "AWS"
        identifiers = var.external_reader_role_arns
      }
    }
  }

  dynamic "statement" {
    for_each = length(var.external_writer_role_arns) == 0 ? [] : [1]

    content {
      sid = "AllowProtectedCrossAccountWriters"
      actions = [
        "s3:GetObject",
        "s3:ListBucket",
        "s3:PutObject",
      ]
      resources = [
        aws_s3_bucket.releases.arn,
        "${aws_s3_bucket.releases.arn}/${local.inventory_prefix}/*",
        "${aws_s3_bucket.releases.arn}/${local.promotion_prefix}/*",
      ]

      principals {
        type        = "AWS"
        identifiers = var.external_writer_role_arns
      }
    }
  }

  statement {
    sid    = "DenyUnencryptedObjectUploads"
    effect = "Deny"
    actions = [
      "s3:PutObject",
    ]
    resources = ["${aws_s3_bucket.releases.arn}/*"]

    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "StringNotEquals"
      variable = "s3:x-amz-server-side-encryption-aws-kms-key-id"
      values   = [aws_kms_key.releases.arn]
    }
  }

  statement {
    sid    = "DenyDeletionOfProtectedArtifacts"
    effect = "Deny"
    actions = [
      "s3:DeleteObject",
      "s3:DeleteObjectVersion",
      "s3:PutLifecycleConfiguration",
      "s3:PutBucketVersioning",
    ]
    resources = [aws_s3_bucket.releases.arn, "${aws_s3_bucket.releases.arn}/*"]

    principals {
      type        = "*"
      identifiers = ["*"]
    }
  }

  # Versioning keeps earlier bytes, but a later write would still shadow a
  # content-addressed object. Requiring the conditional-write header makes every
  # write create-only.
  statement {
    sid       = "RequireConditionalWrites"
    effect    = "Deny"
    actions   = ["s3:PutObject"]
    resources = ["${aws_s3_bucket.releases.arn}/*"]

    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "Null"
      variable = "s3:if-none-match"
      values   = ["true"]
    }
  }

  # s3:prefix is a list-operation key and is absent from PutObject, so a negated
  # condition on it would deny every write. Object keys are restricted through
  # resource ARNs instead.
  statement {
    sid     = "DenyWritesOutsideProtectedPrefixes"
    effect  = "Deny"
    actions = ["s3:PutObject"]
    not_resources = [
      for prefix in local.protected_prefixes : "${aws_s3_bucket.releases.arn}/${prefix}"
    ]

    principals {
      type        = "*"
      identifiers = ["*"]
    }
  }
}

resource "aws_s3_bucket_policy" "releases" {
  bucket = aws_s3_bucket.releases.id
  policy = data.aws_iam_policy_document.releases.json

  depends_on = [aws_s3_bucket_public_access_block.releases]
}

data "aws_iam_policy_document" "writer" {
  statement {
    actions   = ["s3:GetObjectRetention", "s3:PutObject", "s3:PutObjectRetention"]
    resources = [for prefix in local.protected_prefixes : "${aws_s3_bucket.releases.arn}/${prefix}"]
  }

  statement {
    actions   = ["s3:GetObject", "s3:ListBucket", "s3:ListBucketVersions"]
    resources = [aws_s3_bucket.releases.arn, "${aws_s3_bucket.releases.arn}/*"]
  }

  statement {
    actions   = ["kms:Encrypt", "kms:Decrypt", "kms:GenerateDataKey", "kms:DescribeKey"]
    resources = [aws_kms_key.releases.arn]
  }
}

resource "aws_iam_role_policy" "writer" {
  name   = "accountable-release-writer"
  policy = data.aws_iam_policy_document.writer.json
  role   = var.writer_role_name
}

data "aws_iam_policy_document" "reader" {
  statement {
    actions   = ["s3:GetObject", "s3:ListBucket"]
    resources = [aws_s3_bucket.releases.arn, "${aws_s3_bucket.releases.arn}/*"]
  }

  statement {
    actions   = ["kms:Decrypt", "kms:DescribeKey"]
    resources = [aws_kms_key.releases.arn]
  }
}

resource "aws_iam_role_policy" "reader" {
  for_each = toset(var.reader_role_names)

  name   = "accountable-release-reader"
  policy = data.aws_iam_policy_document.reader.json
  role   = each.value
}
