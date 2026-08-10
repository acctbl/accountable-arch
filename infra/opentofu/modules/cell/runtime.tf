locals {
  bootstrap_config = templatefile("${path.module}/templates/bootstrap.toml.tftpl", {
    aws_region     = data.aws_region.current.region
    database_host  = aws_db_instance.cell.address
    environment    = var.environment
    kms_key_arn    = aws_kms_key.cell.arn
    parameter_root = local.parameter_root
  })
  runtime_template_values = {
    account_id  = var.account_id
    aws_region  = data.aws_region.current.region
    cell_id     = var.cell_id
    data_bucket = aws_s3_bucket.data.bucket

    database_host = aws_db_instance.cell.address
    environment   = var.environment
    kms_key_arn   = aws_kms_key.cell.arn
    revision      = var.configuration_revision
  }

  api_config = templatefile("${path.module}/templates/api.toml.tftpl", merge(local.runtime_template_values, {
    cloudfront_domain   = aws_cloudfront_distribution.cell.domain_name
    parameter_path      = "${local.parameter_root}/api"
    trusted_proxy_cidrs = join(", ", [for cidr in values(local.application_subnets) : jsonencode(cidr)])
  }))

  migrate_config = templatefile("${path.module}/templates/migrate.toml.tftpl", merge(local.runtime_template_values, {
    parameter_path = "${local.parameter_root}/migrate"
  }))

  preflight_config = templatefile("${path.module}/templates/preflight.toml.tftpl", merge(local.runtime_template_values, {
    parameter_path = "${local.parameter_root}/api"
  }))
}
