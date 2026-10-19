locals {
  ingress_security_group_ids = distinct(compact(concat(
    [var.eks_cluster_security_group_id, var.eks_node_security_group_id],
    var.additional_allowed_security_group_ids
  )))
}

# DB Subnet Group spanning Private Subnets
resource "aws_db_subnet_group" "rds" {
  name        = "${var.environment_name}-rds-subnets"
  description = "Database subnet group for ${var.environment_name} RDS instances"
  subnet_ids  = var.subnet_ids

  tags = merge(var.tags, {
    Name = "${var.environment_name}-rds-subnets"
  })
}

# Dedicated RDS Security Group
resource "aws_security_group" "rds" {
  name        = "${var.environment_name}-rds-sg"
  description = "Security group for ${var.environment_name} RDS PostgreSQL and MySQL instances"
  vpc_id      = var.vpc_id

  tags = merge(var.tags, {
    Name = "${var.environment_name}-rds-sg"
  })
}

# PostgreSQL Ingress from EKS Security Groups (Port 5432)
resource "aws_security_group_rule" "postgres_ingress_sg" {
  count                    = length(local.ingress_security_group_ids)
  type                     = "ingress"
  from_port                = 5432
  to_port                  = 5432
  protocol                 = "tcp"
  source_security_group_id = local.ingress_security_group_ids[count.index]
  security_group_id        = aws_security_group.rds.id
  description              = "Allow PostgreSQL access from EKS security group"
}

# MySQL Ingress from EKS Security Groups (Port 3306)
resource "aws_security_group_rule" "mysql_ingress_sg" {
  count                    = length(local.ingress_security_group_ids)
  type                     = "ingress"
  from_port                = 3306
  to_port                  = 3306
  protocol                 = "tcp"
  source_security_group_id = local.ingress_security_group_ids[count.index]
  security_group_id        = aws_security_group.rds.id
  description              = "Allow MySQL access from EKS security group"
}

# Fallback VPC CIDR Ingress for PostgreSQL (if specified)
resource "aws_security_group_rule" "postgres_ingress_cidr" {
  count             = var.vpc_cidr != "" && length(local.ingress_security_group_ids) == 0 ? 1 : 0
  type              = "ingress"
  from_port         = 5432
  to_port           = 5432
  protocol          = "tcp"
  cidr_blocks       = [var.vpc_cidr]
  security_group_id = aws_security_group.rds.id
  description       = "Allow PostgreSQL access from VPC CIDR"
}

# Fallback VPC CIDR Ingress for MySQL (if specified)
resource "aws_security_group_rule" "mysql_ingress_cidr" {
  count             = var.vpc_cidr != "" && length(local.ingress_security_group_ids) == 0 ? 1 : 0
  type              = "ingress"
  from_port         = 3306
  to_port           = 3306
  protocol          = "tcp"
  cidr_blocks       = [var.vpc_cidr]
  security_group_id = aws_security_group.rds.id
  description       = "Allow MySQL access from VPC CIDR"
}

# Egress Rule
resource "aws_security_group_rule" "rds_egress" {
  type              = "egress"
  from_port         = 0
  to_port           = 0
  protocol          = "-1"
  cidr_blocks       = ["0.0.0.0/0"]
  security_group_id = aws_security_group.rds.id
  description       = "Allow all outbound traffic from RDS instances"
}

# RDS PostgreSQL 16 Instance (db.t4g.micro - retaildb for cart, orders, checkout)
resource "aws_db_instance" "postgres" {
  identifier             = "${var.environment_name}-postgres"
  engine                 = "postgres"
  engine_version         = "16"
  instance_class         = var.postgres_instance_class
  allocated_storage      = 20
  storage_type           = "gp3"
  db_name                = "retaildb"
  username               = var.postgres_username
  password               = var.postgres_password
  multi_az               = false
  publicly_accessible    = false
  skip_final_snapshot    = true
  deletion_protection    = false
  db_subnet_group_name   = aws_db_subnet_group.rds.name
  vpc_security_group_ids = [aws_security_group.rds.id]

  tags = merge(var.tags, {
    Name = "${var.environment_name}-postgres"
  })
}

# RDS MySQL 8.0 Instance (db.t4g.micro - catalogdb for catalog, recommendations)
resource "aws_db_instance" "mysql" {
  identifier             = "${var.environment_name}-mysql"
  engine                 = "mysql"
  engine_version         = "8.0"
  instance_class         = var.mysql_instance_class
  allocated_storage      = 20
  storage_type           = "gp3"
  db_name                = "catalogdb"
  username               = var.mysql_username
  password               = var.mysql_password
  multi_az               = false
  publicly_accessible    = false
  skip_final_snapshot    = true
  deletion_protection    = false
  db_subnet_group_name   = aws_db_subnet_group.rds.name
  vpc_security_group_ids = [aws_security_group.rds.id]

  tags = merge(var.tags, {
    Name = "${var.environment_name}-mysql"
  })
}
