locals {
  collector_parameter_path = "${local.parameter_root}/collector"
  collector_token_names = [
    "grafana-otlp-token-active",
    "grafana-otlp-token-standby",
  ]
  collector_service_dns = "collector.${var.cell_id}.accountable.internal"

  collector_init_document = jsonencode({
    schema_version        = 1
    environment           = var.environment
    cell_id               = var.cell_id
    aws_region            = data.aws_region.current.region
    parameter_path        = local.collector_parameter_path
    otlp_token_ref        = var.collector_otlp_token_ref
    grafana_otlp_endpoint = var.grafana_otlp_endpoint
    grafana_instance_id   = var.grafana_instance_id
  })

  # The API reaches the Collector over the private cell network only. Regional
  # TLS terminates at the Grafana gateway, which the Collector calls itself.
  collector_endpoint = "http://${local.collector_service_dns}:${local.collector_application_port}"
}

resource "aws_service_discovery_private_dns_namespace" "cell" {
  description = "Private discovery for cell runtime services"
  name        = "${var.cell_id}.accountable.internal"
  vpc         = aws_vpc.cell.id
}

resource "aws_service_discovery_service" "collector" {
  name = "collector"

  dns_config {
    namespace_id   = aws_service_discovery_private_dns_namespace.cell.id
    routing_policy = "MULTIVALUE"

    dns_records {
      ttl  = 15
      type = "A"
    }
  }
}

resource "aws_security_group" "collector" {
  name        = "${local.name}-collector"
  description = "Private cell telemetry Collector"
  vpc_id      = aws_vpc.cell.id
}

resource "aws_vpc_security_group_ingress_rule" "runtime_to_collector" {
  for_each = {
    api       = aws_security_group.api.id
    bootstrap = aws_security_group.bootstrap.id
    migrate   = aws_security_group.migrate.id
  }

  description                  = "Managed runtimes to the Collector application receiver"
  from_port                    = local.collector_application_port
  ip_protocol                  = "tcp"
  referenced_security_group_id = each.value
  security_group_id            = aws_security_group.collector.id
  to_port                      = local.collector_application_port

  lifecycle {
    postcondition {
      condition     = self.cidr_ipv4 != "0.0.0.0/0" && self.cidr_ipv6 != "::/0"
      error_message = "Security-group ingress must not be world-open."
    }
  }
}

resource "aws_vpc_security_group_ingress_rule" "api_to_collector_sli" {
  description                  = "API tasks to the Collector SLI receiver"
  from_port                    = local.collector_sli_port
  ip_protocol                  = "tcp"
  referenced_security_group_id = aws_security_group.api.id
  security_group_id            = aws_security_group.collector.id
  to_port                      = local.collector_sli_port

  lifecycle {
    postcondition {
      condition     = self.cidr_ipv4 != "0.0.0.0/0" && self.cidr_ipv6 != "::/0"
      error_message = "Security-group ingress must not be world-open."
    }
  }
}

resource "aws_vpc_security_group_egress_rule" "runtime_to_collector" {
  for_each = {
    api       = aws_security_group.api.id
    bootstrap = aws_security_group.bootstrap.id
    migrate   = aws_security_group.migrate.id
  }

  description                  = "Managed runtimes to the Collector application receiver"
  from_port                    = local.collector_application_port
  ip_protocol                  = "tcp"
  referenced_security_group_id = aws_security_group.collector.id
  security_group_id            = each.value
  to_port                      = local.collector_application_port
}

resource "aws_vpc_security_group_egress_rule" "api_to_collector_sli" {
  description                  = "API tasks to the Collector SLI receiver"
  from_port                    = local.collector_sli_port
  ip_protocol                  = "tcp"
  referenced_security_group_id = aws_security_group.collector.id
  security_group_id            = aws_security_group.api.id
  to_port                      = local.collector_sli_port
}

# Grafana Cloud OTLP, Parameter Store, and CloudWatch Logs do not publish stable
# shared destination prefixes.
resource "aws_vpc_security_group_egress_rule" "collector_https" {
  cidr_ipv4         = "0.0.0.0/0"
  description       = "TLS access to Grafana Cloud and regional AWS APIs"
  from_port         = 443
  ip_protocol       = "tcp"
  security_group_id = aws_security_group.collector.id
  to_port           = 443
}

resource "aws_vpc_security_group_egress_rule" "collector_credentials" {
  cidr_ipv4         = "169.254.170.2/32"
  description       = "ECS task-role credential endpoint"
  from_port         = 80
  ip_protocol       = "tcp"
  security_group_id = aws_security_group.collector.id
  to_port           = 80
}

resource "aws_cloudwatch_log_group" "emf_sli" {
  name              = "/accountable/${var.cell_id}/emf-sli"
  retention_in_days = 30
}

resource "aws_cloudwatch_log_group" "emf_internal" {
  name              = "/accountable/${var.cell_id}/emf-internal"
  retention_in_days = 30
}

resource "aws_iam_role" "collector" {
  name               = "${local.name}-collector"
  assume_role_policy = data.aws_iam_policy_document.ecs_task_assume.json
}

data "aws_iam_policy_document" "collector" {
  statement {
    actions = [
      "logs:CreateLogStream",
      "logs:PutLogEvents",
    ]
    resources = [
      aws_cloudwatch_log_group.emf_sli.arn,
      "${aws_cloudwatch_log_group.emf_sli.arn}:*",
      aws_cloudwatch_log_group.emf_internal.arn,
      "${aws_cloudwatch_log_group.emf_internal.arn}:*",
    ]
  }

  statement {
    actions = local.parameter_read_actions
    resources = [
      for name in local.collector_token_names : "${local.parameter_arn_prefix}/collector/${name}"
    ]
  }

  statement {
    actions   = ["kms:Decrypt"]
    resources = [aws_kms_key.cell.arn]

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["ssm.${data.aws_region.current.region}.amazonaws.com"]
    }
  }
}

resource "aws_iam_role_policy" "collector" {
  name   = "${local.name}-collector"
  policy = data.aws_iam_policy_document.collector.json
  role   = aws_iam_role.collector.id
}

resource "aws_ecs_task_definition" "collector" {
  container_definitions = jsonencode([
    merge(local.runtime_scratch, {
      command   = ["collector-init"]
      essential = false
      environment = [{
        name  = "ACCOUNTABLE_CONFIG_BASE64"
        value = base64encode(local.collector_init_document)
      }]
      image                  = var.image_uri
      logConfiguration       = local.log_configuration
      name                   = "collector-init"
      readonlyRootFilesystem = true
    }),
    merge(local.collector_scratch_read_only, {
      command   = ["--config", local.collector_config_file]
      essential = true
      dependsOn = [{
        condition     = "SUCCESS"
        containerName = "collector-init"
      }]
      image            = var.collector_image_uri
      logConfiguration = local.log_configuration
      name             = "collector"
      portMappings = [
        {
          containerPort = local.collector_application_port
          hostPort      = local.collector_application_port
          name          = "otlp-app"
          protocol      = "tcp"
        },
        {
          containerPort = local.collector_sli_port
          hostPort      = local.collector_sli_port
          name          = "otlp-sli"
          protocol      = "tcp"
        },
      ]
      readonlyRootFilesystem = true
    }),
  ])
  cpu                      = 512
  execution_role_arn       = aws_iam_role.execution.arn
  family                   = "${local.name}-collector"
  memory                   = 1024
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  task_role_arn            = aws_iam_role.collector.arn

  volume {
    name = local.scratch_volume_name
  }

  runtime_platform {
    cpu_architecture        = "ARM64"
    operating_system_family = "LINUX"
  }
}

resource "aws_ecs_service" "collector" {
  name             = "${local.name}-collector"
  cluster          = aws_ecs_cluster.cell.id
  desired_count    = var.collector_desired_count
  launch_type      = "FARGATE"
  platform_version = "1.4.0"
  task_definition  = aws_ecs_task_definition.collector.arn

  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }

  network_configuration {
    assign_public_ip = false
    security_groups  = [aws_security_group.collector.id]
    subnets          = [for subnet in aws_subnet.application : subnet.id]
  }

  service_registries {
    registry_arn = aws_service_discovery_service.collector.arn
  }

  lifecycle {
    postcondition {
      condition     = !self.network_configuration[0].assign_public_ip
      error_message = "The Collector service must not assign public IP addresses."
    }
  }
}
