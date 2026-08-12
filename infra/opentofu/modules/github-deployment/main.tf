locals {
  apply_role_name            = "accountable-${var.environment}-apply"
  foundation_apply_role_name = "accountable-${var.environment}-foundation-apply"
  plan_role_name             = "accountable-${var.environment}-plan"
  state_bucket               = "accountable-tofu-state-${var.account_id}-${var.region}"
  state_key_alias            = "alias/accountable-${var.environment}-tofu-state"
  state_key_arn              = "arn:aws:kms:${var.region}:${var.account_id}:key/*"
  cell_key_aliases = var.environment == "development" ? [
    "alias/accountable-development-01",
    "alias/accountable-proof-01",
  ] : ["alias/accountable-${var.environment}-01"]
  cell_key_alias_arns = [
    for alias in local.cell_key_aliases : "arn:aws:kms:${var.region}:${var.account_id}:${alias}"
  ]

  delivery_ecr_actions = [
    "ecr:BatchCheckLayerAvailability",
    "ecr:BatchGetImage",
    "ecr:CompleteLayerUpload",
    "ecr:DescribeImages",
    "ecr:DescribeRepositories",
    "ecr:GetDownloadUrlForLayer",
    "ecr:InitiateLayerUpload",
    "ecr:ListImages",
    "ecr:PutImage",
    "ecr:UploadLayerPart",
  ]
  delivery_ecs_actions = [
    "ecs:DescribeServices",
    "ecs:DescribeTasks",
    "ecs:ListTasks",
    "ecs:RunTask",
    "ecs:StopTask",
  ]
  delivery_web_actions = [
    "s3:DeleteObject",
    "s3:GetBucketLocation",
    "s3:GetObject",
    "s3:ListBucket",
    "s3:PutObject",
  ]
  delivery_cloudfront_actions = [
    "cloudfront:CreateInvalidation",
    "cloudfront:GetDistribution",
    "cloudfront:GetInvalidation",
  ]
  foundation_ecr_actions = [
    "ecr:CreateRepository",
    "ecr:DeleteLifecyclePolicy",
    "ecr:DeleteRepository",
    "ecr:DescribeRepositories",
    "ecr:GetLifecyclePolicy",
    "ecr:GetRepositoryPolicy",
    "ecr:ListTagsForResource",
    "ecr:PutImageScanningConfiguration",
    "ecr:PutImageTagMutability",
    "ecr:PutLifecyclePolicy",
    "ecr:TagResource",
    "ecr:UntagResource",
  ]
  observability_actions = [
    "cloudwatch:DeleteAlarms",
    "cloudwatch:DescribeAlarms",
    "cloudwatch:GetMetricData",
    "cloudwatch:GetMetricStatistics",
    "cloudwatch:ListMetrics",
    "cloudwatch:ListTagsForResource",
    "cloudwatch:PutMetricAlarm",
    "cloudwatch:SetAlarmState",
    "cloudwatch:TagResource",
    "cloudwatch:UntagResource",
    "events:DeleteRule",
    "events:DescribeRule",
    "events:ListTagsForResource",
    "events:ListTargetsByRule",
    "events:PutRule",
    "events:PutTargets",
    "events:RemoveTargets",
    "events:TagResource",
    "events:UntagResource",
    "lambda:AddPermission",
    "lambda:CreateFunction",
    "lambda:DeleteFunction",
    "lambda:GetFunction",
    "lambda:GetFunctionCodeSigningConfig",
    "lambda:GetFunctionConfiguration",
    "lambda:GetPolicy",
    "lambda:InvokeFunction",
    "lambda:ListFunctions",
    "lambda:ListTags",
    "lambda:ListVersionsByFunction",
    "lambda:RemovePermission",
    "lambda:TagResource",
    "lambda:UntagResource",
    "lambda:UpdateFunctionCode",
    "lambda:UpdateFunctionConfiguration",
    "logs:CreateLogGroup",
    "logs:DeleteLogGroup",
    "logs:DeleteMetricFilter",
    "logs:DeleteResourcePolicy",
    "logs:DescribeLogGroups",
    "logs:DescribeMetricFilters",
    "logs:DescribeResourcePolicies",
    "logs:FilterLogEvents",
    "logs:GetQueryResults",
    "logs:ListTagsForResource",
    "logs:PutMetricFilter",
    "logs:PutResourcePolicy",
    "logs:PutRetentionPolicy",
    "logs:StartQuery",
    "logs:StopQuery",
    "logs:TagResource",
    "logs:UntagResource",
    "sns:CreateTopic",
    "sns:DeleteTopic",
    "sns:GetSubscriptionAttributes",
    "sns:GetTopicAttributes",
    "sns:ListSubscriptionsByTopic",
    "sns:ListTagsForResource",
    "sns:SetSubscriptionAttributes",
    "sns:SetTopicAttributes",
    "sns:Subscribe",
    "sns:TagResource",
    "sns:Unsubscribe",
    "sns:UntagResource",
    "sqs:CreateQueue",
    "sqs:DeleteQueue",
    "sqs:GetQueueAttributes",
    "sqs:GetQueueUrl",
    "sqs:ListQueueTags",
    "sqs:SetQueueAttributes",
    "sqs:TagQueue",
    "sqs:UntagQueue",
  ]
  deployment_policy_actions = [
    "iam:CreatePolicy",
    "iam:CreatePolicyVersion",
    "iam:DeletePolicy",
    "iam:DeletePolicyVersion",
    "iam:GetPolicy",
    "iam:GetPolicyVersion",
    "iam:ListEntitiesForPolicy",
    "iam:ListPolicyTags",
    "iam:ListPolicyVersions",
    "iam:SetDefaultPolicyVersion",
    "iam:TagPolicy",
    "iam:UntagPolicy",
  ]
  approved_policy_arns = [
    "arn:aws:iam::${var.account_id}:policy/accountable-${var.environment}-cell",
    "arn:aws:iam::${var.account_id}:policy/accountable-${var.environment}-delivery",
    "arn:aws:iam::${var.account_id}:policy/accountable-${var.environment}-edge",
    "arn:aws:iam::${var.account_id}:policy/accountable-${var.environment}-foundation",
    "arn:aws:iam::${var.account_id}:policy/accountable-${var.environment}-observability",
    "arn:aws:iam::aws:policy/ReadOnlyAccess",
    "arn:aws:iam::aws:policy/service-role/AWSBackupServiceRolePolicyForBackup",
  ]
  pass_role_services = ["backup.amazonaws.com", "ecs-tasks.amazonaws.com", "lambda.amazonaws.com"]

  workflow_ref_prefix = "${var.github_repository}/.github/workflows"
  plan_workflow_files = concat(
    ["cell-plan.yml", "foundation-plan.yml", "observability-plan.yml", "oidc-contract.yml"],
    var.environment == "development" ? ["observability-proof.yml", "promotion.yml"] : [],
  )
  apply_workflow_files = concat(
    ["cell-plan.yml", "cell-runtime.yml", "observability-plan.yml", "observability-proof.yml", "observability-tokens.yml"],
    var.environment == "development" ? [
      "collector-image.yml",
      "image-build.yml",
      "k6-image.yml",
      "release-retention.yml",
      "release.yml",
    ] : [],
    var.environment == "staging" ? ["observability-proof-schedule.yml"] : [],
    var.environment == "production" ? ["observability-proof-schedule.yml"] : [],
  )
  plan_oidc_subjects = [
    for workflow in local.plan_workflow_files :
    "${var.github_oidc_subject_prefix}:ref:refs/heads/main:workflow_ref:${local.workflow_ref_prefix}/${workflow}@refs/heads/main"
  ]
  apply_oidc_subjects = [
    for workflow in local.apply_workflow_files :
    "${var.github_oidc_subject_prefix}:environment:${var.environment}:workflow_ref:${local.workflow_ref_prefix}/${workflow}@refs/heads/main"
  ]
  promotion_oidc_subjects = var.environment == "development" ? [
    "${var.github_oidc_subject_prefix}:environment:staging:workflow_ref:${local.workflow_ref_prefix}/promotion.yml@refs/heads/main",
    ] : var.environment == "staging" ? [
    "${var.github_oidc_subject_prefix}:environment:production:workflow_ref:${local.workflow_ref_prefix}/promotion.yml@refs/heads/main",
  ] : []
  foundation_apply_oidc_subject = "${var.github_oidc_subject_prefix}:environment:${var.environment}:workflow_ref:${local.workflow_ref_prefix}/foundation-plan.yml@refs/heads/main"
}

resource "aws_iam_openid_connect_provider" "github" {
  url            = "https://token.actions.githubusercontent.com"
  client_id_list = ["sts.amazonaws.com"]
}

data "aws_iam_policy_document" "plan_assume" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github.arn]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:sub"
      values   = local.plan_oidc_subjects
    }
  }
}

data "aws_iam_policy_document" "apply_assume" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github.arn]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:sub"
      values   = concat(local.apply_oidc_subjects, local.promotion_oidc_subjects)
    }
  }
}

data "aws_iam_policy_document" "foundation_apply_assume" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github.arn]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:sub"
      values   = [local.foundation_apply_oidc_subject]
    }
  }
}

resource "aws_iam_role" "plan" {
  name                 = local.plan_role_name
  assume_role_policy   = data.aws_iam_policy_document.plan_assume.json
  max_session_duration = 3600
}

resource "aws_iam_role_policy_attachment" "plan_read_only" {
  role       = aws_iam_role.plan.name
  policy_arn = "arn:aws:iam::aws:policy/ReadOnlyAccess"
}

data "aws_iam_policy_document" "plan_observability_token" {
  statement {
    actions   = ["ssm:GetParameter"]
    resources = ["arn:aws:ssm:${var.region}:${var.account_id}:parameter/accountable/observability/${var.environment}/grafana-plan-token"]
  }

  statement {
    actions   = ["kms:Decrypt"]
    resources = ["arn:aws:kms:${var.region}:${var.account_id}:key/*"]

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["ssm.${var.region}.amazonaws.com"]
    }

    condition {
      test     = "ForAnyValue:StringEquals"
      variable = "kms:ResourceAliases"
      values   = ["alias/accountable-${var.environment}-observability"]
    }
  }
}

resource "aws_iam_role_policy" "plan_observability_token" {
  name   = "accountable-observability-plan-token"
  role   = aws_iam_role.plan.id
  policy = data.aws_iam_policy_document.plan_observability_token.json
}

data "aws_iam_policy_document" "plan_release_key" {
  statement {
    actions   = ["kms:Decrypt", "kms:DescribeKey"]
    resources = ["arn:aws:kms:${var.region}:453722413624:key/*"]
  }
}

resource "aws_iam_role_policy" "plan_release_key" {
  name   = "accountable-release-evidence"
  role   = aws_iam_role.plan.id
  policy = data.aws_iam_policy_document.plan_release_key.json
}

resource "aws_iam_role" "apply" {
  name                 = local.apply_role_name
  assume_role_policy   = data.aws_iam_policy_document.apply_assume.json
  max_session_duration = 3600
}

resource "aws_iam_role" "foundation_apply" {
  name                 = local.foundation_apply_role_name
  assume_role_policy   = data.aws_iam_policy_document.foundation_apply_assume.json
  max_session_duration = 3600
}

resource "aws_iam_role_policy_attachment" "foundation_apply_read_only" {
  role       = aws_iam_role.foundation_apply.name
  policy_arn = "arn:aws:iam::aws:policy/ReadOnlyAccess"
}

data "aws_iam_policy_document" "apply_cell" {
  # EC2 networking APIs have uneven resource-level authorization support. Keep
  # them region-bound and enumerate only the operations used by the cell.
  statement {
    sid = "ManageCellNetwork"
    actions = [
      "ec2:AllocateAddress",
      "ec2:AssociateRouteTable",
      "ec2:AttachInternetGateway",
      "ec2:AuthorizeSecurityGroupEgress",
      "ec2:AuthorizeSecurityGroupIngress",
      "ec2:CreateInternetGateway",
      "ec2:CreateNatGateway",
      "ec2:CreateRoute",
      "ec2:CreateRouteTable",
      "ec2:CreateSecurityGroup",
      "ec2:CreateSubnet",
      "ec2:CreateTags",
      "ec2:CreateVpc",
      "ec2:CreateVpcEndpoint",
      "ec2:DeleteInternetGateway",
      "ec2:DeleteNatGateway",
      "ec2:DeleteRoute",
      "ec2:DeleteRouteTable",
      "ec2:DeleteSecurityGroup",
      "ec2:DeleteSubnet",
      "ec2:DeleteTags",
      "ec2:DeleteVpc",
      "ec2:DeleteVpcEndpoints",
      "ec2:DescribeAccountAttributes",
      "ec2:DescribeAddresses",
      "ec2:DescribeAddressesAttribute",
      "ec2:DescribeAvailabilityZones",
      "ec2:DescribeInternetGateways",
      "ec2:DescribeManagedPrefixLists",
      "ec2:DescribeNatGateways",
      "ec2:DescribeNetworkInterfaces",
      "ec2:DescribePrefixLists",
      "ec2:DescribeRouteTables",
      "ec2:DescribeSecurityGroupRules",
      "ec2:DescribeSecurityGroups",
      "ec2:DescribeSubnets",
      "ec2:DescribeVpcAttribute",
      "ec2:DescribeVpcEndpoints",
      "ec2:DescribeVpcs",
      "ec2:DetachInternetGateway",
      "ec2:DisassociateAddress",
      "ec2:DisassociateRouteTable",
      "ec2:ModifySubnetAttribute",
      "ec2:ModifyVpcAttribute",
      "ec2:ModifyVpcEndpoint",
      "ec2:ReleaseAddress",
      "ec2:RevokeSecurityGroupEgress",
      "ec2:RevokeSecurityGroupIngress",
    ]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "aws:RequestedRegion"
      values   = [var.region]
    }
  }

  statement {
    sid = "ManageCellLoadBalancing"
    actions = [
      "elasticloadbalancing:AddTags",
      "elasticloadbalancing:CreateListener",
      "elasticloadbalancing:CreateLoadBalancer",
      "elasticloadbalancing:CreateTargetGroup",
      "elasticloadbalancing:DeleteListener",
      "elasticloadbalancing:DeleteLoadBalancer",
      "elasticloadbalancing:DeleteTargetGroup",
      "elasticloadbalancing:DescribeListenerAttributes",
      "elasticloadbalancing:DescribeListeners",
      "elasticloadbalancing:DescribeLoadBalancerAttributes",
      "elasticloadbalancing:DescribeLoadBalancers",
      "elasticloadbalancing:DescribeTags",
      "elasticloadbalancing:DescribeTargetGroupAttributes",
      "elasticloadbalancing:DescribeTargetGroups",
      "elasticloadbalancing:DescribeTargetHealth",
      "elasticloadbalancing:ModifyListener",
      "elasticloadbalancing:ModifyListenerAttributes",
      "elasticloadbalancing:ModifyLoadBalancerAttributes",
      "elasticloadbalancing:ModifyTargetGroup",
      "elasticloadbalancing:ModifyTargetGroupAttributes",
      "elasticloadbalancing:RemoveTags",
      "elasticloadbalancing:SetSecurityGroups",
      "elasticloadbalancing:SetSubnets",
    ]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "aws:RequestedRegion"
      values   = [var.region]
    }
  }

  statement {
    sid = "ManageCellECS"
    actions = [
      "ecs:CreateCluster",
      "ecs:CreateService",
      "ecs:DeleteCluster",
      "ecs:DeleteService",
      "ecs:DeregisterTaskDefinition",
      "ecs:DescribeClusters",
      "ecs:DescribeServices",
      "ecs:DescribeTaskDefinition",
      "ecs:ListTagsForResource",
      "ecs:RegisterTaskDefinition",
      "ecs:TagResource",
      "ecs:UntagResource",
      "ecs:UpdateClusterSettings",
      "ecs:UpdateService",
    ]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "aws:RequestedRegion"
      values   = [var.region]
    }
  }

  statement {
    sid = "ManageCellBackups"
    actions = [
      "backup:CreateBackupPlan",
      "backup:CreateBackupSelection",
      "backup:CreateBackupVault",
      "backup:DeleteBackupPlan",
      "backup:DeleteBackupSelection",
      "backup:DeleteBackupVault",
      "backup:DescribeBackupVault",
      "backup:GetBackupPlan",
      "backup:GetBackupSelection",
      "backup:ListBackupSelections",
      "backup:ListTags",
      "backup:TagResource",
      "backup:UntagResource",
      "backup:UpdateBackupPlan",
    ]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "aws:RequestedRegion"
      values   = [var.region]
    }
  }

  statement {
    sid = "ManageCellLogs"
    actions = [
      "logs:CreateLogGroup",
      "logs:DeleteLogGroup",
      "logs:DescribeLogGroups",
      "logs:ListTagsForResource",
      "logs:PutRetentionPolicy",
      "logs:TagResource",
      "logs:UntagResource",
    ]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "aws:RequestedRegion"
      values   = [var.region]
    }
  }

  statement {
    sid = "ManageCellDatabase"
    actions = [
      "rds:AddTagsToResource",
      "rds:CreateDBInstance",
      "rds:CreateDBSubnetGroup",
      "rds:DeleteDBInstance",
      "rds:DeleteDBSubnetGroup",
      "rds:DescribeDBInstances",
      "rds:DescribeDBSubnetGroups",
      "rds:DescribeDBParameters",
      "rds:DescribeDBParameterGroups",
      "rds:DescribeDBSnapshots",
      "rds:ListTagsForResource",
      "rds:ModifyDBInstance",
      "rds:ModifyDBSubnetGroup",
      "rds:RemoveTagsFromResource",
    ]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "aws:RequestedRegion"
      values   = [var.region]
    }
  }

  statement {
    sid = "CreateRDSManagedMasterSecret"
    actions = [
      "secretsmanager:CreateSecret",
      "secretsmanager:RotateSecret",
      "secretsmanager:TagResource",
    ]
    resources = ["arn:aws:secretsmanager:${var.region}:${var.account_id}:secret:rds!*"]
  }

  statement {
    sid       = "CreateCellKeys"
    actions   = ["kms:CreateKey"]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "aws:RequestedRegion"
      values   = [var.region]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:RequestTag/CellId"
      values   = [for alias in local.cell_key_aliases : trimprefix(alias, "alias/accountable-")]
    }
  }

  statement {
    sid       = "TagNewCellKeys"
    actions   = ["kms:TagResource"]
    resources = ["arn:aws:kms:${var.region}:${var.account_id}:key/*"]

    condition {
      test     = "StringEquals"
      variable = "aws:RequestTag/CellId"
      values   = [for alias in local.cell_key_aliases : trimprefix(alias, "alias/accountable-")]
    }
  }

  statement {
    sid       = "ListCellKeyAliases"
    actions   = ["kms:ListAliases"]
    resources = ["*"]
  }

  statement {
    sid = "ManageRegisteredCellAliases"
    actions = [
      "kms:CreateAlias",
      "kms:DeleteAlias",
      "kms:UpdateAlias",
    ]
    resources = local.cell_key_alias_arns
  }

}

resource "aws_iam_policy" "apply_cell" {
  name   = "accountable-${var.environment}-cell"
  policy = data.aws_iam_policy_document.apply_cell.json

  depends_on = [aws_iam_role_policy.apply_iam]

  lifecycle {
    precondition {
      condition     = length(replace(data.aws_iam_policy_document.apply_cell.json, "/\\s/", "")) <= 6144
      error_message = "The regional cell IAM policy exceeds AWS's 6,144-character managed-policy limit."
    }
  }
}

resource "aws_iam_role_policy_attachment" "apply_cell" {
  role       = aws_iam_role.apply.name
  policy_arn = aws_iam_policy.apply_cell.arn
}

data "aws_iam_policy_document" "apply_edge" {

  statement {
    sid = "ManageCellBuckets"
    actions = [
      "s3:CreateBucket",
      "s3:DeleteBucket",
      "s3:DeleteBucketPolicy",
      "s3:DeleteBucketWebsite",
      "s3:DeleteObject",
      "s3:DeleteObjectVersion",
      "s3:GetAccelerateConfiguration",
      "s3:GetBucketAcl",
      "s3:GetBucketCORS",
      "s3:GetBucketLocation",
      "s3:GetBucketLogging",
      "s3:GetBucketObjectLockConfiguration",
      "s3:GetBucketOwnershipControls",
      "s3:GetBucketPolicy",
      "s3:GetBucketPolicyStatus",
      "s3:GetBucketPublicAccessBlock",
      "s3:GetBucketRequestPayment",
      "s3:GetBucketTagging",
      "s3:GetBucketVersioning",
      "s3:GetBucketWebsite",
      "s3:GetEncryptionConfiguration",
      "s3:GetLifecycleConfiguration",
      "s3:GetObject",
      "s3:GetReplicationConfiguration",
      "s3:ListBucket",
      "s3:ListBucketVersions",
      "s3:PutBucketOwnershipControls",
      "s3:PutBucketPolicy",
      "s3:PutBucketPublicAccessBlock",
      "s3:PutBucketTagging",
      "s3:PutBucketVersioning",
      "s3:PutEncryptionConfiguration",
      "s3:PutObject",
    ]
    resources = [
      "arn:aws:s3:::accountable-*-${var.account_id}-data",
      "arn:aws:s3:::accountable-*-${var.account_id}-data/*",
      "arn:aws:s3:::accountable-*-${var.account_id}-web",
      "arn:aws:s3:::accountable-*-${var.account_id}-web/*",
    ]
  }

  statement {
    sid = "CreateCellEdge"
    actions = [
      "cloudfront:CreateAnycastIpList",
      "cloudfront:CreateCachePolicy",
      "cloudfront:CreateDistribution",
      "cloudfront:CreateDistributionWithTags",
      "cloudfront:CreateFunction",
      "cloudfront:CreateOriginAccessControl",
      "cloudfront:CreateOriginRequestPolicy",
      "cloudfront:CreateResponseHeadersPolicy",
      "cloudfront:CreateVpcOrigin",
      "cloudfront:ListAnycastIpLists",
      "cloudfront:ListCachePolicies",
      "cloudfront:ListDistributions",
      "cloudfront:ListFunctions",
      "cloudfront:ListOriginAccessControls",
      "cloudfront:ListOriginRequestPolicies",
      "cloudfront:ListResponseHeadersPolicies",
      "cloudfront:ListVpcOrigins",
    ]
    resources = ["*"]
  }

  statement {
    sid = "ManageCellEdge"
    actions = [
      "cloudfront:DeleteAnycastIpList",
      "cloudfront:DeleteCachePolicy",
      "cloudfront:DeleteDistribution",
      "cloudfront:DeleteFunction",
      "cloudfront:DeleteOriginAccessControl",
      "cloudfront:DeleteOriginRequestPolicy",
      "cloudfront:DeleteResponseHeadersPolicy",
      "cloudfront:DeleteVpcOrigin",
      "cloudfront:DescribeFunction",
      "cloudfront:GetAnycastIpList",
      "cloudfront:GetCachePolicy",
      "cloudfront:GetDistribution",
      "cloudfront:GetDistributionConfig",
      "cloudfront:GetFunction",
      "cloudfront:GetOriginAccessControl",
      "cloudfront:GetOriginRequestPolicy",
      "cloudfront:GetResponseHeadersPolicy",
      "cloudfront:GetVpcOrigin",
      "cloudfront:ListTagsForResource",
      "cloudfront:PublishFunction",
      "cloudfront:TagResource",
      "cloudfront:UntagResource",
      "cloudfront:UpdateAnycastIpList",
      "cloudfront:UpdateCachePolicy",
      "cloudfront:UpdateDistribution",
      "cloudfront:UpdateFunction",
      "cloudfront:UpdateOriginAccessControl",
      "cloudfront:UpdateOriginRequestPolicy",
      "cloudfront:UpdateResponseHeadersPolicy",
      "cloudfront:UpdateVpcOrigin",
    ]
    resources = ["arn:aws:cloudfront::${var.account_id}:*"]
  }

  statement {
    sid = "CreateCellWAF"
    actions = [
      "wafv2:CheckCapacity",
      "wafv2:CreateWebACL",
      "wafv2:ListAvailableManagedRuleGroups",
      "wafv2:ListWebACLs",
    ]
    resources = ["*"]
  }

  statement {
    sid = "ManageCellWAF"
    actions = [
      "wafv2:DeleteWebACL",
      "wafv2:GetWebACL",
      "wafv2:ListTagsForResource",
      "wafv2:TagResource",
      "wafv2:UntagResource",
      "wafv2:UpdateWebACL",
    ]
    resources = ["arn:aws:wafv2:us-east-1:${var.account_id}:global/webacl/accountable-*/*"]
  }
}

resource "aws_iam_policy" "apply_edge" {
  name   = "accountable-${var.environment}-edge"
  policy = data.aws_iam_policy_document.apply_edge.json

  depends_on = [aws_iam_role_policy.apply_iam]

  lifecycle {
    precondition {
      condition     = length(replace(data.aws_iam_policy_document.apply_edge.json, "/\\s/", "")) <= 6144
      error_message = "The edge IAM policy exceeds AWS's 6,144-character managed-policy limit."
    }
  }
}

resource "aws_iam_role_policy_attachment" "apply_edge" {
  role       = aws_iam_role.apply.name
  policy_arn = aws_iam_policy.apply_edge.arn
}

data "aws_iam_policy_document" "apply_delivery" {
  statement {
    sid       = "AuthenticateToECR"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"]
  }

  statement {
    sid = "ReadProtectedReleaseEvidence"
    actions = [
      "s3:GetObject",
      "s3:ListBucket",
    ]
    resources = [
      "arn:aws:s3:::accountable-releases-453722413624-${var.region}",
      "arn:aws:s3:::accountable-releases-453722413624-${var.region}/*",
    ]
  }

  statement {
    sid     = "WriteProtectedOperationalEvidence"
    actions = ["s3:PutObject"]
    resources = [
      "arn:aws:s3:::accountable-releases-453722413624-${var.region}/observability/v1/cell-inventory/*",
      "arn:aws:s3:::accountable-releases-453722413624-${var.region}/observability/v1/promotion-evidence/*",
    ]
  }

  statement {
    sid = "UseProtectedReleaseEvidenceKey"
    actions = [
      "kms:Decrypt",
      "kms:DescribeKey",
      "kms:Encrypt",
      "kms:GenerateDataKey",
    ]
    resources = ["arn:aws:kms:${var.region}:453722413624:key/*"]
  }

  statement {
    sid     = "PublishAndInspectRuntimeImages"
    actions = local.delivery_ecr_actions
    resources = [
      "arn:aws:ecr:${var.region}:${var.account_id}:repository/accountable",
      "arn:aws:ecr:${var.region}:${var.account_id}:repository/accountable-collector",
      "arn:aws:ecr:${var.region}:${var.account_id}:repository/accountable-k6",
    ]
  }

  # Observability tokens are written by the protected token workflow only, at
  # exact names, and never by an application runtime.
  statement {
    sid = "ReplaceObservabilityTokens"
    actions = [
      "ssm:PutParameter",
      "ssm:GetParameter",
      "ssm:GetParameters",
      "ssm:DeleteParameter",
    ]
    resources = [
      "arn:aws:ssm:${var.region}:${var.account_id}:parameter/accountable/observability/${var.environment}/*",
      "arn:aws:ssm:${var.region}:${var.account_id}:parameter/accountable/*/${var.environment}/collector/grafana-otlp-token-*",
    ]
  }

  statement {
    sid = "UseObservabilityKeyForTokens"
    actions = [
      "kms:Decrypt",
      "kms:Encrypt",
      "kms:DescribeKey",
    ]
    resources = ["arn:aws:kms:${var.region}:${var.account_id}:key/*"]

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["ssm.${var.region}.amazonaws.com"]
    }
  }

  statement {
    sid       = "RunCellProofTasks"
    actions   = local.delivery_ecs_actions
    resources = ["arn:aws:ecs:${var.region}:${var.account_id}:*"]
  }

  statement {
    sid = "InspectLiveCellPosture"
    actions = [
      "elasticloadbalancing:DescribeLoadBalancers",
      "rds:DescribeDBInstances",
    ]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "aws:RequestedRegion"
      values   = [var.region]
    }
  }

  statement {
    sid     = "PublishCellWebRelease"
    actions = local.delivery_web_actions
    resources = [
      "arn:aws:s3:::accountable-*-${var.account_id}-web",
      "arn:aws:s3:::accountable-*-${var.account_id}-web/*",
    ]
  }

  statement {
    sid       = "InvalidateCellEdge"
    actions   = local.delivery_cloudfront_actions
    resources = ["arn:aws:cloudfront::${var.account_id}:distribution/*"]
  }
}

resource "aws_iam_policy" "apply_delivery" {
  name   = "accountable-${var.environment}-delivery"
  policy = data.aws_iam_policy_document.apply_delivery.json

  depends_on = [aws_iam_role_policy.apply_iam]

  lifecycle {
    precondition {
      condition     = length(replace(data.aws_iam_policy_document.apply_delivery.json, "/\\s/", "")) <= 6144
      error_message = "The delivery IAM policy exceeds AWS's 6,144-character managed-policy limit."
    }
  }
}

resource "aws_iam_role_policy_attachment" "apply_delivery" {
  role       = aws_iam_role.apply.name
  policy_arn = aws_iam_policy.apply_delivery.arn
}

data "aws_iam_policy_document" "apply_observability" {
  statement {
    sid       = "ManageRegionalObservability"
    actions   = local.observability_actions
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "aws:RequestedRegion"
      values   = [var.region]
    }
  }

  statement {
    sid       = "PublishRegisteredObservabilityMetrics"
    actions   = ["cloudwatch:PutMetricData"]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "cloudwatch:namespace"
      values = [
        "Accountable/GrafanaCapacity",
        "Accountable/Observability",
      ]
    }
  }

  statement {
    sid = "ManagePublicHealthChecks"
    actions = [
      "route53:ChangeTagsForResource",
      "route53:CreateHealthCheck",
      "route53:DeleteHealthCheck",
      "route53:GetHealthCheck",
      "route53:GetHealthCheckCount",
      "route53:GetHealthCheckLastFailureReason",
      "route53:ListHealthChecks",
      "route53:ListTagsForResource",
      "route53:UpdateHealthCheck",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_policy" "apply_observability" {
  name   = "accountable-${var.environment}-observability"
  policy = data.aws_iam_policy_document.apply_observability.json

  depends_on = [aws_iam_role_policy.apply_iam]

  lifecycle {
    precondition {
      condition     = length(replace(data.aws_iam_policy_document.apply_observability.json, "/\\s/", "")) <= 6144
      error_message = "The observability IAM policy exceeds AWS's 6,144-character managed-policy limit."
    }
  }
}

resource "aws_iam_role_policy_attachment" "apply_observability" {
  role       = aws_iam_role.apply.name
  policy_arn = aws_iam_policy.apply_observability.arn
}

data "aws_iam_policy_document" "apply_foundation" {
  statement {
    sid       = "ManageAccountPublicAccessBlock"
    actions   = ["s3:GetAccountPublicAccessBlock", "s3:PutAccountPublicAccessBlock"]
    resources = ["*"]
  }

  statement {
    sid     = "ManageRuntimeRepositories"
    actions = local.foundation_ecr_actions
    resources = [
      "arn:aws:ecr:${var.region}:${var.account_id}:repository/accountable",
      "arn:aws:ecr:${var.region}:${var.account_id}:repository/accountable-collector",
      "arn:aws:ecr:${var.region}:${var.account_id}:repository/accountable-k6",
    ]
  }

  statement {
    sid = "ManageRegistryReplication"
    actions = [
      "ecr:DeleteRegistryPolicy",
      "ecr:GetRegistryPolicy",
      "ecr:PutRegistryPolicy",
      "ecr:PutReplicationConfiguration",
    ]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "aws:RequestedRegion"
      values   = [var.region]
    }
  }

  statement {
    sid = "ManageGitHubOIDCProvider"
    actions = [
      "iam:AddClientIDToOpenIDConnectProvider",
      "iam:CreateOpenIDConnectProvider",
      "iam:DeleteOpenIDConnectProvider",
      "iam:GetOpenIDConnectProvider",
      "iam:ListOpenIDConnectProviderTags",
      "iam:RemoveClientIDFromOpenIDConnectProvider",
      "iam:TagOpenIDConnectProvider",
      "iam:UntagOpenIDConnectProvider",
      "iam:UpdateOpenIDConnectProviderThumbprint",
    ]
    resources = ["arn:aws:iam::${var.account_id}:oidc-provider/token.actions.githubusercontent.com"]
  }

  statement {
    sid = "ManageReleaseStorage"
    actions = [
      "s3:CreateBucket",
      "s3:DeleteBucketPolicy",
      "s3:GetBucketLocation",
      "s3:GetBucketObjectLockConfiguration",
      "s3:GetBucketOwnershipControls",
      "s3:GetBucketPolicy",
      "s3:GetBucketPublicAccessBlock",
      "s3:GetBucketTagging",
      "s3:GetBucketVersioning",
      "s3:GetEncryptionConfiguration",
      "s3:ListBucket",
      "s3:PutBucketObjectLockConfiguration",
      "s3:PutBucketOwnershipControls",
      "s3:PutBucketPolicy",
      "s3:PutBucketPublicAccessBlock",
      "s3:PutBucketTagging",
      "s3:PutBucketVersioning",
      "s3:PutEncryptionConfiguration",
    ]
    resources = ["arn:aws:s3:::accountable-releases-${var.account_id}-${var.region}"]
  }

  statement {
    sid = "ManageReleaseKey"
    actions = [
      "kms:CreateAlias",
      "kms:CreateKey",
      "kms:DescribeKey",
      "kms:EnableKeyRotation",
      "kms:GetKeyPolicy",
      "kms:GetKeyRotationStatus",
      "kms:ListAliases",
      "kms:ListResourceTags",
      "kms:PutKeyPolicy",
      "kms:TagResource",
      "kms:UntagResource",
      "kms:UpdateAlias",
    ]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "aws:RequestedRegion"
      values   = [var.region]
    }
  }
}

resource "aws_iam_policy" "apply_foundation" {
  name   = "accountable-${var.environment}-foundation"
  policy = data.aws_iam_policy_document.apply_foundation.json

  depends_on = [aws_iam_role_policy.apply_iam]

  lifecycle {
    precondition {
      condition     = length(replace(data.aws_iam_policy_document.apply_foundation.json, "/\\s/", "")) <= 6144
      error_message = "The foundation IAM policy exceeds AWS's 6,144-character managed-policy limit."
    }
  }
}

resource "aws_iam_role_policy_attachment" "apply_foundation" {
  role       = aws_iam_role.foundation_apply.name
  policy_arn = aws_iam_policy.apply_foundation.arn
}

data "aws_iam_policy_document" "plan_state" {
  statement {
    sid = "InspectStateBucket"
    actions = [
      "s3:GetBucketLocation",
      "s3:GetBucketVersioning",
      "s3:ListBucket",
    ]
    resources = ["arn:aws:s3:::${local.state_bucket}"]
  }

  statement {
    sid       = "ReadStateObjects"
    actions   = ["s3:GetObject"]
    resources = ["arn:aws:s3:::${local.state_bucket}/*"]
  }

  statement {
    sid = "LockState"
    actions = [
      "s3:DeleteObject",
      "s3:GetObject",
      "s3:PutObject",
    ]
    resources = ["arn:aws:s3:::${local.state_bucket}/*.tflock"]
  }

  statement {
    sid = "UseStateKey"
    actions = [
      "kms:Decrypt",
      "kms:DescribeKey",
      "kms:Encrypt",
      "kms:GenerateDataKey",
    ]
    resources = [local.state_key_arn]

    condition {
      test     = "ForAnyValue:StringEquals"
      variable = "kms:ResourceAliases"
      values   = [local.state_key_alias]
    }
  }
}

resource "aws_iam_role_policy" "plan_state" {
  name   = "accountable-state"
  role   = aws_iam_role.plan.id
  policy = data.aws_iam_policy_document.plan_state.json
}

data "aws_iam_policy_document" "apply_state" {
  source_policy_documents = [data.aws_iam_policy_document.plan_state.json]

  statement {
    sid = "WriteStateObjects"
    actions = [
      "s3:DeleteObject",
      "s3:PutObject",
    ]
    resources = ["arn:aws:s3:::${local.state_bucket}/*"]
  }
}

resource "aws_iam_role_policy" "apply_state" {
  name   = "accountable-state"
  role   = aws_iam_role.apply.id
  policy = data.aws_iam_policy_document.apply_state.json
}

resource "aws_iam_role_policy" "foundation_apply_state" {
  name   = "accountable-state"
  role   = aws_iam_role.foundation_apply.id
  policy = data.aws_iam_policy_document.apply_state.json
}

data "aws_iam_policy_document" "apply_iam" {
  statement {
    sid    = "ProtectFoundationDeploymentIdentity"
    effect = "Deny"
    actions = [
      "iam:AttachRolePolicy",
      "iam:DeletePolicy",
      "iam:DeletePolicyVersion",
      "iam:DeleteRole",
      "iam:DeleteRolePolicy",
      "iam:DetachRolePolicy",
      "iam:PutRolePolicy",
      "iam:SetDefaultPolicyVersion",
      "iam:UpdateAssumeRolePolicy",
      "iam:UpdateRole",
      "iam:UpdateRoleDescription",
    ]
    resources = [
      "arn:aws:iam::${var.account_id}:role/${local.foundation_apply_role_name}",
      "arn:aws:iam::${var.account_id}:policy/accountable-${var.environment}-foundation",
    ]
  }

  statement {
    sid = "ManageAccountableRuntimeRoles"
    actions = [
      "iam:CreateRole",
      "iam:DeleteRole",
      "iam:DeleteRolePolicy",
      "iam:GetRole",
      "iam:GetRolePolicy",
      "iam:ListAttachedRolePolicies",
      "iam:ListInstanceProfilesForRole",
      "iam:ListRolePolicies",
      "iam:ListRoleTags",
      "iam:PutRolePolicy",
      "iam:TagRole",
      "iam:UntagRole",
      "iam:UpdateAssumeRolePolicy",
      "iam:UpdateRole",
      "iam:UpdateRoleDescription",
    ]
    resources = ["arn:aws:iam::${var.account_id}:role/accountable-*"]
  }

  statement {
    sid       = "AttachApprovedAccountablePolicies"
    actions   = ["iam:AttachRolePolicy"]
    resources = ["arn:aws:iam::${var.account_id}:role/accountable-*"]

    condition {
      test     = "ArnEquals"
      variable = "iam:PolicyARN"
      values   = local.approved_policy_arns
    }
  }

  statement {
    sid       = "DetachAccountableRolePolicies"
    actions   = ["iam:DetachRolePolicy"]
    resources = ["arn:aws:iam::${var.account_id}:role/accountable-*"]
  }

  statement {
    sid       = "PassAccountableTaskRolesOnlyToECS"
    actions   = ["iam:PassRole"]
    resources = ["arn:aws:iam::${var.account_id}:role/accountable-*"]

    condition {
      test     = "StringEquals"
      variable = "iam:PassedToService"
      values   = local.pass_role_services
    }
  }

  statement {
    sid       = "ManageAccountableDeploymentPolicies"
    actions   = local.deployment_policy_actions
    resources = ["arn:aws:iam::${var.account_id}:policy/accountable-*"]
  }

  statement {
    sid       = "CreateRequiredServiceRoles"
    actions   = ["iam:CreateServiceLinkedRole"]
    resources = ["*"]

    condition {
      test     = "StringLike"
      variable = "iam:AWSServiceName"
      values = [
        "autoscaling.amazonaws.com",
        "ecs.amazonaws.com",
        "elasticloadbalancing.amazonaws.com",
        "rds.amazonaws.com",
        "vpcorigin.cloudfront.amazonaws.com",
      ]
    }
  }
}

resource "aws_iam_role_policy" "apply_iam" {
  name   = "accountable-runtime-iam"
  role   = aws_iam_role.apply.id
  policy = data.aws_iam_policy_document.apply_iam.json
}

resource "aws_iam_role_policy" "foundation_apply_iam" {
  name   = "accountable-foundation-iam"
  role   = aws_iam_role.foundation_apply.id
  policy = data.aws_iam_policy_document.apply_iam.json
}
