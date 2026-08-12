output "folder_uids" {
  value = { for name, folder in grafana_folder.environment : name => folder.uid }
}

output "cloudwatch_data_source_uids" {
  value = { for name, source in grafana_data_source.cloudwatch : name => source.uid }
}
