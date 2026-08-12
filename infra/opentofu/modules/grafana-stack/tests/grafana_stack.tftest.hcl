mock_provider "grafana" {}

variables {
  region = "eu-west-2"
  cloudwatch_role_arns = {
    development = "arn:aws:iam::453722413624:role/accountable-development-grafana-cloudwatch"
  }
  alert_topic_arns = {
    development = {
      critical = "arn:aws:sns:eu-west-2:453722413624:accountable-development-observability-critical"
      proof    = "arn:aws:sns:eu-west-2:453722413624:accountable-development-observability-proof"
      warning  = "arn:aws:sns:eu-west-2:453722413624:accountable-development-observability-warning"
    }
  }
  grafana_cloud_external_id = "1765140"
  runbook_base_url          = "https://docs.getaccountable.com/runbooks"
  dashboard_base_url        = "https://acctbl.grafana.net"
}

run "only_active_environments_are_provisioned" {
  command = plan

  assert {
    condition = (
      toset(local.environments) == toset(["development"]) &&
      length(grafana_folder.environment) == 1 &&
      length(grafana_data_source.cloudwatch) == 1 &&
      length(grafana_dashboard.environment) == 6 &&
      length(grafana_contact_point.sns) == 3
    )
    error_message = "The shared stack must provision dashboards, routes, and alerts only for active environments."
  }
}
