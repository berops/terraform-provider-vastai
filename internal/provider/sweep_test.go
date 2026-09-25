package provider

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"terraform-provider-vastai/internal/vastai"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// testAccResourcePrefix starts the label of every instance and the comment of
// every SSH key the acceptance tests create, so the sweepers can tell them
// apart from real ones.
const testAccResourcePrefix = "tf-acc-"

// TestMain enables sweepers: `go test ./internal/provider -v -sweep=all`
// removes instances and SSH keys left behind by acceptance tests that were
// killed before they could clean up, e.g. by a cancelled CI job.
func TestMain(m *testing.M) {
	resource.TestMain(m)
}

func init() {
	resource.AddTestSweepers("vastai_instance", &resource.Sweeper{
		Name: "vastai_instance",
		F:    sweepInstances,
	})
	resource.AddTestSweepers("vastai_ssh_key", &resource.Sweeper{
		Name: "vastai_ssh_key",
		F:    sweepSSHKeys,
	})
}

// sweepInstances destroys instances labelled with the test prefix on both
// accounts, the instance test rents with the team key.
func sweepInstances(string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var errs []error
	for name, key := range map[string]string{"team": teamAPIKey(), "personal": personalAPIKey()} {
		if key == "" {
			log.Printf("[WARN] skipping %s account: API key not set", name)
			continue
		}
		client, err := vastai.New(key, testAccApiURL())
		if err != nil {
			return err
		}
		instances, err := client.ListInstances(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s account: %w", name, err))
			continue
		}
		for _, inst := range instances {
			if !strings.HasPrefix(inst.Label, testAccResourcePrefix) {
				continue
			}
			log.Printf("[INFO] destroying instance %d (%s, %s)", inst.ID, inst.Label, inst.ActualStatus)
			if err := client.DestroyInstance(ctx, inst.ID); err != nil {
				errs = append(errs, fmt.Errorf("%s account: %w", name, err))
			}
		}
	}
	return errors.Join(errs...)
}

// sweepSSHKeys deletes keys whose comment starts with the test prefix. Only
// the personal account is checked, SSH keys cannot be managed with a team key.
func sweepSSHKeys(string) error {
	if personalAPIKey() == "" {
		log.Printf("[WARN] skipping personal account: API key not set")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	client, err := vastai.New(personalAPIKey(), testAccApiURL())
	if err != nil {
		return err
	}
	keys, err := client.ListSSHKeys(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, key := range keys {
		// "ssh-ed25519 <base64> <comment>"
		fields := strings.Fields(key.PublicKey)
		if len(fields) < 3 || !strings.HasPrefix(fields[2], testAccResourcePrefix) {
			continue
		}
		log.Printf("[INFO] deleting ssh key %d (%s)", key.ID, fields[2])
		if err := client.DeleteSSHKey(ctx, key.ID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
