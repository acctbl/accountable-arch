output "account_id" {
  value = module.environment.account_id
}

output "apply_role_arn" {
  value = module.environment.apply_role_arn
}

output "foundation_apply_role_arn" {
  value = module.environment.foundation_apply_role_arn
}

output "image_repository_url" {
  value = module.environment.image_repository_url
}

output "collector_repository_arn" {
  value = aws_ecr_repository.collector.arn
}

output "collector_repository_url" {
  value = aws_ecr_repository.collector.repository_url
}

output "synthetic_repository_arn" {
  value = module.environment.synthetic_image_repository_arn
}

output "synthetic_repository_url" {
  value = module.environment.synthetic_image_repository_url
}

output "plan_role_arn" {
  value = module.environment.plan_role_arn
}
