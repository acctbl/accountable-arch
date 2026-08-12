output "bucket" {
  value = aws_s3_bucket.releases.bucket
}

output "bucket_arn" {
  value = aws_s3_bucket.releases.arn
}

output "kms_key_arn" {
  value = aws_kms_key.releases.arn
}

output "release_prefix" {
  value = local.release_prefix
}

output "cell_inventory_prefix" {
  value = local.inventory_prefix
}
