# All parameters from `create instance` endpoint are supported apart from `price`
# https://docs.vast.ai/api-reference/instances/create-instance
resource "vastai_instance" "gpu" {
  provider       = vastai.team
  id             = local.cheapest_vm_offer.id
  label          = "example-instance"
  image          = "docker.io/vastai/kvm:@vastai-automatic-tag"
  vm             = true
  runtype        = "ssh"
  cancel_unavail = true
  disk           = 150
}
