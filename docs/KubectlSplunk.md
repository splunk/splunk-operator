# kubectl-splunk Plugin

## Overview

`kubectl-splunk` is a kubectl plugin that allows you to execute Splunk commands directly within Splunk pods running in a Kubernetes cluster. It simplifies the management and interaction with Splunk instances deployed as StatefulSets or Deployments by providing a convenient command-line interface.

This plugin supports various features such as:

- Executing Splunk CLI commands inside Splunk pods.
- Specifying pods directly via command line or configuration file.
- Interactive shell access to Splunk pods.
- Copying files to and from Splunk pods.
- Handling authentication securely.
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
- [Features](#features)
- [Authentication](#authentication)
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
- `-v`: Increase output verbosity (use `-v`, `-vv`, or `-vvv`).
- `--version`: Show program version and exit.

### Authentication Options

- `-u`, `--username`: Username for Splunk authentication.
- `-p`, `--password`: Password for Splunk authentication (will prompt if not provided).

### Mode Options

- **exec**: Followed by the Splunk command and its arguments.
- **cp**: Requires `src` and `dest` arguments for source and destination paths.

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

### Use Pod Name from Configuration File

If you have set the `pod_name` in your configuration file, you can run commands without specifying the pod:

```bash
kubectl splunk exec status
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

**Note**: Use `:` to indicate the remote path in the pod.

### Use Authentication

```bash
kubectl splunk -u admin exec list user
```

You will be prompted for the password if not provided with `-p` or `--password`.

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
```

- **namespace**: Default Kubernetes namespace.
- **selector**: Default label selector to identify Splunk pods.
- **splunk_path**: Path to the Splunk CLI inside the container.
- **pod_name**: Default pod name to use if not specified via command line.

### Environment Variables

You can set environment variables to override defaults:

- `KUBECTL_SPLUNK_NAMESPACE`: Sets the default namespace.
- `KUBECTL_SPLUNK_SELECTOR`: Sets the default label selector.
- `KUBECTL_SPLUNK_PATH`: Sets the default Splunk CLI path.
- `KUBECTL_SPLUNK_POD`: Sets the default pod name.

**Example**:

```bash
export KUBECTL_SPLUNK_NAMESPACE=splunk-namespace
export KUBECTL_SPLUNK_SELECTOR=app=splunk
export KUBECTL_SPLUNK_POD=splunk-idxc-indexer-0
```

### Pod Selection Behavior

The script determines which pod to use based on the following priority:

1. **Command-line Argument**: Pod specified with `--pod` or `-P` takes the highest priority.
2. **Environment Variable**: If `KUBECTL_SPLUNK_POD` is set, it will be used if no pod is specified on the command line.
3. **Configuration File**: The `pod_name` from `~/.kubectl_splunk_config` is used if neither of the above is provided.
4. **Interactive Selection**: If no pod is specified and multiple pods are found, the script will list them and prompt for selection.
5. **Automatic Selection**: If only one pod is found, the script will use it automatically.

---

## Features

- **Execute Splunk Commands**: Run any Splunk CLI command directly within the pod.
- **Pod Selection**: Specify a pod directly via command line, environment variable, or configuration file. If not specified, the script will prompt for selection when multiple pods are present.
- **Interactive Shell**: Start a shell session inside the Splunk pod.
- **Copy Files**: Transfer files to and from Splunk pods.
- **Authentication Support**: Securely handle Splunk authentication credentials.
- **Configuration Flexibility**: Use config files or environment variables for defaults.
- **Verbosity Control**: Adjust logging levels for more or less output.
- **Caching**: Pod information is cached for improved performance.
- **Cross-Platform**: Works on Linux, macOS, and Windows.
- **Auto-Completion**: Supports shell auto-completion for commands and options.

---

## Authentication

If the Splunk CLI requires authentication, you can provide credentials using `-u` and `-p`:

```bash
kubectl splunk -u admin -p mypassword exec list user
```

If you omit `-p`, you will be securely prompted for the password.

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
source ~/.bashrc
```

---

## Troubleshooting

- **Ambiguous Option Error**: If you encounter an error like `ambiguous option: --p could match --pod, --password`, ensure you are using the correct option names. Use `--pod` or `-P` for specifying the pod, and `--password` or `-p` for the password.

- **Pod Not Found**: If the specified pod is not found, verify the pod name and namespace. Use `kubectl get pods -n <namespace>` to list available pods.

- **Multiple Pods**: If multiple pods are found and you did not specify a pod, the plugin will prompt you to select one. Use `--pod`, `-P`, environment variable, or configuration file to specify a pod directly and avoid the prompt.

- **Copy Mode Limitations**: The `cp` mode requires you to specify a single pod. Ensure you specify a pod via command line, environment variable, or configuration file.

- **Permission Denied**: If you receive permission errors, ensure you have the necessary permissions to access the Kubernetes cluster and the Splunk pods.

- **Caching Issues**: If you believe the script is using an outdated pod from the cache, clear the cache by deleting the cache file.

---

## License

This plugin is released under the [MIT License](https://opensource.org/licenses/MIT).

---

## Contributing

Contributions are welcome! Please submit issues and pull requests via the project's GitHub repository.

---


## Feedback

If you have any questions, suggestions, or need assistance, please open an issue on the project's GitHub repository or contact support.

---

Thank you for using `kubectl-splunk`! We hope this plugin enhances your productivity when managing Splunk deployments on Kubernetes.

