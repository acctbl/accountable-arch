data "aws_ec2_managed_prefix_list" "cloudfront" {
  name = "com.amazonaws.global.cloudfront.origin-facing"
}

resource "aws_security_group" "alb" {
  name        = "${local.name}-alb"
  description = "CloudFront VPC origin to the internal API load balancer"
  vpc_id      = aws_vpc.cell.id
}

resource "aws_vpc_security_group_ingress_rule" "cloudfront_to_alb" {
  description       = "CloudFront origin traffic"
  from_port         = 80
  ip_protocol       = "tcp"
  prefix_list_id    = data.aws_ec2_managed_prefix_list.cloudfront.id
  security_group_id = aws_security_group.alb.id
  to_port           = 80

  lifecycle {
    postcondition {
      condition     = self.cidr_ipv4 != "0.0.0.0/0" && self.cidr_ipv6 != "::/0"
      error_message = "Security-group ingress must not be world-open."
    }
  }
}

resource "aws_security_group" "api" {
  name        = "${local.name}-api"
  description = "Long-running API tasks"
  vpc_id      = aws_vpc.cell.id
}

resource "aws_vpc_security_group_ingress_rule" "alb_to_api" {
  description                  = "Internal ALB to API tasks"
  from_port                    = local.api_port
  ip_protocol                  = "tcp"
  referenced_security_group_id = aws_security_group.alb.id
  security_group_id            = aws_security_group.api.id
  to_port                      = local.api_port

  lifecycle {
    postcondition {
      condition     = self.cidr_ipv4 != "0.0.0.0/0" && self.cidr_ipv6 != "::/0"
      error_message = "Security-group ingress must not be world-open."
    }
  }
}

resource "aws_vpc_security_group_egress_rule" "alb_to_api" {
  description                  = "Internal ALB to API tasks"
  from_port                    = local.api_port
  ip_protocol                  = "tcp"
  referenced_security_group_id = aws_security_group.api.id
  security_group_id            = aws_security_group.alb.id
  to_port                      = local.api_port
}

resource "aws_security_group" "migrate" {
  name        = "${local.name}-migrate"
  description = "One-off database migration tasks"
  vpc_id      = aws_vpc.cell.id
}

resource "aws_security_group" "bootstrap" {
  name        = "${local.name}-bootstrap"
  description = "One-off database bootstrap tasks"
  vpc_id      = aws_vpc.cell.id
}

resource "aws_security_group" "database" {
  name        = "${local.name}-database"
  description = "Private PostgreSQL"
  vpc_id      = aws_vpc.cell.id
}

resource "aws_vpc_security_group_ingress_rule" "api_to_database" {
  description                  = "API tasks to PostgreSQL"
  from_port                    = local.database_port
  ip_protocol                  = "tcp"
  referenced_security_group_id = aws_security_group.api.id
  security_group_id            = aws_security_group.database.id
  to_port                      = local.database_port

  lifecycle {
    postcondition {
      condition     = self.cidr_ipv4 != "0.0.0.0/0" && self.cidr_ipv6 != "::/0"
      error_message = "Security-group ingress must not be world-open."
    }
  }
}

resource "aws_vpc_security_group_ingress_rule" "migrate_to_database" {
  description                  = "Migration tasks to PostgreSQL"
  from_port                    = local.database_port
  ip_protocol                  = "tcp"
  referenced_security_group_id = aws_security_group.migrate.id
  security_group_id            = aws_security_group.database.id
  to_port                      = local.database_port

  lifecycle {
    postcondition {
      condition     = self.cidr_ipv4 != "0.0.0.0/0" && self.cidr_ipv6 != "::/0"
      error_message = "Security-group ingress must not be world-open."
    }
  }
}

resource "aws_vpc_security_group_ingress_rule" "bootstrap_to_database" {
  description                  = "Bootstrap tasks to PostgreSQL"
  from_port                    = local.database_port
  ip_protocol                  = "tcp"
  referenced_security_group_id = aws_security_group.bootstrap.id
  security_group_id            = aws_security_group.database.id
  to_port                      = local.database_port

  lifecycle {
    postcondition {
      condition     = self.cidr_ipv4 != "0.0.0.0/0" && self.cidr_ipv6 != "::/0"
      error_message = "Security-group ingress must not be world-open."
    }
  }
}

# Regional AWS APIs do not publish stable shared destination prefixes. Closing this
# rule requires interface endpoints for SSM, KMS, ECR, and CloudWatch Logs.
#trivy:ignore:AVD-AWS-0104:exp:2026-11-05
resource "aws_vpc_security_group_egress_rule" "runtime_https" {
  for_each = {
    api       = aws_security_group.api.id
    bootstrap = aws_security_group.bootstrap.id
    migrate   = aws_security_group.migrate.id
  }

  cidr_ipv4         = "0.0.0.0/0"
  description       = "TLS access to regional AWS APIs"
  from_port         = 443
  ip_protocol       = "tcp"
  security_group_id = each.value
  to_port           = 443
}

resource "aws_vpc_security_group_egress_rule" "runtime_credentials" {
  for_each = {
    api       = aws_security_group.api.id
    bootstrap = aws_security_group.bootstrap.id
    migrate   = aws_security_group.migrate.id
  }

  cidr_ipv4         = "169.254.170.2/32"
  description       = "ECS task-role credential endpoint"
  from_port         = 80
  ip_protocol       = "tcp"
  security_group_id = each.value
  to_port           = 80
}

resource "aws_vpc_security_group_egress_rule" "runtime_to_database" {
  for_each = {
    api       = aws_security_group.api.id
    bootstrap = aws_security_group.bootstrap.id
    migrate   = aws_security_group.migrate.id
  }

  description                  = "Runtime access to PostgreSQL"
  from_port                    = local.database_port
  ip_protocol                  = "tcp"
  referenced_security_group_id = aws_security_group.database.id
  security_group_id            = each.value
  to_port                      = local.database_port
}
