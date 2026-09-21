# Since Vast.ai does not support creating SSH keys for team accounts, we initialize
# two providers: one tied to a personal account and the other to a team account.
provider "vastai" {
  alias   = "personal"
  api_key = "YOUR_API_KEY"
}

provider "vastai" {
  alias   = "team"
  api_key = "YOUR_API_KEY"
}
