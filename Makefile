# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION ?= $(shell go list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d.%d",$$3, $$2}')

.PHONY: all

all: build

build: ## Build the metaldhcp binary.
	CGO_ENABLED=0 go build -o bin/metaldhcp .

clean: ## Remove built binary.
	rm -f bin/metaldhcp

.PHONY: docker-build
docker-build: ## Build docker image.
	docker build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image.
	docker push ${IMG}

.PHONY: fmt
fmt: goimports ## Run goimports against code.
	$(GOIMPORTS) -w .

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: add-license
add-license: addlicense ## Add license headers to all go files.
	find . -name '*.go' -exec "$(ADDLICENSE)" -f hack/license-header.txt {} +

.PHONY: check-license
check-license: addlicense ## Check that every file has a license header present.
	find . -name '*.go' -exec "$(ADDLICENSE)" -check -c 'SAP SE or an SAP affiliate company and IronCore contributors' {} +

lint: golangci-lint ## Run golangci-lint against code.
	$(GOLANGCI_LINT) run ./...

.PHONY: generate
generate: controller-gen goimports ## Generate DeepCopy methods.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."
	$(GOIMPORTS) -w .

.PHONY: manifests
manifests: controller-gen ## Generate CRD manifests.
	$(CONTROLLER_GEN) crd paths="./api/..." output:crd:artifacts:config=config/crd/bases

.PHONY: check-gen
check-gen: generate manifests docs fmt ## Run code generation, manifests generation, and formatting checks.

.PHONY: docs
docs: crd-ref-docs ## Generate CRD API reference docs.
	$(CRD_REF_DOCS) --source-path=./api --renderer=markdown --output-path=docs/api.md --config=hack/crd-ref-docs.yaml

.PHONY: test
test: generate manifests fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./... -coverprofile cover.out

##@ Dev environment

.PHONY: chart-lint
chart-lint: ## Lint and render the Helm chart.
	helm lint ./chart/metaldhcp
	helm lint ./chart/metaldhcp -f dev/values.yaml
	helm template metaldhcp ./chart/metaldhcp > /dev/null
	helm template metaldhcp ./chart/metaldhcp -f dev/values.yaml > /dev/null

.PHONY: test-dhcp
test-dhcp: ## Run all dev DHCP test scenarios against the local kind cluster.
	./dev/test-dhcp.sh

KIND_CLUSTER_NAME ?= metaldhcp
KIND_REGISTRY_PORT ?= 5001

.PHONY: kind-create
kind-create: ## Create the metaldhcp kind cluster (with local registry) if it does not exist.
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) KIND_REGISTRY_PORT=$(KIND_REGISTRY_PORT) KUBECTL=$(KUBECTL) \
	  ./hack/kind-with-registry.sh

.PHONY: kind-delete
kind-delete: ## Delete the metaldhcp kind cluster and its local registry.
	kind delete cluster --name=$(KIND_CLUSTER_NAME)
	docker stop kind-registry-metaldhcp && docker rm kind-registry-metaldhcp || true

.PHONY: tilt-up
tilt-up: kind-create ## Start Tilt (creates cluster if needed).
	tilt up --context kind-$(KIND_CLUSTER_NAME)

.PHONY: tilt-down
tilt-down: ## Tear down Tilt resources (does not delete the cluster).
	tilt down --context kind-$(KIND_CLUSTER_NAME)

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen-$(CONTROLLER_TOOLS_VERSION)
ENVTEST ?= $(LOCALBIN)/setup-envtest-$(ENVTEST_VERSION)
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)
GOIMPORTS ?= $(LOCALBIN)/goimports-$(GOIMPORTS_VERSION)
ADDLICENSE ?= $(LOCALBIN)/addlicense
CRD_REF_DOCS ?= $(LOCALBIN)/crd-ref-docs-$(CRD_REF_DOCS_VERSION)

## Tool Versions
CONTROLLER_TOOLS_VERSION ?= v0.21.0
ENVTEST_VERSION ?= latest
GOLANGCI_LINT_VERSION ?= v2.11.0
GOIMPORTS_VERSION ?= v0.38.0
ADDLICENSE_VERSION ?= v1.1.1
CRD_REF_DOCS_VERSION ?= v0.3.0

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,${GOLANGCI_LINT_VERSION})

.PHONY: goimports
goimports: $(GOIMPORTS) ## Download goimports locally if necessary.
$(GOIMPORTS): $(LOCALBIN)
	$(call go-install-tool,$(GOIMPORTS),golang.org/x/tools/cmd/goimports,$(GOIMPORTS_VERSION))

.PHONY: addlicense
addlicense: $(ADDLICENSE) ## Download addlicense locally if necessary.
$(ADDLICENSE): $(LOCALBIN)
	$(call go-install-tool,$(ADDLICENSE),github.com/google/addlicense,$(ADDLICENSE_VERSION))

.PHONY: crd-ref-docs
crd-ref-docs: $(CRD_REF_DOCS) ## Download crd-ref-docs locally if necessary.
$(CRD_REF_DOCS): $(LOCALBIN)
	$(call go-install-tool,$(CRD_REF_DOCS),github.com/elastic/crd-ref-docs,$(CRD_REF_DOCS_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary (ideally with version)
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f $(1) ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv "$$(echo "$(1)" | sed "s/-$(3)$$//")" $(1) ;\
}
endef
