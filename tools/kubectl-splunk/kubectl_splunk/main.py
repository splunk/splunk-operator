# kubectl_splunk/main.py

import sys
import subprocess
import argparse
import logging
import os
import time
import configparser
import json
import getpass
import requests
from concurrent.futures import ThreadPoolExecutor
from argparse import RawDescriptionHelpFormatter

try:
    import argcomplete
except ImportError:
    argcomplete = None

def load_config():
    """Load configuration from file and environment variables."""
    config = configparser.ConfigParser()
    config_file = os.path.expanduser('~/.kubectl_splunk_config')
    if os.path.exists(config_file):
        config.read(config_file)
        namespace = config.get('DEFAULT', 'namespace', fallback='default')
        selector = config.get('DEFAULT', 'selector', fallback='app=splunk')
        splunk_path = config.get('DEFAULT', 'splunk_path', fallback='splunk')
        pod_name = config.get('DEFAULT', 'pod_name', fallback=None)
        local_port = config.getint('DEFAULT', 'local_port', fallback=8089)
    else:
        namespace = 'default'
        selector = 'app=splunk'
        splunk_path = '/opt/splunk/bin/splunk'
        pod_name = None
        local_port = 8089
    

    # Override with environment variables if set
    namespace = os.environ.get('KUBECTL_SPLUNK_NAMESPACE', namespace)
    selector = os.environ.get('KUBECTL_SPLUNK_SELECTOR', selector)
    splunk_path = os.environ.get('KUBECTL_SPLUNK_PATH', splunk_path)
    pod_name = os.environ.get('KUBECTL_SPLUNK_POD', pod_name)
    local_port = int(os.environ.get('KUBECTL_SPLUNK_LOCAL_PORT', local_port))
    return namespace, selector, splunk_path, pod_name, local_port

def parse_args(namespace, selector, splunk_path, pod_name_from_config, local_port_from_config):
    """Parse command-line arguments."""
    epilog_text = """
Examples:
  kubectl splunk exec search "index=_internal | head 10"
  kubectl splunk -n splunk-namespace -l app=splunk -P splunk-idxc-indexer-0 exec status
  kubectl splunk rest GET /services/server/info
  kubectl splunk --interactive
    """
    parser = argparse.ArgumentParser(
        description='kubectl plugin to run Splunk commands within a Splunk pod',
        epilog=epilog_text,
        formatter_class=RawDescriptionHelpFormatter
    )

    parser.add_argument('-n', '--namespace', default=namespace,
                        help='Specify the Kubernetes namespace (default from config/env or "default")')
    parser.add_argument('-l', '--selector', default=selector,
                        help='Label selector to identify the Splunk pod(s) (default from config/env or "app=splunk")')
    parser.add_argument('--context', help='Specify the Kubernetes context')
    parser.add_argument('-P', '--pod', default=pod_name_from_config,
                        help='Specify the exact pod name to run the command on (default from config/env if set)')
    parser.add_argument('-i', '--interactive', action='store_true',
                        help='Start an interactive shell inside the Splunk pod')
    parser.add_argument('--splunk-path', default=splunk_path,
                        help='Path to the Splunk CLI inside the container (default from config/env or "splunk")')
    parser.add_argument('--local-port', type=int, default=local_port_from_config,
                        help='Local port for port-forwarding in REST mode (default from config/env or 8000)')
    parser.add_argument('-v', '--verbose', action='count', default=0,
                        help='Increase output verbosity (e.g., -v, -vv, -vvv)')
    parser.add_argument('--version', action='version', version='kubectl-splunk 1.6',
                        help='Show program version and exit')

    auth_group = parser.add_argument_group('Authentication')
    auth_group.add_argument('-u', '--username', help='Username for Splunk authentication (default: admin)')
    auth_group.add_argument('-p', '--password', help='Password for Splunk authentication (will prompt or auto-detect if not provided)')
    auth_group.add_argument('--insecure', action='store_true',
                            help='Disable SSL certificate verification')
    auth_group.add_argument('--save-credentials', action='store_true',
                            help='Save credentials securely for future use')

    subparsers = parser.add_subparsers(dest='mode', required=True, help='Available modes')

    # Subparser for exec mode
    exec_parser = subparsers.add_parser('exec', help='Execute a Splunk command')
    exec_parser.add_argument('splunk_command', nargs=argparse.REMAINDER, help='Splunk command to execute')

    # Subparser for copy mode
    cp_parser = subparsers.add_parser('cp', help='Copy files to/from the Splunk pod')
    cp_parser.add_argument('src', help='Source file path')
    cp_parser.add_argument('dest', help='Destination file path')

    # Subparser for rest mode
    rest_parser = subparsers.add_parser('rest', help='Execute a Splunk REST API call')
    rest_parser.add_argument('method', choices=['GET', 'POST', 'PUT', 'DELETE'], help='HTTP method')
    rest_parser.add_argument('endpoint', help='Splunk REST API endpoint (e.g., /services/server/info)')
    rest_parser.add_argument('--data', help='Data to send with the request (for POST/PUT)')
    rest_parser.add_argument('--params', help='Query parameters (e.g., "key1=value1&key2=value2")')

    if argcomplete:
        argcomplete.autocomplete(parser)

    args = parser.parse_args()

    return args

def setup_logging(verbosity):
    """Set up logging based on verbosity level."""
    if verbosity >= 3:
        level = logging.DEBUG
    elif verbosity == 2:
        level = logging.INFO
    elif verbosity == 1:
        level = logging.WARNING
    else:
        level = logging.ERROR

    logging.basicConfig(level=level, format='%(levelname)s: %(message)s')

def get_pods(args):
    """Retrieve the list of Splunk pods based on the label selector and namespace."""
    if args.pod:
        # If a specific pod is specified, return it directly
        return [args.pod]
    else:
        kubectl_cmd = ['kubectl']
        if args.context:
            kubectl_cmd.extend(['--context', args.context])
        kubectl_cmd.extend(['get', 'pods', '-n', args.namespace, '-l', args.selector,
                            '-o', 'jsonpath={.items[*].metadata.name}'])

        logging.debug(f"Running command: {' '.join(kubectl_cmd)}")

        try:
            pods_output = subprocess.check_output(kubectl_cmd, universal_newlines=True).strip()
        except subprocess.CalledProcessError as e:
            logging.error(f"Failed to get pods: {e}")
            sys.exit(1)

        pods = pods_output.split()
        if not pods:
            logging.error(f"No Splunk pods found with selector '{args.selector}' in namespace '{args.namespace}'.")
            sys.exit(1)

        logging.debug(f"Found pods: {pods}")
        return pods

def select_pods(pods, args):
    """Allow the user to select pods when multiple pods are present."""
    if len(pods) == 1:
        return pods

    print("Multiple Splunk pods found:")
    for idx, pod in enumerate(pods):
        print(f"{idx + 1}. {pod}")
    print("0. Run command on all pods")

    while True:
        try:
            choice = int(input("Select a pod by number (or 0 to run on all): "))
            if choice == 0:
                return pods
            elif 1 <= choice <= len(pods):
                return [pods[choice - 1]]
            else:
                print(f"Please enter a number between 0 and {len(pods)}.")
        except ValueError:
            print("Invalid input. Please enter a number.")

def cache_pod(pod_name, namespace, selector):
    """Cache the pod name to a file."""
    cache_file = '/tmp/kubectl_splunk_cache.json'
    cache_data = {
        'pod_name': pod_name,
        'namespace': namespace,
        'selector': selector,
        'timestamp': time.time()
    }
    with open(cache_file, 'w') as f:
        json.dump(cache_data, f)

def load_cached_pod(namespace, selector):
    """Load the cached pod name if available and valid."""
    cache_file = '/tmp/kubectl_splunk_cache.json'
    if os.path.exists(cache_file):
        with open(cache_file, 'r') as f:
            cache = json.load(f)
        if (time.time() - cache['timestamp'] < 300 and
                cache['namespace'] == namespace and
                cache['selector'] == selector):
            return cache['pod_name']
    return None

def get_credentials(args, pod_name):
    """Retrieve stored credentials, prompt the user, or retrieve from pod."""
    credentials_file = os.path.expanduser('~/.kubectl_splunk_credentials')
    username = args.username
    password = args.password

    # Default username to 'admin' if not provided
    if not username:
        username = 'admin'

    # Load credentials from file if available
    if not password:
        if os.path.exists(credentials_file):
            try:
                with open(credentials_file, 'r') as f:
                    creds = json.load(f)
                username = creds.get('username', username)
                password = creds.get('password', password)
            except Exception as e:
                logging.warning("Failed to read stored credentials.")
                os.remove(credentials_file)  # Remove corrupted credentials file

    # Retrieve password from pod if still not available
    if not password and username == 'admin':
        password = retrieve_password_from_pod(args, pod_name)
        if not password:
            logging.error("Failed to retrieve password from pod.")
            sys.exit(1)

    # Prompt for missing credentials
    if not password:
        password = getpass.getpass(prompt='Splunk Password: ')

    # Save credentials if requested
    if args.save_credentials:
        creds = {'username': username, 'password': password}
        try:
            with open(credentials_file, 'w') as f:
                json.dump(creds, f)
            os.chmod(credentials_file, 0o600)  # Set file permissions to be readable by owner only
        except Exception as e:
            logging.warning("Failed to save credentials.")

    return username, password

def retrieve_password_from_pod(args, pod_name):
    """Retrieve the admin password from the pod's /mnt/splunk-secrets/password file."""
    kubectl_cmd = ['kubectl']
    if args.context:
        kubectl_cmd.extend(['--context', args.context])
    kubectl_cmd.extend(['exec', '-n', args.namespace, pod_name, '--', 'cat', '/mnt/splunk-secrets/password'])

    logging.debug(f"Retrieving password from pod {pod_name}")
    try:
        password = subprocess.check_output(kubectl_cmd, universal_newlines=True).strip()
        return password
    except subprocess.CalledProcessError as e:
        logging.error(f"Failed to retrieve password from pod {pod_name}: {e}")
        return None

def execute_on_pod(args, pod_name):
    """Execute the Splunk command on a single pod."""
    kubectl_cmd = ['kubectl']
    if args.context:
        kubectl_cmd.extend(['--context', args.context])
    kubectl_cmd.extend(['exec', '-n', args.namespace, pod_name, '--'])

    if args.mode == 'exec':
        cmd = kubectl_cmd + [args.splunk_path]
        cmd += args.splunk_command  # Add the Splunk command arguments
        # Authentication
        username, password = get_credentials(args, pod_name)
        cmd += ['-auth', f"{username}:{'********'}"]  # Masked password for logging
        logging.debug(f"Executing command on pod {pod_name}: {' '.join(cmd)}")
        # Replace masked password with actual password in the command to be executed
        cmd[-1] = f"{username}:{password}"
    elif args.mode == 'interactive':
        cmd = kubectl_cmd + ['/bin/bash']
        logging.debug(f"Starting interactive shell on pod {pod_name}")
    else:
        return

    try:
        subprocess.run(cmd, check=True)
    except subprocess.CalledProcessError as e:
        logging.error(f"Command failed on pod {pod_name} with exit code {e.returncode}")
        sys.exit(e.returncode)

def copy_to_pod(args, pod_name):
    """Copy files to/from the Splunk pod."""
    kubectl_cmd = ['kubectl']
    if args.context:
        kubectl_cmd.extend(['--context', args.context])
    src = args.src
    dest = args.dest
    # If source or destination starts with ':', it's assumed to be remote
    if src.startswith(':'):
        src = f"{args.namespace}/{pod_name}:{src[1:]}"
    if dest.startswith(':'):
        dest = f"{args.namespace}/{pod_name}:{dest[1:]}"
    kubectl_cmd.extend(['cp', src, dest])

    logging.debug(f"Copying files with command: {' '.join(kubectl_cmd)}")
    try:
        subprocess.run(kubectl_cmd, check=True)
    except subprocess.CalledProcessError as e:
        logging.error(f"Copy command failed with exit code {e.returncode}")
        sys.exit(e.returncode)

def execute_rest_call(args, pod_name):
    """Execute a Splunk REST API call."""
    # Start port-forwarding
    port = 8089
    local_port = args.local_port  # User-specified local port
    kubectl_cmd = ['kubectl']
    if args.context:
        kubectl_cmd.extend(['--context', args.context])
    kubectl_cmd.extend([
        'port-forward', '-n', args.namespace, pod_name,
        f'{local_port}:{port}'
    ])

    logging.debug(f"Starting port-forward with command: {' '.join(kubectl_cmd)}")
    # Start port-forwarding as a background process
    port_forward_proc = subprocess.Popen(kubectl_cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        # Wait for port-forwarding to start
        for _ in range(10):
            if port_forward_proc.poll() is not None:
                raise Exception("Port-forward process terminated unexpectedly.")
            time.sleep(0.5)
        # Build the URL
        url = f'https://127.0.0.1:{local_port}{args.endpoint}'
        # Handle authentication
        username, password = get_credentials(args, pod_name)
        auth = (username, password)

        # Handle SSL verification
        verify_ssl = not args.insecure

        # Prepare request parameters
        method = args.method.upper()
        headers = {'Content-Type': 'application/json'}
        data = args.data
        params = {}
        if args.params:
            # Parse query parameters
            params = dict(param.split('=') for param in args.params.split('&'))

        logging.debug(f"Sending {method} request to {url}")
        # Send the request
        response = requests.request(
            method=method,
            url=url,
            headers=headers,
            params=params,
            data=data,
            auth=auth,
            verify=verify_ssl
        )
        # Print the response
        print(response.text)
        if response.status_code >= 400:
            logging.error(f"HTTP {response.status_code}: {response.reason}")
            sys.exit(1)
    except Exception as e:
        logging.error(f"Error during REST API call: {e}")
        sys.exit(1)
    finally:
        # Terminate port-forwarding
        port_forward_proc.terminate()
        port_forward_proc.wait()

def main():
    # Load configuration
    namespace, selector, splunk_path, pod_name_from_config, local_port_from_config = load_config()

    # Parse arguments
    args = parse_args(namespace, selector, splunk_path, pod_name_from_config, local_port_from_config)

    # Set up logging
    setup_logging(args.verbose)
    # Ensure sensitive information is not logged
    masked_args = vars(args).copy()
    if masked_args.get('password'):
        masked_args['password'] = '********'
    logging.debug(f"Arguments: {masked_args}")

    # Handle interactive shell separately
    if args.interactive:
        args.mode = 'interactive'
        args.splunk_command = []

    # Load cached pod if available
    pod_name = None
    if args.pod:
        pods = [args.pod]
    else:
        pod_name = load_cached_pod(args.namespace, args.selector)
        pods = []

        if pod_name:
            logging.info(f"Using cached pod: {pod_name}")
            pods = [pod_name]
        else:
            # Get list of pods
            pods = get_pods(args)
            if len(pods) > 1 and args.mode != 'cp':
                pods = select_pods(pods, args)
            pod_name = pods[0]
            # Cache the pod name
            cache_pod(pod_name, args.namespace, args.selector)

    # Handle modes
    if args.mode in ['exec', 'interactive']:
        # Execute commands on pods (in parallel if multiple pods)
        with ThreadPoolExecutor() as executor:
            futures = []
            for pod in pods:
                futures.append(executor.submit(execute_on_pod, args, pod))
            for future in futures:
                future.result()  # To catch exceptions
    elif args.mode == 'cp':
        # Copy files (cannot copy to multiple pods at once)
        if len(pods) > 1:
            logging.error("Copy mode does not support multiple pods. Please specify a single pod.")
            sys.exit(1)
        copy_to_pod(args, pods[0])
    elif args.mode == 'rest':
        # REST API call (only supports a single pod)
        if len(pods) > 1:
            logging.error("REST mode does not support multiple pods. Please specify a single pod.")
            sys.exit(1)
        execute_rest_call(args, pods[0])
    else:
        logging.error(f"Unknown mode: {args.mode}")
        sys.exit(1)

if __name__ == '__main__':
    main()

