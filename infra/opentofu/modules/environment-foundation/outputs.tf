output "account_id" {
  value = module.account.account_id
}

output "environment" {
  value = module.account.environment
}

output "image_repository_arn" {
  value = aws_ecr_repository.application.arn
}

output "image_repository_name" {
  value = aws_ecr_repository.application.name
}

output "image_repository_url" {
  value = aws_ecr_repository.application.repository_url
}

output "synthetic_image_repository_arn" {
  value = aws_ecr_repository.synthetic.arn
}

output "synthetic_image_repository_name" {
  value = aws_ecr_repository.synthetic.name
}

output "synthetic_image_repository_url" {
  value = aws_ecr_repository.synthetic.repository_url
}

output "apply_role_arn" {
  value = module.github.apply_role_arn
}

output "foundation_apply_role_arn" {
  value = module.github.foundation_apply_role_arn
}

output "oidc_provider_arn" {
  value = module.github.oidc_provider_arn
}

output "plan_role_arn" {
  value = module.github.plan_role_arn
}
