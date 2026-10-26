##@ Noah Local Development

# Local development loop for the Noah integration on a Kraken vCluster.
# Included by the root Makefile, so every path here is relative to the
# repository root, not to this file.
NOAH_LOCAL_DIR = tools/noah-local-dev
NOAH_LOCAL_STATE_DIR ?= .noah-local-dev
NOAH_LOCAL_DEPLOYMENT_ID_FILE = $(NOAH_LOCAL_STATE_DIR)/deployment-id
NOAH_LOCAL_KRAKEN_RUNTIME_FILE = $(NOAH_LOCAL_STATE_DIR)/kraken-runtime.json
NOAH_LOCAL_PORT_FORWARD_PID_FILE = $(NOAH_LOCAL_STATE_DIR)/noah-port-forward.pid
NOAH_LOCAL_PORT_FORWARD_LOG = $(NOAH_LOCAL_STATE_DIR)/noah-port-forward.log
NOAH_LOCAL_CHART = helm/charts/noah
NOAH_LOCAL_FIXTURES ?= $(NOAH_LOCAL_DIR)/fixtures/c3.yaml
NOAH_LOCAL_OPERATOR_CHART ?= helm-chart/splunk-operator
# Avoid stale or unauthenticated repositories in the developer's global Helm
# configuration.
NOAH_LOCAL_HELM_STATE_DIR ?= $(NOAH_LOCAL_STATE_DIR)/helm
NOAH_LOCAL_HELM_REPOSITORY_CONFIG ?= $(NOAH_LOCAL_HELM_STATE_DIR)/repositories.yaml
NOAH_LOCAL_HELM_REPOSITORY_CACHE ?= $(NOAH_LOCAL_HELM_STATE_DIR)/repository

NOAH_LOCAL_DEPLOYMENT_ID ?=
NOAH_LOCAL_CONTEXT ?= kraken
NOAH_LOCAL_KRAKEN_BASE_URL ?= https://kraken.cyclops-pdx10-play-payne.splunk8s.io
NOAH_LOCAL_KRAKEN_WAIT_TIMEOUT ?= 20m
# CI can set these optional fields so leaked deployments remain identifiable
# and expire even if the runner disappears. Local deployments remain untagged
# and use Kraken's normal lease policy unless the developer opts in.
NOAH_LOCAL_KRAKEN_TTL_SECONDS ?=
NOAH_LOCAL_KRAKEN_PURPOSE ?=
NOAH_LOCAL_KRAKEN_WORKFLOW_TAG ?=
NOAH_LOCAL_KRAKEN_PIPELINE_TAG ?=
NOAH_LOCAL_KRAKEN_SHA_TAG ?=
# Kraken's standalone SmartBus contract currently provisions the runtime
# ServiceAccount in splunk-operator. create-cluster rejects another namespace
# before applying fixtures so the AWS identity cannot be silently misbound.
NOAH_LOCAL_NAMESPACE ?= splunk-operator
NOAH_LOCAL_RELEASE ?= noah
NOAH_LOCAL_OPERATOR_RELEASE ?= splunk-operator
# Branch pipelines publish this exact commit-addressed image.
NOAH_LOCAL_OPERATOR_TAG ?= $(shell git rev-parse HEAD)
NOAH_LOCAL_OPERATOR_IMAGE ?= docker-test.repo.splunkdev.net/sok/splunk-operator:$(NOAH_LOCAL_OPERATOR_TAG)
NOAH_LOCAL_SPLUNK_IMAGE ?= $(RELATED_IMAGE_SPLUNK_ENTERPRISE)
# NOAH_LOCAL_PORT is threaded through the chart and port-forward, but the
# endpoint in $(NOAH_LOCAL_FIXTURES) is plain YAML and must be edited to match.
NOAH_LOCAL_PORT ?= 8443
NOAH_LOCAL_LICENSE_FILE ?=
NOAH_LOCAL_C3_NAME ?= c3
# Noah waits on PostgreSQL and Redis and runs migrations before it reports ready.
# Kraken supplies the S3/SQS runtime, so this workflow only binds PostgreSQL.
NOAH_LOCAL_DEPLOY_TIMEOUT ?= 10m
NOAH_LOCAL_HELM_ARGS ?=
# Helm 4's watcher can leave healthy ClusterIP Services at Unknown on Kraken.
# Retain Helm 3's polling behavior when Helm 4 is installed.
NOAH_LOCAL_HELM_WAIT_ARGS ?= $(shell \
	version="$$(helm version --template '{{.Version}}' 2>/dev/null || true)"; \
	if printf '%s' "$$version" | grep -q '^v4'; then \
		printf '%s' '--wait=legacy'; \
	else \
		printf '%s' '--wait'; \
	fi)
NOAH_LOCAL_OPERATOR_TIMEOUT ?= 5m
NOAH_LOCAL_OPERATOR_HELM_ARGS ?=
NOAH_LOCAL_GINKGO ?= $(GOBIN)/ginkgo
NOAH_LOCAL_GINKGO_ARGS ?=
NOAH_LOCAL_TEST_LABEL_FILTER ?= tier:noah-e2e && cloud:kraken
NOAH_LOCAL_READY_TIMEOUT ?= 30m
NOAH_LOCAL_DATA_PATH_TIMEOUT ?= 3m
# cert-manager issues the operator's webhook serving certificate. It installs as a
# separate release, before the operator chart: Helm validates a whole release
# against the API server up front, so one release cannot both create the
# cert-manager CRDs and use them.
# Keep this version in step with the dependency in the operator chart's Chart.yaml.
NOAH_LOCAL_CERT_MANAGER_VERSION ?= v1.21.3
NOAH_LOCAL_CERT_MANAGER_CHART ?= $(NOAH_LOCAL_OPERATOR_CHART)/charts/cert-manager-$(NOAH_LOCAL_CERT_MANAGER_VERSION).tgz
NOAH_LOCAL_CERT_MANAGER_RELEASE ?= cert-manager
NOAH_LOCAL_CERT_MANAGER_NAMESPACE ?= cert-manager
NOAH_LOCAL_CERT_MANAGER_TIMEOUT ?= 5m
# Where a locally run operator reads its webhook serving certificate from. Must
# match webhookCertDir in cmd/main.go.
NOAH_LOCAL_WEBHOOK_CERT_DIR ?= /tmp/k8s-webhook-server/serving-certs
# Named separately from the chart's own certificate and Secret. Both workflows
# share a namespace, and Helm refuses to adopt resources it does not own, so
# reusing the chart's names breaks a later in-cluster install.
NOAH_LOCAL_WEBHOOK_CERT_SECRET ?= splunk-operator-local-dev-webhook-cert
NOAH_LOCAL_WEBHOOK_CERTIFICATE ?= splunk-operator-local-dev-serving-cert

.PHONY: noah-local-c3-deploy
noah-local-c3-deploy: ## Deploy a complete C3 with an in-cluster operator, without running attached tests.
	# Keep provisioning ordered even when the caller enables parallel Make.
	@$(MAKE) --no-print-directory noah-local-cluster
	@$(MAKE) --no-print-directory install
	@$(MAKE) --no-print-directory noah-local-deploy
	@$(MAKE) --no-print-directory noah-local-operator-deploy
	@$(MAKE) --no-print-directory noah-local-fixtures

.PHONY: noah-local-c3-up
noah-local-c3-up: ## Create a complete C3 deployment with an in-cluster operator.
	@$(MAKE) --no-print-directory noah-local-c3-deploy
	@$(MAKE) --no-print-directory noah-local-ready
	@$(MAKE) --no-print-directory noah-local-aws-runtime
	@printf '\n%s\n\n' 'Noah C3 passed the Ginkgo readiness and Kraken AWS runtime scenarios.'

.PHONY: noah-local-up
noah-local-up: ## Prepare a C3 deployment for an operator running locally.
	# The context, CRDs, services, fixtures, certificate, and forward are ordered dependencies.
	@$(MAKE) --no-print-directory noah-local-cluster
	@$(MAKE) --no-print-directory install
	@$(MAKE) --no-print-directory noah-local-deploy
	@$(MAKE) --no-print-directory noah-local-fixtures
	@$(MAKE) --no-print-directory noah-local-webhook-certs
	@$(MAKE) --no-print-directory noah-local-port-forward
	@printf '\n%s\n\n' 'Noah is ready. Run the operator locally with:'
	@printf '    %s \\\n      %s \\\n      %s \\\n      %s\n\n' \
		'RELATED_IMAGE_SPLUNK_ENTERPRISE='"'"'<immutable Noah-capable Splunk image>'"'" \
		'SPLUNK_GENERAL_TERMS='"'"'<your accepted terms>'"'" \
		'WATCH_NAMESPACE=$(NOAH_LOCAL_NAMESPACE)' \
		'go run ./cmd/main.go'
	@printf '%s\n' \
		'RELATED_IMAGE_SPLUNK_ENTERPRISE must identify an immutable Noah-capable Splunk image.' \
		'SPLUNK_GENERAL_TERMS is deliberately not set for you: see docs/README.md.' \
		'Scale any in-cluster operator to 0 first, or both will reconcile at once.'

.PHONY: noah-local-down
noah-local-down: noah-local-stop-port-forward noah-local-destroy

.PHONY: noah-local-cluster
noah-local-cluster: ## Create and configure a Kraken vCluster.
	@mkdir -p "$(NOAH_LOCAL_STATE_DIR)"
	@set -eu; \
		deployment_id="$(NOAH_LOCAL_DEPLOYMENT_ID)"; \
		deployment_id_source=explicit; \
		if test -z "$$deployment_id" && test -s "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)"; then \
			deployment_id="$$(cat "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)")"; \
			deployment_id_source=saved; \
		fi; \
		if test -n "$$deployment_id"; then \
			if deployment_status="$$(kraken status --base-url "$(NOAH_LOCAL_KRAKEN_BASE_URL)" \
				"$$deployment_id" 2>&1)"; then \
				deployment_state="$$(printf '%s\n' "$$deployment_status" | \
					jq -er '.state | strings | select(length > 0)')" || { \
					printf 'Kraken returned an invalid status for deployment %s.\n' \
						"$$deployment_id" >&2; \
					exit 1; \
				}; \
				case "$$deployment_state" in \
					ready) ;; \
					terminating|terminated) \
						if test "$$deployment_id_source" = saved; then \
							deployment_id=''; \
						else \
							printf 'Requested Kraken deployment %s is %s and cannot be reused.\n' \
								"$$deployment_id" "$$deployment_state" >&2; \
							exit 1; \
						fi \
						;; \
					failed) \
						printf 'Kraken deployment %s is failed. Inspect `kraken status --base-url %s %s` and its events, then clean it up before retrying.\n' \
							"$$deployment_id" "$(NOAH_LOCAL_KRAKEN_BASE_URL)" "$$deployment_id" >&2; \
						exit 1 \
						;; \
					*) \
						printf 'Kraken deployment %s is %s; wait for it to become ready before retrying.\n' \
							"$$deployment_id" "$$deployment_state" >&2; \
						exit 1 \
						;; \
				esac; \
			else \
				case "$$deployment_status" in \
					*status=404*) \
						if test "$$deployment_id_source" = saved; then \
							deployment_id=''; \
						else \
							printf 'Requested Kraken deployment %s does not exist.\n' \
								"$$deployment_id" >&2; \
							exit 1; \
						fi \
						;; \
					*) printf '%s\n' "$$deployment_status" >&2; exit 1 ;; \
				esac; \
			fi; \
		fi; \
		deployment_id="$$( \
			NOAH_LOCAL_NAMESPACE="$(NOAH_LOCAL_NAMESPACE)" \
			NOAH_LOCAL_CONTEXT="$(NOAH_LOCAL_CONTEXT)" \
			NOAH_LOCAL_DEPLOYMENT_ID_FILE="$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)" \
			NOAH_LOCAL_KRAKEN_RUNTIME_FILE="$(NOAH_LOCAL_KRAKEN_RUNTIME_FILE)" \
			NOAH_LOCAL_KRAKEN_BASE_URL="$(NOAH_LOCAL_KRAKEN_BASE_URL)" \
			NOAH_LOCAL_KRAKEN_WAIT_TIMEOUT="$(NOAH_LOCAL_KRAKEN_WAIT_TIMEOUT)" \
			NOAH_LOCAL_KRAKEN_TTL_SECONDS="$(NOAH_LOCAL_KRAKEN_TTL_SECONDS)" \
			NOAH_LOCAL_KRAKEN_PURPOSE="$(NOAH_LOCAL_KRAKEN_PURPOSE)" \
			NOAH_LOCAL_KRAKEN_WORKFLOW_TAG="$(NOAH_LOCAL_KRAKEN_WORKFLOW_TAG)" \
			NOAH_LOCAL_KRAKEN_PIPELINE_TAG="$(NOAH_LOCAL_KRAKEN_PIPELINE_TAG)" \
			NOAH_LOCAL_KRAKEN_SHA_TAG="$(NOAH_LOCAL_KRAKEN_SHA_TAG)" \
			$(NOAH_LOCAL_DIR)/create-cluster "$$deployment_id")"; \
		test -n "$$deployment_id"; \
		printf '%s\n' "$$deployment_id"

.PHONY: noah-local-destroy
noah-local-destroy: ## Destroy the saved Kraken vCluster.
	@set -eu; \
		deployment_id="$(NOAH_LOCAL_DEPLOYMENT_ID)"; \
		if test -z "$$deployment_id" && test -s "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)"; then \
			deployment_id="$$(cat "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)")"; \
		fi; \
		if test -z "$$deployment_id"; then \
			printf '%s\n' 'No Kraken deployment found.'; \
			exit 0; \
		fi; \
		if delete_output="$$(kraken delete --base-url "$(NOAH_LOCAL_KRAKEN_BASE_URL)" \
			"$$deployment_id" --json 2>&1)"; then \
			printf '%s\n' "$$delete_output"; \
		else \
			case "$$delete_output" in \
				*status=404*) printf 'Kraken deployment %s no longer exists.\n' "$$deployment_id" ;; \
				*) printf '%s\n' "$$delete_output" >&2; exit 1 ;; \
			esac; \
		fi; \
		if test -s "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)" && \
			test "$$(cat "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)")" = "$$deployment_id"; then \
			rm -f "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)" "$(NOAH_LOCAL_KRAKEN_RUNTIME_FILE)"; \
		fi

.PHONY: noah-local-deployment-id
noah-local-deployment-id: ## Print the saved Kraken deployment ID.
	@test -s "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)" || { \
		printf '%s\n' 'No deployment ID found. Run `make noah-local-cluster` first.' >&2; \
		exit 1; \
	}
	@cat "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)"

.PHONY: noah-local-deploy
noah-local-deploy: ## Install or upgrade Noah, PostgreSQL and Redis in the vCluster.
	helm upgrade --install "$(NOAH_LOCAL_RELEASE)" "$(NOAH_LOCAL_CHART)" \
		--kube-context "$(NOAH_LOCAL_CONTEXT)" \
		--namespace "$(NOAH_LOCAL_NAMESPACE)" \
		--set-string fullnameOverride="$(NOAH_LOCAL_RELEASE)" \
		--set service.port=$(NOAH_LOCAL_PORT) \
		$(NOAH_LOCAL_HELM_WAIT_ARGS) --timeout $(NOAH_LOCAL_DEPLOY_TIMEOUT) $(NOAH_LOCAL_HELM_ARGS) \
		--set minio.enabled=false

.PHONY: noah-local-operator-chart-deps
noah-local-operator-chart-deps: ## Fetch the operator chart's subchart dependencies.
	@mkdir -p "$(NOAH_LOCAL_HELM_REPOSITORY_CACHE)"
	HELM_REPOSITORY_CONFIG="$(NOAH_LOCAL_HELM_REPOSITORY_CONFIG)" \
	HELM_REPOSITORY_CACHE="$(NOAH_LOCAL_HELM_REPOSITORY_CACHE)" \
		helm repo add jetstack https://charts.jetstack.io --force-update
	HELM_REPOSITORY_CONFIG="$(NOAH_LOCAL_HELM_REPOSITORY_CONFIG)" \
	HELM_REPOSITORY_CACHE="$(NOAH_LOCAL_HELM_REPOSITORY_CACHE)" \
		helm dependency build --skip-refresh "$(NOAH_LOCAL_OPERATOR_CHART)"

.PHONY: noah-local-cert-manager
noah-local-cert-manager: noah-local-operator-chart-deps ## Install cert-manager, which issues the operator's webhook certificate.
	@# Install the exact dependency archive already resolved for the operator
	@# chart. This avoids a second repository fetch and prevents version drift.
	helm upgrade --install "$(NOAH_LOCAL_CERT_MANAGER_RELEASE)" \
		"$(NOAH_LOCAL_CERT_MANAGER_CHART)" \
		--kube-context "$(NOAH_LOCAL_CONTEXT)" \
		--namespace "$(NOAH_LOCAL_CERT_MANAGER_NAMESPACE)" --create-namespace \
		--set crds.enabled=true \
		$(NOAH_LOCAL_HELM_WAIT_ARGS) --timeout "$(NOAH_LOCAL_CERT_MANAGER_TIMEOUT)"
	@# --wait covers the Deployments, but the webhook only admits Certificate
	@# resources once its own serving certificate is in place, so give the API
	@# server a moment to start routing to it before the operator chart applies one.
	kubectl --context "$(NOAH_LOCAL_CONTEXT)" \
		--namespace "$(NOAH_LOCAL_CERT_MANAGER_NAMESPACE)" \
		wait --for=condition=Available --timeout="$(NOAH_LOCAL_CERT_MANAGER_TIMEOUT)" \
		deployment/cert-manager deployment/cert-manager-cainjector deployment/cert-manager-webhook

.PHONY: noah-local-webhook-certs
noah-local-webhook-certs: noah-local-cert-manager ## Issue and copy out the webhook serving certificate for a locally run operator.
	@# The operator chart is not installed in this workflow, so nothing else
	@# creates a Certificate for cert-manager to act on.
	kubectl --context "$(NOAH_LOCAL_CONTEXT)" --namespace "$(NOAH_LOCAL_NAMESPACE)" \
		apply -f "$(NOAH_LOCAL_DIR)/local-operator-cert.yaml"
	kubectl --context "$(NOAH_LOCAL_CONTEXT)" --namespace "$(NOAH_LOCAL_NAMESPACE)" \
		wait --for=condition=Ready --timeout="$(NOAH_LOCAL_CERT_MANAGER_TIMEOUT)" \
		"certificate/$(NOAH_LOCAL_WEBHOOK_CERTIFICATE)"
	@# The certificate is only valid for the in-cluster Service DNS names, which is
	@# enough for the operator to start; exercising /convert needs the in-cluster
	@# workflow.
	@set -eu; \
		mkdir -p "$(NOAH_LOCAL_WEBHOOK_CERT_DIR)"; \
		kubectl --context "$(NOAH_LOCAL_CONTEXT)" --namespace "$(NOAH_LOCAL_NAMESPACE)" \
			get secret "$(NOAH_LOCAL_WEBHOOK_CERT_SECRET)" \
			--output "jsonpath={.data['tls\.crt']}" \
			| base64 -d > "$(NOAH_LOCAL_WEBHOOK_CERT_DIR)/tls.crt"; \
		kubectl --context "$(NOAH_LOCAL_CONTEXT)" --namespace "$(NOAH_LOCAL_NAMESPACE)" \
			get secret "$(NOAH_LOCAL_WEBHOOK_CERT_SECRET)" \
			--output "jsonpath={.data['tls\.key']}" \
			| base64 -d > "$(NOAH_LOCAL_WEBHOOK_CERT_DIR)/tls.key"; \
		chmod 600 "$(NOAH_LOCAL_WEBHOOK_CERT_DIR)/tls.key"; \
		printf 'Wrote tls.crt and tls.key to %s from Secret %s.\n' \
			"$(NOAH_LOCAL_WEBHOOK_CERT_DIR)" "$(NOAH_LOCAL_WEBHOOK_CERT_SECRET)"

.PHONY: noah-local-operator-deploy
noah-local-operator-deploy: noah-local-operator-chart-deps noah-local-cert-manager ## Install or upgrade a Noah-enabled operator in the vCluster.
	@set -eu; \
		test -n "$(NOAH_LOCAL_OPERATOR_IMAGE)" || { \
			printf '%s\n' 'Set NOAH_LOCAL_OPERATOR_IMAGE to an immutable staged operator image.' >&2; \
			exit 1; \
		}; \
		test -n "$(NOAH_LOCAL_SPLUNK_IMAGE)" || { \
			printf '%s\n' 'Set NOAH_LOCAL_SPLUNK_IMAGE to an immutable Noah-capable Splunk image.' >&2; \
			exit 1; \
		}; \
		test -n "$(SPLUNK_GENERAL_TERMS)" || { \
			printf '%s\n' 'Set SPLUNK_GENERAL_TERMS after following docs/README.md.' >&2; \
			exit 1; \
		}; \
		pull_secret="$$(kubectl --context "$(NOAH_LOCAL_CONTEXT)" --namespace "$(NOAH_LOCAL_NAMESPACE)" \
			get serviceaccount default --output json | \
			jq -er '[.imagePullSecrets[]?.name | select(startswith("kraken-artifactory-creds-"))] as $$names | if ($$names | length) == 1 then $$names[0] else error("expected exactly one Kraken image pull Secret reference") end')"; \
		helm upgrade --install "$(NOAH_LOCAL_OPERATOR_RELEASE)" "$(NOAH_LOCAL_OPERATOR_CHART)" \
			--kube-context "$(NOAH_LOCAL_CONTEXT)" \
			--namespace "$(NOAH_LOCAL_NAMESPACE)" \
			--set-string splunkOperator.nameOverride="$(NOAH_LOCAL_OPERATOR_RELEASE)" \
			--set-string splunkOperator.image.repository="$(NOAH_LOCAL_OPERATOR_IMAGE)" \
			--set splunkOperator.image.pullPolicy=Always \
			--set-string "splunkOperator.imagePullSecrets[0].name=$$pull_secret" \
			--set-string image.repository="$(NOAH_LOCAL_SPLUNK_IMAGE)" \
			--set splunkOperator.clusterWideAccess=false \
			--set-string splunkOperator.splunkGeneralTerms="$(SPLUNK_GENERAL_TERMS)" \
			--set-string splunkOperator.nodeSelector.workload=splunk \
			--set-string "splunkOperator.tolerations[0].key=workload" \
			--set-string "splunkOperator.tolerations[0].operator=Equal" \
			--set-string "splunkOperator.tolerations[0].value=splunk" \
			--set-string "splunkOperator.tolerations[0].effect=NoSchedule" \
			--set-string splunkOperator.persistentVolumeClaim.storageClassName=gp3-automode \
			$(NOAH_LOCAL_HELM_WAIT_ARGS) --timeout "$(NOAH_LOCAL_OPERATOR_TIMEOUT)" \
			$(NOAH_LOCAL_OPERATOR_HELM_ARGS)

.PHONY: noah-local-fixtures
noah-local-fixtures: noah-local-runtime-context ## Create prerequisite Secrets and apply the sample C3 custom resources.
	@set -eu; \
		if test -n "$(NOAH_LOCAL_LICENSE_FILE)"; then \
			test -f "$(NOAH_LOCAL_LICENSE_FILE)" || { \
				printf 'License file not found: %s\n' "$(NOAH_LOCAL_LICENSE_FILE)" >&2; \
				exit 1; \
			}; \
			kubectl --context "$(NOAH_LOCAL_CONTEXT)" --namespace "$(NOAH_LOCAL_NAMESPACE)" \
				create secret generic splunk-license \
				--from-file=enterprise.lic="$(NOAH_LOCAL_LICENSE_FILE)" \
				--dry-run=client --output yaml | \
			kubectl --context "$(NOAH_LOCAL_CONTEXT)" --namespace "$(NOAH_LOCAL_NAMESPACE)" \
				apply -f -; \
		elif ! kubectl --context "$(NOAH_LOCAL_CONTEXT)" --namespace "$(NOAH_LOCAL_NAMESPACE)" \
			get secret splunk-license >/dev/null 2>&1; then \
			printf '%s\n' \
				'splunk-license does not exist.' \
				'Pass NOAH_LOCAL_LICENSE_FILE=/absolute/path/to/enterprise.lic.' >&2; \
			exit 1; \
		fi
	$(NOAH_LOCAL_DIR)/create-auth-secret \
		"$(NOAH_LOCAL_CONTEXT)" "$(NOAH_LOCAL_NAMESPACE)" "noah-auth"
	$(NOAH_LOCAL_DIR)/create-splunk-secret \
		"$(NOAH_LOCAL_CONTEXT)" "$(NOAH_LOCAL_NAMESPACE)"
	@set -eu; \
		command -v envsubst >/dev/null 2>&1 || { \
			printf '%s\n' 'envsubst is required but was not found on PATH.' >&2; \
			exit 1; \
		}; \
		rendered="$$( \
			$(NOAH_LOCAL_DIR)/with-kraken-runtime "$(NOAH_LOCAL_KRAKEN_RUNTIME_FILE)" \
				envsubst '$${NOAH_SPLUNK_SERVICE_ACCOUNT} $${NOAH_SMARTSTORE_AUTH_REGION} $${NOAH_SMARTSTORE_OBJECT_STORE_PATH} $${NOAH_SMARTSTORE_OBJECT_STORE_ENDPOINT} $${NOAH_SMARTSTORE_QUEUE_ENDPOINT} $${NOAH_SMARTSTORE_QUEUE_URL}' \
				< "$(NOAH_LOCAL_FIXTURES)" \
		)"; \
		printf '%s\n' "$$rendered" | \
			kubectl --context "$(NOAH_LOCAL_CONTEXT)" --namespace "$(NOAH_LOCAL_NAMESPACE)" apply -f -

.PHONY: noah-local-ready
noah-local-ready: setup/ginkgo noah-local-runtime-context ## Run the Noah C3 Ginkgo readiness gate without changing indexed data.
	$(NOAH_LOCAL_DIR)/with-kraken-runtime "$(NOAH_LOCAL_KRAKEN_RUNTIME_FILE)" \
		env NOAH_TEST_NAMESPACE="$(NOAH_LOCAL_NAMESPACE)" \
		NOAH_TEST_OPERATOR_NAME="$(NOAH_LOCAL_OPERATOR_RELEASE)-controller-manager" \
		NOAH_TEST_NOAH_DEPLOYMENT="$(NOAH_LOCAL_RELEASE)" \
		NOAH_TEST_C3_NAME="$(NOAH_LOCAL_C3_NAME)" \
		NOAH_TEST_READY_TIMEOUT="$(NOAH_LOCAL_READY_TIMEOUT)" \
		"$(NOAH_LOCAL_GINKGO)" -v --trace \
		--label-filter='tier:noah-e2e && scenario:readiness' ./test/noah

.PHONY: noah-local-aws-runtime
noah-local-aws-runtime: setup/ginkgo noah-local-runtime-context ## Verify Kraken IRSA and SmartStore configuration on every Indexer.
	$(NOAH_LOCAL_DIR)/with-kraken-runtime "$(NOAH_LOCAL_KRAKEN_RUNTIME_FILE)" \
		env NOAH_TEST_NAMESPACE="$(NOAH_LOCAL_NAMESPACE)" \
		NOAH_TEST_OPERATOR_NAME="$(NOAH_LOCAL_OPERATOR_RELEASE)-controller-manager" \
		NOAH_TEST_NOAH_DEPLOYMENT="$(NOAH_LOCAL_RELEASE)" \
		NOAH_TEST_C3_NAME="$(NOAH_LOCAL_C3_NAME)" \
		NOAH_TEST_READY_TIMEOUT="$(NOAH_LOCAL_READY_TIMEOUT)" \
		"$(NOAH_LOCAL_GINKGO)" -v --trace \
		--label-filter='tier:noah-e2e && scenario:aws-runtime' ./test/noah

.PHONY: noah-local-data-path
noah-local-data-path: setup/ginkgo noah-local-runtime-context ## Verify per-indexer ingest and distributed search through the Noah-backed C3.
	$(NOAH_LOCAL_DIR)/with-kraken-runtime "$(NOAH_LOCAL_KRAKEN_RUNTIME_FILE)" \
		env NOAH_TEST_NAMESPACE="$(NOAH_LOCAL_NAMESPACE)" \
		NOAH_TEST_OPERATOR_NAME="$(NOAH_LOCAL_OPERATOR_RELEASE)-controller-manager" \
		NOAH_TEST_NOAH_DEPLOYMENT="$(NOAH_LOCAL_RELEASE)" \
		NOAH_TEST_C3_NAME="$(NOAH_LOCAL_C3_NAME)" \
		NOAH_TEST_READY_TIMEOUT="$(NOAH_LOCAL_READY_TIMEOUT)" \
		NOAH_TEST_DATA_PATH_TIMEOUT="$(NOAH_LOCAL_DATA_PATH_TIMEOUT)" \
		"$(NOAH_LOCAL_GINKGO)" -v --trace \
		--label-filter='tier:noah-e2e && scenario:data-path' ./test/noah

.PHONY: noah-local-test
noah-local-test: setup/ginkgo noah-local-runtime-context ## Run every attached Noah Kraken scenario in one Ginkgo suite.
	$(NOAH_LOCAL_DIR)/with-kraken-runtime "$(NOAH_LOCAL_KRAKEN_RUNTIME_FILE)" \
		env NOAH_TEST_NAMESPACE="$(NOAH_LOCAL_NAMESPACE)" \
		NOAH_TEST_OPERATOR_NAME="$(NOAH_LOCAL_OPERATOR_RELEASE)-controller-manager" \
		NOAH_TEST_NOAH_DEPLOYMENT="$(NOAH_LOCAL_RELEASE)" \
		NOAH_TEST_C3_NAME="$(NOAH_LOCAL_C3_NAME)" \
		NOAH_TEST_READY_TIMEOUT="$(NOAH_LOCAL_READY_TIMEOUT)" \
		NOAH_TEST_DATA_PATH_TIMEOUT="$(NOAH_LOCAL_DATA_PATH_TIMEOUT)" \
		"$(NOAH_LOCAL_GINKGO)" -v --trace \
		$(NOAH_LOCAL_GINKGO_ARGS) \
		--label-filter='$(NOAH_LOCAL_TEST_LABEL_FILTER)' ./test/noah

# Every attached Kraken scenario validates the same external S3/SQS contract.
.PHONY: noah-local-runtime-context
noah-local-runtime-context: noah-local-test-context ## Verify saved Kraken runtime metadata belongs to the active vCluster.
	@set -eu; \
		test -s "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)" || { \
			printf '%s\n' 'No Kraken deployment ID found. Run `make noah-local-cluster` first.' >&2; \
			exit 1; \
		}; \
		test -s "$(NOAH_LOCAL_KRAKEN_RUNTIME_FILE)" || { \
			printf '%s\n' 'No Kraken runtime metadata found. Run `make noah-local-cluster` first.' >&2; \
			exit 1; \
		}; \
		deployment_id="$$(cat "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)")"; \
		runtime_deployment_id="$$(jq -er '.deployment_id' "$(NOAH_LOCAL_KRAKEN_RUNTIME_FILE)")"; \
		test "$$runtime_deployment_id" = "$$deployment_id" || { \
			printf 'Kraken runtime metadata belongs to %s; expected %s.\n' \
				"$$runtime_deployment_id" "$$deployment_id" >&2; \
			exit 1; \
		}; \
		runtime_namespace="$$(jq -er '.service_account.namespace' "$(NOAH_LOCAL_KRAKEN_RUNTIME_FILE)")"; \
		test "$$runtime_namespace" = "$(NOAH_LOCAL_NAMESPACE)" || { \
			printf 'Kraken runtime ServiceAccount is in %s; expected %s.\n' \
				"$$runtime_namespace" "$(NOAH_LOCAL_NAMESPACE)" >&2; \
			exit 1; \
		}; \
		runtime_server="$$(jq -er '.cluster_server' "$(NOAH_LOCAL_KRAKEN_RUNTIME_FILE)")"; \
		current_server="$$(kubectl config view --minify --output 'jsonpath={.clusters[0].cluster.server}')"; \
		test -n "$$current_server" && test "$$runtime_server" = "$$current_server" || { \
			printf '%s\n' 'The active kraken context does not belong to the saved deployment.' >&2; \
			exit 1; \
		}; \
		$(NOAH_LOCAL_DIR)/with-kraken-runtime "$(NOAH_LOCAL_KRAKEN_RUNTIME_FILE)" true

.PHONY: noah-local-test-context
noah-local-test-context: ## Fail fast unless the active kubeconfig context is the saved Kraken context.
	@set -eu; \
		current_context="$$(kubectl config current-context 2>/dev/null || true)"; \
		if test "$$current_context" != "$(NOAH_LOCAL_CONTEXT)"; then \
			printf 'Active Kubernetes context is %s; expected %s.\n' \
				"$${current_context:-<unset>}" "$(NOAH_LOCAL_CONTEXT)" >&2; \
			printf 'Select it with: kubectl config use-context %s\n' "$(NOAH_LOCAL_CONTEXT)" >&2; \
			exit 1; \
		fi

.PHONY: noah-local-port-forward
noah-local-port-forward: ## Forward the Noah service so a locally run operator can reach it.
	@mkdir -p "$(NOAH_LOCAL_STATE_DIR)"
	@set -eu; \
		noah_host='$(NOAH_LOCAL_RELEASE).$(NOAH_LOCAL_NAMESPACE).svc'; \
		if ! grep -q "[[:space:]]$$noah_host\([[:space:]]\|$$\)" /etc/hosts 2>/dev/null; then \
			printf '%s\n' \
				"/etc/hosts has no entry for $$noah_host." \
				'A locally run operator resolves the NoahCluster endpoint through it:' \
				'' \
				"    printf '%s\\n' '127.0.0.1 $$noah_host' | sudo tee -a /etc/hosts" \
				'' >&2; \
		fi
	@set -eu; \
		if test -s "$(NOAH_LOCAL_PORT_FORWARD_PID_FILE)" && \
			kill -0 "$$(cat "$(NOAH_LOCAL_PORT_FORWARD_PID_FILE)")" 2>/dev/null; then \
			printf 'Noah port-forward is already running (PID %s).\n' \
				"$$(cat "$(NOAH_LOCAL_PORT_FORWARD_PID_FILE)")"; \
			exit 0; \
		fi; \
		nohup kubectl --context "$(NOAH_LOCAL_CONTEXT)" \
			--namespace "$(NOAH_LOCAL_NAMESPACE)" \
			port-forward "service/$(NOAH_LOCAL_RELEASE)" "$(NOAH_LOCAL_PORT):$(NOAH_LOCAL_PORT)" \
			> "$(NOAH_LOCAL_PORT_FORWARD_LOG)" 2>&1 </dev/null & \
		pid=$$!; \
		printf '%s\n' "$$pid" > "$(NOAH_LOCAL_PORT_FORWARD_PID_FILE)"; \
		sleep 1; \
		if ! kill -0 "$$pid" 2>/dev/null; then \
			cat "$(NOAH_LOCAL_PORT_FORWARD_LOG)" >&2; \
			rm -f "$(NOAH_LOCAL_PORT_FORWARD_PID_FILE)"; \
			exit 1; \
		fi; \
		printf 'Noah port-forward listening on localhost:%s (PID %s).\nLog: %s\n' \
			"$(NOAH_LOCAL_PORT)" "$$pid" "$(NOAH_LOCAL_PORT_FORWARD_LOG)"

.PHONY: noah-local-stop-port-forward
noah-local-stop-port-forward: ## Stop the Noah port-forward.
	@set -eu; \
		if ! test -s "$(NOAH_LOCAL_PORT_FORWARD_PID_FILE)"; then \
			printf '%s\n' 'No Noah port-forward is running.'; \
			exit 0; \
		fi; \
		pid="$$(cat "$(NOAH_LOCAL_PORT_FORWARD_PID_FILE)")"; \
		process_command="$$(ps -p "$$pid" -o command= 2>/dev/null || true)"; \
		case "$$process_command" in \
			*kubectl*'port-forward service/$(NOAH_LOCAL_RELEASE)'*) \
				kill "$$pid"; \
				printf 'Stopped Noah port-forward (PID %s).\n' "$$pid" ;; \
			*) printf '%s\n' 'No Noah port-forward is running.' ;; \
		esac; \
		rm -f "$(NOAH_LOCAL_PORT_FORWARD_PID_FILE)"

.PHONY: noah-local-lint
noah-local-lint: noah-local-operator-chart-deps ## Lint the Noah chart and local deployment helpers.
	helm lint $(NOAH_LOCAL_CHART)
	@set -eu; \
		default_render="$$(helm template $(NOAH_LOCAL_RELEASE) $(NOAH_LOCAL_CHART) \
			--namespace $(NOAH_LOCAL_NAMESPACE) \
			--set-string fullnameOverride=$(NOAH_LOCAL_RELEASE))"; \
		if ! printf '%s\n' "$$default_render" | grep -q 'app.kubernetes.io/component: minio'; then \
			printf '%s\n' 'MinIO resources did not render with the chart default.' >&2; \
			exit 1; \
		fi; \
		disabled_render="$$(helm template $(NOAH_LOCAL_RELEASE) $(NOAH_LOCAL_CHART) \
			--namespace $(NOAH_LOCAL_NAMESPACE) \
			--set-string fullnameOverride=$(NOAH_LOCAL_RELEASE) \
			--set minio.enabled=false)"; \
		if printf '%s\n' "$$disabled_render" | grep -q 'app.kubernetes.io/component: minio'; then \
			printf '%s\n' 'MinIO resources rendered while minio.enabled=false.' >&2; \
			exit 1; \
		fi
	helm lint $(NOAH_LOCAL_OPERATOR_CHART)
	helm template $(NOAH_LOCAL_OPERATOR_RELEASE) $(NOAH_LOCAL_OPERATOR_CHART) \
		--namespace $(NOAH_LOCAL_NAMESPACE) \
		--set-string splunkOperator.nameOverride=$(NOAH_LOCAL_OPERATOR_RELEASE) \
		--set-string splunkOperator.image.repository=$(NOAH_LOCAL_OPERATOR_IMAGE) \
		--set splunkOperator.image.pullPolicy=Always \
		--set-string 'splunkOperator.imagePullSecrets[0].name=kraken-artifactory-creds-test' \
		--set-string image.repository=docker.example.invalid/splunk:latest \
		--set splunkOperator.clusterWideAccess=false \
		--set-string splunkOperator.nodeSelector.workload=splunk \
		--set-string 'splunkOperator.tolerations[0].key=workload' \
		--set-string 'splunkOperator.tolerations[0].operator=Equal' \
		--set-string 'splunkOperator.tolerations[0].value=splunk' \
		--set-string 'splunkOperator.tolerations[0].effect=NoSchedule' \
		--set-string splunkOperator.persistentVolumeClaim.storageClassName=gp3-automode >/dev/null
	@set -eu; \
		command -v envsubst >/dev/null 2>&1 || { \
			printf '%s\n' 'envsubst is required but was not found on PATH.' >&2; \
			exit 1; \
		}; \
		export NOAH_SPLUNK_SERVICE_ACCOUNT=splunk-runtime; \
		export NOAH_SMARTSTORE_AUTH_REGION=us-west-2; \
		export NOAH_SMARTSTORE_OBJECT_STORE_PATH=s3://example-bucket/noah-c3; \
		export NOAH_SMARTSTORE_OBJECT_STORE_ENDPOINT=https://s3.us-west-2.amazonaws.com; \
		export NOAH_SMARTSTORE_QUEUE_ENDPOINT=https://sqs.us-west-2.amazonaws.com; \
		export NOAH_SMARTSTORE_QUEUE_URL=https://sqs.us-west-2.amazonaws.com/example/noah-c3; \
		rendered="$$(envsubst '$${NOAH_SPLUNK_SERVICE_ACCOUNT} $${NOAH_SMARTSTORE_AUTH_REGION} $${NOAH_SMARTSTORE_OBJECT_STORE_PATH} $${NOAH_SMARTSTORE_OBJECT_STORE_ENDPOINT} $${NOAH_SMARTSTORE_QUEUE_ENDPOINT} $${NOAH_SMARTSTORE_QUEUE_URL}' \
			< "$(NOAH_LOCAL_FIXTURES)")"; \
		printf '%s\n' "$$rendered" | yq eval-all '.' - >/dev/null; \
		case $$rendered in *'$${NOAH_'*) \
			printf '%s\n' 'Rendered C3 fixture still contains a Noah placeholder.' >&2; \
			exit 1 ;; \
		esac; \
		if printf '%s\n' "$$rendered" | grep -Eq 'AWS_ACCESS_KEY_ID|AWS_SECRET_ACCESS_KEY'; then \
			printf '%s\n' 'Rendered C3 fixture contains static AWS credential variables.' >&2; \
			exit 1; \
		fi; \
		for fixture_kind in LicenseManager IndexerCluster SearchHeadCluster; do \
			for volume_config in etcVolumeStorageConfig varVolumeStorageConfig; do \
				case $$volume_config in \
					etcVolumeStorageConfig) expected_capacity=10Gi ;; \
					varVolumeStorageConfig) expected_capacity=20Gi ;; \
				esac; \
				export fixture_kind volume_config expected_capacity; \
				if ! printf '%s\n' "$$rendered" | yq eval-all -e ' \
					select(.kind == strenv(fixture_kind)) | \
					(.spec[strenv(volume_config)].ephemeralStorage == false and \
					 .spec[strenv(volume_config)].storageClassName == "gp3-automode" and \
					 .spec[strenv(volume_config)].storageCapacity == strenv(expected_capacity)) \
				' - >/dev/null; then \
					printf '%s %s must use a persistent gp3-automode %s volume.\n' \
						"$$fixture_kind" "$$volume_config" "$$expected_capacity" >&2; \
					exit 1; \
				fi; \
			done; \
		done; \
		for expected in \
			'serviceAccount: splunk-runtime' \
			'path: s3://example-bucket/noah-c3' \
			'"remote.s3.endpoint": https://s3.us-west-2.amazonaws.com' \
			'"remote.s3.auth_region": us-west-2' \
			'"remote.s3.sqs.endpoint": https://sqs.us-west-2.amazonaws.com' \
			'"remote.s3.sqs.queue_url": https://sqs.us-west-2.amazonaws.com/example/noah-c3'; do \
			printf '%s\n' "$$rendered" | grep -Fq "$$expected" || { \
				printf 'Rendered C3 fixture is missing expected runtime setting: %s\n' "$$expected" >&2; \
				exit 1; \
			}; \
		done
	@set -eu; \
		deploy_command="$$( \
			$(MAKE) --no-print-directory -n noah-local-deploy \
				NOAH_LOCAL_HELM_ARGS='--set minio.enabled=true' \
		)"; \
		case "$$deploy_command" in \
			*'--set minio.enabled=true'*'--set minio.enabled=false'*) ;; \
			*) printf '%s\n' 'Kraken deployment does not enforce minio.enabled=false after caller Helm arguments.' >&2; exit 1 ;; \
		esac
	@test -x $(NOAH_LOCAL_DIR)/with-kraken-runtime || { \
		printf '%s\n' 'with-kraken-runtime must be executable.' >&2; \
		exit 1; \
	}
	@for script in $(NOAH_LOCAL_DIR)/create-cluster $(NOAH_LOCAL_DIR)/create-auth-secret $(NOAH_LOCAL_DIR)/create-splunk-secret $(NOAH_LOCAL_DIR)/with-kraken-runtime; do \
		sh -n "$$script" || exit 1; \
		if command -v shellcheck >/dev/null 2>&1; then shellcheck -s sh "$$script" || exit 1; fi; \
	done
	@printf '%s\n' 'Noah local development chart and deployment helpers are clean.'
