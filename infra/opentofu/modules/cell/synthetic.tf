locals {
  synthetic_desired_count = var.synthetic_observation_enabled && var.api_desired_count > 0 ? 1 : 0
}

resource "aws_security_group" "synthetic" {
  count = var.synthetic_observation_enabled ? 1 : 0

  name        = "${local.name}-synthetic"
  description = "Continuous production-equivalent synthetic workload"
  vpc_id      = aws_vpc.cell.id
}

resource "aws_vpc_security_group_egress_rule" "synthetic_https" {
  count = var.synthetic_observation_enabled ? 1 : 0

  cidr_ipv4         = "0.0.0.0/0"
  description       = "TLS access to the public cell edge and regional AWS APIs"
  from_port         = 443
  ip_protocol       = "tcp"
  security_group_id = aws_security_group.synthetic[0].id
  to_port           = 443
}

resource "aws_vpc_security_group_egress_rule" "synthetic_credentials" {
  count = var.synthetic_observation_enabled ? 1 : 0

  cidr_ipv4         = "169.254.170.2/32"
  description       = "ECS task-role credential endpoint"
  from_port         = 80
  ip_protocol       = "tcp"
  security_group_id = aws_security_group.synthetic[0].id
  to_port           = 80
}

resource "aws_ecs_task_definition" "synthetic" {
  count = var.synthetic_observation_enabled ? 1 : 0

  container_definitions = jsonencode([{
    command   = ["run", "/scripts/observability.js"]
    essential = true
    environment = [
      { name = "ACCOUNTABLE_DURATION", value = "168h" },
      { name = "ACCOUNTABLE_PROFILE", value = "soak" },
      { name = "ACCOUNTABLE_RATE", value = "1" },
      { name = "ACCOUNTABLE_URL", value = "https://${aws_cloudfront_distribution.cell.domain_name}" },
      { name = "K6_NO_USAGE_REPORT", value = "true" },
    ]
    image                  = var.synthetic_image_uri
    logConfiguration       = local.log_configuration
    name                   = "synthetic"
    readonlyRootFilesystem = true
    secrets = [{
      name      = "ACCOUNTABLE_PROOF_KEY"
      valueFrom = "${local.parameter_arn_prefix}/api/proof-key-active"
    }]
    stopTimeout = 30
  }])
  cpu                      = 256
  execution_role_arn       = aws_iam_role.synthetic_execution[0].arn
  family                   = "${local.name}-synthetic"
  memory                   = 512
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  task_role_arn            = aws_iam_role.synthetic[0].arn

  runtime_platform {
    cpu_architecture        = "ARM64"
    operating_system_family = "LINUX"
  }
}

resource "aws_ecs_service" "synthetic" {
  count = var.synthetic_observation_enabled ? 1 : 0

  name             = "${local.name}-synthetic"
  cluster          = aws_ecs_cluster.cell.id
  desired_count    = local.synthetic_desired_count
  launch_type      = "FARGATE"
  platform_version = "1.4.0"
  task_definition  = aws_ecs_task_definition.synthetic[0].arn

  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }

  network_configuration {
    assign_public_ip = false
    security_groups  = [aws_security_group.synthetic[0].id]
    subnets          = [for subnet in aws_subnet.application : subnet.id]
  }

  lifecycle {
    postcondition {
      condition     = !self.network_configuration[0].assign_public_ip
      error_message = "The synthetic workload must not assign a public IP address."
    }
  }
}

resource "aws_cloudwatch_metric_alarm" "synthetic_task_count" {
  count = local.synthetic_desired_count

  alarm_name          = "${local.name}-synthetic-task-count"
  namespace           = "ECS/ContainerInsights"
  metric_name         = "RunningTaskCount"
  statistic           = "Minimum"
  period              = 60
  evaluation_periods  = 2
  threshold           = 1
  comparison_operator = "LessThanThreshold"
  treat_missing_data  = "breaching"
  dimensions = {
    ClusterName = aws_ecs_cluster.cell.name
    ServiceName = aws_ecs_service.synthetic[0].name
  }
  alarm_actions = [var.alarm_input_topic_arn]
  ok_actions    = [var.alarm_input_topic_arn]

  alarm_description = jsonencode({
    schema_version  = 1
    owner           = "platform"
    severity        = local.alarm_severity
    action          = local.alarm_action
    scope           = "cell"
    signal          = "synthetic.running_task"
    environment     = var.environment
    cell_id         = var.cell_id
    summary         = "Continuous staging synthetic traffic is not running"
    runbook_url     = "${var.runbook_base_url}/service-dependencies"
    recovery_signal = "one synthetic task remains running for 2 minutes"
    dashboard_url   = "${var.dashboard_base_url}/d/accountable-${var.environment}-alerts-deployments"
    identity_url    = "${var.dashboard_base_url}/d/accountable-${var.environment}-service-identity"
  })
}
