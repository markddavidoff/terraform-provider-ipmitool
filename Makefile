SECRETS_FILE := secrets/idrac.enc.env
SECRETS_EXAMPLE := secrets/idrac.env.example

.PHONY: help
help:
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

## --- provider ---

PROVIDER_NAMESPACE := markddavidoff
PROVIDER_NAME      := ipmitool
PROVIDER_VERSION   := 0.1.0
PROVIDER_BINARY    := terraform-provider-$(PROVIDER_NAME)
PLUGIN_OS_ARCH     := $(shell go env GOOS)_$(shell go env GOARCH)

.PHONY: build
build: ## Build the provider binary
	@go build -o $(PROVIDER_BINARY) .

.PHONY: test
test: ## Run unit tests
	@go test ./internal/...

.PHONY: test-verbose
test-verbose: ## Run unit tests verbosely
	@go test -v ./internal/...

.PHONY: testacc
testacc: ## Run acceptance tests against real BMC (needs $(SECRETS_FILE), TF_ACC=1)
	@sops exec-env $(SECRETS_FILE) 'TF_ACC=1 TF_VAR_ipmi_host=$$IPMI_HOST TF_VAR_ipmi_user=$$IPMI_USER TF_VAR_ipmi_pass=$$IPMI_PASS go test -v -timeout 5m -run TestAcc ./internal/provider/...'

.PHONY: tidy
tidy: ## go mod tidy
	@go mod tidy

## --- ci gates ---

.PHONY: ci
ci: vet staticcheck test vulncheck gitleaks docs-validate ## Run all PR-time gates (matches the CI workflow)

.PHONY: vet
vet: ## go vet ./...
	@go vet ./...

.PHONY: staticcheck
staticcheck: ## staticcheck ./...
	@go run honnef.co/go/tools/cmd/staticcheck ./...

.PHONY: vulncheck
vulncheck: ## govulncheck ./...
	@go run golang.org/x/vuln/cmd/govulncheck ./...

.PHONY: gitleaks
gitleaks: ## Scan for committed secrets (requires gitleaks installed)
	@command -v gitleaks >/dev/null 2>&1 || { echo "gitleaks not installed — brew install gitleaks"; exit 1; }
	@gitleaks detect --source . --redact --verbose --no-banner

.PHONY: docs-validate
docs-validate: ## Validate Registry docs match schema
	@go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs validate

.PHONY: docs
docs: ## Regenerate Registry docs from schema descriptions (tfplugindocs)
	@go generate ./...

.PHONY: install-local
install-local: build ## Install the provider to ~/.terraform.d for local TF testing
	@mkdir -p ~/.terraform.d/plugins/registry.terraform.io/$(PROVIDER_NAMESPACE)/$(PROVIDER_NAME)/$(PROVIDER_VERSION)/$(PLUGIN_OS_ARCH)
	@cp $(PROVIDER_BINARY) ~/.terraform.d/plugins/registry.terraform.io/$(PROVIDER_NAMESPACE)/$(PROVIDER_NAME)/$(PROVIDER_VERSION)/$(PLUGIN_OS_ARCH)/
	@echo "installed → ~/.terraform.d/plugins/registry.terraform.io/$(PROVIDER_NAMESPACE)/$(PROVIDER_NAME)/$(PROVIDER_VERSION)/$(PLUGIN_OS_ARCH)/"

## --- secrets ---

.PHONY: secrets-init
secrets-init: ## Create encrypted secrets file from example (prompts $$EDITOR after copy)
	@if [ -f $(SECRETS_FILE) ]; then \
		echo "$(SECRETS_FILE) already exists. Use 'make secrets-edit' instead."; exit 1; \
	fi
	@cp $(SECRETS_EXAMPLE) secrets/idrac.env
	@$${EDITOR:-vi} secrets/idrac.env
	@sops --encrypt --input-type dotenv --output-type dotenv \
		secrets/idrac.env > $(SECRETS_FILE)
	@rm secrets/idrac.env
	@echo "Encrypted to $(SECRETS_FILE)"

.PHONY: secrets-edit
secrets-edit: ## Edit encrypted secrets in place (sops opens $$EDITOR with decrypted view)
	@sops $(SECRETS_FILE)

.PHONY: secrets-show
secrets-show: ## Print decrypted secrets to stdout (DO NOT PIPE TO FILE)
	@sops --decrypt $(SECRETS_FILE)

.PHONY: secrets-set-one
secrets-set-one: ## Set/update one secret. Usage: make secrets-set-one KEY=IPMI_PASS
	@test -n "$(KEY)" || { echo "Usage: make secrets-set-one KEY=<NAME>"; exit 1; }
	@scripts/secrets-set-one.sh $(KEY) $(SECRETS_FILE)

.PHONY: secrets-rotate
secrets-rotate: ## Re-encrypt with current .sops.yaml recipient list
	@sops updatekeys $(SECRETS_FILE)

