variable "environment_name" {
  description = "Name of the environment"
  type        = string
}

variable "vpc_id" {
  description = "VPC ID where RDS instances are deployed"
  type        = string
}

variable "subnet_ids" {
  description = "List of VPC private subnet IDs for RDS DB Subnet Group"
  type        = list(string)
}

variable "eks_cluster_security_group_id" {
  description = "Security group ID of EKS cluster primary"
  type        = string
  default     = ""
}

variable "eks_node_security_group_id" {
  description = "Security group ID of EKS worker nodes"
  type        = string
  default     = ""
}

variable "additional_allowed_security_group_ids" {
  description = "Additional security group IDs allowed to access RDS instances"
  type        = list(string)
  default     = []
}

variable "vpc_cidr" {
  description = "Optional VPC CIDR block for fallback ingress rules"
  type        = string
  default     = ""
}

variable "postgres_instance_class" {
  description = "RDS PostgreSQL DB instance class"
  type        = string
  default     = "db.t4g.micro"
}

variable "mysql_instance_class" {
  description = "RDS MySQL DB instance class"
  type        = string
  default     = "db.t4g.micro"
}

variable "postgres_username" {
  description = "RDS PostgreSQL master username"
  type        = string
  default     = "postgres"
}

variable "postgres_password" {
  description = "RDS PostgreSQL master password"
  type        = string
  default     = "retailstore_pg_pass123!"
  sensitive   = true
}

variable "mysql_username" {
  description = "RDS MySQL master username"
  type        = string
  default     = "catalog_user"
}

variable "mysql_password" {
  description = "RDS MySQL master password"
  type        = string
  default     = "retailstore_my_pass123!"
  sensitive   = true
}

variable "tags" {
  description = "Resource tags"
  type        = map(string)
  default     = {}
}
