# SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
# SPDX-License-Identifier: MIT
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
k8s_yaml([
    "config/crd/bases/dhcp.metal.ironcore.dev_oobsubnets.yaml",
    "config/crd/bases/dhcp.metal.ironcore.dev_dhcpleases.yaml",
])
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
    build_args={"TARGETARCH": arch, "TARGETOS": "linux"},
)
k8s_yaml(helm(
    "./chart/metaldhcp",
    values=["./dev/values.yaml"],
))
k8s_resource("metaldhcp", resource_deps=["crds"])
local_resource(
    "metaldhcp-config-reload",
    cmd="kubectl rollout restart deployment/metaldhcp -n metaldhcp-system",
    deps=["dev/values.yaml", "chart/metaldhcp/templates/configmap.yaml"],
    resource_deps=["metaldhcp"],
)
