/*
 * Copyright (c) HashiCorp, Inc.
 * SPDX-License-Identifier: MPL-2.0
 */

// This file is maintained in the GoogleCloudPlatform/magic-modules repository and copied into the downstream provider repositories. Any changes to this file in the downstream will be overwritten.

package projects

import ProviderNameGa
import builds.*
import jetbrains.buildServer.configs.kotlin.Project
import projects.reused.mmUpstream
import projects.reused.nightlyTests
import replaceCharsId
import vcs_roots.HashiCorpVCSRootGa
import vcs_roots.ModularMagicianVCSRootGa

// googleSubProjectGa returns a subproject that is used for testing terraform-provider-google (GA)
fun googleSubProjectGa(allConfig: AllContextParameters): Project {

    val gaId = replaceCharsId("GOOGLE")

    // Get config for using the GA and VCR identities
    val gaConfig = getGaAcceptanceTestConfig(allConfig)
    val vcrConfig = getVcrAcceptanceTestConfig(allConfig)

    return Project{
        id(gaId)
        name = "Google"
        description = "Subproject containing builds for testing the GA version of the Google provider"

        // Nightly Test project that uses hashicorp/terraform-provider-google
        subProject(nightlyTests(gaId, ProviderNameGa, HashiCorpVCSRootGa, gaConfig, NightlyTriggerConfiguration(daysOfWeek="1-3,5-7"))) // All nights except Wednesday (4) for GA; feature branch testing happens on Wednesday and TeamCity numbers days Sun=1...Sat=7

        // MM Upstream project that uses modular-magician/terraform-provider-google
        subProject(mmUpstream(gaId, ProviderNameGa, ModularMagicianVCSRootGa, HashiCorpVCSRootGa, vcrConfig, NightlyTriggerConfiguration()))

        params {
            readOnlySettings()
        }
    }
}