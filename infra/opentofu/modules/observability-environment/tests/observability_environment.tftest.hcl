mock_provider "aws" {
  mock_data "aws_iam_policy_document" {
    defaults = {
      json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}"
    }
  }

  mock_resource "aws_cloudwatch_event_rule" {
    defaults = {
      arn = "arn:aws:events:eu-west-2:453722413624:rule/accountable-observability"
    }
  }

  mock_resource "aws_cloudwatch_log_group" {
    defaults = {
      arn = "arn:aws:logs:eu-west-2:453722413624:log-group:accountable-observability"
    }
  }

  mock_resource "aws_iam_role" {
    defaults = {
      arn = "arn:aws:iam::453722413624:role/accountable-observability"
    }
  }

  mock_resource "aws_lambda_function" {
    defaults = {
      arn = "arn:aws:lambda:eu-west-2:453722413624:function:accountable-observability"
    }
  }

  mock_resource "aws_sns_topic" {
    defaults = {
      arn = "arn:aws:sns:eu-west-2:453722413624:accountable-observability"
    }
  }

  mock_resource "aws_sqs_queue" {
    defaults = {
      arn = "arn:aws:sqs:eu-west-2:453722413624:accountable-observability"
    }
  }
}

variables {
  account_id                             = "453722413624"
  region                                 = "eu-west-2"
  environment                            = "development"
  kms_key_arn                            = "arn:aws:kms:eu-west-2:453722413624:key/00000000-0000-0000-0000-000000000000"
  alert_email_addresses                  = ["alerts@example.invalid"]
  translator_package_path                = "tests/observability_environment.tftest.hcl"
  canary_package_path                    = "tests/observability_environment.tftest.hcl"
  capacity_package_path                  = "tests/observability_environment.tftest.hcl"
  enable_capacity_producer               = true
  canary_active                          = false
  grafana_capacity_cloud_token_parameter = "/accountable/observability/development/grafana-capacity-cloud-token"
  grafana_capacity_stack_token_parameter = "/accountable/observability/development/grafana-capacity-stack-token"
  cells = {
    development-01 = "arn:aws:ecs:eu-west-2:453722413624:cluster/accountable-development-01"
    proof-01       = "arn:aws:ecs:eu-west-2:453722413624:cluster/accountable-proof-01"
  }
  grafana_base_url          = "https://acctbl.grafana.net"
  grafana_token_parameter   = "/accountable/observability/development/grafana-health-token"
  runbook_base_url          = "https://docs.getaccountable.com/runbooks"
  dashboard_base_url        = "https://acctbl.grafana.net"
  grafana_cloud_account_id  = "008923505280"
  grafana_cloud_external_id = "1765140"
}

run "runtime_logs_belong_to_environment_observability" {
  command = plan

  assert {
    condition = (
      aws_cloudwatch_log_group.runtime["development-01"].name == "/accountable/development-01" &&
      aws_cloudwatch_log_group.runtime["proof-01"].name == "/accountable/proof-01" &&
      alltrue([for log_group in aws_cloudwatch_log_group.runtime : log_group.kms_key_id == var.kms_key_arn])
    )
    error_message = "Environment observability must own one encrypted runtime log group for every registered cell."
  }

  assert {
    condition = (
      length(aws_cloudwatch_log_metric_filter.stdout) == 8 &&
      alltrue([
        for filter in aws_cloudwatch_log_metric_filter.stdout :
        contains(["/accountable/development-01", "/accountable/proof-01"], filter.log_group_name)
      ])
    )
    error_message = "Every registered cell runtime log group must have every safe stdout metric filter."
  }

  assert {
    condition = (
      length(aws_cloudwatch_metric_alarm.emf_health) == 12 &&
      contains(keys(aws_cloudwatch_metric_alarm.emf_health), "runtime-development-01-EMFParsingErrors") &&
      contains(keys(aws_cloudwatch_metric_alarm.emf_health), "runtime-proof-01-EMFParsingErrors")
    )
    error_message = "Every runtime log group must have independent EMF health alarms."
  }

  assert {
    condition = (
      aws_cloudwatch_event_rule.canary.state == "DISABLED" &&
      length(aws_cloudwatch_metric_alarm.canary) == 0
    )
    error_message = "The external canary must not report incidents before the cell and Grafana stack are activated."
  }
}

run "external_canary_activates_fail_closed" {
  command = plan

  variables {
    canary_active = true
  }

  assert {
    condition = (
      aws_cloudwatch_event_rule.canary.state == "ENABLED" &&
      length(aws_cloudwatch_metric_alarm.canary) == 1 &&
      one(aws_cloudwatch_metric_alarm.canary).treat_missing_data == "breaching"
    )
    error_message = "Canary activation must enable both scheduled proof and its fail-closed alarm."
  }
}
