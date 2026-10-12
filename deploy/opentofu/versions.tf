terraform {
  required_version = ">= 1.9.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
  # State backend is intentionally NOT configured here.
  # After bootstrapping the state bucket out-of-band, add:
  #
  #   backend "s3" {
  #     bucket         = "quizforge-tofu-state-<unique-suffix>"
  #     key            = "staging/tofu.tfstate"
  #     region         = "ap-southeast-1"
  #     encrypt        = true
  #     use_lockfile   = true
  #   }
  #
  # Until then, state is local: keep it encrypted at rest, back it up, and
  # never commit it (*.tfstate* is gitignored). Staging and production MUST use
  # separate keys (and ideally separate buckets/accounts).
}
