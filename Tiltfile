# SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
# SPDX-License-Identifier: Apache-2.0
#
# Dev environment: metaldhcp on a local kind cluster.
# Bring-up:  make tilt-up
# Manual trigger and observation are documented in dev/README.md.

allow_k8s_contexts("kind-metaldhcp")

# Use the local registry created by hack/kind-with-registry.sh when available.
reg_port = "5001"
reg_host = "localhost:%s" % reg_port
def image_ref(name):
    return "%s/%s" % (reg_host, name)

# --- CRDs (owned by metaldhcp) --------------------------------------------------
# CRDs are included via --include-crds in the helm() call below.
# Naming them here enables resource_deps ordering on "crds".
k8s_resource(
    new_name="crds",
    objects=[
        "oobsubnets.dhcp.metal.ironcore.dev:customresourcedefinition",
        "dhcpleases.dhcp.metal.ironcore.dev:customresourcedefinition",
    ],
)

# --- metaldhcp (built in-container from source) ---------------------------------
arch = str(local("go env GOARCH", quiet=True)).strip()
docker_build(
    image_ref("metaldhcp"),
    ".",
    dockerfile="Dockerfile.dev",
    build_args={"TARGETARCH": arch, "TARGETOS": "linux"},
)
k8s_yaml(helm(
    "./chart/metaldhcp",
    values=["./dev/values.yaml"],
))
k8s_resource("metaldhcp", resource_deps=["crds"])
