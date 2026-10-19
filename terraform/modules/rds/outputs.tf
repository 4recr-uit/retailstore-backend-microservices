output "rds_postgres_endpoint" {
  description = "Connection endpoint of RDS PostgreSQL instance"
  value       = aws_db_instance.postgres.endpoint
}

output "rds_postgres_address" {
  description = "Hostname/address of RDS PostgreSQL instance"
  value       = aws_db_instance.postgres.address
}

output "rds_postgres_port" {
  description = "Port of RDS PostgreSQL instance"
  value       = aws_db_instance.postgres.port
}

output "rds_postgres_db_name" {
  description = "Database name of RDS PostgreSQL instance"
  value       = aws_db_instance.postgres.db_name
}

output "rds_postgres_username" {
  description = "Master username of RDS PostgreSQL instance"
  value       = aws_db_instance.postgres.username
}

output "rds_postgres_password" {
  description = "Master password of RDS PostgreSQL instance"
  value       = aws_db_instance.postgres.password
  sensitive   = true
}

output "rds_mysql_endpoint" {
  description = "Connection endpoint of RDS MySQL instance"
  value       = aws_db_instance.mysql.endpoint
}

output "rds_mysql_address" {
  description = "Hostname/address of RDS MySQL instance"
  value       = aws_db_instance.mysql.address
}

output "rds_mysql_port" {
  description = "Port of RDS MySQL instance"
  value       = aws_db_instance.mysql.port
}

output "rds_mysql_db_name" {
  description = "Database name of RDS MySQL instance"
  value       = aws_db_instance.mysql.db_name
}

output "rds_mysql_username" {
  description = "Master username of RDS MySQL instance"
  value       = aws_db_instance.mysql.username
}

output "rds_mysql_password" {
  description = "Master password of RDS MySQL instance"
  value       = aws_db_instance.mysql.password
  sensitive   = true
}

output "rds_security_group_id" {
  description = "ID of RDS Security Group"
  value       = aws_security_group.rds.id
}

output "rds_db_subnet_group_id" {
  description = "ID of RDS DB Subnet Group"
  value       = aws_db_subnet_group.rds.id
}
