# tests/test_kubectl_splunk.py

import unittest
from unittest.mock import patch, mock_open, MagicMock
import os
import sys
import subprocess
import json

# Add the parent directory to sys.path to locate the kubectl_splunk package
sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), '..')))

from kubectl_splunk.main import (
    load_config,
    parse_args,
    setup_logging,
    get_pods,
    select_pods,
    cache_pod,
    load_cached_pod,
    get_credentials,
    retrieve_password_from_pod,
    execute_on_pod,
    copy_to_pod,
    execute_rest_call
)

class TestKubectlSplunk(unittest.TestCase):

    @patch('kubectl_splunk.main.os.path.exists', return_value=True)
    @patch('kubectl_splunk.main.configparser.ConfigParser')
    def test_load_config_with_file(self, mock_configparser, mock_exists):
        # Configure the mock ConfigParser instance
        mock_config = MagicMock()
        mock_configparser.return_value = mock_config

        # Mock the 'get' method for string values
        mock_config.get.side_effect = [
            'splunk-namespace',          # namespace
            'app=splunk',                # selector
            '/opt/splunk/bin/splunk',    # splunk_path
            'splunk-idxc-indexer-0'      # pod_name
        ]

        # Mock the 'getint' method for integer values
        mock_config.getint.return_value = 8089

        # Call the function under test
        namespace, selector, splunk_path, pod_name, local_port = load_config()

        # Assertions
        mock_config.read.assert_called_once_with(os.path.expanduser('~/.kubectl_splunk_config'))
        self.assertEqual(namespace, 'splunk-namespace')
        self.assertEqual(selector, 'app=splunk')
        self.assertEqual(splunk_path, '/opt/splunk/bin/splunk')
        self.assertEqual(pod_name, 'splunk-idxc-indexer-0')
        self.assertEqual(local_port, 8089)


    @patch('kubectl_splunk.main.os.path.exists')
    def test_load_config_without_file(self, mock_exists):
        mock_exists.return_value = False

        namespace, selector, splunk_path, pod_name, local_port = load_config()

        self.assertEqual(namespace, 'default')
        self.assertEqual(selector, 'app=splunk')
        self.assertEqual(splunk_path, '/opt/splunk/bin/splunk')
        self.assertIsNone(pod_name)
        self.assertEqual(local_port, 8089)

    @patch('kubectl_splunk.main.argparse.ArgumentParser.parse_args')
    def test_parse_args_exec_mode(self, mock_parse_args):
        mock_args = MagicMock()
        mock_args.namespace = 'default'
        mock_args.selector = 'app=splunk'
        mock_args.context = None
        mock_args.pod = None
        mock_args.interactive = False
        mock_args.splunk_path = '/opt/splunk/bin/splunk'
        mock_args.local_port = 8089
        mock_args.verbose = 0
        mock_args.version = False
        mock_args.username = 'admin'
        mock_args.password = 'password'
        mock_args.insecure = False
        mock_args.save_credentials = False
        mock_args.mode = 'exec'
        mock_args.splunk_command = ['list', 'user']
        mock_parse_args.return_value = mock_args

        args = parse_args('default', 'app=splunk', 'splunk', None, 8089)
        self.assertEqual(args.mode, 'exec')
        self.assertEqual(args.splunk_command, ['list', 'user'])

    @patch('kubectl_splunk.main.subprocess.check_output')
    @patch('kubectl_splunk.main.logging')
    def test_get_pods_single_pod(self, mock_logging, mock_check_output):
        mock_check_output.return_value = 'splunk-pod-1'

        args = MagicMock()
        args.pod = None
        args.context = None
        args.namespace = 'default'
        args.selector = 'app=splunk'

        pods = get_pods(args)
        self.assertEqual(pods, ['splunk-pod-1'])
        mock_check_output.assert_called_once()

    @patch('kubectl_splunk.main.subprocess.check_output')
    @patch('kubectl_splunk.main.logging')
    def test_get_pods_no_pods_found(self, mock_logging, mock_check_output):
        mock_check_output.return_value = ''

        args = MagicMock()
        args.pod = None
        args.context = None
        args.namespace = 'default'
        args.selector = 'app=splunk'

        with self.assertRaises(SystemExit) as cm:
            get_pods(args)
        self.assertEqual(cm.exception.code, 1)
        mock_logging.error.assert_called_with(
            "No Splunk pods found with selector 'app=splunk' in namespace 'default'."
        )

    def test_select_pods_single_pod(self):
        pods = ['splunk-pod-1']
        args = MagicMock()
        selected = select_pods(pods, args)
        self.assertEqual(selected, ['splunk-pod-1'])

    @patch('builtins.input', return_value='1')
    def test_select_pods_multiple_pods(self, mock_input):
        pods = ['splunk-pod-1', 'splunk-pod-2']
        args = MagicMock()
        selected = select_pods(pods, args)
        self.assertEqual(selected, ['splunk-pod-1'])

    @patch('builtins.input', return_value='0')
    def test_select_pods_run_on_all(self, mock_input):
        pods = ['splunk-pod-1', 'splunk-pod-2']
        args = MagicMock()
        selected = select_pods(pods, args)
        self.assertEqual(selected, ['splunk-pod-1', 'splunk-pod-2'])

    @patch('kubectl_splunk.main.open', new_callable=mock_open)
    @patch('kubectl_splunk.main.json.dump')
    def test_cache_pod(self, mock_json_dump, mock_open_file):
        cache_pod('splunk-pod-1', 'default', 'app=splunk')
        mock_open_file.assert_called_with('/tmp/kubectl_splunk_cache.json', 'w')
        mock_json_dump.assert_called()

    @patch('kubectl_splunk.main.os.path.exists', return_value=True) 
    @patch('kubectl_splunk.main.open', new_callable=mock_open, read_data='{"pod_name": "splunk-pod-1", "namespace": "default", "selector": "app=splunk", "timestamp": 1000}')
    @patch('kubectl_splunk.main.time.time', return_value=1299)  # 300 seconds later
    def test_load_cached_pod_valid(self, mock_time, mock_open_file, mock_exists):
        namespace = 'default'
        selector = 'app=splunk'
        pod_name = load_cached_pod(namespace, selector)
        self.assertEqual(pod_name, 'splunk-pod-1')

    @patch('kubectl_splunk.main.os.path.exists', return_value=True)
    @patch('kubectl_splunk.main.open', new_callable=mock_open, read_data='{"pod_name": "splunk-pod-1", "namespace": "default", "selector": "app=splunk", "timestamp": 1000}')
    @patch('kubectl_splunk.main.time.time', return_value=1600)  # 600 seconds later, cache expired
    def test_load_cached_pod_expired(self, mock_time, mock_open_file, mock_exists):
        namespace = 'default'
        selector = 'app=splunk'
        pod_name = load_cached_pod(namespace, selector)
        self.assertIsNone(pod_name)

    @patch('kubectl_splunk.main.open', new_callable=mock_open, read_data='{"username": "admin", "password": "password"}')
    @patch('kubectl_splunk.main.json.load')
    @patch('kubectl_splunk.main.os.path.exists', return_value=True)
    @patch('kubectl_splunk.main.retrieve_password_from_pod')
    def test_get_credentials_from_file(self, mock_retrieve_password, mock_exists, mock_json_load, mock_open_file):
        mock_json_load.return_value = {'username': 'admin', 'password': 'password'}
        args = MagicMock()
        args.username = None
        args.password = None
        args.save_credentials = False

        with patch('kubectl_splunk.main.getpass.getpass', return_value='password') as mock_getpass:
            username, password = get_credentials(args, 'splunk-pod-1')
            self.assertEqual(username, 'admin')
            self.assertEqual(password, 'password')
            mock_retrieve_password.assert_not_called()

    @patch('kubectl_splunk.main.open', new_callable=mock_open)
    @patch('kubectl_splunk.main.os.path.exists', return_value=False)
    @patch('kubectl_splunk.main.retrieve_password_from_pod')
    @patch('kubectl_splunk.main.getpass.getpass', return_value='password')
    def test_get_credentials_from_pod(self, mock_getpass, mock_retrieve_password, mock_exists, mock_open_file):
        mock_retrieve_password.return_value = 'password'
        args = MagicMock()
        args.username = None
        args.password = None
        args.save_credentials = False

        username, password = get_credentials(args, 'splunk-pod-1')
        self.assertEqual(username, 'admin')
        self.assertEqual(password, 'password')
        mock_retrieve_password.assert_called_once_with(args, 'splunk-pod-1')

    @patch('kubectl_splunk.main.subprocess.check_output')
    def test_retrieve_password_from_pod_success(self, mock_check_output):
        mock_check_output.return_value = 'password123'

        args = MagicMock()
        args.context = None
        args.namespace = 'default'

        password = retrieve_password_from_pod(args, 'splunk-pod-1')
        self.assertEqual(password, 'password123')
        mock_check_output.assert_called_once_with([
            'kubectl', 'exec', '-n', 'default', 'splunk-pod-1', '--', 'cat', '/mnt/splunk-secrets/password'
        ], universal_newlines=True)

    @patch('kubectl_splunk.main.subprocess.run')
    @patch('kubectl_splunk.main.get_credentials')
    @patch('kubectl_splunk.main.logging')
    def test_execute_on_pod_exec_mode(self, mock_logging, mock_get_credentials, mock_subprocess_run):
        mock_get_credentials.return_value = ('admin', 'password123')
        args = MagicMock()
        args.mode = 'exec'
        args.splunk_path = '/opt/splunk/bin/splunk'
        args.splunk_command = ['list', 'user']
        args.username = 'admin'
        args.password = None
        args.save_credentials = False
        args.context = None
        args.namespace = 'default'

        execute_on_pod(args, 'splunk-pod-1')

        expected_cmd = [
            'kubectl', 'exec', '-n', 'default', 'splunk-pod-1', '--',
            '/opt/splunk/bin/splunk', 'list', 'user', '-auth', 'admin:password123'
        ]
        mock_subprocess_run.assert_called_once_with(expected_cmd, check=True)
        mock_logging.debug.assert_called()

    @patch('kubectl_splunk.main.subprocess.run')
    @patch('kubectl_splunk.main.logging')
    def test_copy_to_pod(self, mock_logging, mock_subprocess_run):
        args = MagicMock()
        args.context = None
        args.namespace = 'default'
        args.src = '/local/path/file.txt'
        args.dest = ':/remote/path/file.txt'

        copy_to_pod(args, 'splunk-pod-1')

        expected_cmd = [
            'kubectl', 'cp',
            '/local/path/file.txt',
            'default/splunk-pod-1:/remote/path/file.txt'
        ]
        mock_subprocess_run.assert_called_once_with(expected_cmd, check=True)
        mock_logging.debug.assert_called()

    @patch('kubectl_splunk.main.subprocess.Popen')
    @patch('kubectl_splunk.main.requests.request')
    @patch('kubectl_splunk.main.get_credentials')
    @patch('kubectl_splunk.main.logging')
    def test_execute_rest_call(self, mock_logging, mock_get_credentials, mock_requests_request, mock_popen):
        mock_get_credentials.return_value = ('admin', 'password123')
        mock_popen.return_value.poll.side_effect = [None] * 10  # Simulate port-forward staying alive
        mock_requests_request.return_value.status_code = 200
        mock_requests_request.return_value.text = 'Success'

        args = MagicMock()
        args.mode = 'rest'
        args.method = 'GET'
        args.endpoint = '/services/server/info'
        args.data = None
        args.params = None
        args.username = 'admin'
        args.password = None
        args.insecure = False
        args.save_credentials = False
        args.context = None
        args.namespace = 'default'
        args.local_port = 8089

        execute_rest_call(args, 'splunk-pod-1')

        # Check that port-forward was started
        expected_port_forward_cmd = [
            'kubectl', 'port-forward', '-n', 'default', 'splunk-pod-1', '8089:8089'
        ]
        mock_popen.assert_called_once_with(expected_port_forward_cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE)

        # Check that REST API call was made
        expected_url = 'https://127.0.0.1:8089/services/server/info'
        mock_requests_request.assert_called_once_with(
            method='GET',
            url=expected_url,
            headers={'Content-Type': 'application/json'},
            params={},
            data=None,
            auth=('admin', 'password123'),
            verify=True
        )

        # Check that the response was printed
        # Since we cannot capture print statements easily here, we assume it was done correctly

    @patch('kubectl_splunk.main.subprocess.run')
    @patch('kubectl_splunk.main.logging')
    def test_execute_on_pod_command_failure(self, mock_logging, mock_subprocess_run):
        mock_subprocess_run.side_effect = subprocess.CalledProcessError(1, 'cmd')

        args = MagicMock()
        args.mode = 'exec'
        args.splunk_path = '/opt/splunk/bin/splunk'
        args.splunk_command = ['list', 'user']
        args.username = 'admin'
        args.password = 'password123'
        args.save_credentials = False
        args.context = None
        args.namespace = 'default'

        with self.assertRaises(SystemExit) as cm:
            execute_on_pod(args, 'splunk-pod-1')
        self.assertEqual(cm.exception.code, 1)
        mock_logging.error.assert_called_with(
            "Command failed on pod splunk-pod-1 with exit code 1"
        )

    @patch('kubectl_splunk.main.subprocess.run')
    @patch('kubectl_splunk.main.logging')
    def test_copy_to_pod_failure(self, mock_logging, mock_subprocess_run):
        mock_subprocess_run.side_effect = subprocess.CalledProcessError(1, 'cp')

        args = MagicMock()
        args.context = None
        args.namespace = 'default'
        args.src = '/local/path/file.txt'
        args.dest = ':/remote/path/file.txt'

        with self.assertRaises(SystemExit) as cm:
            copy_to_pod(args, 'splunk-pod-1')
        self.assertEqual(cm.exception.code, 1)
        mock_logging.error.assert_called_with(
            "Copy command failed with exit code 1"
        )

    @patch('kubectl_splunk.main.subprocess.Popen')
    @patch('kubectl_splunk.main.requests.request')
    @patch('kubectl_splunk.main.get_credentials')
    @patch('kubectl_splunk.main.logging')
    def test_execute_rest_call_http_error(self, mock_logging, mock_get_credentials, mock_requests_request, mock_popen):
        mock_get_credentials.return_value = ('admin', 'password123')
        mock_popen.return_value.poll.side_effect = [None] * 10  # Simulate port-forward staying alive
        mock_requests_request.return_value.status_code = 404
        mock_requests_request.return_value.reason = 'Not Found'
        mock_requests_request.return_value.text = 'Error'

        args = MagicMock()
        args.mode = 'rest'
        args.method = 'GET'
        args.endpoint = '/services/server/info'
        args.data = None
        args.params = None
        args.username = 'admin'
        args.password = None
        args.insecure = False
        args.save_credentials = False
        args.context = None
        args.namespace = 'default'
        args.local_port = 8089

        with self.assertRaises(SystemExit) as cm:
            execute_rest_call(args, 'splunk-pod-1')
        self.assertEqual(cm.exception.code, 1)
        mock_logging.error.assert_called_with(
            "HTTP 404: Not Found"
        )

if __name__ == '__main__':
    unittest.main()

