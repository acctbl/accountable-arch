locals {
  apply_role_name = "accountable-${var.environment}-apply"
  plan_role_name  = "accountable-${var.environment}-plan"
  state_bucket    = "accountable-tofu-state-${var.account_id}-${var.region}"
  state_key_alias = "alias/accountable-${var.environment}-tofu-state"
  state_key_arn   = "arn:aws:kms:${var.region}:${var.account_id}:key/*"

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
    "arn:aws:iam::aws:policy/ReadOnlyAccess",
    "arn:aws:iam::aws:policy/service-role/AWSBackupServiceRolePolicyForBackup",
  ]
  pass_role_services = ["backup.amazonaws.com", "ecs-tasks.amazonaws.com"]
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
      values   = ["${var.github_oidc_subject_prefix}:ref:refs/heads/main"]
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
      values   = ["${var.github_oidc_subject_prefix}:environment:${var.environment}"]
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

resource "aws_iam_role" "apply" {
  name                 = local.apply_role_name
  assume_role_policy   = data.aws_iam_policy_document.apply_assume.json
  max_session_duration = 3600
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
    sid = "CreateCellKeys"
    actions = [
      "kms:CreateAlias",
      "kms:CreateKey",
      "kms:ListAliases",
    ]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "aws:RequestedRegion"
      values   = [var.region]
    }
  }

  statement {
    sid = "ManageCellKeys"
    actions = [
      "kms:DeleteAlias",
      "kms:DescribeKey",
      "kms:EnableKeyRotation",
      "kms:GetKeyPolicy",
      "kms:GetKeyRotationStatus",
      "kms:ListResourceTags",
      "kms:PutKeyPolicy",
      "kms:ScheduleKeyDeletion",
      "kms:TagResource",
      "kms:UntagResource",
      "kms:UpdateAlias",
    ]
    resources = [
      "arn:aws:kms:${var.region}:${var.account_id}:alias/accountable-*",
      "arn:aws:kms:${var.region}:${var.account_id}:key/*",
    ]
  }

  statement {
    sid       = "GrantCellKeysToAWSResources"
    actions   = ["kms:CreateGrant"]
    resources = ["arn:aws:kms:${var.region}:${var.account_id}:key/*"]

    condition {
      test     = "Bool"
      variable = "kms:GrantIsForAWSResource"
      values   = ["true"]
    }
  }

  statement {
    sid = "UseCellKeysThroughAWSResources"
    actions = [
      "kms:Decrypt",
      "kms:GenerateDataKey",
    ]
    resources = ["arn:aws:kms:${var.region}:${var.account_id}:key/*"]

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values = [
        "rds.${var.region}.amazonaws.com",
        "s3.${var.region}.amazonaws.com",
        "secretsmanager.${var.region}.amazonaws.com",
      ]
    }
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
      "cloudfront:CreateCachePolicy",
      "cloudfront:CreateDistribution",
      "cloudfront:CreateDistributionWithTags",
      "cloudfront:CreateFunction",
      "cloudfront:CreateOriginAccessControl",
      "cloudfront:CreateOriginRequestPolicy",
      "cloudfront:CreateResponseHeadersPolicy",
      "cloudfront:CreateVpcOrigin",
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
      "cloudfront:DeleteCachePolicy",
      "cloudfront:DeleteDistribution",
      "cloudfront:DeleteFunction",
      "cloudfront:DeleteOriginAccessControl",
      "cloudfront:DeleteOriginRequestPolicy",
      "cloudfront:DeleteResponseHeadersPolicy",
      "cloudfront:DeleteVpcOrigin",
      "cloudfront:DescribeFunction",
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
    sid       = "PublishAndInspectRuntimeImages"
    actions   = local.delivery_ecr_actions
    resources = ["arn:aws:ecr:${var.region}:${var.account_id}:repository/accountable"]
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

data "aws_iam_policy_document" "apply_foundation" {
  statement {
    sid       = "ManageAccountPublicAccessBlock"
    actions   = ["s3:GetAccountPublicAccessBlock", "s3:PutAccountPublicAccessBlock"]
    resources = ["*"]
  }

  statement {
    sid       = "ManageApplicationRepository"
    actions   = local.foundation_ecr_actions
    resources = ["arn:aws:ecr:${var.region}:${var.account_id}:repository/accountable"]
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
  role       = aws_iam_role.apply.name
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

data "aws_iam_policy_document" "apply_iam" {
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
