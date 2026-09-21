package provider

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const testAccSshKeyResourceName = "vastai_ssh_key.test"

func TestAccSshKeyResourceCreateUpdate(t *testing.T) {
	usePersonalAPIKey(t)

	firstSshKey := generateSshKey(t, "testFirstSshKey")
	secondSshKey := generateSshKey(t, "testSecondSshKey")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkIfKeyWasDestroyed(t),
		Steps: []resource.TestStep{
			// create key
			{
				Config: sshKeyResourceConfig(firstSshKey),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testAccSshKeyResourceName, plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(testAccSshKeyResourceName, tfjsonpath.New("public_key"), knownvalue.StringExact(firstSshKey)),
					statecheck.ExpectKnownValue(testAccSshKeyResourceName, tfjsonpath.New("id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(testAccSshKeyResourceName, tfjsonpath.New("user_id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(testAccSshKeyResourceName, tfjsonpath.New("created_at"), knownvalue.NotNull()),
				},
				Check: checkIfKeyExists(t, firstSshKey),
			},
			// update key
			{
				Config: sshKeyResourceConfig(secondSshKey),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testAccSshKeyResourceName, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(testAccSshKeyResourceName, tfjsonpath.New("public_key"), knownvalue.StringExact(secondSshKey)),
				},
				Check: checkIfKeyExists(t, secondSshKey),
			},
			// reapply key, idempotency check
			{
				Config: sshKeyResourceConfig(secondSshKey),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
			// delete key behind Terraform's back, the next plan must recreate it
			{
				PreConfig: deleteSshKeyViaApi(t, secondSshKey),
				Config:    sshKeyResourceConfig(secondSshKey),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testAccSshKeyResourceName, plancheck.ResourceActionCreate),
					},
				},
				Check: checkIfKeyExists(t, secondSshKey),
			},
		},
	})
}

func deleteSshKeyViaApi(t *testing.T, publicKey string) func() {
	return func() {
		client := newVastAiClient(t)
		keys, err := client.ListSSHKeys(context.Background())
		if err != nil {
			t.Fatalf("listing ssh keys: %v", err)
		}
		for _, k := range keys {
			if k.PublicKey == publicKey {
				if err := client.DeleteSSHKey(context.Background(), k.ID); err != nil {
					t.Fatalf("deleting ssh key %d: %v", k.ID, err)
				}
			}
		}
	}
}

func sshKeyResourceConfig(publicKey string) string {
	return fmt.Sprintf(
		`resource "vastai_ssh_key" "test" {
		  public_key = %q
		}`, publicKey)
}

func generateSshKey(t *testing.T, comment string) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating ed25519 key: %v", err)
	}
	const keyType = "ssh-ed25519"
	blob := binary.BigEndian.AppendUint32(nil, uint32(len(keyType)))
	blob = append(blob, keyType...)
	blob = binary.BigEndian.AppendUint32(blob, uint32(len(pub)))
	blob = append(blob, pub...)
	return fmt.Sprintf("%s %s %s", keyType, base64.StdEncoding.EncodeToString(blob), comment)
}

func checkIfKeyExists(t *testing.T, wantPublicKey string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[testAccSshKeyResourceName]
		if !ok {
			return fmt.Errorf("%s not found in state", testAccSshKeyResourceName)
		}
		id, err := strconv.ParseInt(rs.Primary.ID, 10, 64)
		if err != nil {
			return fmt.Errorf("parsing id %q: %w", rs.Primary.ID, err)
		}

		keys, err := newVastAiClient(t).ListSSHKeys(context.Background())
		if err != nil {
			return fmt.Errorf("listing ssh keys: %w", err)
		}
		for _, k := range keys {
			if k.ID != id {
				continue
			}
			if k.PublicKey != wantPublicKey {
				return fmt.Errorf("ssh key %d has public key %q, want %q", id, k.PublicKey, wantPublicKey)
			}
			return nil
		}
		return fmt.Errorf("ssh key %d not found on the account", id)
	}
}

func checkIfKeyWasDestroyed(t *testing.T) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		keys, err := newVastAiClient(t).ListSSHKeys(context.Background())
		if err != nil {
			return fmt.Errorf("listing ssh keys: %w", err)
		}
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "vastai_ssh_key" {
				continue
			}
			for _, k := range keys {
				if strconv.FormatInt(k.ID, 10) == rs.Primary.ID {
					return fmt.Errorf("ssh key %s still exists after destroy", rs.Primary.ID)
				}
			}
		}
		return nil
	}
}
