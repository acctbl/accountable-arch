mock_provider "aws" {
  alias = "regional"

  mock_data "aws_iam_policy_document" {
    defaults = {
      json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}"
    }
  }

  mock_data "aws_caller_identity" {
    defaults = {
      account_id = "453722413624"
    }
  }

  mock_data "aws_ec2_managed_prefix_list" {
    defaults = {
      id = "pl-12345678"
    }
  }

  mock_data "aws_region" {
    defaults = {
      region = "eu-west-2"
    }
  }

  mock_resource "aws_cloudfront_distribution" {
    defaults = {
      arn         = "arn:aws:cloudfront::453722413624:distribution/E1234567890"
      domain_name = "d111111abcdef8.cloudfront.net"
    }
  }

  mock_resource "aws_cloudfront_function" {
    defaults = {
      arn = "arn:aws:cloudfront::453722413624:function/accountable"
    }
  }

  mock_resource "aws_db_instance" {
    defaults = {
      address = "accountable.cluster-example.eu-west-2.rds.amazonaws.com"
      master_user_secret = [{
        kms_key_id    = "arn:aws:kms:eu-west-2:453722413624:key/00000000-0000-0000-0000-000000000000"
        secret_arn    = "arn:aws:secretsmanager:eu-west-2:453722413624:secret:accountable"
        secret_status = "active"
      }]
    }
  }

  mock_resource "aws_kms_key" {
    defaults = {
      arn = "arn:aws:kms:eu-west-2:453722413624:key/00000000-0000-0000-0000-000000000000"
      id  = "00000000-0000-0000-0000-000000000000"
    }
  }

  mock_resource "aws_iam_role" {
    defaults = {
      arn = "arn:aws:iam::453722413624:role/accountable"
    }
  }

  mock_resource "aws_lb" {
    defaults = {
      arn      = "arn:aws:elasticloadbalancing:eu-west-2:453722413624:loadbalancer/app/accountable/0000000000000000"
      dns_name = "internal-accountable.eu-west-2.elb.amazonaws.com"
    }
  }

  mock_resource "aws_lb_target_group" {
    defaults = {
      arn = "arn:aws:elasticloadbalancing:eu-west-2:453722413624:targetgroup/accountable/0000000000000000"
    }
  }
}

mock_provider "aws" {
  alias = "global"
}

variables {
  account_id                    = "453722413624"
  api_desired_count             = 1
  api_machine_identity_id       = "identity-api"
  availability_zones            = ["eu-west-2a", "eu-west-2b"]
  bootstrap_machine_identity_id = "identity-bootstrap"
  cell_id                       = "development-01"
  configuration_revision        = "proof"
  cell_lifecycle                = "ephemeral"
  environment                   = "development"
  image_repository_arn          = "arn:aws:ecr:eu-west-2:453722413624:repository/accountable"
  image_uri                     = "453722413624.dkr.ecr.eu-west-2.amazonaws.com/accountable@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  infisical_project_id          = "project-development"
  migrate_machine_identity_id   = "identity-migrate"
  vpc_cidr                      = "10.20.0.0/16"
}

run "private_runtime_is_fail_closed" {
  command = plan

  providers = {
    aws        = aws.regional
    aws.global = aws.global
  }

  assert {
    condition     = aws_db_instance.cell.multi_az && !aws_db_instance.cell.publicly_accessible
    error_message = "The cell database must be private and Multi-AZ."
  }

  assert {
    condition = (
      aws_db_instance.cell.backup_retention_period == 1 &&
      !aws_db_instance.cell.deletion_protection &&
      aws_db_instance.cell.delete_automated_backups &&
      aws_db_instance.cell.skip_final_snapshot
    )
    error_message = "An ephemeral development database must remain cheap and cleanly destroyable after its proof window."
  }

  assert {
    condition     = length(aws_backup_plan.cell) == 0 && length(aws_backup_vault.cell) == 0
    error_message = "An ephemeral cell must not create a backup vault it would leave behind."
  }

  assert {
    condition     = aws_lb.api.internal && !aws_lb.api.enable_deletion_protection
    error_message = "The ephemeral API load balancer must be internal and remain destroyable after the proof window."
  }

  assert {
    condition     = !aws_ecs_service.api.network_configuration[0].assign_public_ip
    error_message = "The API service must not assign public IP addresses."
  }

  assert {
    condition = alltrue([
      for rule in aws_vpc_security_group_egress_rule.runtime_https :
      rule.ip_protocol == "tcp" && rule.from_port == 443 && rule.to_port == 443
    ])
    error_message = "Public runtime egress must be restricted to TLS."
  }
}

run "storage_and_edge_are_fail_closed" {
  command = plan

  providers = {
    aws        = aws.regional
    aws.global = aws.global
  }

  assert {
    condition = (
      aws_s3_bucket_public_access_block.data.block_public_acls &&
      aws_s3_bucket_public_access_block.data.block_public_policy &&
      aws_s3_bucket_public_access_block.data.ignore_public_acls &&
      aws_s3_bucket_public_access_block.data.restrict_public_buckets &&
      aws_s3_bucket_public_access_block.web.block_public_acls &&
      aws_s3_bucket_public_access_block.web.block_public_policy &&
      aws_s3_bucket_public_access_block.web.ignore_public_acls &&
      aws_s3_bucket_public_access_block.web.restrict_public_buckets
    )
    error_message = "Every cell bucket must enable every S3 public-access control."
  }

  assert {
    condition     = aws_s3_bucket.data.force_destroy && aws_s3_bucket.web.force_destroy
    error_message = "Ephemeral cell buckets must be emptyable during the proof-cycle destroy."
  }

  assert {
    condition = alltrue([
      for certificate in aws_cloudfront_distribution.cell.viewer_certificate :
      certificate.cloudfront_default_certificate && certificate.minimum_protocol_version == null
    ])
    error_message = "The default CloudFront certificate must omit the minimum protocol version until a custom certificate is configured."
  }

  assert {
    condition     = aws_cloudfront_distribution.cell.web_acl_id == aws_wafv2_web_acl.edge.arn
    error_message = "The CloudFront distribution must use the cell WAF web ACL."
  }

  assert {
    condition = one([
      for item in aws_cloudfront_response_headers_policy.runtime_config.custom_headers_config[0].items : item.value
      if item.header == "Cache-Control"
    ]) == "no-store"
    error_message = "The runtime configuration must not be stored by browsers or intermediate caches."
  }
}

run "tasks_use_the_image_digest" {
  command = plan

  providers = {
    aws        = aws.regional
    aws.global = aws.global
  }

  assert {
    condition     = strcontains(aws_ecs_task_definition.api.container_definitions, "@sha256:")
    error_message = "The API task must reference an image manifest digest."
  }

  assert {
    condition     = strcontains(aws_ecs_task_definition.migrate.container_definitions, "@sha256:")
    error_message = "The migration task must reference an image manifest digest."
  }

  assert {
    condition     = strcontains(aws_ecs_task_definition.bootstrap.container_definitions, "@sha256:")
    error_message = "The bootstrap task must reference an image manifest digest."
  }
}

run "tasks_use_tmpfs_runtime_scratch" {
  command = plan

  providers = {
    aws        = aws.regional
    aws.global = aws.global
  }

  assert {
    condition = alltrue([
      for definitions in [
        aws_ecs_task_definition.api.container_definitions,
        aws_ecs_task_definition.migrate.container_definitions,
        aws_ecs_task_definition.preflight.container_definitions,
        aws_ecs_task_definition.bootstrap.container_definitions,
      ] :
      strcontains(definitions, "\"tmpfs\"") &&
      strcontains(definitions, "/run/accountable") &&
      strcontains(definitions, "uid=65532") &&
      strcontains(definitions, "gid=65532") &&
      strcontains(definitions, "mode=0700") &&
      strcontains(definitions, "\"readonlyRootFilesystem\":true") &&
      !strcontains(definitions, "mountPoints") &&
      !strcontains(definitions, "sourceVolume")
    ])
    error_message = "Runtime tasks must use a tmpfs at /run/accountable owned by the image user instead of root-owned bind mounts."
  }

  assert {
    condition = alltrue([
      length(aws_ecs_task_definition.api.volume) == 0,
      length(aws_ecs_task_definition.migrate.volume) == 0,
      length(aws_ecs_task_definition.preflight.volume) == 0,
      length(aws_ecs_task_definition.bootstrap.volume) == 0,
    ])
    error_message = "Runtime tasks must not declare empty bind-mount volumes."
  }
}

run "documents_carry_the_security_headers" {
  command = plan

  providers = {
    aws        = aws.regional
    aws.global = aws.global
  }

  assert {
    condition = alltrue([
      for config in aws_cloudfront_response_headers_policy.documents.security_headers_config : (
        config.content_type_options[0].override &&
        config.frame_options[0].frame_option == "DENY" &&
        config.strict_transport_security[0].include_subdomains &&
        config.strict_transport_security[0].preload &&
        config.referrer_policy[0].referrer_policy == "strict-origin-when-cross-origin"
      )
    ])
    error_message = "Documents must carry the frame, referrer, content-type, and transport security headers."
  }

  assert {
    condition = alltrue([
      for config in aws_cloudfront_response_headers_policy.documents.security_headers_config : alltrue([
        for directive in ["default-src 'self'", "object-src 'none'", "base-uri 'none'", "frame-ancestors 'none'"] :
        contains(split("; ", config.content_security_policy[0].content_security_policy), directive)
      ]) && config.content_security_policy[0].override
    ])
    error_message = "Documents must deny by default and deny the sources an application never needs."
  }

  assert {
    condition = alltrue([
      for config in aws_cloudfront_response_headers_policy.documents.security_headers_config :
      can(regex("^script-src 'self' 'sha256-[A-Za-z0-9+/=]+'$", one([
        for directive in split("; ", config.content_security_policy[0].content_security_policy) :
        directive if startswith(directive, "script-src ")
      ])))
    ])
    error_message = "The script-src directive must pin the inline script by hash and allow no other source."
  }
}

run "only_bootstrap_uses_the_secret_execution_role" {
  command = plan

  override_resource {
    target = aws_iam_role.execution
    values = {
      arn = "arn:aws:iam::453722413624:role/accountable-development-01-execution"
    }
  }

  override_resource {
    target = aws_iam_role.bootstrap_execution
    values = {
      arn = "arn:aws:iam::453722413624:role/accountable-development-01-bootstrap-execution"
    }
  }

  providers = {
    aws        = aws.regional
    aws.global = aws.global
  }

  assert {
    condition = (
      aws_ecs_task_definition.api.execution_role_arn == aws_iam_role.execution.arn &&
      aws_ecs_task_definition.migrate.execution_role_arn == aws_iam_role.execution.arn &&
      aws_ecs_task_definition.preflight.execution_role_arn == aws_iam_role.execution.arn &&
      aws_ecs_task_definition.bootstrap.execution_role_arn == aws_iam_role.bootstrap_execution.arn &&
      aws_iam_role.execution.arn != aws_iam_role.bootstrap_execution.arn
    )
    error_message = "Only bootstrap may use the execution role that can retrieve the RDS master secret."
  }

  assert {
    condition = (
      !contains(concat(local.execution_global_actions, local.execution_repository_actions, local.execution_log_actions), "secretsmanager:GetSecretValue") &&
      contains(local.bootstrap_secret_actions, "secretsmanager:GetSecretValue") &&
      contains(local.bootstrap_key_actions, "kms:Decrypt")
    )
    error_message = "The shared execution policy must be secret-free and the bootstrap execution policy must hold the secret and key permissions."
  }

  assert {
    condition = (
      !strcontains(aws_ecs_task_definition.api.container_definitions, "ACCOUNTABLE_DATABASE_MASTER_PASSWORD") &&
      !strcontains(aws_ecs_task_definition.migrate.container_definitions, "ACCOUNTABLE_DATABASE_MASTER_PASSWORD") &&
      !strcontains(aws_ecs_task_definition.preflight.container_definitions, "ACCOUNTABLE_DATABASE_MASTER_PASSWORD") &&
      strcontains(aws_ecs_task_definition.bootstrap.container_definitions, "ACCOUNTABLE_DATABASE_MASTER_PASSWORD")
    )
    error_message = "Only the bootstrap task definition may reference the RDS master password."
  }
}

run "wrong_account_is_rejected" {
  command = plan

  providers = {
    aws        = aws.regional
    aws.global = aws.global
  }

  variables {
    account_id = "723225039926"
  }

  expect_failures = [aws_kms_key.cell]
}

run "durable_cell_keeps_recovery_and_deletion_safeguards" {
  command = plan

  providers = {
    aws        = aws.regional
    aws.global = aws.global
  }

  variables {
    cell_lifecycle                     = "durable"
    database_final_snapshot_identifier = "accountable-staging-01-final-20260805"
    environment                        = "staging"
  }

  assert {
    condition = (
      aws_db_instance.cell.backup_retention_period == 35 &&
      aws_db_instance.cell.deletion_protection &&
      !aws_db_instance.cell.delete_automated_backups &&
      !aws_db_instance.cell.skip_final_snapshot
    )
    error_message = "Durable databases must keep 35-day PITR, retained automated backups, deletion protection, and a final snapshot."
  }

  assert {
    condition = (
      aws_lb.api.enable_deletion_protection &&
      !aws_s3_bucket.data.force_destroy &&
      !aws_s3_bucket.web.force_destroy
    )
    error_message = "Durable cells must protect the ALB from deletion and must never erase bucket contents during destroy."
  }

  assert {
    condition = (
      one(aws_backup_plan.cell[0].rule).schedule == "cron(0 3 1 * ? *)" &&
      one(one(aws_backup_plan.cell[0].rule).lifecycle).delete_after == 365 &&
      one(aws_backup_selection.cell[0].resources) == aws_db_instance.cell.arn &&
      aws_backup_vault.cell[0].kms_key_arn == aws_kms_key.cell.arn
    )
    error_message = "Durable cells must take monthly database backups into the cell-encrypted vault and keep them for a year."
  }
}

run "staging_cannot_use_an_ephemeral_cell" {
  command = plan

  providers = {
    aws        = aws.regional
    aws.global = aws.global
  }

  variables {
    cell_lifecycle = "ephemeral"
    environment    = "staging"
  }

  expect_failures = [aws_db_instance.cell]
}

run "durable_database_requires_final_snapshot_name" {
  command = plan

  providers = {
    aws        = aws.regional
    aws.global = aws.global
  }

  variables {
    cell_lifecycle = "durable"
    environment    = "production"
  }

  expect_failures = [aws_db_instance.cell]
}
