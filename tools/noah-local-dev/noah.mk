##@ Noah Local Development

# Local development loop for the Noah integration on a Kraken vCluster.
# Included by the root Makefile, so every path here is relative to the
# repository root, not to this file. See tools/noah-local-dev/README.md.
NOAH_LOCAL_DIR = tools/noah-local-dev
NOAH_LOCAL_STATE_DIR ?= .noah-local-dev
NOAH_LOCAL_DEPLOYMENT_ID_FILE = $(NOAH_LOCAL_STATE_DIR)/deployment-id
NOAH_LOCAL_PORT_FORWARD_PID_FILE = $(NOAH_LOCAL_STATE_DIR)/noah-port-forward.pid
NOAH_LOCAL_PORT_FORWARD_LOG = $(NOAH_LOCAL_STATE_DIR)/noah-port-forward.log
NOAH_LOCAL_CHART = helm/charts/noah
NOAH_LOCAL_FIXTURES ?= $(NOAH_LOCAL_DIR)/fixtures/c3.yaml
NOAH_LOCAL_OPERATOR_CHART ?= helm-chart/splunk-operator

NOAH_LOCAL_DEPLOYMENT_ID ?=
NOAH_LOCAL_CONTEXT ?= kraken
NOAH_LOCAL_NAMESPACE ?= splunk-operator
NOAH_LOCAL_RELEASE ?= noah
NOAH_LOCAL_OPERATOR_RELEASE ?= splunk-operator
# Branch pipelines publish this exact commit-addressed image.
NOAH_LOCAL_OPERATOR_TAG ?= $(shell git rev-parse HEAD)
NOAH_LOCAL_OPERATOR_IMAGE ?= docker-test.repo.splunkdev.net/sok/splunk-operator:$(NOAH_LOCAL_OPERATOR_TAG)
NOAH_LOCAL_SPLUNK_IMAGE ?= $(RELATED_IMAGE_SPLUNK_ENTERPRISE)
# NOAH_LOCAL_NAMESPACE and NOAH_LOCAL_PORT are threaded through the cluster
# setup, the chart and the port-forward, but the endpoint in
# $(NOAH_LOCAL_FIXTURES) is plain YAML and must be edited to match.
NOAH_LOCAL_PORT ?= 8443
NOAH_LOCAL_LICENSE_FILE ?=
NOAH_LOCAL_C3_NAME ?= c3
# Noah waits on PostgreSQL and Redis and runs migrations before it reports ready.
# A fresh vCluster also pulls MinIO and binds the PostgreSQL and MinIO PVCs.
NOAH_LOCAL_DEPLOY_TIMEOUT ?= 10m
NOAH_LOCAL_HELM_ARGS ?=
NOAH_LOCAL_OPERATOR_TIMEOUT ?= 5m
NOAH_LOCAL_OPERATOR_HELM_ARGS ?=

.PHONY: noah-local-c3-up
noah-local-c3-up: noah-local-cluster install noah-local-deploy noah-local-operator-deploy noah-local-fixtures ## Create a complete C3 deployment with an in-cluster operator.
	@printf '\n%s\n\n' 'Noah C3 is deployed. Once the C3 Pods are running, verify it with:'
	@printf '    %s\n\n' 'make noah-local-smoke'

.PHONY: noah-local-up
noah-local-up: noah-local-cluster install noah-local-deploy noah-local-fixtures noah-local-port-forward ## Prepare a C3 deployment for an operator running locally.
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
		if test -z "$$deployment_id" && test -s "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)"; then \
			deployment_id="$$(cat "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)")"; \
			if deployment_status="$$(kraken status "$$deployment_id" 2>&1)"; then \
				case "$$(printf '%s\n' "$$deployment_status" | jq -er '.state')" in \
					terminating|terminated) deployment_id='' ;; \
				esac; \
			else \
				case "$$deployment_status" in \
					*status=404*) deployment_id='' ;; \
					*) printf '%s\n' "$$deployment_status" >&2; exit 1 ;; \
				esac; \
			fi; \
		fi; \
		deployment_id="$$( \
			NOAH_LOCAL_NAMESPACE="$(NOAH_LOCAL_NAMESPACE)" \
			NOAH_LOCAL_DEPLOYMENT_ID_FILE="$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)" \
			$(NOAH_LOCAL_DIR)/create-cluster "$$deployment_id")"; \
		test -n "$$deployment_id"; \
		printf '%s\n' "$$deployment_id" > "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)"; \
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
		if delete_output="$$(kraken delete "$$deployment_id" --json 2>&1)"; then \
			printf '%s\n' "$$delete_output"; \
		else \
			case "$$delete_output" in \
				*status=404*) printf 'Kraken deployment %s no longer exists.\n' "$$deployment_id" ;; \
				*) printf '%s\n' "$$delete_output" >&2; exit 1 ;; \
			esac; \
		fi; \
		if test -s "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)" && \
			test "$$(cat "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)")" = "$$deployment_id"; then \
			rm -f "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)"; \
		fi

.PHONY: noah-local-deployment-id
noah-local-deployment-id: ## Print the saved Kraken deployment ID.
	@test -s "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)" || { \
		printf '%s\n' 'No deployment ID found. Run `make noah-local-cluster` first.' >&2; \
		exit 1; \
	}
	@cat "$(NOAH_LOCAL_DEPLOYMENT_ID_FILE)"

.PHONY: noah-local-deploy
noah-local-deploy: ## Install or upgrade Noah, PostgreSQL, Redis and MinIO in the vCluster.
	helm upgrade --install "$(NOAH_LOCAL_RELEASE)" "$(NOAH_LOCAL_CHART)" \
		--kube-context "$(NOAH_LOCAL_CONTEXT)" \
		--namespace "$(NOAH_LOCAL_NAMESPACE)" \
		--set service.port=$(NOAH_LOCAL_PORT) \
		--wait --timeout $(NOAH_LOCAL_DEPLOY_TIMEOUT) $(NOAH_LOCAL_HELM_ARGS)

.PHONY: noah-local-operator-chart-deps
noah-local-operator-chart-deps: ## Fetch the operator chart's subchart dependencies.
	helm repo add jetstack https://charts.jetstack.io --force-update
	helm dependency build "$(NOAH_LOCAL_OPERATOR_CHART)"

.PHONY: noah-local-operator-deploy
noah-local-operator-deploy: noah-local-operator-chart-deps ## Install or upgrade a Noah-enabled operator in the vCluster.
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
			jq -er '[.imagePullSecrets[]?.name | select(startswith("kraken-artifactory-creds-"))][0]')"; \
		helm upgrade --install "$(NOAH_LOCAL_OPERATOR_RELEASE)" "$(NOAH_LOCAL_OPERATOR_CHART)" \
			--kube-context "$(NOAH_LOCAL_CONTEXT)" \
			--namespace "$(NOAH_LOCAL_NAMESPACE)" \
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
			--wait --timeout "$(NOAH_LOCAL_OPERATOR_TIMEOUT)" \
			$(NOAH_LOCAL_OPERATOR_HELM_ARGS)

.PHONY: noah-local-fixtures
noah-local-fixtures: ## Create prerequisite Secrets and apply the sample C3 custom resources.
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
	kubectl --context "$(NOAH_LOCAL_CONTEXT)" --namespace "$(NOAH_LOCAL_NAMESPACE)" \
		apply -f "$(NOAH_LOCAL_FIXTURES)"

.PHONY: noah-local-smoke
noah-local-smoke: ## Verify Noah health, then index on every C3 peer and search through the SHC.
	KUBE_CONTEXT="$(NOAH_LOCAL_CONTEXT)" \
	NAMESPACE="$(NOAH_LOCAL_NAMESPACE)" \
	C3_NAME="$(NOAH_LOCAL_C3_NAME)" \
		$(NOAH_LOCAL_DIR)/smoke-test

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
noah-local-lint: ## Lint the Noah chart and the local development scripts.
	helm lint $(NOAH_LOCAL_CHART)
	helm template $(NOAH_LOCAL_RELEASE) $(NOAH_LOCAL_CHART) \
		--namespace $(NOAH_LOCAL_NAMESPACE) >/dev/null
	helm lint $(NOAH_LOCAL_OPERATOR_CHART)
	helm template $(NOAH_LOCAL_OPERATOR_RELEASE) $(NOAH_LOCAL_OPERATOR_CHART) \
		--namespace $(NOAH_LOCAL_NAMESPACE) \
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
	yq eval-all '.' "$(NOAH_LOCAL_FIXTURES)" >/dev/null
	@for script in $(NOAH_LOCAL_DIR)/create-cluster $(NOAH_LOCAL_DIR)/create-auth-secret $(NOAH_LOCAL_DIR)/smoke-test; do \
		sh -n "$$script" || exit 1; \
		if command -v shellcheck >/dev/null 2>&1; then shellcheck -s sh "$$script" || exit 1; fi; \
	done
	@printf '%s\n' 'Noah local development chart and scripts are clean.'
