output "account_id" {
  value = module.environment.account_id
}

output "environment" {
  value = module.environment.environment
}

output "image_repository_arn" {
  value = module.environment.image_repository_arn
}

output "image_repository_url" {
  value = module.environment.image_repository_url
}

output "apply_role_arn" {
  value = module.environment.apply_role_arn
}

output "foundation_apply_role_arn" {
  value = module.environment.foundation_apply_role_arn
}

output "plan_role_arn" {
  value = module.environment.plan_role_arn
}

output "release_bucket" {
  value = module.release_storage.bucket
}

output "release_kms_key_arn" {
  value = module.release_storage.kms_key_arn
}

output "collector_image_repository_arn" {
  value = aws_ecr_repository.collector.arn
}

output "collector_image_repository_url" {
  value = aws_ecr_repository.collector.repository_url
}

output "synthetic_image_repository_arn" {
  value = module.environment.synthetic_image_repository_arn
}

output "synthetic_image_repository_url" {
  value = module.environment.synthetic_image_repository_url
}
