mock_provider "aws" {
  mock_data "aws_iam_policy_document" {
    defaults = {
      json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}"
    }
  }

  mock_resource "aws_iam_openid_connect_provider" {
    defaults = {
      arn = "arn:aws:iam::453722413624:oidc-provider/token.actions.githubusercontent.com"
    }
  }

  mock_resource "aws_iam_role" {
    defaults = {
      arn = "arn:aws:iam::453722413624:role/accountable"
    }
  }

  mock_resource "aws_iam_policy" {
    defaults = {
      arn = "arn:aws:iam::453722413624:policy/accountable"
    }
  }
}

variables {
  account_id                 = "453722413624"
  environment                = "development"
  github_oidc_subject_prefix = "repo:acctbl@309473689/accountable@1318144297"
  github_repository          = "acctbl/accountable"
  region                     = "eu-west-2"
}

run "plan_and_apply_trust_are_separate" {
  command = plan

  assert {
    condition = (
      aws_iam_role.plan.name == "accountable-development-plan" &&
      aws_iam_role.apply.name == "accountable-development-apply" &&
      aws_iam_role.foundation_apply.name == "accountable-development-foundation-apply"
    )
    error_message = "Plan, runtime apply, and foundation apply must use separate roles."
  }

  assert {
    condition = (
      aws_iam_role_policy_attachment.plan_read_only.policy_arn == "arn:aws:iam::aws:policy/ReadOnlyAccess" &&
      aws_iam_policy.apply_cell.name == "accountable-development-cell" &&
      aws_iam_policy.apply_edge.name == "accountable-development-edge" &&
      aws_iam_policy.apply_delivery.name == "accountable-development-delivery" &&
      aws_iam_policy.apply_foundation.name == "accountable-development-foundation" &&
      aws_iam_policy.apply_observability.name == "accountable-development-observability" &&
      alltrue([
        for policy_arn in [
          aws_iam_role_policy_attachment.apply_cell.policy_arn,
          aws_iam_role_policy_attachment.apply_edge.policy_arn,
          aws_iam_role_policy_attachment.apply_delivery.policy_arn,
          aws_iam_role_policy_attachment.apply_observability.policy_arn,
        ] : policy_arn != "arn:aws:iam::aws:policy/PowerUserAccess"
      ]) &&
      aws_iam_role_policy_attachment.apply_foundation.role == aws_iam_role.foundation_apply.name &&
      aws_iam_role_policy_attachment.foundation_apply_read_only.policy_arn == "arn:aws:iam::aws:policy/ReadOnlyAccess"
    )
    error_message = "The plan role must remain read-only and the apply role must use only Accountable-managed policies."
  }

  assert {
    condition = (
      contains(local.plan_oidc_subjects, "repo:acctbl@309473689/accountable@1318144297:ref:refs/heads/main:workflow_ref:acctbl/accountable/.github/workflows/cell-plan.yml@refs/heads/main") &&
      contains(local.plan_oidc_subjects, "repo:acctbl@309473689/accountable@1318144297:ref:refs/heads/main:workflow_ref:acctbl/accountable/.github/workflows/foundation-plan.yml@refs/heads/main") &&
      contains(local.apply_oidc_subjects, "repo:acctbl@309473689/accountable@1318144297:environment:development:workflow_ref:acctbl/accountable/.github/workflows/release.yml@refs/heads/main") &&
      local.foundation_apply_oidc_subject == "repo:acctbl@309473689/accountable@1318144297:environment:development:workflow_ref:acctbl/accountable/.github/workflows/foundation-plan.yml@refs/heads/main" &&
      contains(local.promotion_oidc_subjects, "repo:acctbl@309473689/accountable@1318144297:environment:staging:workflow_ref:acctbl/accountable/.github/workflows/promotion.yml@refs/heads/main") &&
      alltrue([for subject in concat(local.plan_oidc_subjects, local.apply_oidc_subjects, local.promotion_oidc_subjects) : strcontains(subject, ":workflow_ref:")])
    )
    error_message = "Every deployment identity must bind the exact immutable repository, context, workflow file, and main ref."
  }
}

run "apply_capabilities_match_the_workflows" {
  command = plan

  assert {
    condition = alltrue([
      contains(local.delivery_ecr_actions, "ecr:PutImage"),
      contains(local.delivery_ecs_actions, "ecs:RunTask"),
      contains(local.delivery_ecs_actions, "ecs:DescribeTasks"),
      contains(local.delivery_ecs_actions, "ecs:ListTasks"),
      contains(local.delivery_web_actions, "s3:ListBucket"),
      contains(local.delivery_web_actions, "s3:PutObject"),
      contains(local.delivery_web_actions, "s3:DeleteObject"),
      contains(local.delivery_cloudfront_actions, "cloudfront:CreateInvalidation"),
      contains(local.delivery_cloudfront_actions, "cloudfront:GetInvalidation"),
      contains(local.pass_role_services, "ecs-tasks.amazonaws.com"),
      contains(local.pass_role_services, "lambda.amazonaws.com"),
      contains(local.observability_actions, "cloudwatch:PutMetricAlarm"),
      contains(local.observability_actions, "events:PutTargets"),
      contains(local.observability_actions, "lambda:CreateFunction"),
      contains(local.observability_actions, "lambda:InvokeFunction"),
      contains(local.observability_actions, "logs:PutMetricFilter"),
      contains(local.observability_actions, "sns:Subscribe"),
      contains(local.observability_actions, "sqs:CreateQueue"),
    ])
    error_message = "The apply role must retain the image, runtime proof, web publish, invalidation, and ECS-only PassRole capabilities used by CI."
  }

  assert {
    condition = (
      aws_iam_role_policy.foundation_apply_state.role == aws_iam_role.foundation_apply.id &&
      aws_iam_role_policy.foundation_apply_iam.role == aws_iam_role.foundation_apply.id &&
      aws_iam_role_policy.apply_state.role == aws_iam_role.apply.id &&
      aws_iam_role_policy.apply_iam.role == aws_iam_role.apply.id
    )
    error_message = "Foundation state and IAM management must stay on the isolated protected role."
  }

  assert {
    condition = alltrue([
      contains(local.foundation_ecr_actions, "ecr:CreateRepository"),
      contains(local.foundation_ecr_actions, "ecr:PutLifecyclePolicy"),
      contains(local.approved_policy_arns, "arn:aws:iam::453722413624:policy/accountable-development-observability"),
      contains(local.deployment_policy_actions, "iam:CreatePolicyVersion"),
      contains(local.deployment_policy_actions, "iam:SetDefaultPolicyVersion"),
      contains(local.deployment_policy_actions, "iam:DeletePolicyVersion"),
      contains(local.approved_policy_arns, "arn:aws:iam::aws:policy/ReadOnlyAccess"),
      !contains(local.approved_policy_arns, "arn:aws:iam::aws:policy/PowerUserAccess"),
      !contains(local.approved_policy_arns, "arn:aws:iam::aws:policy/AdministratorAccess"),
    ])
    error_message = "The apply role must be able to converge the account, image repository, and its bounded deployment policies."
  }
}

run "state_access_is_account_scoped" {
  command = plan

  assert {
    condition     = local.state_bucket == "accountable-tofu-state-453722413624-eu-west-2"
    error_message = "State access must be scoped to the target account bucket."
  }

  assert {
    condition     = local.state_key_arn == "arn:aws:kms:eu-west-2:453722413624:key/*" && local.state_key_alias == "alias/accountable-development-tofu-state"
    error_message = "State-key access must select keys in the target account through the environment alias."
  }
}

run "cell_key_authority_excludes_shared_keys" {
  command = plan

  assert {
    condition = (
      toset(local.cell_key_aliases) == toset([
        "alias/accountable-development-01",
        "alias/accountable-proof-01",
      ]) &&
      !contains(local.cell_key_aliases, "alias/accountable-development-observability") &&
      !contains(local.cell_key_aliases, "alias/accountable-development-tofu-state") &&
      toset([for alias in local.cell_key_aliases : trimprefix(alias, "alias/accountable-")]) == toset([
        "development-01",
        "proof-01",
      ])
    )
    error_message = "Cell KMS authority must be limited to registered cell aliases and exclude shared infrastructure keys."
  }

  assert {
    condition = (
      anytrue([
        for statement in data.aws_iam_policy_document.apply_cell.statement :
        statement.sid == "CreateCellKeys" &&
        toset(statement.actions) == toset(["kms:CreateKey"]) &&
        toset(statement.resources) == toset(["*"])
      ]) &&
      anytrue([
        for statement in data.aws_iam_policy_document.apply_cell.statement :
        statement.sid == "TagNewCellKeys" &&
        toset(statement.actions) == toset(["kms:TagResource"]) &&
        toset(statement.resources) == toset(["arn:aws:kms:eu-west-2:453722413624:key/*"])
      ]) &&
      anytrue([
        for statement in data.aws_iam_policy_document.apply_cell.statement :
        statement.sid == "ManageRegisteredCellAliases" &&
        toset(statement.actions) == toset(["kms:CreateAlias", "kms:DeleteAlias", "kms:UpdateAlias"]) &&
        toset(statement.resources) == toset(local.cell_key_alias_arns)
      ]) &&
      alltrue([
        for statement in data.aws_iam_policy_document.apply_cell.statement :
        statement.sid != "ManageRegisteredCellAliases" ||
        !contains(statement.resources, "arn:aws:kms:eu-west-2:453722413624:alias/accountable-development-observability")
      ])
    )
    error_message = "Cell key creation, tagging, and alias management must be separate and limited to registered cell resources."
  }
}
