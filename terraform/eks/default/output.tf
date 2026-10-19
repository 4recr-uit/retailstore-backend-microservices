output "configure_kubectl" {
  description = "Command to update kubeconfig for this cluster"
  value       = module.retail_app_eks.configure_kubectl
}

output "retail_app_url" {
  description = "URL to access the retail store application"
  value = try(
    "http://${data.kubernetes_service.ui_service.status[0].load_balancer[0].ingress[0].hostname}",
    "LoadBalancer provisioning - run: kubectl get svc -n ui ui"
  )
}

output "rds_postgres_endpoint" {
  description = "Connection endpoint of RDS PostgreSQL instance (retaildb)"
  value       = module.rds.rds_postgres_endpoint
}

output "rds_postgres_address" {
  description = "Hostname/address of RDS PostgreSQL instance"
  value       = module.rds.rds_postgres_address
}

output "rds_mysql_endpoint" {
  description = "Connection endpoint of RDS MySQL instance (catalogdb)"
  value       = module.rds.rds_mysql_endpoint
}

output "rds_mysql_address" {
  description = "Hostname/address of RDS MySQL instance"
  value       = module.rds.rds_mysql_address
}

output "rds_security_group_id" {
  description = "Security group ID of RDS instances"
  value       = module.rds.rds_security_group_id
}
