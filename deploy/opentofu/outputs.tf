output "app_url" {
  description = "Public staging URL (UNSET until domain_name is configured)."
  value       = local.app_fqdn != "" ? "https://${local.app_fqdn}" : "UNSET-configure-dns-first"
}

output "instance_id" {
  description = "EC2 instance ID (SSM target for deploys). → secrets.PROD_INSTANCE_ID"
  value       = aws_instance.app.id
}

output "public_ip" {
  description = "Elastic IP attached to the instance."
  value       = aws_eip.app.public_ip
}

output "ecr_api" {
  description = "API repository URL. → secrets.ECR_REGISTRY (registry prefix)"
  value       = aws_ecr_repository.api.repository_url
}

output "ecr_web" {
  description = "Web repository URL."
  value       = aws_ecr_repository.web.repository_url
}

output "deploy_role_arn" {
  description = "GitHub OIDC deploy role. → secrets.DEPLOY_ROLE_ARN"
  value       = aws_iam_role.deploy.arn
}

output "backup_bucket" {
  description = "S3 bucket receiving nightly pg_dump archives."
  value       = aws_s3_bucket.backups.id
}

output "ssm_secret_path" {
  description = "Parameter Store namespace; create the 7 secret names here out-of-band."
  value       = "/${var.ssm_path}/"
}

output "log_groups" {
  description = "CloudWatch log groups wired to the awslogs driver."
  value       = [for g in aws_cloudwatch_log_group.app : g.name]
}
