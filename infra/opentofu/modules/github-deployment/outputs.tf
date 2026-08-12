output "apply_role_arn" {
  value = aws_iam_role.apply.arn
}

output "foundation_apply_role_arn" {
  value = aws_iam_role.foundation_apply.arn
}

output "oidc_provider_arn" {
  value = aws_iam_openid_connect_provider.github.arn
}

output "plan_role_arn" {
  value = aws_iam_role.plan.arn
}
