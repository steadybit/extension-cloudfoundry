#!/usr/bin/env bash
# Set up a KIND cluster with Korifi and a test app.
# Usage: ./setup.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/helpers.sh"

check_prerequisites() {
  header "Checking prerequisites..."
  local missing=0
  for tool in kind kubectl cf go docker; do
    if command -v "$tool" >/dev/null 2>&1; then
      info "$tool: $(command -v "$tool")"
    else
      fail "$tool is not installed"
      missing=1
    fi
  done
  if [ $missing -ne 0 ]; then
    echo "Please install the missing tools and try again."
    exit 1
  fi

  if ! docker info >/dev/null 2>&1; then
    fail "Docker is not running"
    exit 1
  fi
  pass "All prerequisites met"
}

create_kind_cluster() {
  header "Creating KIND cluster '${KIND_CLUSTER_NAME}'..."

  if kind get clusters 2>/dev/null | grep -q "^${KIND_CLUSTER_NAME}$"; then
    warn "Cluster '${KIND_CLUSTER_NAME}' already exists, skipping creation"
    return 0
  fi

  cat <<EOF | kind create cluster --name "$KIND_CLUSTER_NAME" --wait 5m --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry]
      config_path = "/etc/containerd/certs.d"
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: 32080
        hostPort: 80
        protocol: TCP
      - containerPort: 32443
        hostPort: 443
        protocol: TCP
      - containerPort: 30050
        hostPort: 30050
        protocol: TCP
EOF

  # Configure containerd registry
  CONTAINER_NAME="${KIND_CLUSTER_NAME}-control-plane"
  docker exec "$CONTAINER_NAME" mkdir -p /etc/containerd/certs.d/localregistry-docker-registry.default.svc.cluster.local:30050
  cat <<EOF | docker exec -i "$CONTAINER_NAME" tee /etc/containerd/certs.d/localregistry-docker-registry.default.svc.cluster.local:30050/hosts.toml > /dev/null
[host."http://127.0.0.1:30050"]
  capabilities = ["pull", "resolve", "push"]
EOF

  pass "KIND cluster created"
}

install_korifi() {
  header "Installing Korifi..."

  if kubectl --context "$KIND_CONTEXT" get namespace korifi >/dev/null 2>&1; then
    warn "Korifi namespace already exists, checking if ready..."
    if kubectl --context "$KIND_CONTEXT" get pods -n korifi -l app=korifi-api -o jsonpath='{.items[0].status.phase}' 2>/dev/null | grep -q Running; then
      warn "Korifi is already running, skipping installation"
      return 0
    fi
  fi

  info "Applying Korifi installer (this may take 10+ minutes)..."
  kubectl --context "$KIND_CONTEXT" apply -f \
    https://github.com/cloudfoundry/korifi/releases/latest/download/install-korifi-kind.yaml

  info "Waiting for installer job to complete..."
  if ! kubectl --context "$KIND_CONTEXT" wait --for=condition=complete job/install-korifi \
    -n korifi-installer --timeout=900s 2>/dev/null; then
    fail "Korifi installer did not complete"
    echo "Installer logs:"
    kubectl --context "$KIND_CONTEXT" logs -n korifi-installer job/install-korifi --tail=30 2>/dev/null || true
    exit 1
  fi

  info "Waiting for Korifi API pods to be ready..."
  kubectl --context "$KIND_CONTEXT" wait --for=condition=ready pod \
    -l app=korifi-api -n korifi --timeout=300s >/dev/null 2>&1

  pass "Korifi installed and ready"
}

setup_cf_service_account() {
  header "Setting up CF service account for the extension..."

  # Create service account if it doesn't exist
  if ! kubectl --context "$KIND_CONTEXT" get sa cf-extension-sa -n cf >/dev/null 2>&1; then
    kubectl --context "$KIND_CONTEXT" create serviceaccount cf-extension-sa -n cf
    kubectl --context "$KIND_CONTEXT" create clusterrolebinding cf-extension-admin \
      --clusterrole=korifi-controllers-admin --serviceaccount=cf:cf-extension-sa
    kubectl --context "$KIND_CONTEXT" create rolebinding cf-extension-root-ns \
      --clusterrole=korifi-controllers-root-namespace-user \
      --serviceaccount=cf:cf-extension-sa -n cf
    info "Service account created"
  else
    info "Service account already exists"
  fi
}

deploy_test_app() {
  header "Deploying test app..."

  cf api https://localhost --skip-ssl-validation >/dev/null 2>&1
  cf auth kind-korifi >/dev/null 2>&1

  # Create org/space if needed
  if ! cf org "$CF_ORG" >/dev/null 2>&1; then
    cf create-org "$CF_ORG" >/dev/null 2>&1
  fi
  if ! cf space "$CF_SPACE" -o "$CF_ORG" >/dev/null 2>&1; then
    cf create-space -o "$CF_ORG" "$CF_SPACE" >/dev/null 2>&1
  fi
  cf target -o "$CF_ORG" -s "$CF_SPACE" >/dev/null 2>&1

  # Push test app if not already deployed
  if cf app "$TEST_APP_NAME" >/dev/null 2>&1; then
    warn "App '$TEST_APP_NAME' already exists"
  else
    info "Pushing '$TEST_APP_NAME' (docker image: $TEST_APP_IMAGE)..."
    cf push "$TEST_APP_NAME" --docker-image "$TEST_APP_IMAGE" --no-route >/dev/null 2>&1
  fi

  # Bind roles for our service account on the org/space namespaces
  ORG_NS=$(kubectl --context "$KIND_CONTEXT" get ns -l "korifi.cloudfoundry.org/org-guid" \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  SPACE_NS=$(kubectl --context "$KIND_CONTEXT" get ns -l "korifi.cloudfoundry.org/space-guid" \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)

  if [ -n "$ORG_NS" ]; then
    kubectl --context "$KIND_CONTEXT" create rolebinding cf-extension-org-manager \
      --clusterrole=korifi-controllers-organization-manager \
      --serviceaccount=cf:cf-extension-sa -n "$ORG_NS" 2>/dev/null || true
  fi
  if [ -n "$SPACE_NS" ]; then
    kubectl --context "$KIND_CONTEXT" create rolebinding cf-extension-space-dev \
      --clusterrole=korifi-controllers-space-developer \
      --serviceaccount=cf:cf-extension-sa -n "$SPACE_NS" 2>/dev/null || true
  fi

  # Verify app is running
  local state
  state=$(get_cf_app_state "$TEST_APP_NAME")
  if [ "$state" = "started" ]; then
    pass "Test app '$TEST_APP_NAME' is running"
  else
    fail "Test app '$TEST_APP_NAME' is in state: $state"
    exit 1
  fi
}

main() {
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Cloud Foundry Extension - Test Setup"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

  check_prerequisites
  create_kind_cluster
  install_korifi
  setup_cf_service_account
  deploy_test_app

  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo -e "  ${GREEN}${BOLD}Setup complete!${NC}"
  echo ""
  echo "  Run the tests:"
  echo "    ./test/test_stop.sh"
  echo "    ./test/test_restart.sh"
  echo "    ./test/test_check.sh"
  echo "    ./test/run_all.sh"
  echo ""
  echo "  Tear down:"
  echo "    ./test/teardown.sh"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

main "$@"
