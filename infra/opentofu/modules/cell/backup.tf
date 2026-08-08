locals {
  durable_cell = var.cell_lifecycle == "durable" ? 1 : 0
}

data "aws_iam_policy_document" "backup_assume" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["backup.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "backup" {
  count = local.durable_cell

  name               = "${local.name}-backup"
  assume_role_policy = data.aws_iam_policy_document.backup_assume.json
}

resource "aws_iam_role_policy_attachment" "backup" {
  count = local.durable_cell

  role       = aws_iam_role.backup[0].name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSBackupServiceRolePolicyForBackup"
}

resource "aws_backup_vault" "cell" {
  count = local.durable_cell

  name        = local.name
  kms_key_arn = aws_kms_key.cell.arn
}

resource "aws_backup_plan" "cell" {
  count = local.durable_cell

  name = local.name

  rule {
    rule_name         = "monthly"
    target_vault_name = aws_backup_vault.cell[0].name
    schedule          = "cron(0 3 1 * ? *)"

    lifecycle {
      delete_after = 365
    }
  }
}

resource "aws_backup_selection" "cell" {
  count = local.durable_cell

  name         = local.name
  iam_role_arn = aws_iam_role.backup[0].arn
  plan_id      = aws_backup_plan.cell[0].id
  resources    = [aws_db_instance.cell.arn]
}
