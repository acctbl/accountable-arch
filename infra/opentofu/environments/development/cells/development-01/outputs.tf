output "api_security_group_id" {
  value = module.cell.api_security_group_id
}

output "api_load_balancer_arn" {
  value = module.cell.api_load_balancer_arn
}

output "api_target_group_arn" {
  value = module.cell.api_target_group_arn
}

output "api_task_role_arn" {
  value = module.cell.api_task_role_arn
}

output "application_subnet_ids" {
  value = module.cell.application_subnet_ids
}

output "bootstrap_security_group_id" {
  value = module.cell.bootstrap_security_group_id
}

output "bootstrap_task_definition_arn" {
  value = module.cell.bootstrap_task_definition_arn
}

output "bootstrap_task_role_arn" {
  value = module.cell.bootstrap_task_role_arn
}

output "data_bucket_name" {
  value = module.cell.data_bucket_name
}

output "cloudfront_distribution_id" {
  value = module.cell.cloudfront_distribution_id
}

output "cloudfront_domain" {
  value = module.cell.cloudfront_domain
}

output "ecs_cluster_arn" {
  value = module.cell.ecs_cluster_arn
}

output "ecs_service_name" {
  value = module.cell.ecs_service_name
}

output "migrate_security_group_id" {
  value = module.cell.migrate_security_group_id
}

output "migrate_task_definition_arn" {
  value = module.cell.migrate_task_definition_arn
}

output "migrate_task_role_arn" {
  value = module.cell.migrate_task_role_arn
}

output "preflight_task_definition_arn" {
  value = module.cell.preflight_task_definition_arn
}

output "rds_instance_identifier" {
  value = module.cell.rds_instance_identifier
}

output "web_bucket_name" {
  value = module.cell.web_bucket_name
}

output "web_runtime_config_base64" { value = module.cell.web_runtime_config_base64 }
output "web_identity" { value = module.cell.web_identity }
output "collector_service_name" { value = module.cell.collector_service_name }
output "runtime_log_group_name" { value = module.cell.runtime_log_group_name }
