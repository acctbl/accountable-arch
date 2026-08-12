data "aws_caller_identity" "current" {}

locals {
  name                = "accountable-${var.cell_id}"
  cell_apply_role_arn = "arn:aws:iam::${var.account_id}:role/accountable-${var.environment}-apply"

  public_subnets = {
    for index, availability_zone in var.availability_zones : availability_zone => cidrsubnet(var.vpc_cidr, 4, index)
  }
  application_subnets = {
    for index, availability_zone in var.availability_zones : availability_zone => cidrsubnet(var.vpc_cidr, 4, index + 4)
  }
  database_subnets = {
    for index, availability_zone in var.availability_zones : availability_zone => cidrsubnet(var.vpc_cidr, 4, index + 8)
  }

  api_port      = 8080
  database_port = 5432

  scratch_path                 = "/run/accountable"
  runtime_log_group_name       = "/accountable/${var.cell_id}"
  runtime_log_group_arn        = "arn:aws:logs:${data.aws_region.current.region}:${var.account_id}:log-group:${local.runtime_log_group_name}"
  collector_application_port   = 4318
  collector_sli_port           = 4319
  collector_grafana_token_file = "/run/accountable/grafana-token"
  collector_config_file        = "/run/accountable/collector.yaml"
  data_bucket                  = "${local.name}-${var.account_id}-data"
  web_bucket                   = "${local.name}-${var.account_id}-web"

  parameter_root = "/accountable/${var.cell_id}/${var.environment}"
  parameter_arn_prefix = join("", [
    "arn:aws:ssm:${data.aws_region.current.region}:${var.account_id}:parameter",
    local.parameter_root,
  ])
}
