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
NOAH_LOCAL_FIXTURES ?= $(NOAH_LOCAL_DIR)/fixtures/indexercluster.yaml

NOAH_LOCAL_DEPLOYMENT_ID ?=
NOAH_LOCAL_CONTEXT ?= kraken
NOAH_LOCAL_NAMESPACE ?= splunk-operator
NOAH_LOCAL_RELEASE ?= noah
# NOAH_LOCAL_NAMESPACE and NOAH_LOCAL_PORT are threaded through the cluster
# setup, the chart and the port-forward, but the endpoint in
# $(NOAH_LOCAL_FIXTURES) is plain YAML and must be edited to match.
NOAH_LOCAL_PORT ?= 8443
NOAH_LOCAL_AUTH_SECRET ?= noah-auth
# Noah waits on PostgreSQL and Redis and runs migrations before it reports ready,
# and a fresh vCluster also has to pull every image and bind a PVC.
NOAH_LOCAL_DEPLOY_TIMEOUT ?= 10m
NOAH_LOCAL_HELM_ARGS ?=

.PHONY: noah-local-up
noah-local-up: noah-local-cluster install noah-local-deploy noah-local-fixtures noah-local-port-forward ## Create the vCluster, install CRDs, deploy Noah, apply fixtures and port-forward.
	@printf '\n%s\n\n' 'Noah is ready. Run the operator locally with:'
	@printf '    %s \\\n      %s \\\n      %s\n\n' \
		'SPLUNK_GENERAL_TERMS='"'"'<your accepted terms>'"'" \
		'WATCH_NAMESPACE=$(NOAH_LOCAL_NAMESPACE)' \
		'go run ./cmd/main.go'
	@printf '%s\n' \
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
noah-local-deploy: ## Install or upgrade Noah, PostgreSQL and Redis in the vCluster.
	helm upgrade --install "$(NOAH_LOCAL_RELEASE)" "$(NOAH_LOCAL_CHART)" \
		--kube-context "$(NOAH_LOCAL_CONTEXT)" \
		--namespace "$(NOAH_LOCAL_NAMESPACE)" \
		--set service.port=$(NOAH_LOCAL_PORT) \
		--wait --timeout $(NOAH_LOCAL_DEPLOY_TIMEOUT) $(NOAH_LOCAL_HELM_ARGS)

.PHONY: noah-local-fixtures
noah-local-fixtures: ## Create the Noah auth Secret and apply the sample custom resources.
	$(NOAH_LOCAL_DIR)/create-auth-secret \
		"$(NOAH_LOCAL_CONTEXT)" "$(NOAH_LOCAL_NAMESPACE)" "$(NOAH_LOCAL_AUTH_SECRET)"
	kubectl --context "$(NOAH_LOCAL_CONTEXT)" --namespace "$(NOAH_LOCAL_NAMESPACE)" \
		apply -f "$(NOAH_LOCAL_FIXTURES)"

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
	@for script in $(NOAH_LOCAL_DIR)/create-cluster $(NOAH_LOCAL_DIR)/create-auth-secret; do \
		sh -n "$$script" || exit 1; \
		if command -v shellcheck >/dev/null 2>&1; then shellcheck -s sh "$$script" || exit 1; fi; \
	done
	@printf '%s\n' 'Noah local development chart and scripts are clean.'
