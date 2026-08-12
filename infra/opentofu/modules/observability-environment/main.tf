locals {
  name = "accountable-${var.environment}-observability"

  # SNS topic ARNs are fully determined by account, region, and name, so the
  # Lambda deployment documents carry them without waiting for an apply.
  topic_arn_prefix = "arn:aws:sns:${var.region}:${var.account_id}:${local.name}"

  # Every safe stdout record that CloudWatch turns into an independent metric.
  # The pattern matches the fixed event name only, never free text.
  stdout_metric_filters = {
    sli_export_failed = {
      event      = "telemetry.sli_export_failed"
      namespace  = "Accountable/Collector"
      metric     = "SLIDeliveryUncertain"
      dimensions = { Environment = var.environment, CellId = "$.accountable_cell_id", Source = "api_exporter" }
    }
    sli_delivery_uncertain = {
      event      = "telemetry.sli_delivery_uncertain"
      namespace  = "Accountable/Collector"
      metric     = "SLIDeliveryUncertain"
      dimensions = { Environment = var.environment, CellId = "$.accountable_cell_id", Source = "collector_exporter" }
    }
    accounting_uncertain = {
      event      = "telemetry.accounting_uncertain"
      namespace  = "Accountable/Collector"
      metric     = "CollectorAccountingUncertain"
      dimensions = { Environment = var.environment, CellId = "$.accountable_cell_id" }
    }
    client_event_dropped = {
      event      = "telemetry.client_event_dropped"
      namespace  = "Accountable/ClientTelemetry"
      metric     = "ClientEventDropped"
      value      = "$.drop_count"
      dimensions = { Environment = var.environment, CellId = "$.accountable_cell_id", Reason = "$.reason" }
    }
  }

  task_stop_sources = {
    api       = { service = "api", metric = "SLIDeliveryUncertain", source = "api_task" }
    collector = { service = "collector", metric = "SLIDeliveryUncertain", source = "collector_task" }
  }

  # One rule for each cell and service, so the safe event can carry the
  # registered cell ID that every other metric joins on.
  task_stop_rules = {
    for pair in setproduct(keys(var.cells), keys(local.task_stop_sources)) :
    "${pair[0]}-${pair[1]}" => {
      cell_id     = pair[0]
      cluster_arn = var.cells[pair[0]]
      service     = local.task_stop_sources[pair[1]].service
      metric      = local.task_stop_sources[pair[1]].metric
      source      = local.task_stop_sources[pair[1]].source
    }
  }
}

resource "aws_sns_topic" "alarm_input" {
  name              = "${local.name}-alarm-input"
  kms_master_key_id = var.kms_key_arn
}

resource "aws_sns_topic" "output" {
  for_each = toset(["critical", "warning", "proof"])

  name              = "${local.name}-${each.key}"
  kms_master_key_id = var.kms_key_arn
}

# The translator-failure route must never depend on the translator, so it has its
# own topic and its own subscriptions.
resource "aws_sns_topic" "translator_failure" {
  name              = "${local.name}-translator-failure"
  kms_master_key_id = var.kms_key_arn
}

data "aws_iam_policy_document" "cloudwatch_topic" {
  for_each = {
    alarm_input        = aws_sns_topic.alarm_input.arn
    translator_failure = aws_sns_topic.translator_failure.arn
  }

  statement {
    actions   = ["sns:Publish"]
    resources = [each.value]

    principals {
      type        = "Service"
      identifiers = ["cloudwatch.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [var.account_id]
    }

    condition {
      test     = "ArnLike"
      variable = "aws:SourceArn"
      values   = ["arn:aws:cloudwatch:*:${var.account_id}:alarm:*"]
    }
  }
}

resource "aws_sns_topic_policy" "cloudwatch" {
  for_each = data.aws_iam_policy_document.cloudwatch_topic

  arn    = each.key == "alarm_input" ? aws_sns_topic.alarm_input.arn : aws_sns_topic.translator_failure.arn
  policy = each.value.json
}

resource "aws_sns_topic_subscription" "email" {
  for_each = {
    for pair in setproduct(["critical", "warning", "proof"], var.alert_email_addresses) :
    "${pair[0]}-${pair[1]}" => { topic = pair[0], address = pair[1] }
  }

  endpoint  = each.value.address
  protocol  = "email"
  topic_arn = aws_sns_topic.output[each.value.topic].arn
}

resource "aws_sns_topic_subscription" "translator_failure_email" {
  for_each = toset(var.alert_email_addresses)

  endpoint  = each.value
  protocol  = "email"
  topic_arn = aws_sns_topic.translator_failure.arn
}

data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "translator" {
  name               = "${local.name}-translator"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

data "aws_iam_policy_document" "translator" {
  statement {
    actions   = ["sns:Publish"]
    resources = [for topic in aws_sns_topic.output : topic.arn]
  }

  statement {
    actions   = ["kms:Decrypt", "kms:GenerateDataKey"]
    resources = [var.kms_key_arn]
  }

  statement {
    actions = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = [
      "${aws_cloudwatch_log_group.translator.arn}:*",
    ]
  }
}

resource "aws_iam_role_policy" "translator" {
  name   = "${local.name}-translator"
  policy = data.aws_iam_policy_document.translator.json
  role   = aws_iam_role.translator.id
}

resource "aws_cloudwatch_log_group" "translator" {
  name              = "/aws/lambda/${local.name}-translator"
  retention_in_days = 30
  kms_key_id        = var.kms_key_arn
}

resource "aws_lambda_function" "translator" {
  function_name                  = "${local.name}-translator"
  role                           = aws_iam_role.translator.arn
  architectures                  = ["arm64"]
  runtime                        = "provided.al2023"
  handler                        = "bootstrap"
  filename                       = var.translator_package_path
  source_code_hash               = filebase64sha256(var.translator_package_path)
  memory_size                    = 256
  timeout                        = 20
  reserved_concurrent_executions = 5

  logging_config {
    log_format = "JSON"
    log_group  = aws_cloudwatch_log_group.translator.name
  }

  depends_on = [aws_iam_role_policy.translator]
}

resource "aws_lambda_permission" "translator" {
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.translator.function_name
  principal     = "sns.amazonaws.com"
  source_arn    = aws_sns_topic.alarm_input.arn
}

resource "aws_sns_topic_subscription" "translator" {
  endpoint  = aws_lambda_function.translator.arn
  protocol  = "lambda"
  topic_arn = aws_sns_topic.alarm_input.arn
}

resource "aws_cloudwatch_metric_alarm" "translator_failure" {
  # SNS invokes Lambda asynchronously. IteratorAge belongs to stream event
  # sources and is never emitted here, so backlog and loss use the asynchronous
  # invocation metrics instead.
  for_each = {
    errors           = { metric = "Errors", statistic = "Sum", threshold = 1 }
    throttles        = { metric = "Throttles", statistic = "Sum", threshold = 1 }
    async_event_age  = { metric = "AsyncEventAge", statistic = "Maximum", threshold = 60000 }
    async_dropped    = { metric = "AsyncEventsDropped", statistic = "Sum", threshold = 1 }
    delivery_failure = { metric = "DestinationDeliveryFailures", statistic = "Sum", threshold = 1 }
  }

  alarm_name          = "${local.name}-translator-${each.key}"
  namespace           = "AWS/Lambda"
  metric_name         = each.value.metric
  statistic           = each.value.statistic
  period              = 60
  evaluation_periods  = 1
  threshold           = each.value.threshold
  comparison_operator = "GreaterThanOrEqualToThreshold"
  treat_missing_data  = "notBreaching"
  dimensions          = { FunctionName = aws_lambda_function.translator.function_name }
  alarm_actions       = [aws_sns_topic.translator_failure.arn]
  ok_actions          = [aws_sns_topic.translator_failure.arn]

  alarm_description = jsonencode({
    schema_version  = 1
    owner           = "platform"
    severity        = "critical"
    action          = "page"
    scope           = "environment"
    signal          = "notification.translator"
    environment     = var.environment
    summary         = "The alert translator cannot deliver alerts"
    runbook_url     = "${var.runbook_base_url}/alert-delivery"
    recovery_signal = "translator errors return to zero for 5 minutes"
    dashboard_url   = "${var.dashboard_base_url}/d/accountable-${var.environment}-alerts-deployments"
    identity_url    = "${var.dashboard_base_url}/d/accountable-${var.environment}-service-identity"
  })
}

resource "aws_cloudwatch_log_group" "task_stops" {
  name              = "/accountable/${var.environment}/task-stops"
  retention_in_days = 30
  kms_key_id        = var.kms_key_arn
}

resource "aws_sqs_queue" "task_stop_dlq" {
  name                      = "${local.name}-task-stop-dlq"
  message_retention_seconds = 1209600
  sqs_managed_sse_enabled   = true
}

data "aws_iam_policy_document" "task_stop_dlq" {
  statement {
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.task_stop_dlq.arn]

    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com"]
    }

    condition {
      test     = "ArnLike"
      variable = "aws:SourceArn"
      values   = [for rule in aws_cloudwatch_event_rule.task_stop : rule.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "task_stop_dlq" {
  queue_url = aws_sqs_queue.task_stop_dlq.id
  policy    = data.aws_iam_policy_document.task_stop_dlq.json
}

resource "aws_cloudwatch_event_rule" "task_stop" {
  for_each = local.task_stop_rules

  name        = "${local.name}-${each.key}-task-stop"
  description = "Unexpected ${each.value.service} task stop in ${each.value.cell_id}"

  # Only bounded stop codes, initiator classes, and exit status reach this rule.
  # A service-scheduler or user stop with status zero is a normal stop.
  event_pattern = jsonencode({
    source      = ["aws.ecs"]
    detail-type = ["ECS Task State Change"]
    detail = {
      lastStatus = ["STOPPED"]
      clusterArn = [each.value.cluster_arn]
      group      = [{ prefix = "service:accountable-" }]
      "$or" = [
        {
          stopCode = ["TaskFailedToStart", "EssentialContainerExited", "SpotInterruption", "TerminationNotice"]
        },
        {
          stopCode      = ["UserInitiated"]
          stoppedReason = ["accountable managed unexpected-stop proof"]
        },
      ]
      containers = {
        name = [each.value.service]
      }
    }
  })
}

resource "aws_cloudwatch_event_target" "task_stop" {
  for_each = local.task_stop_rules

  rule = aws_cloudwatch_event_rule.task_stop[each.key].name
  arn  = aws_cloudwatch_log_group.task_stops.arn

  retry_policy {
    maximum_event_age_in_seconds = 86400
    maximum_retry_attempts       = 185
  }

  dead_letter_config {
    arn = aws_sqs_queue.task_stop_dlq.arn
  }

  # The safe event carries only bounded classification values, never the raw
  # ECS stop reason.
  input_transformer {
    input_paths = {
      group    = "$.detail.group"
      stopCode = "$.detail.stopCode"
    }
    input_template = jsonencode({
      event_name                  = "telemetry.task_uncertain"
      deployment_environment_name = var.environment
      accountable_cell_id         = each.value.cell_id
      source                      = each.value.source
      metric                      = each.value.metric
      group                       = "<group>"
      stop_code                   = "<stopCode>"
    })
  }

  depends_on = [aws_sqs_queue_policy.task_stop_dlq]
}

resource "aws_cloudwatch_metric_alarm" "task_stop_delivery" {
  for_each = {
    for pair in setproduct(keys(local.task_stop_rules), ["failed", "dlq"]) :
    "${pair[0]}-${pair[1]}" => {
      rule   = pair[0]
      metric = pair[1] == "failed" ? "FailedInvocations" : "InvocationsSentToDLQ"
    }
  }

  alarm_name          = "${local.name}-${each.key}"
  namespace           = "AWS/Events"
  metric_name         = each.value.metric
  statistic           = "Sum"
  period              = 60
  evaluation_periods  = 1
  threshold           = 1
  comparison_operator = "GreaterThanOrEqualToThreshold"
  treat_missing_data  = "notBreaching"
  dimensions          = { RuleName = aws_cloudwatch_event_rule.task_stop[each.value.rule].name }
  alarm_actions       = [aws_sns_topic.translator_failure.arn]
  ok_actions          = [aws_sns_topic.translator_failure.arn]

  alarm_description = jsonencode({
    schema_version  = 1
    owner           = "platform"
    severity        = "critical"
    action          = "page"
    scope           = "environment"
    signal          = "telemetry.task_stop_detection"
    environment     = var.environment
    summary         = "Unexpected task-stop evidence could not reach its independent detector"
    runbook_url     = "${var.runbook_base_url}/collector-delivery"
    recovery_signal = "EventBridge task-stop delivery errors return to zero"
    dashboard_url   = "${var.dashboard_base_url}/d/accountable-${var.environment}-alerts-deployments"
    identity_url    = "${var.dashboard_base_url}/d/accountable-${var.environment}-service-identity"
  })
}

resource "aws_cloudwatch_log_metric_filter" "task_stop" {
  for_each = local.task_stop_sources

  name           = "${local.name}-${each.key}-task-stop"
  log_group_name = aws_cloudwatch_log_group.task_stops.name
  pattern        = "{ $.event_name = \"telemetry.task_uncertain\" && $.source = \"${each.value.source}\" }"

  metric_transformation {
    name      = each.value.metric
    namespace = "Accountable/Collector"
    value     = "1"
    unit      = "None"

    dimensions = {
      Environment = "$.deployment_environment_name"
      CellId      = "$.accountable_cell_id"
      Source      = each.value.source
    }
  }
}

# An unexpected API task stop also makes client event delivery unknown.
resource "aws_cloudwatch_log_metric_filter" "client_event_uncertain" {
  name           = "${local.name}-client-event-uncertain"
  log_group_name = aws_cloudwatch_log_group.task_stops.name
  pattern        = "{ $.event_name = \"telemetry.task_uncertain\" && $.source = \"api_task\" }"

  metric_transformation {
    name      = "ClientEventDeliveryUncertain"
    namespace = "Accountable/ClientTelemetry"
    value     = "1"
    unit      = "None"

    dimensions = {
      Environment = "$.deployment_environment_name"
      CellId      = "$.accountable_cell_id"
    }
  }
}

resource "aws_cloudwatch_log_metric_filter" "collector_delivery_uncertain" {
  name           = "${local.name}-collector-delivery-uncertain"
  log_group_name = aws_cloudwatch_log_group.task_stops.name
  pattern        = "{ $.event_name = \"telemetry.task_uncertain\" && $.source = \"collector_task\" }"

  metric_transformation {
    name      = "CollectorDeliveryUncertain"
    namespace = "Accountable/Collector"
    value     = "1"
    unit      = "None"

    dimensions = {
      Environment = "$.deployment_environment_name"
      CellId      = "$.accountable_cell_id"
    }
  }
}

resource "aws_cloudwatch_log_metric_filter" "stdout" {
  for_each = local.stdout_metric_filters

  name           = "${local.name}-${each.key}"
  log_group_name = var.runtime_log_group_name
  pattern        = "{ $.event_name = \"${each.value.event}\" }"

  metric_transformation {
    name       = each.value.metric
    namespace  = each.value.namespace
    value      = try(each.value.value, "1")
    unit       = "None"
    dimensions = each.value.dimensions
  }
}

data "aws_iam_policy_document" "events_to_logs" {
  statement {
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${aws_cloudwatch_log_group.task_stops.arn}:*"]

    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com", "delivery.logs.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [var.account_id]
    }
  }
}

resource "aws_cloudwatch_log_resource_policy" "task_stops" {
  policy_name     = "${local.name}-task-stops"
  policy_document = data.aws_iam_policy_document.events_to_logs.json
}

resource "aws_iam_role" "observability" {
  name               = "${local.name}-canary"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

data "aws_iam_policy_document" "observability" {
  statement {
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${aws_cloudwatch_log_group.canary.arn}:*"]
  }

  statement {
    actions   = ["ssm:GetParameters"]
    resources = ["arn:aws:ssm:${var.region}:${var.account_id}:parameter${var.grafana_token_parameter}"]
  }

  statement {
    actions   = ["kms:Decrypt"]
    resources = [var.kms_key_arn]

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["ssm.${var.region}.amazonaws.com"]
    }
  }
}

resource "aws_iam_role_policy" "observability" {
  name   = "${local.name}-canary"
  policy = data.aws_iam_policy_document.observability.json
  role   = aws_iam_role.observability.id
}

resource "aws_cloudwatch_log_group" "canary" {
  name              = "/aws/lambda/${local.name}-canary"
  retention_in_days = 30
  kms_key_id        = var.kms_key_arn
}

resource "aws_lambda_function" "canary" {
  function_name                  = "${local.name}-canary"
  role                           = aws_iam_role.observability.arn
  architectures                  = ["arm64"]
  runtime                        = "provided.al2023"
  handler                        = "bootstrap"
  filename                       = var.canary_package_path
  source_code_hash               = filebase64sha256(var.canary_package_path)
  memory_size                    = 256
  timeout                        = 60
  reserved_concurrent_executions = 2

  logging_config {
    log_format = "JSON"
    log_group  = aws_cloudwatch_log_group.canary.name
  }

  depends_on = [aws_iam_role_policy.observability]
}

resource "aws_cloudwatch_event_rule" "canary" {
  name                = "${local.name}-canary"
  description         = "Grafana health canary"
  schedule_expression = "rate(5 minutes)"
}

resource "aws_sqs_queue" "canary_dlq" {
  name                      = "${local.name}-canary-dlq"
  message_retention_seconds = 1209600
  sqs_managed_sse_enabled   = true
}

data "aws_iam_policy_document" "canary_dlq" {
  statement {
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.canary_dlq.arn]

    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com"]
    }

    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_cloudwatch_event_rule.canary.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "canary_dlq" {
  queue_url = aws_sqs_queue.canary_dlq.id
  policy    = data.aws_iam_policy_document.canary_dlq.json
}

resource "aws_cloudwatch_event_target" "canary" {
  rule = aws_cloudwatch_event_rule.canary.name
  arn  = aws_lambda_function.canary.arn

  dead_letter_config { arn = aws_sqs_queue.canary_dlq.arn }
  retry_policy {
    maximum_event_age_in_seconds = 3600
    maximum_retry_attempts       = 12
  }
}

resource "aws_lambda_permission" "canary" {
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.canary.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.canary.arn
}

resource "aws_cloudwatch_metric_alarm" "canary" {
  alarm_name          = "${local.name}-canary"
  namespace           = "Accountable/Observability"
  metric_name         = "CanarySuccess"
  statistic           = "Minimum"
  period              = 300
  evaluation_periods  = 2
  threshold           = 1
  comparison_operator = "LessThanThreshold"
  treat_missing_data  = "breaching"

  dimensions = {
    StackName   = "acctbl"
    Environment = var.environment
  }

  alarm_actions = [aws_sns_topic.alarm_input.arn]
  ok_actions    = [aws_sns_topic.alarm_input.arn]

  alarm_description = jsonencode({
    schema_version  = 1
    owner           = "platform"
    severity        = "critical"
    action          = var.environment == "production" ? "page" : "warning"
    scope           = "environment"
    signal          = "grafana.canary"
    environment     = var.environment
    summary         = "Grafana is unreachable from outside Grafana"
    runbook_url     = "${var.runbook_base_url}/external-canary"
    recovery_signal = "two consecutive successful canary runs"
    dashboard_url   = "${var.dashboard_base_url}/d/accountable-${var.environment}-alerts-deployments"
    identity_url    = "${var.dashboard_base_url}/d/accountable-${var.environment}-service-identity"
  })
}

resource "aws_iam_role" "capacity" {
  count = var.enable_capacity_producer ? 1 : 0

  name               = "accountable-grafana-capacity"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

data "aws_iam_policy_document" "capacity" {
  count = var.enable_capacity_producer ? 1 : 0

  statement {
    actions = [
      "cloudwatch:GetMetricData",
      "cloudwatch:PutMetricData",
    ]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "aws:RequestedRegion"
      values   = [var.region]
    }
  }

  statement {
    actions = ["ssm:GetParameters"]
    resources = [
      "arn:aws:ssm:${var.region}:${var.account_id}:parameter${var.grafana_capacity_cloud_token_parameter}",
      "arn:aws:ssm:${var.region}:${var.account_id}:parameter${var.grafana_capacity_stack_token_parameter}",
    ]
  }

  statement {
    actions   = ["kms:Decrypt"]
    resources = [var.kms_key_arn]

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["ssm.${var.region}.amazonaws.com"]
    }
  }

  statement {
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${aws_cloudwatch_log_group.capacity[0].arn}:*"]
  }
}

resource "aws_iam_role_policy" "capacity" {
  count = var.enable_capacity_producer ? 1 : 0

  name   = "accountable-grafana-capacity"
  policy = data.aws_iam_policy_document.capacity[0].json
  role   = aws_iam_role.capacity[0].id
}

resource "aws_cloudwatch_log_group" "capacity" {
  count = var.enable_capacity_producer ? 1 : 0

  name              = "/aws/lambda/accountable-grafana-capacity"
  retention_in_days = 30
  kms_key_id        = var.kms_key_arn
}

resource "aws_lambda_function" "capacity" {
  count = var.enable_capacity_producer ? 1 : 0

  function_name                  = "accountable-grafana-capacity"
  role                           = aws_iam_role.capacity[0].arn
  architectures                  = ["arm64"]
  runtime                        = "provided.al2023"
  handler                        = "bootstrap"
  filename                       = var.capacity_package_path
  source_code_hash               = filebase64sha256(var.capacity_package_path)
  memory_size                    = 256
  timeout                        = 60
  reserved_concurrent_executions = 1

  logging_config {
    log_format = "JSON"
    log_group  = aws_cloudwatch_log_group.capacity[0].name
  }

  depends_on = [aws_iam_role_policy.capacity]
}

resource "aws_cloudwatch_event_rule" "capacity" {
  count = var.enable_capacity_producer ? 1 : 0

  name                = "accountable-grafana-capacity"
  description         = "Grafana shared-stack capacity producer"
  schedule_expression = "rate(15 minutes)"
}

resource "aws_sqs_queue" "capacity_dlq" {
  count = var.enable_capacity_producer ? 1 : 0

  name                      = "accountable-grafana-capacity-dlq"
  message_retention_seconds = 1209600
  sqs_managed_sse_enabled   = true
}

data "aws_iam_policy_document" "capacity_dlq" {
  count = var.enable_capacity_producer ? 1 : 0

  statement {
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.capacity_dlq[0].arn]

    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com"]
    }

    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_cloudwatch_event_rule.capacity[0].arn]
    }
  }
}

resource "aws_sqs_queue_policy" "capacity_dlq" {
  count = var.enable_capacity_producer ? 1 : 0

  queue_url = aws_sqs_queue.capacity_dlq[0].id
  policy    = data.aws_iam_policy_document.capacity_dlq[0].json
}

resource "aws_cloudwatch_event_target" "capacity" {
  count = var.enable_capacity_producer ? 1 : 0

  rule = aws_cloudwatch_event_rule.capacity[0].name
  arn  = aws_lambda_function.capacity[0].arn

  dead_letter_config { arn = aws_sqs_queue.capacity_dlq[0].arn }
  retry_policy {
    maximum_event_age_in_seconds = 3600
    maximum_retry_attempts       = 12
  }
}

resource "aws_lambda_permission" "capacity" {
  count = var.enable_capacity_producer ? 1 : 0

  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.capacity[0].function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.capacity[0].arn
}

resource "aws_cloudwatch_metric_alarm" "capacity_health" {
  for_each = var.enable_capacity_producer ? {
    read = {
      metric              = "CapacityReadSuccess"
      threshold           = 1
      comparison_operator = "LessThanThreshold"
      summary             = "Grafana capacity cannot be read"
      recovery            = "two successful capacity reads"
    }
    tier = {
      metric              = "TierPolicyCompliant"
      threshold           = 1
      comparison_operator = "LessThanThreshold"
      summary             = "The Grafana tier is not approved"
      recovery            = "two compliant capacity samples"
    }
    fresh = {
      metric              = "CapacitySampleFresh"
      threshold           = 1
      comparison_operator = "LessThanThreshold"
      summary             = "The shared Grafana capacity sample is stale or absent"
      recovery            = "two fresh capacity samples"
    }
  } : {}

  alarm_name          = "accountable-grafana-capacity-${each.key}"
  namespace           = "Accountable/GrafanaCapacity"
  metric_name         = each.value.metric
  statistic           = "Minimum"
  period              = 900
  evaluation_periods  = 2
  threshold           = each.value.threshold
  comparison_operator = each.value.comparison_operator
  treat_missing_data  = "breaching"
  dimensions          = { StackName = "acctbl" }
  alarm_actions       = [aws_sns_topic.alarm_input.arn]
  ok_actions          = [aws_sns_topic.alarm_input.arn]

  alarm_description = jsonencode({
    schema_version  = 1
    owner           = "platform"
    severity        = "critical"
    action          = "page"
    scope           = "stack"
    signal          = "grafana.capacity.${each.key}"
    stack_name      = "acctbl"
    summary         = each.value.summary
    runbook_url     = "${var.runbook_base_url}/collector-capacity"
    recovery_signal = each.value.recovery
    dashboard_url   = "${var.dashboard_base_url}/d/accountable-development-collector-capacity"
    identity_url    = "${var.dashboard_base_url}/d/accountable-development-service-identity"
  })
}

resource "aws_cloudwatch_metric_alarm" "capacity_threshold" {
  for_each = var.enable_capacity_producer ? {
    for pair in setproduct(
      ["metric_series", "log_ingestion", "trace_ingestion", "grafana_users", "irm_users", "alert_rules", "alert_instances"],
      ["warning", "critical"],
    ) : "${pair[0]}-${pair[1]}" => { limit = pair[0], severity = pair[1] }
  } : {}

  alarm_name          = "accountable-grafana-capacity-${each.key}"
  namespace           = "Accountable/GrafanaCapacity"
  metric_name         = "ForecastUsagePercent"
  statistic           = "Maximum"
  period              = 900
  evaluation_periods  = 2
  threshold           = each.value.severity == "critical" ? 85 : 70
  comparison_operator = "GreaterThanOrEqualToThreshold"
  treat_missing_data  = "breaching"
  dimensions          = { StackName = "acctbl", Limit = each.value.limit }
  alarm_actions       = [aws_sns_topic.alarm_input.arn]
  ok_actions          = [aws_sns_topic.alarm_input.arn]

  alarm_description = jsonencode({
    schema_version  = 1
    owner           = "platform"
    severity        = each.value.severity
    action          = each.value.severity == "critical" ? "page" : "warning"
    scope           = "stack"
    signal          = "grafana.capacity.${each.value.limit}"
    stack_name      = "acctbl"
    summary         = "Grafana ${each.value.limit} capacity is above ${each.value.severity == "critical" ? 85 : 70} percent"
    runbook_url     = "${var.runbook_base_url}/collector-capacity"
    recovery_signal = "forecast capacity remains below the threshold for 30 minutes"
    dashboard_url   = "${var.dashboard_base_url}/d/accountable-development-collector-capacity"
    identity_url    = "${var.dashboard_base_url}/d/accountable-development-service-identity"
  })
}

locals {
  scheduled_rules = merge(
    { canary = aws_cloudwatch_event_rule.canary.name },
    var.enable_capacity_producer ? { capacity = aws_cloudwatch_event_rule.capacity[0].name } : {},
  )
}

resource "aws_cloudwatch_metric_alarm" "scheduled_delivery" {
  for_each = {
    for pair in setproduct(keys(local.scheduled_rules), ["FailedInvocations", "InvocationsSentToDLQ"]) :
    "${pair[0]}-${pair[1]}" => { rule = local.scheduled_rules[pair[0]], metric = pair[1] }
  }

  alarm_name          = "${local.name}-schedule-${each.key}"
  namespace           = "AWS/Events"
  metric_name         = each.value.metric
  statistic           = "Sum"
  period              = 300
  evaluation_periods  = 1
  threshold           = 1
  comparison_operator = "GreaterThanOrEqualToThreshold"
  treat_missing_data  = "notBreaching"
  dimensions          = { RuleName = each.value.rule }
  alarm_actions       = [aws_sns_topic.translator_failure.arn]
  ok_actions          = [aws_sns_topic.translator_failure.arn]

  alarm_description = jsonencode({
    schema_version  = 1, owner = "platform", severity = "critical",
    action          = var.environment == "production" ? "page" : "warning", scope = "environment",
    signal          = "eventbridge.schedule_delivery", environment = var.environment,
    summary         = "A scheduled observability invocation was not delivered",
    runbook_url     = "${var.runbook_base_url}/alert-delivery",
    recovery_signal = "scheduled delivery errors remain zero for 15 minutes",
    dashboard_url   = "${var.dashboard_base_url}/d/accountable-${var.environment}-alerts-deployments", identity_url = "${var.dashboard_base_url}/d/accountable-${var.environment}-service-identity",
  })
}

locals {
  emf_log_groups = merge(
    {
      runtime = var.runtime_log_group_name
      canary  = aws_cloudwatch_log_group.canary.name
    },
    var.enable_capacity_producer ? { capacity = aws_cloudwatch_log_group.capacity[0].name } : {},
  )
}

resource "aws_cloudwatch_metric_alarm" "emf_health" {
  for_each = {
    for pair in setproduct(keys(local.emf_log_groups), ["EMFParsingErrors", "EMFValidationErrors", "EMFDisabledErrors"]) :
    "${pair[0]}-${pair[1]}" => { log_group = local.emf_log_groups[pair[0]], metric = pair[1] }
  }

  alarm_name          = "${local.name}-emf-${each.key}"
  namespace           = "AWS/Logs"
  metric_name         = each.value.metric
  statistic           = "Sum"
  period              = 300
  evaluation_periods  = 1
  threshold           = 1
  comparison_operator = "GreaterThanOrEqualToThreshold"
  treat_missing_data  = "notBreaching"
  dimensions          = { LogGroupName = each.value.log_group }
  alarm_actions       = [aws_sns_topic.translator_failure.arn]
  ok_actions          = [aws_sns_topic.translator_failure.arn]

  alarm_description = jsonencode({
    schema_version  = 1
    owner           = "platform"
    severity        = "critical"
    action          = "page"
    scope           = "environment"
    signal          = "cloudwatch.emf"
    environment     = var.environment
    summary         = "CloudWatch rejected embedded metrics"
    runbook_url     = "${var.runbook_base_url}/collector-delivery"
    recovery_signal = "CloudWatch EMF errors return to zero"
    dashboard_url   = "${var.dashboard_base_url}/d/accountable-${var.environment}-collector-capacity"
    identity_url    = "${var.dashboard_base_url}/d/accountable-${var.environment}-service-identity"
  })
}

# Grafana Cloud reads CloudWatch by assuming this role. The trust policy names
# the Grafana account and requires the external ID Grafana issues, so no other
# principal can assume it.
data "aws_iam_policy_document" "grafana_cloudwatch_assume" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "AWS"
      identifiers = ["arn:aws:iam::${var.grafana_cloud_account_id}:root"]
    }

    condition {
      test     = "StringEquals"
      variable = "sts:ExternalId"
      values   = [var.grafana_cloud_external_id]
    }
  }
}

# Metrics and metric metadata only. Grafana can never read log content, which
# keeps telemetry out of a second copy.
data "aws_iam_policy_document" "grafana_cloudwatch_read" {
  statement {
    sid = "ReadAccountableMetrics"
    actions = [
      "cloudwatch:DescribeAlarmsForMetric",
      "cloudwatch:DescribeAlarmHistory",
      "cloudwatch:DescribeAlarms",
      "cloudwatch:ListMetrics",
      "cloudwatch:GetMetricData",
      "cloudwatch:GetMetricStatistics",
    ]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "aws:RequestedRegion"
      values   = [var.region]
    }
  }

  statement {
    sid       = "ResolveMetricDimensions"
    actions   = ["tag:GetResources"]
    resources = ["*"]
  }

  statement {
    sid     = "PublishAccountableAlerts"
    actions = ["sns:Publish"]
    resources = [
      aws_sns_topic.output["critical"].arn,
      aws_sns_topic.output["proof"].arn,
      aws_sns_topic.output["warning"].arn,
    ]
  }
}

resource "aws_iam_role" "grafana_cloudwatch" {
  name                 = "accountable-${var.environment}-grafana-cloudwatch"
  assume_role_policy   = data.aws_iam_policy_document.grafana_cloudwatch_assume.json
  max_session_duration = 3600
}

resource "aws_iam_role_policy" "grafana_cloudwatch" {
  name   = "accountable-${var.environment}-grafana-cloudwatch"
  policy = data.aws_iam_policy_document.grafana_cloudwatch_read.json
  role   = aws_iam_role.grafana_cloudwatch.id
}
