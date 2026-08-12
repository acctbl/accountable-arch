resource "aws_vpc" "cell" {
  cidr_block           = var.vpc_cidr
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = {
    Name = local.name
  }
}

resource "aws_internet_gateway" "cell" {
  vpc_id = aws_vpc.cell.id

  tags = {
    Name = local.name
  }
}

resource "aws_subnet" "public" {
  for_each = local.public_subnets

  availability_zone       = each.key
  cidr_block              = each.value
  map_public_ip_on_launch = false
  vpc_id                  = aws_vpc.cell.id

  tags = {
    Name = "${local.name}-public-${each.key}"
    Tier = "public"
  }
}

resource "aws_subnet" "application" {
  for_each = local.application_subnets

  availability_zone       = each.key
  cidr_block              = each.value
  map_public_ip_on_launch = false
  vpc_id                  = aws_vpc.cell.id

  tags = {
    Name = "${local.name}-application-${each.key}"
    Tier = "application"
  }
}

resource "aws_subnet" "database" {
  for_each = local.database_subnets

  availability_zone       = each.key
  cidr_block              = each.value
  map_public_ip_on_launch = false
  vpc_id                  = aws_vpc.cell.id

  tags = {
    Name = "${local.name}-database-${each.key}"
    Tier = "database"
  }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.cell.id

  tags = {
    Name = "${local.name}-public"
  }
}

resource "aws_route" "public_internet" {
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.cell.id
  route_table_id         = aws_route_table.public.id
}

resource "aws_route_table_association" "public" {
  for_each = aws_subnet.public

  route_table_id = aws_route_table.public.id
  subnet_id      = each.value.id
}

resource "aws_eip" "nat" {
  count = var.synthetic_observation_enabled ? 1 : 0

  domain = "vpc"

  depends_on = [aws_internet_gateway.cell]

  tags = {
    Name = "${local.name}-nat"
  }
}

resource "aws_nat_gateway" "cell" {
  count = var.synthetic_observation_enabled ? 1 : 0

  allocation_id = aws_eip.nat[0].id
  subnet_id     = aws_subnet.public[var.availability_zones[0]].id

  tags = {
    Name = local.name
  }
}

resource "aws_route_table" "application" {
  vpc_id = aws_vpc.cell.id

  tags = {
    Name = "${local.name}-application"
  }
}

resource "aws_route" "application_internet" {
  count = var.synthetic_observation_enabled ? 1 : 0

  destination_cidr_block = "0.0.0.0/0"
  nat_gateway_id         = aws_nat_gateway.cell[0].id
  route_table_id         = aws_route_table.application.id
}

resource "aws_route_table_association" "application" {
  for_each = aws_subnet.application

  route_table_id = aws_route_table.application.id
  subnet_id      = each.value.id
}

resource "aws_route_table" "database" {
  vpc_id = aws_vpc.cell.id

  tags = {
    Name = "${local.name}-database"
  }
}

resource "aws_route_table_association" "database" {
  for_each = aws_subnet.database

  route_table_id = aws_route_table.database.id
  subnet_id      = each.value.id
}

resource "aws_vpc_endpoint" "s3" {
  service_name      = "com.amazonaws.${data.aws_region.current.region}.s3"
  route_table_ids   = [aws_route_table.application.id]
  vpc_endpoint_type = "Gateway"
  vpc_id            = aws_vpc.cell.id

  tags = {
    Name = "${local.name}-s3"
  }
}

data "aws_region" "current" {}

data "aws_ec2_managed_prefix_list" "s3" {
  name = "com.amazonaws.${data.aws_region.current.region}.s3"
}

locals {
  regional_endpoint_services = toset([
    "ecr.api",
    "ecr.dkr",
    "kms",
    "logs",
    "secretsmanager",
    "ssm",
  ])

  task_security_groups = merge(
    {
      api       = aws_security_group.api.id
      bootstrap = aws_security_group.bootstrap.id
      collector = aws_security_group.collector.id
      migrate   = aws_security_group.migrate.id
    },
    var.synthetic_observation_enabled ? {
      synthetic = aws_security_group.synthetic[0].id
    } : {},
  )
}

resource "aws_security_group" "regional_endpoints" {
  name        = "${local.name}-regional-endpoints"
  description = "Private regional AWS service endpoints"
  vpc_id      = aws_vpc.cell.id
}

resource "aws_vpc_security_group_ingress_rule" "task_to_regional_endpoints" {
  for_each = local.task_security_groups

  description                  = "${each.key} task access to regional AWS service endpoints"
  from_port                    = 443
  ip_protocol                  = "tcp"
  referenced_security_group_id = each.value
  security_group_id            = aws_security_group.regional_endpoints.id
  to_port                      = 443
}

resource "aws_vpc_security_group_egress_rule" "task_to_regional_endpoints" {
  for_each = local.task_security_groups

  description                  = "${each.key} task access to regional AWS service endpoints"
  from_port                    = 443
  ip_protocol                  = "tcp"
  referenced_security_group_id = aws_security_group.regional_endpoints.id
  security_group_id            = each.value
  to_port                      = 443
}

resource "aws_vpc_security_group_egress_rule" "task_to_s3" {
  for_each = local.task_security_groups

  description       = "${each.key} task access to regional S3"
  from_port         = 443
  ip_protocol       = "tcp"
  prefix_list_id    = data.aws_ec2_managed_prefix_list.s3.id
  security_group_id = each.value
  to_port           = 443
}

resource "aws_vpc_security_group_egress_rule" "task_to_dns_tcp" {
  for_each = local.task_security_groups

  cidr_ipv4         = "${cidrhost(var.vpc_cidr, 2)}/32"
  description       = "${each.key} task access to the VPC DNS resolver over TCP"
  from_port         = 53
  ip_protocol       = "tcp"
  security_group_id = each.value
  to_port           = 53
}

resource "aws_vpc_security_group_egress_rule" "task_to_dns_udp" {
  for_each = local.task_security_groups

  cidr_ipv4         = "${cidrhost(var.vpc_cidr, 2)}/32"
  description       = "${each.key} task access to the VPC DNS resolver over UDP"
  from_port         = 53
  ip_protocol       = "udp"
  security_group_id = each.value
  to_port           = 53
}

resource "aws_vpc_endpoint" "regional" {
  for_each = local.regional_endpoint_services

  private_dns_enabled = true
  security_group_ids  = [aws_security_group.regional_endpoints.id]
  service_name        = "com.amazonaws.${data.aws_region.current.region}.${each.value}"
  subnet_ids          = [for subnet in aws_subnet.application : subnet.id]
  vpc_endpoint_type   = "Interface"
  vpc_id              = aws_vpc.cell.id

  tags = {
    Name = "${local.name}-${replace(each.value, ".", "-")}"
  }
}

resource "aws_security_group" "grafana_otlp" {
  name        = "${local.name}-grafana-otlp"
  description = "Grafana Cloud OTLP PrivateLink endpoint"
  vpc_id      = aws_vpc.cell.id
}

resource "aws_vpc_security_group_ingress_rule" "collector_to_grafana_otlp" {
  description                  = "Collector access to Grafana Cloud OTLP PrivateLink"
  from_port                    = 443
  ip_protocol                  = "tcp"
  referenced_security_group_id = aws_security_group.collector.id
  security_group_id            = aws_security_group.grafana_otlp.id
  to_port                      = 443
}

resource "aws_vpc_security_group_egress_rule" "collector_to_grafana_otlp" {
  description                  = "Collector access to Grafana Cloud OTLP PrivateLink"
  from_port                    = 443
  ip_protocol                  = "tcp"
  referenced_security_group_id = aws_security_group.grafana_otlp.id
  security_group_id            = aws_security_group.collector.id
  to_port                      = 443
}

resource "aws_vpc_endpoint" "grafana_otlp" {
  private_dns_enabled = true
  security_group_ids  = [aws_security_group.grafana_otlp.id]
  service_name        = var.grafana_otlp_private_link_service_name
  subnet_ids          = [for subnet in aws_subnet.application : subnet.id]
  vpc_endpoint_type   = "Interface"
  vpc_id              = aws_vpc.cell.id

  tags = {
    Name = "${local.name}-grafana-otlp"
  }
}
