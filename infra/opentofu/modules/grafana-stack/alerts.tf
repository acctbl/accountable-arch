locals {
  burn_windows = {
    fast   = { long = "1h", short = "5m", long_seconds = 3600, short_seconds = 300, threshold = 14.4, severity = "critical" }
    high   = { long = "6h", short = "30m", long_seconds = 21600, short_seconds = 1800, threshold = 6, severity = "critical" }
    medium = { long = "1d", short = "2h", long_seconds = 86400, short_seconds = 7200, threshold = 3, severity = "warning" }
    slow   = { long = "3d", short = "6h", long_seconds = 259200, short_seconds = 21600, threshold = 1, severity = "warning" }
  }

  slo_rules = {
    for item in flatten([
      for environment in local.environments : [
        for product in [
          { name = "availability", bad = "accountable_sli_availability_bad_total", objective = 0.001 },
          { name = "latency", bad = "accountable_sli_latency_slow_total", objective = 0.01 },
          ] : [
          for window_name, window in local.burn_windows : {
            key         = "${environment}-${product.name}-${window_name}"
            environment = environment
            product     = product
            window_name = window_name
            window      = window
          }
        ]
      ]
    ]) : item.key => item
  }

  signal_rules = {
    browser-api-failure = {
      expression = "(sum(increase(accountable_client_event_count_total{deployment_environment_name=\"$environment\",event_name=\"api.request.completed\",outcome=\"failure\"}[15m])) / clamp_min(sum(increase(accountable_client_event_count_total{deployment_environment_name=\"$environment\",event_name=\"api.request.completed\"}[15m])), 1) > 0.05) and (sum(increase(accountable_client_event_count_total{deployment_environment_name=\"$environment\",event_name=\"api.request.completed\"}[15m])) >= 20)"
      severity   = "warning"
      duration   = "2m"
    }
    browser-runtime-errors = {
      expression = "(sum(increase(accountable_client_event_count_total{deployment_environment_name=\"$environment\",event_name=\"app.error\"}[15m])) / clamp_min(10 * sum(increase(accountable_client_event_count_total{deployment_environment_name=\"$environment\",event_name=\"app.ready\"}[15m])), 1) > 0.05) and (10 * sum(increase(accountable_client_event_count_total{deployment_environment_name=\"$environment\",event_name=\"app.ready\"}[15m])) >= 100)"
      severity   = "warning"
      duration   = "2m"
    }
    web-vital-lcp = {
      expression = "(histogram_quantile(0.75, sum by (le) (rate(accountable_browser_web_vital_duration_us_bucket{deployment_environment_name=\"$environment\",event_name=\"web_vital.lcp\"}[15m]))) > 2500000) and (sum(increase(accountable_browser_web_vital_duration_us_count{deployment_environment_name=\"$environment\",event_name=\"web_vital.lcp\"}[15m])) >= 100)"
      severity   = "warning"
      duration   = "2m"
    }
    web-vital-inp = {
      expression = "(histogram_quantile(0.75, sum by (le) (rate(accountable_browser_web_vital_duration_us_bucket{deployment_environment_name=\"$environment\",event_name=\"web_vital.inp\"}[15m]))) > 200000) and (sum(increase(accountable_browser_web_vital_duration_us_count{deployment_environment_name=\"$environment\",event_name=\"web_vital.inp\"}[15m])) >= 100)"
      severity   = "warning"
      duration   = "2m"
    }
    web-vital-cls = {
      expression = "(histogram_quantile(0.75, sum by (le) (rate(accountable_browser_web_vital_cls_ppm_bucket{deployment_environment_name=\"$environment\"}[15m]))) > 100000) and (sum(increase(accountable_browser_web_vital_cls_ppm_count{deployment_environment_name=\"$environment\"}[15m])) >= 100)"
      severity   = "warning"
      duration   = "2m"
    }
    runtime-identity-cardinality = {
      expression = "count(count by (service_name, accountable_cell_id, service_version, accountable_release_id, accountable_configuration_fingerprint) (accountable_server_request_count_total{deployment_environment_name=\"$environment\"})) > 2"
      severity   = "critical"
      duration   = "5m"
    }
    rule-evaluation-failure = {
      expression = "sum(increase(grafana_alerting_rule_evaluations_total{status=\"error\"}[5m])) > 0"
      severity   = "critical"
      duration   = "1m"
    }
    authenticated-proof = {
      expression = "sum by (accountable_cell_id) (increase(accountable_observability_proof_total{deployment_environment_name=\"$environment\",proof_type=\"end_to_end_v1\"}[15m])) > 0"
      severity   = "proof"
      duration   = "0s"
    }
  }

  environment_signal_rules = {
    for item in flatten([
      for environment in local.environments : [
        for name, rule in local.signal_rules : {
          key         = "${environment}-${name}"
          environment = environment
          name        = name
          rule        = rule
        }
      ]
    ]) : item.key => item
  }

  capacity_rules = {
    for pair in setproduct(
      ["metric_series", "log_ingestion", "trace_ingestion", "grafana_users", "irm_users", "alert_rules", "alert_instances"],
      ["warning", "critical"],
      ) : "${pair[0]}-${pair[1]}" => {
      limit     = pair[0]
      severity  = pair[1]
      threshold = pair[1] == "critical" ? 85 : 70
    }
  }
}

resource "grafana_rule_group" "capacity" {
  for_each = local.capacity_rules

  name             = "accountable-capacity-${each.key}"
  folder_uid       = grafana_folder.environment["development"].uid
  interval_seconds = 300

  rule {
    name           = "${replace(each.value.limit, "_", " ")} ${each.value.severity}"
    uid            = "acct-cap-${substr(sha256(each.key), 0, 16)}"
    condition      = "B"
    for            = "0s"
    no_data_state  = "Alerting"
    exec_err_state = "Alerting"
    is_paused      = false

    data {
      ref_id         = "A"
      datasource_uid = grafana_data_source.cloudwatch["development"].uid
      relative_time_range {
        from = 1800
        to   = 0
      }
      model = jsonencode({
        refId      = "A", datasource = { type = "cloudwatch", uid = grafana_data_source.cloudwatch["development"].uid },
        namespace  = "Accountable/GrafanaCapacity", metricName = "ForecastUsagePercent", statistic = "Maximum",
        dimensions = { StackName = ["acctbl"], Limit = [each.value.limit] }, region = var.region, period = "900"
      })
    }

    data {
      ref_id         = "B"
      datasource_uid = "__expr__"
      relative_time_range {
        from = 0
        to   = 0
      }
      model = jsonencode({ refId = "B", type = "threshold", expression = "A", conditions = [{ evaluator = { params = [each.value.threshold], type = "gt" }, operator = { type = "and" }, query = { params = ["A"] }, reducer = { params = [], type = "last" }, type = "query" }], datasource = { type = "__expr__", uid = "__expr__" } })
    }

    labels = {
      owner       = "platform"
      severity    = each.value.severity
      action      = each.value.severity == "critical" ? "page" : "warning"
      scope       = "stack"
      signal      = "grafana.capacity.${each.value.limit}"
      stack_name  = "acctbl"
      environment = "development"
    }
    annotations = {
      summary         = "Grafana ${replace(each.value.limit, "_", " ")} capacity is above ${each.value.threshold} percent"
      dashboard_url   = "${var.dashboard_base_url}/d/accountable-development-collector-capacity"
      runbook_url     = "${var.runbook_base_url}/collector-capacity"
      identity_url    = "${var.dashboard_base_url}/d/accountable-development-service-identity"
      recovery_signal = "capacity remains below threshold for 30 minutes"
    }
  }
}

resource "grafana_rule_group" "slo" {
  for_each = local.slo_rules

  name             = "accountable-${each.value.environment}-${each.value.product.name}-${each.value.window_name}"
  folder_uid       = grafana_folder.environment[each.value.environment].uid
  interval_seconds = 60

  rule {
    name           = "${each.value.product.name} burn ${each.value.window.long} and ${each.value.window.short}"
    uid            = "acct-${substr(each.value.environment, 0, 4)}-${substr(each.value.product.name, 0, 4)}-${each.value.window_name}"
    condition      = "C"
    for            = "0s"
    no_data_state  = "Alerting"
    exec_err_state = "Alerting"
    is_paused      = false

    data {
      ref_id         = "A"
      datasource_uid = "grafanacloud-acctbl-prom"
      relative_time_range {
        from = each.value.window.long_seconds
        to   = 0
      }
      model = jsonencode({
        refId = "A", datasource = { type = "prometheus", uid = "grafanacloud-acctbl-prom" }, editorMode = "code", instant = true,
        expr  = "sum(increase(${each.value.product.bad}{deployment_environment_name=\"${each.value.environment}\"}[${each.value.window.long}])) / clamp_min(sum(increase(accountable_sli_request_eligible_total{deployment_environment_name=\"${each.value.environment}\"}[${each.value.window.long}])), 1) / ${each.value.product.objective}"
      })
    }

    data {
      ref_id         = "B"
      datasource_uid = "grafanacloud-acctbl-prom"
      relative_time_range {
        from = each.value.window.short_seconds
        to   = 0
      }
      model = jsonencode({
        refId = "B", datasource = { type = "prometheus", uid = "grafanacloud-acctbl-prom" }, editorMode = "code", instant = true,
        expr  = "sum(increase(${each.value.product.bad}{deployment_environment_name=\"${each.value.environment}\"}[${each.value.window.short}])) / clamp_min(sum(increase(accountable_sli_request_eligible_total{deployment_environment_name=\"${each.value.environment}\"}[${each.value.window.short}])), 1) / ${each.value.product.objective}"
      })
    }

    data {
      ref_id         = "C"
      datasource_uid = "__expr__"
      relative_time_range {
        from = 0
        to   = 0
      }
      model = jsonencode({ refId = "C", type = "math", expression = "$A > ${each.value.window.threshold} && $B > ${each.value.window.threshold}", datasource = { type = "__expr__", uid = "__expr__" } })
    }

    labels = {
      owner       = "platform"
      severity    = each.value.window.severity
      action      = each.value.environment == "production" && each.value.window.severity == "critical" ? "page" : "warning"
      scope       = "environment"
      signal      = "slo.${each.value.product.name}.burn"
      environment = each.value.environment
    }
    annotations = {
      summary         = "${each.value.product.name} error budget is burning too quickly"
      dashboard_url   = "${var.dashboard_base_url}/d/accountable-${each.value.environment}-slo-budget"
      runbook_url     = "${var.runbook_base_url}/slo-budget"
      identity_url    = "${var.dashboard_base_url}/d/accountable-${each.value.environment}-service-identity"
      recovery_signal = "both burn windows remain below threshold for 5 minutes"
    }
  }
}

resource "grafana_rule_group" "signal" {
  for_each = local.environment_signal_rules

  name             = "accountable-${each.value.key}"
  folder_uid       = grafana_folder.environment[each.value.environment].uid
  interval_seconds = 60

  rule {
    name           = replace(each.value.name, "-", " ")
    uid            = "acct-${substr(each.value.environment, 0, 4)}-${substr(sha256(each.value.name), 0, 12)}"
    condition      = "A"
    for            = each.value.rule.duration
    no_data_state  = each.value.name == "authenticated-proof" ? "OK" : "Alerting"
    exec_err_state = "Alerting"
    is_paused      = false

    data {
      ref_id         = "A"
      datasource_uid = "grafanacloud-acctbl-prom"
      relative_time_range {
        from = 900
        to   = 0
      }
      model = jsonencode({
        refId = "A", datasource = { type = "prometheus", uid = "grafanacloud-acctbl-prom" }, editorMode = "code", instant = true,
        expr  = replace(each.value.rule.expression, "$environment", each.value.environment)
      })
    }

    labels = {
      owner       = "platform"
      severity    = each.value.rule.severity
      action      = each.value.rule.severity == "proof" ? "proof" : each.value.environment == "production" && each.value.rule.severity == "critical" ? "page" : "warning"
      scope       = each.value.name == "authenticated-proof" ? "cell" : "environment"
      signal      = replace(each.value.name, "-", ".")
      environment = each.value.environment
    }
    annotations = {
      summary       = replace(each.value.name, "-", " ")
      dashboard_url = "${var.dashboard_base_url}/d/accountable-${each.value.environment}-alerts-deployments"
      runbook_url = each.value.name == "rule-evaluation-failure" ? "${var.runbook_base_url}/alert-delivery" : (
        each.value.name == "authenticated-proof" || each.value.name == "runtime-identity-cardinality" ? "${var.runbook_base_url}/identity-proof" : "${var.runbook_base_url}/browser-health"
      )
      identity_url    = "${var.dashboard_base_url}/d/accountable-${each.value.environment}-service-identity"
      recovery_signal = each.value.name == "authenticated-proof" ? "the proof alert resolves automatically" : "the alert query remains below threshold for 5 minutes"
    }
  }
}

resource "grafana_notification_policy" "accountable" {
  contact_point   = grafana_contact_point.sns["development-warning"].name
  group_by        = ["environment", "severity", "alertname"]
  group_wait      = "30s"
  group_interval  = "5m"
  repeat_interval = "4h"

  dynamic "policy" {
    for_each = {
      for pair in setproduct(local.environments, ["page", "proof", "warning"]) :
      "${pair[0]}-${pair[1]}" => {
        environment = pair[0]
        action      = pair[1]
        route       = pair[1] == "page" ? "critical" : pair[1]
      }
    }

    content {
      contact_point = grafana_contact_point.sns["${policy.value.environment}-${policy.value.route}"].name
      group_by      = ["environment", "severity", "alertname"]

      matcher {
        label = "environment"
        match = "="
        value = policy.value.environment
      }

      matcher {
        label = "action"
        match = "="
        value = policy.value.action
      }
    }
  }
}
