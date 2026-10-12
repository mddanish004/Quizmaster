# QuizForge AI staging infrastructure (single EC2 + managed services).
# Nothing here is applied without explicit approval (Gate B).
# See docs/AWS_SETUP_GUIDE.md for the operator runbook and
# docs/INFRASTRUCTURE_REVIEW.md for the security review.

provider "aws" {
  region = var.region
}

locals {
  name_prefix = "${var.project}-${var.environment}"
  app_fqdn    = var.domain_name != "" ? "${var.subdomain}.${var.domain_name}" : ""
  common_tags = merge({
    Project     = "QuizForge"
    Environment = var.environment
    ManagedBy   = "opentofu"
  }, var.extra_tags)
}

data "aws_caller_identity" "current" {}

data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
  filter {
    name   = "default-for-az"
    values = ["true"]
  }
}

# Dynamic ARM64 AMI discovery: latest Amazon Linux 2023 ARM64 kernel-default.
# No hardcoded AMI IDs; re-resolved on every plan (pin via ami_override only
# if a release must be frozen for an incident).
data "aws_ssm_parameter" "al2023_arm64" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64"
}

# ---------------------------------------------------------------------------
# Network: default VPC, locked-down security group, one Elastic IP.
# Public subnet + public IP are required (single-host portfolio topology:
# Caddy must terminate public TLS directly; no ALB/NAT by design).
# Risk note: the instance is directly internet-reachable on 80/443 only;
# SSH is closed and admin access is via SSM Session Manager.
# ---------------------------------------------------------------------------

resource "aws_security_group" "app" {
  name_prefix = "${local.name_prefix}-"
  description = "QuizForge ${var.environment}: public web only, no SSH."
  vpc_id      = data.aws_vpc.default.id
  tags        = merge(local.common_tags, { Name = "${local.name_prefix}-sg" })

  lifecycle { create_before_destroy = true }
}

resource "aws_vpc_security_group_ingress_rule" "http" {
  security_group_id = aws_security_group.app.id
  description       = "ACME redirect + health (Caddy upgrades to TLS)."
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
  cidr_ipv4         = "0.0.0.0/0"
}

resource "aws_vpc_security_group_ingress_rule" "https" {
  security_group_id = aws_security_group.app.id
  description       = "Public HTTPS (UI, REST, WSS)."
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
  cidr_ipv4         = "0.0.0.0/0"
}

# Egress allowlist: 443 (Neon/HF/Google/ECR/SSM/CloudWatch/LE) + 80 (package
# mirrors, ACME) + DNS + NTP. Everything else (notably inbound SSH and
# non-web outbound) is denied by default.
resource "aws_vpc_security_group_egress_rule" "https_out" {
  security_group_id = aws_security_group.app.id
  description       = "TLS to AWS APIs, Neon, Hugging Face, Google, LE."
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
  cidr_ipv4         = "0.0.0.0/0"
}

resource "aws_vpc_security_group_egress_rule" "http_out" {
  security_group_id = aws_security_group.app.id
  description       = "Package mirrors and ACME HTTP validation."
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
  cidr_ipv4         = "0.0.0.0/0"
}

resource "aws_vpc_security_group_egress_rule" "dns_udp" {
  security_group_id = aws_security_group.app.id
  description       = "DNS resolution."
  from_port         = 53
  to_port           = 53
  ip_protocol       = "udp"
  cidr_ipv4         = "0.0.0.0/0"
}

resource "aws_vpc_security_group_egress_rule" "dns_tcp" {
  security_group_id = aws_security_group.app.id
  description       = "DNS resolution (TCP fallback)."
  from_port         = 53
  to_port           = 53
  ip_protocol       = "tcp"
  cidr_ipv4         = "0.0.0.0/0"
}

resource "aws_vpc_security_group_egress_rule" "ntp" {
  security_group_id = aws_security_group.app.id
  description       = "Clock sync (TLS cert validation depends on it)."
  from_port         = 123
  to_port           = 123
  ip_protocol       = "udp"
  cidr_ipv4         = "0.0.0.0/0"
}

resource "aws_eip" "app" {
  domain = "vpc"
  tags   = merge(local.common_tags, { Name = "${local.name_prefix}-eip" })
}

# ---------------------------------------------------------------------------
# IAM: least-privilege instance role + GitHub OIDC deploy role.
# ---------------------------------------------------------------------------

resource "aws_iam_role" "instance" {
  name        = "${local.name_prefix}-instance"
  description = "QuizForge ${var.environment} EC2: SSM core + scoped app permissions."
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
  tags = local.common_tags
}

resource "aws_iam_role_policy_attachment" "ssm_core" {
  role       = aws_iam_role.instance.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

# Scoped app permissions. SES send is deliberately OMITTED: the app ships with
# MAIL_PROVIDER=mock; add a statement scoped to the verified identity only after
# SES production access is granted (see AWS_SETUP_GUIDE step 4/11).
resource "aws_iam_role_policy" "app" {
  name = "${local.name_prefix}-app"
  role = aws_iam_role.instance.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "ReadAppParameters"
        Effect   = "Allow"
        Action   = ["ssm:GetParameter", "ssm:GetParameters"]
        Resource = "arn:aws:ssm:${var.region}:${data.aws_caller_identity.current.account_id}:parameter/${var.ssm_path}/*"
      },
      {
        Sid      = "EcrAuth"
        Effect   = "Allow"
        Action   = ["ecr:GetAuthorizationToken"]
        Resource = "*"
      },
      {
        Sid    = "EcrPull"
        Effect = "Allow"
        Action = [
          "ecr:BatchCheckLayerAvailability",
          "ecr:BatchGetImage",
          "ecr:GetDownloadUrlForLayer",
        ]
        Resource = [
          aws_ecr_repository.api.arn,
          aws_ecr_repository.web.arn,
          aws_ecr_repository.migrate.arn,
        ]
      },
      {
        Sid      = "WriteAppLogs"
        Effect   = "Allow"
        Action   = ["logs:CreateLogStream", "logs:DescribeLogStreams", "logs:PutLogEvents"]
        Resource = [for g in aws_cloudwatch_log_group.app : "${g.arn}:*"]
      },
      {
        Sid      = "ListBackupBucket"
        Effect   = "Allow"
        Action   = ["s3:ListBucket"]
        Resource = [aws_s3_bucket.backups.arn]
      },
      {
        Sid      = "WriteBackups"
        Effect   = "Allow"
        Action   = ["s3:PutObject", "s3:GetObject", "s3:AbortMultipartUpload"]
        Resource = ["${aws_s3_bucket.backups.arn}/nightly/*"]
      },
    ]
  })
}

resource "aws_iam_instance_profile" "app" {
  name = "${local.name_prefix}-instance"
  role = aws_iam_role.instance.name
  tags = local.common_tags
}

# GitHub Actions OIDC: short-lived credentials, no static keys. Trust is limited
# to the exact repository AND the production environment / release tags.
resource "aws_iam_openid_connect_provider" "github" {
  url = "https://token.actions.githubusercontent.com"
  client_id_list = [
    "sts.amazonaws.com",
  ]
  # Verify current thumbprints at apply time:
  # https://docs.github.com/en/actions/security-for-github-actions/security-hardening-your-deployments/about-security-hardening-with-openid-connect
  thumbprint_list = [
    "6938fd4d98bab03faadb97b34396831e3780aea1",
  ]
  tags = local.common_tags
}

resource "aws_iam_role" "deploy" {
  name        = "${local.name_prefix}-github-deploy"
  description = "QuizForge ${var.environment}: assumed by GitHub Actions via OIDC."
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Federated = aws_iam_openid_connect_provider.github.arn }
      Action    = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com"
        }
        StringLike = {
          "token.actions.githubusercontent.com:sub" = [
            "repo:${var.github_repo}:environment:production",
            "repo:${var.github_repo}:ref:refs/tags/v*",
          ]
        }
      }
    }]
  })
  tags = local.common_tags
}

resource "aws_iam_role_policy" "deploy" {
  name = "${local.name_prefix}-deploy"
  role = aws_iam_role.deploy.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "EcrPushPull"
        Effect   = "Allow"
        Action   = ["ecr:GetAuthorizationToken"]
        Resource = "*"
      },
      {
        Sid    = "EcrImages"
        Effect = "Allow"
        Action = [
          "ecr:BatchCheckLayerAvailability",
          "ecr:BatchGetImage",
          "ecr:CompleteLayerUpload",
          "ecr:GetDownloadUrlForLayer",
          "ecr:InitiateLayerUpload",
          "ecr:PutImage",
          "ecr:UploadLayerPart",
        ]
        Resource = [
          aws_ecr_repository.api.arn,
          aws_ecr_repository.web.arn,
          aws_ecr_repository.migrate.arn,
        ]
      },
      {
        Sid      = "SsmDeploy"
        Effect   = "Allow"
        Action   = ["ssm:SendCommand"]
        Resource = [aws_instance.app.arn]
        Condition = {
          StringEquals = {
            "ssm:DocumentName" = "AWS-RunShellScript"
          }
        }
      },
      {
        Sid      = "SsmResults"
        Effect   = "Allow"
        Action   = ["ssm:GetCommandInvocation", "ssm:ListCommandInvocations"]
        Resource = ["arn:aws:ssm:${var.region}:${data.aws_caller_identity.current.account_id}:*"]
      },
    ]
  })
}

# ---------------------------------------------------------------------------
# Compute: one Graviton host. IMDSv2 required, encrypted root, no SSH key.
# ---------------------------------------------------------------------------

resource "aws_instance" "app" {
  ami           = data.aws_ssm_parameter.al2023_arm64.value
  instance_type = var.instance_type
  subnet_id     = tolist(data.aws_subnets.default.ids)[0]

  iam_instance_profile        = aws_iam_instance_profile.app.name
  vpc_security_group_ids      = [aws_security_group.app.id]
  associate_public_ip_address = true

  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required" # IMDSv2 only; v1 (spoofable) refused
    http_put_response_hop_limit = 1
    instance_metadata_tags      = "disabled"
  }

  root_block_device {
    volume_size = var.ebs_size_gb
    volume_type = "gp3"
    encrypted   = true # default aws/ebs key; CMK only if compliance requires it
    tags        = merge(local.common_tags, { Name = "${local.name_prefix}-root" })
  }

  user_data = templatefile("${path.module}/user_data.sh.tmpl", {
    region      = var.region
    environment = var.environment
    log_group   = aws_cloudwatch_log_group.app["host"].name
    compose_ver = var.compose_version
  })
  # Immutable host: user-data changes replace the instance (EIP reassociates,
  # Caddy reissues certs). Surprising but reproducible; review the plan diff.
  user_data_replace_on_change = true

  tags = merge(local.common_tags, { Name = "${local.name_prefix}-app" })
}

resource "aws_eip_association" "app" {
  instance_id   = aws_instance.app.id
  allocation_id = aws_eip.app.id
}

# ---------------------------------------------------------------------------
# Containers: two private ECR repos, immutable tags, scan on push, keep last 5.
# ---------------------------------------------------------------------------

resource "aws_ecr_repository" "api" {
  name                 = "${var.project}-api"
  image_tag_mutability = "IMMUTABLE"
  force_delete         = false
  image_scanning_configuration { scan_on_push = true }
  encryption_configuration { encryption_type = "AES256" }
  tags = local.common_tags
}

resource "aws_ecr_repository" "web" {
  name                 = "${var.project}-web"
  image_tag_mutability = "IMMUTABLE"
  force_delete         = false
  image_scanning_configuration { scan_on_push = true }
  encryption_configuration { encryption_type = "AES256" }
  tags = local.common_tags
}

resource "aws_ecr_repository" "migrate" {
  name                 = "${var.project}-migrate"
  image_tag_mutability = "IMMUTABLE"
  force_delete         = false
  image_scanning_configuration { scan_on_push = true }
  encryption_configuration { encryption_type = "AES256" }
  tags = local.common_tags
}

resource "aws_ecr_lifecycle_policy" "api" {
  repository = aws_ecr_repository.api.name
  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "Keep last 5 releases."
      selection    = { tagStatus = "any", countType = "imageCountMoreThan", countNumber = 5 }
      action       = { type = "expire" }
    }]
  })
}

resource "aws_ecr_lifecycle_policy" "web" {
  repository = aws_ecr_repository.web.name
  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "Keep last 5 releases."
      selection    = { tagStatus = "any", countType = "imageCountMoreThan", countNumber = 5 }
      action       = { type = "expire" }
    }]
  })
}

resource "aws_ecr_lifecycle_policy" "migrate" {
  repository = aws_ecr_repository.migrate.name
  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "Keep last 5 releases."
      selection    = { tagStatus = "any", countType = "imageCountMoreThan", countNumber = 5 }
      action       = { type = "expire" }
    }]
  })
}

# ---------------------------------------------------------------------------
# Config namespace: public (non-secret) values only. Secrets are created
# OUT-OF-BAND by the operator (console/CLI) as SecureString under the same
# path — never via Terraform, so values never enter variables or state.
# Required secret names: DATABASE_URL, MIGRATIONS_DATABASE_URL, CSRF_SECRET,
# PASSWORD_PEPPER, GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, HF_TOKEN.
# ---------------------------------------------------------------------------

resource "aws_ssm_parameter" "app_domain" {
  name  = "/${var.ssm_path}/APP_DOMAIN"
  type  = "String"
  value = local.app_fqdn != "" ? local.app_fqdn : "UNSET-configure-dns-first"
  tags  = local.common_tags
}

# ---------------------------------------------------------------------------
# Observability: log groups (awslogs driver, non-blocking) + host alarms.
# ---------------------------------------------------------------------------

resource "aws_cloudwatch_log_group" "app" {
  for_each          = toset(["api", "web", "host"])
  name              = "/${local.name_prefix}/${each.key}"
  retention_in_days = var.log_retention_days
  tags              = local.common_tags
}

resource "aws_sns_topic" "alerts" {
  count = var.alert_email != "" ? 1 : 0
  name  = "${local.name_prefix}-alerts"
  tags  = local.common_tags
}

resource "aws_sns_topic_subscription" "alerts_email" {
  count     = var.alert_email != "" ? 1 : 0
  topic_arn = aws_sns_topic.alerts[0].arn
  protocol  = "email"
  endpoint  = var.alert_email
  # Operator MUST confirm the subscription email; alarms are silent until then.
}

resource "aws_cloudwatch_metric_alarm" "status_check" {
  alarm_name          = "${local.name_prefix}-status-check-failed"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "StatusCheckFailed"
  namespace           = "AWS/EC2"
  period              = 60
  statistic           = "Maximum"
  threshold           = 0
  alarm_description   = "EC2 system/instance status check failed."
  dimensions          = { InstanceId = aws_instance.app.id }
  alarm_actions       = [for t in aws_sns_topic.alerts : t.arn]
  tags                = local.common_tags
}

resource "aws_cloudwatch_metric_alarm" "cpu_high" {
  alarm_name          = "${local.name_prefix}-cpu-high"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "CPUUtilization"
  namespace           = "AWS/EC2"
  period              = 300
  statistic           = "Average"
  threshold           = 80
  alarm_description   = "Sustained CPU >80%: resize or investigate before events."
  dimensions          = { InstanceId = aws_instance.app.id }
  alarm_actions       = [for t in aws_sns_topic.alerts : t.arn]
  tags                = local.common_tags
}

# ---------------------------------------------------------------------------
# Backups: versioned, encrypted, private bucket; nightly pg_dump lands in
# nightly/ and expires after var.backup_retention_days (default 14).
# ---------------------------------------------------------------------------

resource "aws_s3_bucket" "backups" {
  bucket_prefix = "${local.name_prefix}-backups-"
  tags          = local.common_tags
}

resource "aws_s3_bucket_versioning" "backups" {
  bucket = aws_s3_bucket.backups.id
  versioning_configuration { status = "Enabled" }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "backups" {
  bucket = aws_s3_bucket.backups.id
  rule {
    apply_server_side_encryption_by_default { sse_algorithm = "AES256" }
    bucket_key_enabled = true
  }
}

resource "aws_s3_bucket_public_access_block" "backups" {
  bucket                  = aws_s3_bucket.backups.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_lifecycle_configuration" "backups" {
  bucket = aws_s3_bucket.backups.id
  rule {
    id     = "expire-nightly"
    status = "Enabled"
    filter { prefix = "nightly/" }
    expiration { days = var.backup_retention_days }
    noncurrent_version_expiration { noncurrent_days = var.backup_retention_days }
    abort_incomplete_multipart_upload { days_after_initiation = 1 }
  }
}

# ---------------------------------------------------------------------------
# Cost guard: monthly budget with actual + forecasted email alerts.
# ---------------------------------------------------------------------------

resource "aws_budgets_budget" "monthly" {
  count        = var.alert_email != "" ? 1 : 0
  name         = "${local.name_prefix}-monthly"
  budget_type  = "COST"
  limit_amount = var.budget_limit_usd
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 80
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.alert_email]
  }
  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = [var.alert_email]
  }
}

# ---------------------------------------------------------------------------
# DNS: record in an EXISTING zone only (no zone/delegation changes).
# Disabled entirely while var.domain_name is empty.
# ---------------------------------------------------------------------------

data "aws_route53_zone" "app" {
  count = var.domain_name != "" ? 1 : 0
  name  = var.domain_name
}

resource "aws_route53_record" "app" {
  count   = var.domain_name != "" ? 1 : 0
  zone_id = data.aws_route53_zone.app[0].zone_id
  name    = local.app_fqdn
  type    = "A"
  ttl     = 300
  records = [aws_eip.app.public_ip]
}
