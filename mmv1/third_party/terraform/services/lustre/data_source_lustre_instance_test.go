package lustre_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-provider-google/google/acctest"
)

func TestAccLustreInstanceDatasource_basic(t *testing.T) {
	t.Parallel()

	context := map[string]interface{}{
		"network_name":  acctest.BootstrapSharedTestNetwork(t, "default-vpc"),
		"random_suffix": acctest.RandString(t, 10),
	}

	acctest.VcrTest(t, resource.TestCase{
		PreCheck:                 func() { acctest.AccTestPreCheck(t) },
		ProtoV5ProviderFactories: acctest.ProtoV5ProviderFactories(t),
		Steps: []resource.TestStep{
			{
				Config: testAccLustreInstanceDatasource_basic(context),
				Check: acctest.CheckDataSourceStateMatchesResourceState(
					"data.google_lustre_instance.default",
					"google_lustre_instance.instance",
				),
			},
			{
				ResourceName:      "google_lustre_instance.instance",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccLustreInstanceDatasource_basic(context map[string]interface{}) string {
	return acctest.Nprintf(`
resource "google_lustre_instance" "instance" {
  instance_id                 = "tf-test-%{random_suffix}"
  location                    = "us-central1-a"
  filesystem                  = "testfs"
  capacity_gib                = 18000
  network                     = data.google_compute_network.lustre-network.id
  gke_support_enabled         = false
  per_unit_storage_throughput = 1000   
}

// This example assumes this network already exists.
// The API creates a tenant network per network authorized for a
// Lustre instance and that network is not deleted when the user-created
// network (authorized_network) is deleted, so this prevents issues
// with tenant network quota.
// If this network hasn't been created and you are using this example in your
// config, add an additional network resource or change
// this from "data"to "resource"
data "google_compute_network" "lustre-network" {
  name = "%{network_name}"
}

data "google_lustre_instance" "default" {
  instance_id             = google_lustre_instance.instance.instance_id
  zone                    = "us-central1-a"
}
`, context)
}
