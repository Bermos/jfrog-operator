# Image to build and deploy.
IMG ?= ghcr.io/bermos/jfrog-operator:latest

# Tool versions.
CONTROLLER_TOOLS_VERSION ?= v0.23.0
ENVTEST_K8S_VERSION ?= 1.37.0
ENVTEST_VERSION ?= release-0.25
GOLANGCI_LINT_VERSION ?= v2.15.0
KUSTOMIZE_VERSION ?= v5.8.3
HELM_VERSION ?= v3.22.1
KIND_VERSION ?= v0.33.0

LOCALBIN ?= $(shell pwd)/bin
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint
KUSTOMIZE ?= $(LOCALBIN)/kustomize
HELM ?= $(LOCALBIN)/helm
KIND ?= $(LOCALBIN)/kind

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Show this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: generate
generate: controller-gen ## Generate deepcopy code, CRDs and RBAC.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd paths="./..." output:crd:artifacts:config=config/crd/bases output:rbac:artifacts:config=config/rbac
	rm -f charts/jfrog-operator/crds/*.yaml
	cp config/crd/bases/*.yaml charts/jfrog-operator/crds/
	cp config/rbac/role.yaml charts/jfrog-operator/files/manager-role.yaml
	cp config/rbac/aggregate_roles.yaml charts/jfrog-operator/files/aggregate-roles.yaml

.PHONY: verify-generate
verify-generate: generate ## Fail if generated files are out of date.
	@test -z "$$(git status --porcelain -- api config charts)" || (git status --porcelain -- api config charts; git diff -- api config charts; echo "generated files are out of date, run 'make generate'"; exit 1)

.PHONY: fmt
fmt: ## Run go fmt.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet.
	go vet ./...

.PHONY: lint
lint: golangci-lint ## Run golangci-lint.
	$(GOLANGCI_LINT) run

.PHONY: test
test: envtest ## Run all tests, including the envtest integration suite.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
		go test -race -count=1 -coverpkg=./internal/... -coverprofile=cover.out ./...

.PHONY: helm-lint
helm-lint: helm ## Lint and render the Helm chart.
	$(HELM) lint charts/jfrog-operator
	$(HELM) template test charts/jfrog-operator --set defaultConnection.enabled=true > /dev/null

.PHONY: e2e
e2e: helm kind ## Run the end-to-end test in a kind cluster (needs docker and kubectl).
	PATH="$(LOCALBIN):$$PATH" test/e2e/run.sh

##@ Build

.PHONY: build
build: ## Build the manager binary.
	go build -o bin/manager ./cmd

.PHONY: run
run: ## Run the operator against the cluster in your kubeconfig.
	go run ./cmd

.PHONY: docker-build
docker-build: ## Build the operator image.
	docker build -t $(IMG) .

##@ Deployment

.PHONY: install
install: kustomize ## Install the CRDs into the cluster in your kubeconfig.
	$(KUSTOMIZE) build config/crd | kubectl apply --server-side -f -

.PHONY: uninstall
uninstall: kustomize ## Remove the CRDs from the cluster in your kubeconfig.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found -f -

.PHONY: deploy
deploy: kustomize ## Deploy the operator to the cluster in your kubeconfig.
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/default | kubectl apply --server-side -f -

.PHONY: undeploy
undeploy: kustomize ## Remove the operator from the cluster in your kubeconfig.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found -f -

##@ Tools

$(LOCALBIN):
	mkdir -p $(LOCALBIN)

.PHONY: controller-gen envtest golangci-lint kustomize helm kind
controller-gen: $(LOCALBIN)
	test -s $(CONTROLLER_GEN) || GOTOOLCHAIN=auto GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)
envtest: $(LOCALBIN)
	test -s $(ENVTEST) || GOTOOLCHAIN=auto GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@$(ENVTEST_VERSION)
golangci-lint: $(LOCALBIN)
	test -s $(GOLANGCI_LINT) || GOTOOLCHAIN=auto GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
kustomize: $(LOCALBIN)
	test -s $(KUSTOMIZE) || GOTOOLCHAIN=auto GOBIN=$(LOCALBIN) go install sigs.k8s.io/kustomize/kustomize/v5@$(KUSTOMIZE_VERSION)
helm: $(LOCALBIN)
	test -s $(HELM) || GOTOOLCHAIN=auto GOBIN=$(LOCALBIN) go install helm.sh/helm/v3/cmd/helm@$(HELM_VERSION)
kind: $(LOCALBIN)
	test -s $(KIND) || GOTOOLCHAIN=auto GOBIN=$(LOCALBIN) go install sigs.k8s.io/kind@$(KIND_VERSION)
