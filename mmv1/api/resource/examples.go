// Copyright 2024 Google Inc.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package resource

import (
	"bytes"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"github.com/GoogleCloudPlatform/magic-modules/mmv1/google"
	"github.com/golang/glog"
)

type IamMember struct {
	Member, Role string
}

// Generates configs to be shown as examples in docs and outputted as tests
// from a shared template
type Examples struct {
	// The name of the example in lower snake_case.
	// Generally takes the form of the resource name followed by some detail
	// about the specific test. For example, "address_with_subnetwork".
	Name string

	// The id of the "primary" resource in an example. Used in import tests.
	// This is the value that will appear in the Terraform config url. For
	// example:
	// resource "google_compute_address" {{primary_resource_id}} {
	//   ...
	// }
	PrimaryResourceId string `yaml:"primary_resource_id"`

	// Optional resource type of the "primary" resource. Used in import tests.
	// If set, this will override the default resource type implied from the
	// object parent
	PrimaryResourceType string `yaml:"primary_resource_type,omitempty"`

	// BootstrapIam will automatically bootstrap the given member/role pairs.
	// This should be used in cases where specific IAM permissions must be
	// present on the default test project, to avoid race conditions between
	// tests. Permissions attached to resources created in a test should instead
	// be provisioned with standard terraform resources.
	BootstrapIam []IamMember `yaml:"bootstrap_iam,omitempty"`

	// Vars is a Hash from template variable names to output variable names.
	// It will use the provided value as a prefix for generated tests, and
	// insert it into the docs verbatim.
	Vars map[string]string

	// Some variables need to hold special values during tests, and cannot
	// be inferred by Open in Cloud Shell.  For instance, org_id
	// needs to be the correct value during integration tests, or else
	// org tests cannot pass. Other examples include an existing project_id,
	// a zone, a service account name, etc.
	//
	// test_env_vars is a Hash from template variable names to one of the
	// following symbols:
	//  - PROJECT_NAME
	//  - CREDENTIALS
	//  - REGION
	//  - ORG_ID
	//  - ORG_TARGET
	//  - BILLING_ACCT
	//  - MASTER_BILLING_ACCT
	//  - SERVICE_ACCT
	//  - CUST_ID
	//  - IDENTITY_USER
	//  - CHRONICLE_ID
	//  - VMWAREENGINE_PROJECT
	// This list corresponds to the `get*FromEnv` methods in provider_test.go.
	TestEnvVars map[string]string `yaml:"test_env_vars,omitempty"`

	// Hash to provider custom override values for generating test config
	// If field my-var is set in this hash, it will replace vars[my-var] in
	// tests. i.e. if vars["network"] = "my-vpc", without override:
	//   - doc config will have `network = "my-vpc"`
	//   - tests config will have `"network = my-vpc%{random_suffix}"`
	//     with context
	//       map[string]interface{}{
	//         "random_suffix": acctest.RandString()
	//       }
	//
	// If test_vars_overrides["network"] = "nameOfVpc()"
	//   - doc config will have `network = "my-vpc"`
	//   - tests will replace with `"network = %{network}"` with context
	//       map[string]interface{}{
	//         "network": nameOfVpc
	//         ...
	//       }
	TestVarsOverrides map[string]string `yaml:"test_vars_overrides,omitempty"`

	// Hash to provider custom override values for generating oics config
	// See test_vars_overrides for more details
	OicsVarsOverrides map[string]string `yaml:"oics_vars_overrides,omitempty"`

	// The version name of of the example's version if it's different than the
	// resource version, eg. `beta`
	//
	// This should be the highest version of all the features used in the
	// example; if there's a single beta field in an example, the example's
	// min_version is beta. This is only needed if an example uses features
	// with a different version than the resource; a beta resource's examples
	// are all automatically versioned at beta.
	//
	// When an example has a version of beta, each resource must use the
	// `google-beta` provider in the config. If the `google` provider is
	// implicitly used, the test will fail.
	//
	// NOTE: Until Terraform 0.12 is released and is used in the OiCS tests, an
	// explicit provider block should be defined. While the tests @ 0.12 will
	// use `google-beta` automatically, past Terraform versions required an
	// explicit block.
	MinVersion string `yaml:"min_version,omitempty"`

	// Extra properties to ignore read on during import.
	// These properties will likely be custom code.
	IgnoreReadExtra []string `yaml:"ignore_read_extra,omitempty"`

	// Whether to skip generating tests for this resource
	ExcludeTest bool `yaml:"exclude_test,omitempty"`

	// Whether to skip generating docs for this example
	ExcludeDocs bool `yaml:"exclude_docs,omitempty"`

	// Whether to skip import tests for this example
	ExcludeImportTest bool `yaml:"exclude_import_test,omitempty"`

	// The name of the primary resource for use in IAM tests. IAM tests need
	// a reference to the primary resource to create IAM policies for
	PrimaryResourceName string `yaml:"primary_resource_name,omitempty"`

	// The name of the location/region override for use in IAM tests. IAM
	// tests may need this if the location is not inherited on the resource
	// for one reason or another
	RegionOverride string `yaml:"region_override,omitempty"`

	// The path to this example's Terraform config.
	// Defaults to `templates/terraform/examples/{{name}}.tf.erb`
	ConfigPath string `yaml:"config_path,omitempty"`

	// If the example should be skipped during VCR testing.
	// This is the case when something about the resource or config causes VCR to fail for example
	// a resource with a unique identifier generated within the resource via id.UniqueId()
	// Or a config with two fine grained resources that have a race condition during create
	SkipVcr bool `yaml:"skip_vcr,omitempty"`

	// The reason to skip a test. For example, a link to a ticket explaining the issue that needs to be resolved before
	// unskipping the test. If this is not empty, the test will be skipped.
	SkipTest string `yaml:"skip_test,omitempty"`

	// Specify which external providers are needed for the testcase.
	// Think before adding as there is latency and adds an external dependency to
	// your test so avoid if you can.
	ExternalProviders []string `yaml:"external_providers,omitempty"`

	DocumentationHCLText string `yaml:"-"`
	TestHCLText          string `yaml:"-"`
	OicsHCLText          string `yaml:"-"`

	// ====================
	// TGC
	// ====================
	// Extra properties to ignore test.
	// These properties are present in Terraform resources schema, but not in CAI assets.
	// Virtual Fields and url parameters are already ignored by default and do not need to be duplicated here.
	TGCTestIgnoreExtra []string `yaml:"tgc_test_ignore_extra,omitempty"`
	// The properties ignored in CAI assets. It is rarely used and only used
	// when the nested field has sent_empty_value: true.
	// But its parent field is C + O and not specified in raw_config.
	// Example: ['RESOURCE.cdnPolicy.signedUrlCacheMaxAgeSec'].
	// "RESOURCE" means that the property is for resource data in CAI asset.
	TGCTestIgnoreInAsset []string `yaml:"tgc_test_ignore_in_asset,omitempty"`
	// The reason to skip a test. For example, a link to a ticket explaining the issue that needs to be resolved before
	// unskipping the test. If this is not empty, the test will be skipped.
	TGCSkipTest string `yaml:"tgc_skip_test,omitempty"`
}

// Set default value for fields
func (e *Examples) UnmarshalYAML(unmarshal func(any) error) error {
	type exampleAlias Examples
	aliasObj := (*exampleAlias)(e)

	err := unmarshal(aliasObj)
	if err != nil {
		return err
	}

	if e.ConfigPath == "" {
		e.ConfigPath = fmt.Sprintf("templates/terraform/examples/%s.tf.tmpl", e.Name)
	}
	e.SetHCLText()

	return nil
}

func (e *Examples) Validate(rName string) {
	if e.Name == "" {
		log.Fatalf("Missing `name` for one example in resource %s", rName)
	}
	e.ValidateExternalProviders()
}

func validateRegexForContents(r *regexp.Regexp, contents string, configPath string, objName string, vars map[string]string) {
	matches := r.FindAllStringSubmatch(contents, -1)
	for _, v := range matches {
		found := false
		for k, _ := range vars {
			if k == v[1] {
				found = true
				break
			}
		}
		if !found {
			log.Fatalf("Failed to find %s environment variable defined in YAML file when validating the file %s. Please define this in %s", v[1], configPath, objName)
		}
	}
}

func (e *Examples) ValidateExternalProviders() {
	// Official providers supported by HashiCorp
	// https://registry.terraform.io/search/providers?namespace=hashicorp&tier=official
	HASHICORP_PROVIDERS := []string{"aws", "random", "null", "template", "azurerm", "kubernetes", "local",
		"external", "time", "vault", "archive", "tls", "helm", "azuread", "http", "cloudinit", "tfe", "dns",
		"consul", "vsphere", "nomad", "awscc", "googleworkspace", "hcp", "boundary", "ad", "azurestack", "opc",
		"oraclepaas", "hcs", "salesforce"}

	var unallowedProviders []string
	for _, p := range e.ExternalProviders {
		if !slices.Contains(HASHICORP_PROVIDERS, p) {
			unallowedProviders = append(unallowedProviders, p)
		}
	}

	if len(unallowedProviders) > 0 {
		log.Fatalf("Providers %#v are not allowed. Only providers published by HashiCorp are allowed.", unallowedProviders)
	}
}

// Executes example templates for documentation and tests
func (e *Examples) SetHCLText() {
	originalVars := e.Vars
	originalTestEnvVars := e.TestEnvVars
	docTestEnvVars := make(map[string]string)
	docs_defaults := map[string]string{
		"PROJECT_NAME":         "my-project-name",
		"PROJECT_NUMBER":       "1111111111111",
		"CREDENTIALS":          "my/credentials/filename.json",
		"REGION":               "us-west1",
		"ORG_ID":               "123456789",
		"ORG_DOMAIN":           "example.com",
		"ORG_TARGET":           "123456789",
		"BILLING_ACCT":         "000000-0000000-0000000-000000",
		"MASTER_BILLING_ACCT":  "000000-0000000-0000000-000000",
		"SERVICE_ACCT":         "my@service-account.com",
		"CUST_ID":              "A01b123xz",
		"IDENTITY_USER":        "cloud_identity_user",
		"PAP_DESCRIPTION":      "description",
		"CHRONICLE_ID":         "00000000-0000-0000-0000-000000000000",
		"VMWAREENGINE_PROJECT": "my-vmwareengine-project",
	}

	// Apply doc defaults to test_env_vars from YAML
	for key := range e.TestEnvVars {
		docTestEnvVars[key] = docs_defaults[e.TestEnvVars[key]]
	}
	e.TestEnvVars = docTestEnvVars
	e.DocumentationHCLText = e.ExecuteTemplate()
	e.DocumentationHCLText = regexp.MustCompile(`\n\n$`).ReplaceAllString(e.DocumentationHCLText, "\n")

	// Remove region tags
	re1 := regexp.MustCompile(`# \[[a-zA-Z_ ]+\]\n`)
	re2 := regexp.MustCompile(`\n# \[[a-zA-Z_ ]+\]`)
	e.DocumentationHCLText = re1.ReplaceAllString(e.DocumentationHCLText, "")
	e.DocumentationHCLText = re2.ReplaceAllString(e.DocumentationHCLText, "")

	testVars := make(map[string]string)
	testTestEnvVars := make(map[string]string)
	// Override vars to inject test values into configs - will have
	//   - "a-example-var-value%{random_suffix}""
	//   - "%{my_var}" for overrides that have custom Golang values
	for key, value := range originalVars {
		var newVal string
		if strings.Contains(value, "-") {
			newVal = fmt.Sprintf("tf-test-%s", value)
		} else if strings.Contains(value, "_") {
			newVal = fmt.Sprintf("tf_test_%s", value)
		} else {
			// Some vars like descriptions shouldn't have prefix
			newVal = value
		}
		// Random suffix is 10 characters and standard name length <= 64
		if len(newVal) > 54 {
			newVal = newVal[:54]
		}
		testVars[key] = fmt.Sprintf("%s%%{random_suffix}", newVal)
	}

	// Apply overrides from YAML
	for key := range e.TestVarsOverrides {
		testVars[key] = fmt.Sprintf("%%{%s}", key)
	}
	for key := range originalTestEnvVars {
		testTestEnvVars[key] = fmt.Sprintf("%%{%s}", key)
	}

	e.Vars = testVars
	e.TestEnvVars = testTestEnvVars
	e.TestHCLText = e.ExecuteTemplate()
	e.TestHCLText = regexp.MustCompile(`\n\n$`).ReplaceAllString(e.TestHCLText, "\n")
	// Remove region tags
	e.TestHCLText = re1.ReplaceAllString(e.TestHCLText, "")
	e.TestHCLText = re2.ReplaceAllString(e.TestHCLText, "")
	e.TestHCLText = SubstituteTestPaths(e.TestHCLText)

	// Reset the example
	e.Vars = originalVars
	e.TestEnvVars = originalTestEnvVars
}

func (e *Examples) ExecuteTemplate() string {
	templateContent, err := os.ReadFile(e.ConfigPath)
	if err != nil {
		glog.Exit(err)
	}

	fileContentString := string(templateContent)

	// Check that any variables in Vars or TestEnvVars used in the example are defined via YAML
	envVarRegex := regexp.MustCompile(`{{index \$\.TestEnvVars "([a-zA-Z_]*)"}}`)
	validateRegexForContents(envVarRegex, fileContentString, e.ConfigPath, "test_env_vars", e.TestEnvVars)
	varRegex := regexp.MustCompile(`{{index \$\.Vars "([a-zA-Z_]*)"}}`)
	validateRegexForContents(varRegex, fileContentString, e.ConfigPath, "vars", e.Vars)

	templateFileName := filepath.Base(e.ConfigPath)

	tmpl, err := template.New(templateFileName).Funcs(google.TemplateFunctions).Parse(fileContentString)
	if err != nil {
		glog.Exit(err)
	}

	contents := bytes.Buffer{}
	if err = tmpl.ExecuteTemplate(&contents, templateFileName, e); err != nil {
		glog.Exit(err)
	}

	rs := contents.String()

	if !strings.HasSuffix(rs, "\n") {
		rs = fmt.Sprintf("%s\n", rs)
	}

	return rs
}

func (e *Examples) OiCSLink() string {
	v := url.Values{}
	v.Add("cloudshell_git_repo", "https://github.com/terraform-google-modules/docs-examples.git")
	v.Add("cloudshell_working_dir", e.Name)
	v.Add("cloudshell_image", "gcr.io/cloudshell-images/cloudshell:latest")
	v.Add("open_in_editor", "main.tf")
	v.Add("cloudshell_print", "./motd")
	v.Add("cloudshell_tutorial", "./tutorial.md")
	u := url.URL{
		Scheme:   "https",
		Host:     "console.cloud.google.com",
		Path:     "/cloudshell/open",
		RawQuery: v.Encode(),
	}
	return u.String()
}

func (e *Examples) TestSlug(productName, resourceName string) string {
	ret := fmt.Sprintf("%s%s_%sExample", productName, resourceName, google.Camelize(e.Name, "lower"))
	return ret
}

func (e *Examples) ResourceType(terraformName string) string {
	if e.PrimaryResourceType != "" {
		return e.PrimaryResourceType
	}
	return terraformName
}

func SubstituteExamplePaths(config string) string {
	config = strings.ReplaceAll(config, "../static/img/header-logo.png", "../static/header-logo.png")
	config = strings.ReplaceAll(config, "path/to/private.key", "../static/ssl_cert/test.key")
	config = strings.ReplaceAll(config, "path/to/id_rsa.pub", "../static/ssh_rsa.pub")
	config = strings.ReplaceAll(config, "path/to/certificate.crt", "../static/ssl_cert/test.crt")
	return config
}

func SubstituteTestPaths(config string) string {
	config = strings.ReplaceAll(config, "../static/img/header-logo.png", "test-fixtures/header-logo.png")
	config = strings.ReplaceAll(config, "path/to/private.key", "test-fixtures/test.key")
	config = strings.ReplaceAll(config, "path/to/certificate.crt", "test-fixtures/test.crt")
	config = strings.ReplaceAll(config, "path/to/index.zip", "%{zip_path}")
	config = strings.ReplaceAll(config, "verified-domain.com", "tf-test-domain%{random_suffix}.gcp.tfacc.hashicorptest.com")
	config = strings.ReplaceAll(config, "path/to/id_rsa.pub", "test-fixtures/ssh_rsa.pub")
	return config
}

// Executes example templates for documentation and tests
func (e *Examples) SetOiCSHCLText() {
	originalVars := e.Vars
	originalTestEnvVars := e.TestEnvVars

	// // Remove region tags
	re1 := regexp.MustCompile(`# \[[a-zA-Z_ ]+\]\n`)
	re2 := regexp.MustCompile(`\n# \[[a-zA-Z_ ]+\]`)

	testVars := make(map[string]string)
	for key, value := range originalVars {
		testVars[key] = fmt.Sprintf("%s-${local.name_suffix}", value)
	}

	// Apply overrides from YAML
	for key, value := range e.OicsVarsOverrides {
		testVars[key] = value
	}

	e.Vars = testVars
	e.OicsHCLText = e.ExecuteTemplate()
	e.OicsHCLText = regexp.MustCompile(`\n\n$`).ReplaceAllString(e.OicsHCLText, "\n")

	// Remove region tags
	e.OicsHCLText = re1.ReplaceAllString(e.OicsHCLText, "")
	e.OicsHCLText = re2.ReplaceAllString(e.OicsHCLText, "")
	e.OicsHCLText = SubstituteExamplePaths(e.OicsHCLText)

	// Reset the example
	e.Vars = originalVars
	e.TestEnvVars = originalTestEnvVars
}
