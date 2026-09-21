resource "vastai_ssh_key" "my_key" {
  provider   = vastai.personal
  public_key = file("PATH_TO_YOUR_KEY")
}
