locals {
  environments = ["development", "staging", "production"]
}

resource "grafana_folder" "environment" {
  for_each = toset(local.environments)

  title = "accountable-${each.value}"
  uid   = "accountable-${each.value}"
}

resource "grafana_service_account" "provisioner" {
  for_each = toset(local.environments)

  name = "accountable-${each.value}-provisioner"
  role = "None"
}

# Folder permissions keep each environment provisioning identity inside its own
# folder. The shared Free stack is not a security boundary, so this is the only
# separation the stack itself can enforce.
resource "grafana_folder_permission" "environment" {
  for_each = toset(local.environments)

  folder_uid = grafana_folder.environment[each.value].uid

  permissions {
    permission = "Admin"
    user_id    = grafana_service_account.provisioner[each.value].id
  }
}

resource "grafana_data_source" "cloudwatch" {
  for_each = var.cloudwatch_role_arns

  name = "accountable-${each.key}-cloudwatch"
  type = "cloudwatch"
  uid  = "accountable-${each.key}-cloudwatch"

  json_data_encoded = jsonencode({
    authType      = "grafana_assume_role"
    assumeRoleArn = each.value
    defaultRegion = var.region
  })
}
