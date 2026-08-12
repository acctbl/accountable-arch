locals {
  alarm_severity = "critical"
  alarm_action   = var.environment == "production" ? "page" : "warning"

  standard_alarms = merge(
    var.api_desired_count > 0 ? {
      api_task_count = {
        namespace   = "ECS/ContainerInsights"
        metric      = "RunningTaskCount"
        statistic   = "Minimum"
        comparison  = "LessThanThreshold"
        threshold   = var.api_desired_count
        period      = 60
        evaluations = 2
        missing     = "breaching"
        dimensions  = { ClusterName = aws_ecs_cluster.cell.name, ServiceName = aws_ecs_service.api.name }
        signal      = "ecs.api.task_count"
        summary     = "API task count is below its deployment target"
        recovery    = "the API service reaches its desired task count for 2 minutes"
      }
    } : {},
    {
      collector_task_count = {
        namespace   = "ECS/ContainerInsights"
        metric      = "RunningTaskCount"
        statistic   = "Minimum"
        comparison  = "LessThanThreshold"
        threshold   = var.collector_desired_count
        period      = 60
        evaluations = 2
        missing     = "breaching"
        dimensions  = { ClusterName = aws_ecs_cluster.cell.name, ServiceName = aws_ecs_service.collector.name }
        signal      = "ecs.collector.task_count"
        summary     = "Collector task count is below its deployment target"
        recovery    = "the Collector service reaches its desired task count for 2 minutes"
      }
      alb_unhealthy = {
        namespace   = "AWS/ApplicationELB"
        metric      = "UnHealthyHostCount"
        statistic   = "Maximum"
        comparison  = "GreaterThanThreshold"
        threshold   = 0
        period      = 60
        evaluations = 2
        missing     = "breaching"
        dimensions  = { LoadBalancer = aws_lb.api.arn_suffix, TargetGroup = aws_lb_target_group.api.arn_suffix }
        signal      = "alb.unhealthy_targets"
        summary     = "The API load balancer has unhealthy targets"
        recovery    = "all registered targets remain healthy for 2 minutes"
      }
      alb_server_errors = {
        namespace   = "AWS/ApplicationELB"
        metric      = "HTTPCode_Target_5XX_Count"
        statistic   = "Sum"
        comparison  = "GreaterThanOrEqualToThreshold"
        threshold   = 5
        period      = 300
        evaluations = 1
        missing     = "notBreaching"
        dimensions  = { LoadBalancer = aws_lb.api.arn_suffix, TargetGroup = aws_lb_target_group.api.arn_suffix }
        signal      = "alb.server_errors"
        summary     = "The API load balancer observed a server-error surge"
        recovery    = "target 5xx responses remain below 5 for 5 minutes"
      }
      sli_heartbeat = {
        namespace   = "Accountable/SLO"
        metric      = "SLIExportHeartbeatCount"
        statistic   = "Sum"
        comparison  = "LessThanThreshold"
        threshold   = 1
        period      = 300
        evaluations = 2
        missing     = "breaching"
        dimensions  = { Environment = var.environment, ServiceName = "accountable-api", CellId = var.cell_id }
        signal      = "sli.heartbeat"
        summary     = "The cell SLI heartbeat is missing"
        recovery    = "two complete 5-minute periods contain a heartbeat"
      }
      rds_storage = {
        namespace   = "AWS/RDS"
        metric      = "FreeStorageSpace"
        statistic   = "Minimum"
        comparison  = "LessThanThreshold"
        threshold   = 5368709120
        period      = 300
        evaluations = 2
        missing     = "breaching"
        dimensions  = { DBInstanceIdentifier = aws_db_instance.cell.identifier }
        signal      = "rds.storage"
        summary     = "RDS free storage is below 5 GiB"
        recovery    = "free storage remains above 5 GiB for 10 minutes"
      }
      rds_memory = {
        namespace   = "AWS/RDS"
        metric      = "FreeableMemory"
        statistic   = "Minimum"
        comparison  = "LessThanThreshold"
        threshold   = 134217728
        period      = 300
        evaluations = 2
        missing     = "breaching"
        dimensions  = { DBInstanceIdentifier = aws_db_instance.cell.identifier }
        signal      = "rds.memory"
        summary     = "RDS freeable memory is below 128 MiB"
        recovery    = "freeable memory remains above 128 MiB for 10 minutes"
      }
      rds_cpu = {
        namespace   = "AWS/RDS"
        metric      = "CPUUtilization"
        statistic   = "Average"
        comparison  = "GreaterThanOrEqualToThreshold"
        threshold   = 80
        period      = 300
        evaluations = 3
        missing     = "breaching"
        dimensions  = { DBInstanceIdentifier = aws_db_instance.cell.identifier }
        signal      = "rds.cpu"
        summary     = "RDS CPU utilization is sustained above 80 percent"
        recovery    = "CPU utilization remains below 80 percent for 15 minutes"
      }
      rds_connections = {
        namespace   = "AWS/RDS"
        metric      = "DatabaseConnections"
        statistic   = "Maximum"
        comparison  = "GreaterThanOrEqualToThreshold"
        threshold   = 80
        period      = 300
        evaluations = 2
        missing     = "breaching"
        dimensions  = { DBInstanceIdentifier = aws_db_instance.cell.identifier }
        signal      = "rds.connections"
        summary     = "RDS database connections are near the instance limit"
        recovery    = "database connections remain below 80 for 10 minutes"
      }
      rds_read_latency = {
        namespace   = "AWS/RDS"
        metric      = "ReadLatency"
        statistic   = "Average"
        comparison  = "GreaterThanOrEqualToThreshold"
        threshold   = 0.1
        period      = 300
        evaluations = 3
        missing     = "notBreaching"
        dimensions  = { DBInstanceIdentifier = aws_db_instance.cell.identifier }
        signal      = "rds.read_latency"
        summary     = "RDS read latency is sustained above 100 milliseconds"
        recovery    = "read latency remains below 100 milliseconds for 15 minutes"
      }
      rds_write_latency = {
        namespace   = "AWS/RDS"
        metric      = "WriteLatency"
        statistic   = "Average"
        comparison  = "GreaterThanOrEqualToThreshold"
        threshold   = 0.1
        period      = 300
        evaluations = 3
        missing     = "notBreaching"
        dimensions  = { DBInstanceIdentifier = aws_db_instance.cell.identifier }
        signal      = "rds.write_latency"
        summary     = "RDS write latency is sustained above 100 milliseconds"
        recovery    = "write latency remains below 100 milliseconds for 15 minutes"
      }
      nat_ports = {
        namespace   = "AWS/NATGateway"
        metric      = "ErrorPortAllocation"
        statistic   = "Sum"
        comparison  = "GreaterThanThreshold"
        threshold   = 0
        period      = 300
        evaluations = 1
        missing     = "notBreaching"
        dimensions  = { NatGatewayId = aws_nat_gateway.cell.id }
        signal      = "nat.port_allocation"
        summary     = "The NAT gateway could not allocate an outbound port"
        recovery    = "port-allocation errors remain zero for 5 minutes"
      }
    },
    var.cell_lifecycle == "durable" ? {
      backup_failure = {
        namespace   = "AWS/Backup"
        metric      = "NumberOfBackupJobsFailed"
        statistic   = "Sum"
        comparison  = "GreaterThanThreshold"
        threshold   = 0
        period      = 3600
        evaluations = 1
        missing     = "notBreaching"
        dimensions  = { BackupVaultName = aws_backup_vault.cell[0].name, ResourceType = "RDS" }
        signal      = "backup.failure"
        summary     = "A durable RDS backup job failed"
        recovery    = "the next scheduled backup completes successfully"
      }
    } : {},
  )

  search_alarms = {
    collector_refusal = {
      expression = "SUM(SEARCH('{Accountable/Collector,Environment,CellId,Pipeline,Signal} MetricName=\"RefusedItemCount\" Environment=\"${var.environment}\" CellId=\"${var.cell_id}\"', 'Sum', 300))"
      comparison = "GreaterThanThreshold"
      threshold  = 0
      missing    = "notBreaching"
      signal     = "collector.refusal"
      summary    = "The Collector refused telemetry before queue ownership"
      recovery   = "refused item counters remain zero for 5 minutes"
    }
    collector_drop = {
      expression = "SUM(SEARCH('{Accountable/Collector,Environment,CellId,Pipeline,Signal} MetricName=\"PermanentlyDroppedItemCount\" Environment=\"${var.environment}\" CellId=\"${var.cell_id}\"', 'Sum', 300))"
      comparison = "GreaterThanThreshold"
      threshold  = 0
      missing    = "notBreaching"
      signal     = "collector.permanent_drop"
      summary    = "The Collector permanently dropped telemetry"
      recovery   = "permanent-drop counters remain zero for 5 minutes"
    }
    collector_memory = {
      expression = "MAX(SEARCH('{Accountable/Collector,Environment,CellId,CollectorTaskId} MetricName=\"CollectorMemoryRssBytes\" Environment=\"${var.environment}\" CellId=\"${var.cell_id}\"', 'Maximum', 300))"
      comparison = "GreaterThanOrEqualToThreshold"
      threshold  = 429496730
      missing    = "breaching"
      signal     = "collector.memory"
      summary    = "Collector memory is above 80 percent of its task limit"
      recovery   = "all Collector tasks remain below 80 percent memory for 10 minutes"
    }
    sli_uncertainty = {
      expression = "SUM(SEARCH('{Accountable/Collector} MetricName=\"SLIDeliveryUncertain\" Environment=\"${var.environment}\" CellId=\"${var.cell_id}\"', 'Sum', 300))"
      comparison = "GreaterThanThreshold"
      threshold  = 0
      missing    = "notBreaching"
      signal     = "sli.delivery_uncertain"
      summary    = "The cell SLI delivery state is uncertain"
      recovery   = "no new uncertainty occurs and the affected period leaves the budget window"
    }
    collector_uncertainty = {
      expression = "SUM(SEARCH('{Accountable/Collector} MetricName=\"CollectorDeliveryUncertain\" Environment=\"${var.environment}\" CellId=\"${var.cell_id}\"', 'Sum', 300)) + SUM(SEARCH('{Accountable/Collector} MetricName=\"CollectorAccountingUncertain\" Environment=\"${var.environment}\" CellId=\"${var.cell_id}\"', 'Sum', 300))"
      comparison = "GreaterThanThreshold"
      threshold  = 0
      missing    = "notBreaching"
      signal     = "collector.delivery_uncertain"
      summary    = "Collector terminal accounting is uncertain"
      recovery   = "the affected uncertainty leaves the 7-day loss window"
    }
    client_loss = {
      expression = "SUM(SEARCH('{Accountable/ClientTelemetry} MetricName=\"ClientEventDropped\" Environment=\"${var.environment}\" CellId=\"${var.cell_id}\"', 'Sum', 300)) + SUM(SEARCH('{Accountable/ClientTelemetry} MetricName=\"ClientEventDeliveryUncertain\" Environment=\"${var.environment}\" CellId=\"${var.cell_id}\"', 'Sum', 300))"
      comparison = "GreaterThanThreshold"
      threshold  = 0
      missing    = "notBreaching"
      signal     = "client.delivery"
      summary    = "Client telemetry was dropped or became uncertain"
      recovery   = "no new client loss occurs for 15 minutes"
    }
  }
}

resource "aws_route53_health_check" "public_api" {
  provider = aws.global

  fqdn              = aws_cloudfront_distribution.cell.domain_name
  port              = 443
  type              = "HTTPS"
  resource_path     = "/api/_health/live"
  request_interval  = 30
  failure_threshold = 3
  measure_latency   = true

  tags = { Name = "${local.name}-public-api" }
}

resource "aws_cloudwatch_metric_alarm" "standard" {
  for_each = local.standard_alarms

  alarm_name          = "${local.name}-${replace(each.key, "_", "-")}"
  namespace           = each.value.namespace
  metric_name         = each.value.metric
  statistic           = each.value.statistic
  period              = each.value.period
  evaluation_periods  = each.value.evaluations
  threshold           = each.value.threshold
  comparison_operator = each.value.comparison
  treat_missing_data  = each.value.missing
  dimensions          = each.value.dimensions
  alarm_actions       = [var.alarm_input_topic_arn]
  ok_actions          = [var.alarm_input_topic_arn]

  alarm_description = jsonencode({
    schema_version = 1, owner = "platform", severity = local.alarm_severity, action = local.alarm_action,
    scope          = "cell", signal = each.value.signal, environment = var.environment, cell_id = var.cell_id,
    summary        = each.value.summary, runbook_url = "${var.runbook_base_url}/service-dependencies", recovery_signal = each.value.recovery,
    dashboard_url  = "${var.dashboard_base_url}/d/accountable-${var.environment}-server-dependencies",
    identity_url   = "${var.dashboard_base_url}/d/accountable-${var.environment}-service-identity",
  })
}

resource "aws_cloudwatch_metric_alarm" "search" {
  for_each = local.search_alarms

  alarm_name          = "${local.name}-${replace(each.key, "_", "-")}"
  comparison_operator = each.value.comparison
  threshold           = each.value.threshold
  evaluation_periods  = each.key == "collector_memory" ? 2 : 1
  datapoints_to_alarm = each.key == "collector_memory" ? 2 : 1
  treat_missing_data  = each.value.missing
  alarm_actions       = [var.alarm_input_topic_arn]
  ok_actions          = [var.alarm_input_topic_arn]

  metric_query {
    id          = "signal"
    expression  = each.value.expression
    label       = each.value.signal
    return_data = true
  }

  alarm_description = jsonencode({
    schema_version  = 1, owner = "platform", severity = local.alarm_severity, action = local.alarm_action,
    scope           = "cell", signal = each.value.signal, environment = var.environment, cell_id = var.cell_id,
    summary         = each.value.summary, runbook_url = each.key == "client_loss" ? "${var.runbook_base_url}/browser-health" : "${var.runbook_base_url}/collector-delivery",
    recovery_signal = each.value.recovery,
    dashboard_url   = "${var.dashboard_base_url}/d/accountable-${var.environment}-collector-capacity",
    identity_url    = "${var.dashboard_base_url}/d/accountable-${var.environment}-service-identity",
  })
}

resource "aws_cloudwatch_metric_alarm" "route53" {
  provider = aws.global

  alarm_name          = "${local.name}-route53-public-api"
  namespace           = "AWS/Route53"
  metric_name         = "HealthCheckStatus"
  statistic           = "Minimum"
  period              = 60
  evaluation_periods  = 2
  threshold           = 1
  comparison_operator = "LessThanThreshold"
  treat_missing_data  = "breaching"
  dimensions          = { HealthCheckId = aws_route53_health_check.public_api.id }
  alarm_actions       = [var.alarm_input_topic_arn]
  ok_actions          = [var.alarm_input_topic_arn]

  alarm_description = jsonencode({
    schema_version  = 1, owner = "platform", severity = local.alarm_severity, action = local.alarm_action,
    scope           = "cell", signal = "route53.public_health", environment = var.environment, cell_id = var.cell_id,
    summary         = "The public API health check is failing", runbook_url = "${var.runbook_base_url}/service-dependencies",
    recovery_signal = "the public health check succeeds for 2 minutes",
    dashboard_url   = "${var.dashboard_base_url}/d/accountable-${var.environment}-service-identity",
    identity_url    = "${var.dashboard_base_url}/d/accountable-${var.environment}-service-identity",
  })
}

resource "aws_cloudwatch_metric_alarm" "cloudfront" {
  provider = aws.global

  alarm_name          = "${local.name}-cloudfront-server-errors"
  namespace           = "AWS/CloudFront"
  metric_name         = "5xxErrorRate"
  statistic           = "Average"
  period              = 300
  evaluation_periods  = 1
  threshold           = 5
  comparison_operator = "GreaterThanOrEqualToThreshold"
  treat_missing_data  = "notBreaching"
  dimensions          = { DistributionId = aws_cloudfront_distribution.cell.id, Region = "Global" }
  alarm_actions       = [var.alarm_input_topic_arn]
  ok_actions          = [var.alarm_input_topic_arn]

  alarm_description = jsonencode({
    schema_version  = 1, owner = "platform", severity = local.alarm_severity, action = local.alarm_action,
    scope           = "cell", signal = "cloudfront.server_errors", environment = var.environment, cell_id = var.cell_id,
    summary         = "CloudFront server errors exceed 5 percent", runbook_url = "${var.runbook_base_url}/service-dependencies",
    recovery_signal = "CloudFront 5xx error rate remains below 5 percent for 5 minutes",
    dashboard_url   = "${var.dashboard_base_url}/d/accountable-${var.environment}-server-dependencies",
    identity_url    = "${var.dashboard_base_url}/d/accountable-${var.environment}-service-identity",
  })
}
