output "api_security_group_id" {
  value = aws_security_group.api.id
}

output "api_load_balancer_arn" {
  value = aws_lb.api.arn
}

output "api_target_group_arn" {
  value = aws_lb_target_group.api.arn
}

output "api_task_role_arn" {
  value = aws_iam_role.api.arn
}

output "application_subnet_ids" {
  value = [for subnet in aws_subnet.application : subnet.id]
}

output "bootstrap_security_group_id" {
  value = aws_security_group.bootstrap.id
}

output "bootstrap_task_role_arn" {
  value = aws_iam_role.bootstrap.arn
}

output "bootstrap_task_definition_arn" {
  value = aws_ecs_task_definition.bootstrap.arn
}

output "data_bucket_name" {
  value = aws_s3_bucket.data.bucket
}

output "cloudfront_distribution_id" {
  value = aws_cloudfront_distribution.cell.id
}

output "cloudfront_domain" {
  value = aws_cloudfront_distribution.cell.domain_name
}

output "ecs_cluster_arn" {
  value = aws_ecs_cluster.cell.arn
}

output "ecs_service_name" {
  value = aws_ecs_service.api.name
}

output "migrate_security_group_id" {
  value = aws_security_group.migrate.id
}

output "migrate_task_role_arn" {
  value = aws_iam_role.migrate.arn
}

output "migrate_task_definition_arn" {
  value = aws_ecs_task_definition.migrate.arn
}

output "preflight_task_definition_arn" {
  value = aws_ecs_task_definition.preflight.arn
}

output "rds_instance_identifier" {
  value = aws_db_instance.cell.identifier
}

output "web_bucket_name" {
  value = aws_s3_bucket.web.bucket
}

output "web_runtime_config_base64" {
  value = base64encode(local.web_runtime_config)
}

output "web_identity" {
  value = {
    release_id                   = var.release_id
    web_asset_digest             = var.web_asset_digest
    configuration_schema_version = 1
    configuration_revision       = var.configuration_revision
    configuration_fingerprint    = local.web_configuration_fingerprint
  }
}

output "runtime_log_group_name" {
  value = local.runtime_log_group_name
}

output "collector_service_name" {
  value = aws_ecs_service.collector.name
}

output "collector_security_group_id" {
  value = aws_security_group.collector.id
}

output "emf_sli_log_group_name" {
  value = aws_cloudwatch_log_group.emf_sli.name
}

output "emf_internal_log_group_name" {
  value = aws_cloudwatch_log_group.emf_internal.name
}

output "kms_key_arn" {
  value = aws_kms_key.cell.arn
}

output "collector_parameter_path" {
  value = local.collector_parameter_path
}

output "synthetic_service_name" {
  value = try(aws_ecs_service.synthetic[0].name, null)
}
