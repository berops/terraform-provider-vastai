package provider

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"testing"
	"time"

	"terraform-provider-vastai/internal/vastai"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const (
	testAccInstanceResourceName = "vastai_instance.test"
	testAccInstanceImage        = "docker.io/vastai/kvm:@vastai-automatic-tag"
	testAccInstanceDisk         = 150
	testAccInstanceLabel        = testAccResourcePrefix + "instance"
	testAccInstanceRelabel      = testAccResourcePrefix + "instance-renamed"
)

func TestAccInstanceResource(t *testing.T) {
	testAccPreCheck(t)
	useTeamAPIKey(t)

	registerSshKey(t)

	var instanceID int64

	nonEmpty := knownvalue.StringRegexp(regexp.MustCompile(`.+`))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkIfInstanceWasDestroyed(t),
		Steps: []resource.TestStep{
			// rent the offer and wait until the instance is running
			{
				Config: instanceResourceConfig(testAccInstanceLabel),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testAccInstanceResourceName, plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// configured
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("image"), knownvalue.StringExact(testAccInstanceImage)),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("disk"), knownvalue.Float64Exact(testAccInstanceDisk)),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("runtype"), knownvalue.StringExact("ssh")),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("label"), knownvalue.StringExact(testAccInstanceLabel)),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("vm"), knownvalue.Bool(true)),
					// computed from the running instance
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("offer_id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("machine_id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("num_gpus"), knownvalue.Int64Exact(1)),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("gpu_name"), nonEmpty),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("ssh_host"), nonEmpty),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("ssh_port"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("public_ipaddr"), nonEmpty),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("dph_total"), knownvalue.NotNull()),
				},
				Check: checkInstanceViaApi(t, vastai.ActualStatusRunning, testAccInstanceLabel, &instanceID),
			},
			// reapply, idempotency check
			{
				Config: instanceResourceConfig(testAccInstanceLabel),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
			// relabel and stop in place, in one update: same contract, no replacement
			{
				Config: instanceResourceConfigWithState(testAccInstanceRelabel, "stopped"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testAccInstanceResourceName, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("label"), knownvalue.StringExact(testAccInstanceRelabel)),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("target_state"), knownvalue.StringExact("stopped")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					checkInstanceViaApi(t, vastai.ActualStatusExited, testAccInstanceRelabel, &instanceID),
				),
			},
			// start again in place
			{
				Config: instanceResourceConfigWithState(testAccInstanceRelabel, "running"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testAccInstanceResourceName, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					checkInstanceViaApi(t, vastai.ActualStatusRunning, testAccInstanceRelabel, &instanceID),
				),
			},
		},
	})
}

func instanceResourceConfig(label string) string {
	return instanceResourceConfigWithState(label, "running")
}

func instanceResourceConfigWithState(label, targetState string) string {
	return fmt.Sprintf(
		`resource "vastai_instance" "test" {
		  image        = %q
		  disk         = %d
		  runtype      = "ssh"
		  vm           = true
		  label        = %q
		  target_state = %q

		  search_offer {
		    verified    = { eq = true }
		    datacenter  = { eq = true }
		    vms_enabled = { eq = true }
		    num_gpus    = { eq = 1 }
		    disk_space  = { gte = %d }
		    reliability = { gte = 0.94 }
		  }
		}`, testAccInstanceImage, testAccInstanceDisk, label, targetState, testAccInstanceDisk)
}

// registerSshKey uploads a throwaway SSH key for the duration of the test and
// removes it afterwards, the API refuses to create an instance on an account
// without one.
func registerSshKey(t *testing.T) {
	t.Helper()

	client, err := vastai.New(personalAPIKey(), testAccApiURL())
	if err != nil {
		t.Fatalf("creating vastai client: %v", err)
	}
	key, err := client.CreateSSHKey(t.Context(), generateSshKey(t, testAccInstanceLabel))
	if err != nil {
		t.Fatalf("registering ssh key: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := client.DeleteSSHKey(ctx, key.ID); err != nil {
			t.Errorf("deleting ssh key %d: %v", key.ID, err)
		}
	})
}

func instanceIDFromState(s *terraform.State) (int64, error) {
	rs, ok := s.RootModule().Resources[testAccInstanceResourceName]
	if !ok {
		return 0, fmt.Errorf("%s not found in state", testAccInstanceResourceName)
	}
	id, err := strconv.ParseInt(rs.Primary.Attributes["id"], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing id %q: %w", rs.Primary.Attributes["id"], err)
	}
	return id, nil
}

func checkInstanceViaApi(t *testing.T, wantActualStatus, wantLabel string, gotID *int64) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		id, err := instanceIDFromState(s)
		if err != nil {
			return err
		}
		inst, err := newVastAiClient(t).ShowInstance(t.Context(), id)
		if err != nil {
			return err
		}
		if actualStatus := inst.ActualStatus.GetOrEmpty(); actualStatus != wantActualStatus {
			return fmt.Errorf("instance %d has actual status %q, want %q", id, actualStatus, wantActualStatus)
		}
		if label := inst.Label.GetOrEmpty(); label != wantLabel {
			return fmt.Errorf("instance %d has label %q, want %q", id, label, wantLabel)
		}
		// Updates must keep the contract rented in the first step.
		if *gotID != 0 && *gotID != id {
			return fmt.Errorf("instance was replaced: id %d, want %d", id, *gotID)
		}
		*gotID = id
		return nil
	}
}

func checkIfInstanceWasDestroyed(t *testing.T) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		client := newVastAiClient(t)
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "vastai_instance" {
				continue
			}
			id, err := strconv.ParseInt(rs.Primary.Attributes["id"], 10, 64)
			if err != nil {
				return fmt.Errorf("parsing id %q: %w", rs.Primary.Attributes["id"], err)
			}
			// Destruction is asynchronous, so a lookup right after destroying
			// may still return the instance.
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			if _, err := client.WaitForIntendedStatus(ctx, id, vastai.IntendedStatusGone, nil); err != nil {
				return err
			}
		}
		return nil
	}
}
