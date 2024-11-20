# kubectl-splunk Plugin

## Overview

`kubectl-splunk` is a `kubectl` plugin that allows you to execute Splunk commands directly within Splunk pods running in a Kubernetes cluster. It simplifies the management and interaction with Splunk instances deployed as StatefulSets or Deployments by providing a convenient command-line interface.

This plugin supports various features such as:

- Executing Splunk CLI commands inside Splunk pods.
- Running Splunk REST API calls via port-forwarding.
- Specifying pods directly via command line or configuration file.
- Automatic retrieval of Splunk admin credentials from pods.
- Interactive shell access to Splunk pods.
- Copying files to and from Splunk pods.
- Handling authentication securely with credential storage.
- Customizable configurations and verbosity levels.
- Cross-platform compatibility.
- Auto-completion support.

---

## Table of Contents

- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Usage](#usage)
  - [Basic Command Structure](#basic-command-structure)
  - [Modes of Operation](#modes-of-operation)
- [Options](#options)
- [Examples](#examples)
- [Configuration](#configuration)
  - [Configuration File](#configuration-file)
  - [Environment Variables](#environment-variables)
  - [Pod Selection Behavior](#pod-selection-behavior)
- [Authentication](#authentication)
  - [Default Credentials](#default-credentials)
  - [Credential Storage](#credential-storage)
- [Features](#features)
- [REST API Mode](#rest-api-mode)
- [Copy Mode](#copy-mode)
- [Interactive Shell](#interactive-shell)
- [Logging and Verbosity](#logging-and-verbosity)
- [Caching](#caching)
- [Auto-Completion](#auto-completion)
- [Troubleshooting](#troubleshooting)
- [License](#license)

---

## Prerequisites

- **Python 3**: Ensure Python 3.x is installed on your system.
- **kubectl**: The Kubernetes command-line tool must be installed and configured.
- **Access to Kubernetes Cluster**: You should have access to the Kubernetes cluster where Splunk is deployed.
- **Splunk CLI in Pods**: The Splunk Command Line Interface should be available within the Splunk pods.
- **Python Packages**: Install required Python packages:
  ```bash
  pip install requests argcomplete
  ```

---

## Installation

1. **Download the Script**

   Save the `kubectl-splunk` script to your local machine:

   ```bash
   curl -o kubectl-splunk https://github.com/splunk/splunk-operator/tree/main/docs/kubectl-splunk
   ```

2. **Make the Script Executable**

   ```bash
   chmod +x kubectl-splunk
   ```

3. **Move the Script to Your PATH**

   ```bash
   sudo mv kubectl-splunk /usr/local/bin/
   ```

4. **Verify Installation**

   Run the help command to verify the installation:

   ```bash
   kubectl splunk --help
   ```

---

## Usage

### Basic Command Structure

```bash
kubectl splunk [global options] <mode> [mode options]
```

### Modes of Operation

- **exec**: Execute a Splunk command inside a Splunk pod.
- **rest**: Execute a Splunk REST API call via port-forwarding.
- **cp**: Copy files to or from a Splunk pod.
- **interactive**: Start an interactive shell inside a Splunk pod (use `--interactive` flag).

---

## Options

### Global Options

- `-n`, `--namespace`: Specify the Kubernetes namespace (default: `default` or from config/env).
- `-l`, `--selector`: Label selector to identify Splunk pods (default: `app=splunk` or from config/env).
- `--context`: Specify the Kubernetes context.
- `-P`, `--pod`: Specify the exact pod name to run the command on (can be set in config/env).
- `-i`, `--interactive`: Start an interactive shell inside the Splunk pod.
- `--splunk-path`: Path to the Splunk CLI inside the container (default: `splunk` or from config/env).
- `--local-port`: Local port for port-forwarding in REST mode (default: `8000` or from config/env).
- `-v`: Increase output verbosity (use `-v`, `-vv`, or `-vvv`).
- `--version`: Show program version and exit.

### Authentication Options

- `-u`, `--username`: Username for Splunk authentication (default: `admin`).
- `-p`, `--password`: Password for Splunk authentication (will prompt or auto-detect if not provided).
- `--insecure`: Disable SSL certificate verification (useful for self-signed certificates).
- `--save-credentials`: Save credentials securely for future use.

### Mode Options

#### **exec**

- **Usage**: `kubectl splunk exec [splunk_command]`
- **Options**:
  - `splunk_command`: Splunk command to execute (e.g., `list user`).

#### **rest**

- **Usage**: `kubectl splunk rest METHOD ENDPOINT [options]`
- **Options**:
  - `METHOD`: HTTP method (`GET`, `POST`, `PUT`, `DELETE`).
  - `ENDPOINT`: Splunk REST API endpoint (e.g., `/services/server/info`).
  - `--data`: Data to send with the request (for `POST`/`PUT`).
  - `--params`: Query parameters (e.g., `"key1=value1&key2=value2"`).

#### **cp**

- **Usage**: `kubectl splunk cp SRC DEST`
- **Options**:
  - `SRC`: Source file path.
  - `DEST`: Destination file path.
  - Use `:` to indicate the remote path in the pod (e.g., `:/path/in/pod`).

---

## Examples

### Execute a Splunk Command

```bash
kubectl splunk exec status
```

### Execute a Splunk Search Command

```bash
kubectl splunk exec search "index=_internal | head 10"
```

### Specify Namespace and Label Selector

```bash
kubectl splunk -n splunk-namespace -l app=splunk exec status
```

### Specify a Pod Directly

```bash
kubectl splunk --pod splunk-idxc-indexer-0 exec status
```

Or using the short alias:

```bash
kubectl splunk -P splunk-idxc-indexer-0 exec status
```

### Start an Interactive Shell

```bash
kubectl splunk --interactive
```

### Copy Files to a Pod

```bash
kubectl splunk cp /local/path/file.txt :/remote/path/file.txt
```

### Copy Files from a Pod

```bash
kubectl splunk cp :/remote/path/file.txt /local/path/file.txt
```

### Execute a REST API Call

```bash
kubectl splunk rest GET /services/server/info --insecure
```

### Create a Search Job (POST Request)

```bash
kubectl splunk rest POST /services/search/jobs --data "search=search index=_internal | head 10" --insecure
```

### Use Authentication and Save Credentials

```bash
kubectl splunk -u admin --save-credentials exec list user
```

### Increase Verbosity

```bash
kubectl splunk -vv exec status
```

### Specify Kubernetes Context

```bash
kubectl splunk --context my-cluster exec status
```

---

## Configuration

### Configuration File

You can create a configuration file to set default values:

**File**: `~/.kubectl_splunk_config`

**Content**:

```ini
[DEFAULT]
namespace = splunk-namespace
selector = app=splunk
splunk_path = splunk
pod_name = splunk-idxc-indexer-0  # Default pod name
local_port = 8000                 # Default local port for REST mode
```

- **namespace**: Default Kubernetes namespace.
- **selector**: Default label selector to identify Splunk pods.
- **splunk_path**: Path to the Splunk CLI inside the container.
- **pod_name**: Default pod name to use if not specified via command line.
- **local_port**: Default local port for port-forwarding in REST mode.

### Environment Variables

You can set environment variables to override defaults:

- `KUBECTL_SPLUNK_NAMESPACE`: Sets the default namespace.
- `KUBECTL_SPLUNK_SELECTOR`: Sets the default label selector.
- `KUBECTL_SPLUNK_PATH`: Sets the default Splunk CLI path.
- `KUBECTL_SPLUNK_POD`: Sets the default pod name.
- `KUBECTL_SPLUNK_LOCAL_PORT`: Sets the default local port for REST mode.

**Example**:

```bash
export KUBECTL_SPLUNK_NAMESPACE=splunk-namespace
export KUBECTL_SPLUNK_SELECTOR=app=splunk
export KUBECTL_SPLUNK_POD=splunk-idxc-indexer-0
export KUBECTL_SPLUNK_LOCAL_PORT=8000
```

### Pod Selection Behavior

The script determines which pod to use based on the following priority:

1. **Command-line Argument**: Pod specified with `--pod` or `-P` takes the highest priority.
2. **Environment Variable**: If `KUBECTL_SPLUNK_POD` is set, it will be used if no pod is specified on the command line.
3. **Configuration File**: The `pod_name` from `~/.kubectl_splunk_config` is used if neither of the above is provided.
4. **Interactive Selection**: If no pod is specified and multiple pods are found, the script will list them and prompt for selection.
5. **Automatic Selection**: If only one pod is found, the script will use it automatically.

---

## Authentication

### Default Credentials

- **Username**: Defaults to `admin` if not specified.
- **Password**: If not provided, the script attempts to retrieve the password from the pod's `/mnt/splunk-secrets/password` file.
- **Automatic Password Retrieval**: Works if the password file is accessible within the pod.

**Usage Without Credentials**:

```bash
kubectl splunk exec list user
```

### Credential Storage

- **Save Credentials**: Use `--save-credentials` to store credentials securely for future use.
- **Credentials File**: Stored in `~/.kubectl_splunk_credentials` with permissions set to `600`.
- **Provide Credentials Once**:

  ```bash
  kubectl splunk -u admin --save-credentials exec list user
  ```

- **Subsequent Commands**: Credentials are used automatically.

**Note**: Passwords are handled securely and are not exposed in logs or command outputs.

---

## Features

- **Execute Splunk Commands**: Run any Splunk CLI command directly within the pod.
- **REST API Support**: Execute Splunk REST API calls via port-forwarding.
- **Pod Selection**: Specify a pod directly via command line, environment variable, or configuration file. If not specified, the script will prompt for selection when multiple pods are present.
- **Automatic Credential Retrieval**: Defaults to `admin` user and retrieves the password from the pod if not provided.
- **Interactive Shell**: Start a shell session inside the Splunk pod.
- **Copy Files**: Transfer files to and from Splunk pods.
- **Authentication Handling**: Securely handle Splunk authentication credentials with options to save them.
- **Configuration Flexibility**: Use config files or environment variables for defaults.
- **Verbosity Control**: Adjust logging levels for more or less output.
- **Caching**: Pod information is cached for improved performance.
- **Auto-Completion**: Supports shell auto-completion for commands and options.
- **Secure Logging**: Sensitive information such as passwords is not logged.

---

## REST API Mode

Use the `rest` mode to execute Splunk REST API calls via port-forwarding.

**Usage**:

```bash
kubectl splunk rest METHOD ENDPOINT [--data DATA] [--params PARAMS] [options]
```

- **METHOD**: HTTP method (`GET`, `POST`, `PUT`, `DELETE`).
- **ENDPOINT**: Splunk REST API endpoint (e.g., `/services/server/info`).
- **Options**:
  - `--data`: Data to send with the request (for `POST`/`PUT`).
  - `--params`: Query parameters (e.g., `"key1=value1&key2=value2"`).
  - `--insecure`: Disable SSL certificate verification.

**Examples**:

- **Get Server Info**:

  ```bash
  kubectl splunk rest GET /services/server/info --insecure
  ```

- **Create a Search Job**:

  ```bash
  kubectl splunk rest POST /services/search/jobs --data "search=search index=_internal | head 10" --insecure
  ```

---

## Copy Mode

Use the `cp` mode to copy files to and from a Splunk pod.

**Copy to Pod**:

```bash
kubectl splunk -P splunk-idxc-indexer-0 cp /local/path/file.txt :/remote/path/file.txt
```

**Copy from Pod**:

```bash
kubectl splunk -P splunk-idxc-indexer-0 cp :/remote/path/file.txt /local/path/file.txt
```

**Note**:

- Use `:` to indicate the remote path in the pod.
- The `cp` mode requires you to specify a single pod using `--pod`, `-P`, environment variable, or configuration file.
- If multiple pods are found and no pod is specified, the script will prompt for selection.

---

## Interactive Shell

Start an interactive shell session inside a Splunk pod using the `--interactive` flag:

```bash
kubectl splunk --interactive
```

- If the pod is specified via command line, environment variable, or config file, the script will use it.
- If multiple pods are found and no pod is specified, the script will prompt for selection.

---

## Logging and Verbosity

Adjust the logging verbosity using the `-v` flag:

- `-v`: Show warnings and errors.
- `-vv`: Show informational messages.
- `-vvv`: Show debug messages.

**Example**:

```bash
kubectl splunk -vv exec status
```

**Note**: Sensitive information such as passwords is masked in logs.

---

## Caching

The plugin caches the pod name used during execution for 5 minutes to improve performance. The cache is stored in `/tmp/kubectl_splunk_cache.json`.

**Behavior**:

- If a pod name is specified via command line, environment variable, or configuration file, the cache will use that pod.
- If no pod name is specified and multiple pods are found, the selected pod will be cached.
- The cache will expire after 5 minutes.

**To Clear the Cache**:

```bash
rm /tmp/kubectl_splunk_cache.json
```

---

## Auto-Completion

You can enable command auto-completion to enhance your command-line experience.

**Install the `argcomplete` Package**:

```bash
pip install argcomplete
```

**Activate Global Completion**:

```bash
activate-global-python-argcomplete --user
```

**Add to Shell Configuration**:

Add the following to your shell's initialization file (e.g., `.bashrc`, `.bash_profile`, or `.zshrc`):

```bash
eval "$(register-python-argcomplete kubectl-splunk)"
```

Reload your shell configuration:

```bash
source ~/.bashrc  # or your shell's config file
```

---

## Troubleshooting

- **Ambiguous Option Error**: If you encounter an error like `ambiguous option: --p could match --pod, --password`, ensure you are using the correct option names. Use `--pod` or `-P` for specifying the pod, and `--password` or `-p` for the password.

- **Pod Not Found**: If the specified pod is not found, verify the pod name and namespace. Use `kubectl get pods -n <namespace>` to list available pods.

- **Multiple Pods**: If multiple pods are found and you did not specify a pod, the plugin will prompt you to select one. Use `--pod`, `-P`, environment variable, or configuration file to specify a pod directly and avoid the prompt.

- **Copy Mode Limitations**: The `cp` mode requires you to specify a single pod. Ensure you specify a pod via command line, environment variable, or configuration file.

- **Permission Denied**: If you receive permission errors, ensure you have the necessary permissions to access the Kubernetes cluster and the Splunk pods.

- **Caching Issues**: If you believe the script is using an outdated pod from the cache, clear the cache by deleting the cache file.

- **Password Retrieval Failure**: If the script fails to retrieve the password from the pod, ensure that:
  - You have the necessary permissions to execute commands in the pod.
  - The password file `/mnt/splunk-secrets/password` exists and is accessible.

---

## License

This plugin is released under the [MIT License](https://opensource.org/licenses/MIT).

---

## Contributing

Contributions are welcome! Please submit issues and pull requests via the project's GitHub repository.

---

## Feedback

If you have any questions, suggestions, or need assistance, please open an issue on the project's GitHub repository or contact [Your Contact Information].

---

Thank you for using `kubectl-splunk`! We hope this plugin enhances your productivity when managing Splunk deployments on Kubernetes.
