locals {
  web_architecture_probe = var.environment != "production"
  web_runtime_config = jsonencode({
    schema_version         = 1
    api_base_url           = "https://${aws_cloudfront_distribution.cell.domain_name}/api"
    architecture_probe     = local.web_architecture_probe
    configuration_revision = var.configuration_revision
  })
  web_configuration_fingerprint = sha256(local.web_runtime_config)

  bootstrap_config = templatefile("${path.module}/templates/bootstrap.toml.tftpl", {
    aws_region         = data.aws_region.current.region
    cell_id            = var.cell_id
    collector_endpoint = local.collector_endpoint
    database_host      = aws_db_instance.cell.address
    environment        = var.environment
    kms_key_arn        = aws_kms_key.cell.arn
    parameter_root     = local.parameter_root
    revision           = var.configuration_revision
  })
  runtime_template_values = {
    account_id  = var.account_id
    aws_region  = data.aws_region.current.region
    cell_id     = var.cell_id
    data_bucket = aws_s3_bucket.data.bucket

    collector_endpoint = local.collector_endpoint
    database_host      = aws_db_instance.cell.address
    environment        = var.environment
    kms_key_arn        = aws_kms_key.cell.arn
    revision           = var.configuration_revision
  }

  api_config = templatefile("${path.module}/templates/api.toml.tftpl", merge(local.runtime_template_values, {
    cloudfront_domain      = aws_cloudfront_distribution.cell.domain_name
    parameter_path         = "${local.parameter_root}/api"
    trusted_proxy_cidrs    = join(", ", [for cidr in values(local.application_subnets) : jsonencode(cidr)])
    web_release_id         = var.release_id
    web_asset_digest       = var.web_asset_digest
    web_config_revision    = var.configuration_revision
    web_config_fingerprint = local.web_configuration_fingerprint
    architecture_probe     = local.web_architecture_probe
    previous_web_identity  = var.previous_web_identity
  }))

  migrate_config = templatefile("${path.module}/templates/migrate.toml.tftpl", merge(local.runtime_template_values, {
    parameter_path = "${local.parameter_root}/migrate"
  }))

  preflight_config = templatefile("${path.module}/templates/preflight.toml.tftpl", merge(local.runtime_template_values, {
    parameter_path = "${local.parameter_root}/api"
  }))
}
