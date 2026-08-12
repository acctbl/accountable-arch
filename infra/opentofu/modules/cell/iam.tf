locals {
  execution_global_actions = ["ecr:GetAuthorizationToken"]
  execution_repository_actions = [
    "ecr:BatchCheckLayerAvailability",
    "ecr:BatchGetImage",
    "ecr:GetDownloadUrlForLayer",
  ]
  execution_log_actions = [
    "logs:CreateLogStream",
    "logs:PutLogEvents",
  ]
  bootstrap_secret_actions = ["secretsmanager:GetSecretValue"]
  bootstrap_key_actions = [
    "kms:Decrypt",
    "kms:DescribeKey",
  ]
  parameter_read_actions  = ["ssm:GetParameters"]
  parameter_write_actions = ["ssm:PutParameter"]
}

data "aws_iam_policy_document" "ecs_task_assume" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "execution" {
  name               = "${local.name}-execution"
  assume_role_policy = data.aws_iam_policy_document.ecs_task_assume.json
}

data "aws_iam_policy_document" "execution" {
  statement {
    actions   = local.execution_global_actions
    resources = ["*"]
  }

  statement {
    actions = local.execution_repository_actions
    resources = compact([
      var.image_repository_arn,
      var.collector_image_repository_arn,
      var.synthetic_image_repository_arn,
    ])
  }

  statement {
    actions   = local.execution_log_actions
    resources = ["${aws_cloudwatch_log_group.runtime.arn}:*"]
  }

}

resource "aws_iam_role_policy" "execution" {
  name   = "${local.name}-execution"
  policy = data.aws_iam_policy_document.execution.json
  role   = aws_iam_role.execution.id
}

resource "aws_iam_role" "bootstrap_execution" {
  name               = "${local.name}-bootstrap-execution"
  assume_role_policy = data.aws_iam_policy_document.ecs_task_assume.json
}

data "aws_iam_policy_document" "bootstrap_execution" {
  source_policy_documents = [data.aws_iam_policy_document.execution.json]

  statement {
    actions   = local.bootstrap_secret_actions
    resources = [aws_db_instance.cell.master_user_secret[0].secret_arn]
  }

  statement {
    actions   = local.bootstrap_key_actions
    resources = [aws_kms_key.cell.arn]
  }
}

resource "aws_iam_role_policy" "bootstrap_execution" {
  name   = "${local.name}-bootstrap-execution"
  policy = data.aws_iam_policy_document.bootstrap_execution.json
  role   = aws_iam_role.bootstrap_execution.id
}

resource "aws_iam_role" "synthetic_execution" {
  count = var.synthetic_observation_enabled ? 1 : 0

  name               = "${local.name}-synthetic-execution"
  assume_role_policy = data.aws_iam_policy_document.ecs_task_assume.json
}

data "aws_iam_policy_document" "synthetic_execution" {
  count = var.synthetic_observation_enabled ? 1 : 0

  statement {
    actions   = local.execution_global_actions
    resources = ["*"]
  }

  statement {
    actions   = local.execution_repository_actions
    resources = [var.synthetic_image_repository_arn]
  }

  statement {
    actions   = local.execution_log_actions
    resources = ["${aws_cloudwatch_log_group.runtime.arn}:*"]
  }

  statement {
    actions   = local.parameter_read_actions
    resources = ["${local.parameter_arn_prefix}/api/proof-key-active"]
  }

  statement {
    actions   = ["kms:Decrypt"]
    resources = [aws_kms_key.cell.arn]

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["ssm.${data.aws_region.current.region}.amazonaws.com"]
    }
  }
}

resource "aws_iam_role_policy" "synthetic_execution" {
  count = var.synthetic_observation_enabled ? 1 : 0

  name   = "${local.name}-synthetic-execution"
  policy = data.aws_iam_policy_document.synthetic_execution[0].json
  role   = aws_iam_role.synthetic_execution[0].id
}

resource "aws_iam_role" "synthetic" {
  count = var.synthetic_observation_enabled ? 1 : 0

  name               = "${local.name}-synthetic"
  assume_role_policy = data.aws_iam_policy_document.ecs_task_assume.json
}

resource "aws_iam_role" "api" {
  name               = "${local.name}-api"
  assume_role_policy = data.aws_iam_policy_document.ecs_task_assume.json
}

data "aws_iam_policy_document" "api" {
  statement {
    actions = [
      "s3:GetBucketPublicAccessBlock",
      "s3:GetEncryptionConfiguration",
      "s3:ListBucket",
    ]
    resources = [aws_s3_bucket.data.arn]
  }

  statement {
    actions = [
      "s3:DeleteObject",
      "s3:GetObject",
      "s3:PutObject",
    ]
    resources = ["${aws_s3_bucket.data.arn}/foundation/*"]
  }

  statement {
    actions = [
      "kms:Decrypt",
      "kms:DescribeKey",
      "kms:Encrypt",
      "kms:GenerateDataKey",
    ]
    resources = [aws_kms_key.cell.arn]
  }

  statement {
    actions = local.parameter_read_actions
    resources = [
      "${local.parameter_arn_prefix}/api/database-password",
      "${local.parameter_arn_prefix}/api/proof-key-active",
      "${local.parameter_arn_prefix}/api/proof-key-standby",
    ]
  }
}

resource "aws_iam_role_policy" "api" {
  name   = "${local.name}-api"
  policy = data.aws_iam_policy_document.api.json
  role   = aws_iam_role.api.id
}

resource "aws_iam_role" "migrate" {
  name               = "${local.name}-migrate"
  assume_role_policy = data.aws_iam_policy_document.ecs_task_assume.json
}

data "aws_iam_policy_document" "migrate" {
  statement {
    actions   = local.parameter_read_actions
    resources = ["${local.parameter_arn_prefix}/migrate/database-password"]
  }

  statement {
    actions   = ["kms:Decrypt"]
    resources = [aws_kms_key.cell.arn]

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["ssm.${data.aws_region.current.region}.amazonaws.com"]
    }
  }
}

resource "aws_iam_role_policy" "migrate" {
  name   = "${local.name}-migrate"
  policy = data.aws_iam_policy_document.migrate.json
  role   = aws_iam_role.migrate.id
}

resource "aws_iam_role" "bootstrap" {
  name               = "${local.name}-bootstrap"
  assume_role_policy = data.aws_iam_policy_document.ecs_task_assume.json
}

data "aws_iam_policy_document" "bootstrap" {
  statement {
    actions = concat(local.parameter_read_actions, local.parameter_write_actions)
    resources = [
      "${local.parameter_arn_prefix}/api/*",
      "${local.parameter_arn_prefix}/migrate/*",
    ]
  }

  # Standard-tier SecureString parameters are encrypted directly under the key,
  # so writes need kms:Encrypt. Advanced-tier envelope encryption would instead
  # need kms:GenerateDataKey. Keep this in step with the tier in secret_ssm.go.
  statement {
    actions = [
      "kms:Decrypt",
      "kms:Encrypt",
    ]
    resources = [aws_kms_key.cell.arn]

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["ssm.${data.aws_region.current.region}.amazonaws.com"]
    }
  }
}

resource "aws_iam_role_policy" "bootstrap" {
  name   = "${local.name}-bootstrap"
  policy = data.aws_iam_policy_document.bootstrap.json
  role   = aws_iam_role.bootstrap.id
}
