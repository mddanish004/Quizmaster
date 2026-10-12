# Non-secret infrastructure inputs. No credentials, tokens, or secret values here.
# Copy to staging.tfvars (gitignored via *.tfvars) and fill in values.

variable "region" {
  description = "AWS region (Singapore, adjacent to Neon Singapore)."
  type        = string
  default     = "ap-southeast-1"
}

variable "environment" {
  description = "Environment name; isolates state paths, tags, SSM paths, log groups."
  type        = string
  default     = "staging"
  validation {
    condition     = can(regex("^[a-z0-9-]{2,24}$", var.environment))
    error_message = "environment must be 2-24 lowercase alphanumeric/hyphen characters."
  }
}

variable "project" {
  description = "Project slug used in resource names and tags."
  type        = string
  default     = "quizforge"
}

variable "instance_type" {
  description = "EC2 instance type. t4g.small is the measured starting candidate."
  type        = string
  default     = "t4g.small"
}

variable "ebs_size_gb" {
  description = "Encrypted gp3 root volume size in GiB."
  type        = number
  default     = 20
}

variable "compose_version" {
  description = "Pinned Docker Compose plugin version installed by user-data."
  type        = string
  default     = "2.32.4"
}

variable "domain_name" {
  description = "Existing parent domain (e.g. example.com). Empty disables DNS resources."
  type        = string
  default     = ""
}

variable "subdomain" {
  description = "Staging hostname prefix; FQDN is <subdomain>.<domain_name>."
  type        = string
  default     = "staging"
}

variable "github_repo" {
  description = "GitHub repository allowed to deploy, as ORG/REPO."
  type        = string
  default     = "mddanish004/Quizmaster"
}

variable "github_owner_id" {
  description = "Immutable numeric GitHub owner ID (GitHub now issues OIDC sub claims as ORG@OWNER_ID/REPO@REPO_ID; read it from any decoded token)."
  type        = string
  default     = "133763727"
}

variable "github_repo_id" {
  description = "Immutable numeric GitHub repository ID (see github_owner_id)."
  type        = string
  default     = "1415490116"
}

variable "alert_email" {
  description = "Email receiving budget + CloudWatch alarm notifications (confirm SNS subscription)."
  type        = string
  default     = ""
}

variable "budget_limit_usd" {
  description = "Monthly cost budget in USD that triggers alerts."
  type        = string
  default     = "30"
}

variable "backup_retention_days" {
  description = "S3 backup object expiry in days."
  type        = number
  default     = 14
}

variable "log_retention_days" {
  description = "CloudWatch log group retention in days."
  type        = number
  default     = 14
}

variable "ssm_path" {
  description = "SSM Parameter Store namespace (no leading slash) for app secrets AND public config."
  type        = string
  default     = "quizforge/staging"
}

variable "extra_tags" {
  description = "Additional tags merged into every taggable resource."
  type        = map(string)
  default     = {}
}
