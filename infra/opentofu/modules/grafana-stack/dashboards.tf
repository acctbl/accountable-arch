locals {
  dashboard_specs = {
    service-identity = {
      title = "Service and active identity"
      panels = [
        { title = "Eligible requests", expr = "sum(rate(accountable_sli_request_eligible_total{deployment_environment_name=\"$environment\"}[5m]))", unit = "reqps" },
        { title = "Active requests", expr = "sum(accountable_server_request_active{deployment_environment_name=\"$environment\"})", unit = "short" },
        { title = "Runtime identity tuples", expr = "count(count by (service_name, service_version, accountable_release_id, accountable_configuration_fingerprint) (accountable_server_request_count_total{deployment_environment_name=\"$environment\"}))", unit = "short" },
        { title = "Proof events", expr = "sum(increase(accountable_observability_proof_total{deployment_environment_name=\"$environment\"}[1h]))", unit = "short" },
      ]
    }
    slo-budget = {
      title = "SLO and 30-day budget"
      panels = [
        { title = "Availability bad ratio", expr = "sum(rate(accountable_sli_availability_bad_total{deployment_environment_name=\"$environment\"}[5m])) / clamp_min(sum(rate(accountable_sli_request_eligible_total{deployment_environment_name=\"$environment\"}[5m])), 1)", unit = "percentunit" },
        { title = "Latency slow ratio", expr = "sum(rate(accountable_sli_latency_slow_total{deployment_environment_name=\"$environment\"}[5m])) / clamp_min(sum(rate(accountable_sli_request_eligible_total{deployment_environment_name=\"$environment\"}[5m])), 1)", unit = "percentunit" },
        { title = "SLI heartbeat", expr = "sum(increase(accountable_sli_export_heartbeat_total{deployment_environment_name=\"$environment\"}[10m]))", unit = "short" },
        { title = "CloudWatch eligible requests", expr = "", unit = "short" },
      ]
    }
    server-dependencies = {
      title = "Request, dependency, and database health"
      panels = [
        { title = "Request duration p95", expr = "histogram_quantile(0.95, sum by (le) (rate(accountable_server_request_duration_bucket{deployment_environment_name=\"$environment\"}[5m])))", unit = "s" },
        { title = "Server failures", expr = "sum(rate(accountable_server_request_count_total{deployment_environment_name=\"$environment\",outcome=\"failure\"}[5m]))", unit = "reqps" },
        { title = "Database duration p95", expr = "histogram_quantile(0.95, sum by (le) (rate(accountable_db_operation_duration_bucket{deployment_environment_name=\"$environment\"}[5m])))", unit = "s" },
        { title = "Outbound failures", expr = "sum(rate(accountable_http_client_request_count_total{deployment_environment_name=\"$environment\",outcome=\"failure\"}[5m]))", unit = "reqps" },
      ]
    }
    browser-vitals = {
      title = "Browser and Web Vitals"
      panels = [
        { title = "Browser API failure ratio", expr = "sum(rate(accountable_client_event_count_total{deployment_environment_name=\"$environment\",event_name=\"api.request.completed\",outcome=\"failure\"}[15m])) / clamp_min(sum(rate(accountable_client_event_count_total{deployment_environment_name=\"$environment\",event_name=\"api.request.completed\"}[15m])), 1)", unit = "percentunit" },
        { title = "Runtime error load", expr = "sum(increase(accountable_client_event_count_total{deployment_environment_name=\"$environment\",event_name=\"app.error\"}[15m])) / clamp_min(10 * sum(increase(accountable_client_event_count_total{deployment_environment_name=\"$environment\",event_name=\"app.ready\"}[15m])), 1)", unit = "short" },
        { title = "LCP p75", expr = "histogram_quantile(0.75, sum by (le) (rate(accountable_browser_web_vital_duration_us_bucket{deployment_environment_name=\"$environment\",event_name=\"web_vital.lcp\"}[15m])))", unit = "µs" },
        { title = "CLS p75 (ppm)", expr = "histogram_quantile(0.75, sum by (le) (rate(accountable_browser_web_vital_cls_ppm_bucket{deployment_environment_name=\"$environment\"}[15m])))", unit = "short" },
      ]
    }
    collector-capacity = {
      title = "Collector delivery and capacity"
      panels = [
        { title = "Permanent drops", expr = "sum(increase(otelcol_PermanentlyDroppedItemCount_total{Environment=\"$environment\"}[1h]))", unit = "short" },
        { title = "Refused items", expr = "sum(increase(otelcol_RefusedItemCount_total{Environment=\"$environment\"}[1h]))", unit = "short" },
        { title = "Oldest pending item", expr = "max(otelcol_OldestPendingItemAgeSeconds{Environment=\"$environment\"})", unit = "s" },
        { title = "Active metric series", expr = "max(grafanacloud_instance_active_series)", unit = "short" },
      ]
    }
    alerts-deployments = {
      title = "Alert and deployment history"
      panels = [
        { title = "Firing Grafana alerts", expr = "sum(grafana_alerting_alerts{state=\"firing\"})", unit = "short" },
        { title = "Rule evaluation failures", expr = "sum(rate(grafana_alerting_rule_evaluations_total{status=\"error\"}[5m]))", unit = "ops" },
        { title = "Authenticated proof", expr = "sum(increase(accountable_observability_proof_total{deployment_environment_name=\"$environment\"}[1h]))", unit = "short" },
        { title = "Runtime starts", expr = "sum(increase(accountable_sli_export_heartbeat_total{deployment_environment_name=\"$environment\"}[1h]))", unit = "short" },
      ]
    }
  }

  dashboards = {
    for pair in setproduct(local.environments, keys(local.dashboard_specs)) :
    "${pair[0]}-${pair[1]}" => {
      environment = pair[0]
      name        = pair[1]
      spec        = local.dashboard_specs[pair[1]]
    }
  }
}

resource "grafana_dashboard" "environment" {
  for_each = local.dashboards

  folder    = grafana_folder.environment[each.value.environment].uid
  overwrite = true
  message   = "Managed production observability dashboard"
  config_json = jsonencode({
    uid           = "accountable-${each.value.environment}-${each.value.name}"
    title         = each.value.spec.title
    tags          = ["accountable", "accountable-${each.value.environment}"]
    timezone      = "utc"
    schemaVersion = 41
    refresh       = "30s"
    time          = { from = "now-6h", to = "now" }
    templating = {
      list = [{ name = "environment", type = "constant", query = each.value.environment, current = { text = each.value.environment, value = each.value.environment }, hide = 2 }]
    }
    panels = [
      for index, panel in each.value.spec.panels : {
        id    = index + 1
        title = panel.title
        type  = "timeseries"
        gridPos = {
          h = 9
          w = 12
          x = (index % 2) * 12
          y = floor(index / 2) * 9
        }
        datasource = panel.expr == "" ? {
          type = "cloudwatch"
          uid  = "accountable-${each.value.environment}-cloudwatch"
          } : {
          type = "prometheus"
          uid  = "grafanacloud-acctbl-prom"
        }
        fieldConfig = { defaults = { unit = panel.unit }, overrides = [] }
        targets = panel.expr == "" ? [{
          refId      = "A"
          namespace  = "Accountable/SLO"
          metricName = "EligibleRequestCount"
          statistic  = "Sum"
          period     = "300"
          region     = var.region
          dimensions = { Environment = each.value.environment, ServiceName = "accountable-api" }
          }] : [{
          refId  = "A"
          expr   = panel.expr
          format = "time_series"
        }]
      }
    ]
  })
}

resource "grafana_contact_point" "sns" {
  for_each = {
    for pair in setproduct(local.environments, ["critical", "proof", "warning"]) :
    "${pair[0]}-${pair[1]}" => { environment = pair[0], route = pair[1] }
  }

  name = "accountable-${each.value.environment}-${each.value.route}"

  sns {
    topic                   = var.alert_topic_arns[each.value.environment][each.value.route]
    auth_provider           = "arn"
    assume_role_arn         = var.cloudwatch_role_arns[each.value.environment]
    external_id             = var.grafana_cloud_external_id
    message_format          = "json"
    disable_resolve_message = false
  }
}
