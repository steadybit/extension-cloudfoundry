# Steadybit extension-cloudfoundry

A [Steadybit](https://www.steadybit.com/) extension for [Cloud Foundry](https://www.cloudfoundry.org/) that discovers applications and provides actions to stop, restart, and check app state.

## Capabilities

### Discovery

- Discovers Cloud Foundry applications via the [CF V3 API](https://v3-apidocs.cloudfoundry.org/)
- Attributes: app name, GUID, space, org, lifecycle type, labels
- Supports both standard CF API (with `include` parameter) and Korifi (fallback to individual space/org lookups)

### Actions

| Action | Type | Description |
|--------|------|-------------|
| **Stop App** | Attack | Stops an application for a configured duration, restarts on rollback |
| **Restart App** | Attack | Restarts an application (instantaneous) |
| **Check App State** | Check | Polls app state and validates against an expected state (Started or Stopped) with "All the time" or "At least once" check modes |

### Note on App State During Restart

The Cloud Foundry V3 API exposes two desired states for an app: `STARTED` and `STOPPED`. When using the **Restart App** action, the app's desired state remains `STARTED` throughout — the restart operation [stops and restarts the underlying processes](https://docs.cloudfoundry.org/devguide/deploy-apps/start-restart-restage.html) without changing the API-level desired state. Only the **Stop App** action sets the state to `STOPPED`.

This means the **Check App State** action will only observe a `STOPPED` state when the app has been explicitly stopped (via the Stop App action or `cf stop`), not during a restart.

## Configuration

| Environment Variable | Helm value | Meaning | Required | Default |
|---|---|---|---|---|
| `STEADYBIT_EXTENSION_API_URL` | `cloudfoundry.apiUrl` | Cloud Foundry API endpoint URL (e.g., `https://api.cf.example.com`) | yes | |
| `STEADYBIT_EXTENSION_USERNAME` | `cloudfoundry.username` | Username for UAA password grant authentication | no | |
| `STEADYBIT_EXTENSION_PASSWORD` | `cloudfoundry.password` | Password for UAA password grant authentication | no | |
| `STEADYBIT_EXTENSION_BEARER_TOKEN` | `cloudfoundry.bearerToken` | Static bearer token (e.g., for Korifi). Skips UAA when set | no | |
| `STEADYBIT_EXTENSION_CLIENT_CERT_PATH` | | Path to client certificate for mTLS authentication | no | |
| `STEADYBIT_EXTENSION_CLIENT_KEY_PATH` | | Path to client key for mTLS authentication | no | |
| `STEADYBIT_EXTENSION_SKIP_TLS_VERIFY` | `cloudfoundry.skipTlsVerify` | Skip TLS certificate verification | no | `false` |
| `STEADYBIT_EXTENSION_DISCOVERY_ATTRIBUTES_EXCLUDES_APP` | `discovery.attributes.excludes.app` | Attributes to exclude from discovery. Supports trailing `*` | no | |

One of the following authentication methods must be configured:
- **UAA password grant**: `USERNAME` + `PASSWORD`
- **Static bearer token**: `BEARER_TOKEN` (e.g., for Korifi)
- **Client certificate**: `CLIENT_CERT_PATH` + `CLIENT_KEY_PATH`

The extension supports all environment variables provided by [steadybit/extension-kit](https://github.com/steadybit/extension-kit#environment-variables).

## Installation

### Kubernetes

Detailed information about agent and extension installation in kubernetes can also be found in
our [documentation](https://docs.steadybit.com/install-and-configure/install-agent/install-on-kubernetes).

#### Recommended (via agent helm chart)

All extensions provide a helm chart that is also integrated in the
[helm-chart](https://github.com/steadybit/helm-charts/tree/main/charts/steadybit-agent) of the agent.

You must provide additional values to activate this extension.

```
--set extension-cloudfoundry.enabled=true \
--set extension-cloudfoundry.cloudfoundry.apiUrl=https://api.cf.example.com \
--set extension-cloudfoundry.cloudfoundry.username=admin \
--set extension-cloudfoundry.cloudfoundry.password=secret \
```

Additional configuration options can be found in
the [helm-chart](https://github.com/steadybit/extension-cloudfoundry/blob/main/charts/steadybit-extension-cloudfoundry/values.yaml) of the
extension.

#### Alternative (via own helm chart)

If you need more control, you can install the extension via its
dedicated [helm-chart](https://github.com/steadybit/extension-cloudfoundry/blob/main/charts/steadybit-extension-cloudfoundry).

```bash
helm repo add steadybit-extension-cloudfoundry https://steadybit.github.io/extension-cloudfoundry
helm repo update
helm upgrade steadybit-extension-cloudfoundry \
    --install \
    --wait \
    --timeout 5m0s \
    --create-namespace \
    --namespace steadybit-agent \
    --set cloudfoundry.apiUrl=https://api.cf.example.com \
    --set cloudfoundry.username=admin \
    --set cloudfoundry.password=secret \
    steadybit-extension-cloudfoundry/steadybit-extension-cloudfoundry
```

### Linux Package

Please use
our [agent-linux.sh script](https://docs.steadybit.com/install-and-configure/install-agent/install-on-linux-hosts)
to install the extension on your Linux machine. The script will download the latest version of the extension and install
it using the package manager.

After installing, configure the extension by editing `/etc/steadybit/extension-cloudfoundry` and then restart the service.

## Local Testing with Korifi

The `test/` directory contains scripts for testing against a local [Korifi](https://github.com/cloudfoundry/korifi) deployment on KIND:

```bash
./test/setup.sh            # Create KIND cluster + install Korifi + deploy test app
./test/start_extension.sh  # Build and run the extension locally
./test/run_all.sh          # Run all attack test suites
./test/teardown.sh         # Delete the KIND cluster
```

Individual test suites can be run separately:

```bash
./test/test_stop.sh        # Test Stop App action
./test/test_restart.sh     # Test Restart App action
./test/test_check.sh       # Test Check App State action
```

## Extension registration

Make sure that the extension is registered with the agent. In most cases this is done automatically. Please refer to
the [documentation](https://docs.steadybit.com/install-and-configure/install-agent/extension-registration) for more
information about extension registration and how to verify.
