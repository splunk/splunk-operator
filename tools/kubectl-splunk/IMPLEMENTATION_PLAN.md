# Implementation Plan: kubectl-splunk Diagnostic & Analysis Features

## Overview

This document provides a detailed implementation plan for adding diagnostic collection and log analysis capabilities to the kubectl-splunk plugin.

## Architecture

```
kubectl_splunk/
├── main.py                    # Main entry point (existing)
├── collectors/               # New: Data collection modules
│   ├── __init__.py
│   ├── base.py              # Base collector class
│   ├── kubernetes.py        # K8s resource collector
│   ├── splunk_config.py     # Splunk configuration collector
│   ├── logs.py              # Log file collector
│   ├── metrics.py           # Performance metrics collector
│   └── operator.py          # Splunk Operator CRD collector
├── analyzers/               # New: Log analysis modules
│   ├── __init__.py
│   ├── base.py              # Base analyzer class
│   ├── patterns.py          # Error pattern matching
│   ├── health.py            # Health check engine
│   ├── performance.py       # Performance analysis
│   └── recommendations.py   # Recommendation engine
├── reporters/               # New: Report generation modules
│   ├── __init__.py
│   ├── base.py              # Base reporter class
│   ├── text.py              # Plain text reports
│   ├── json.py              # JSON reports
│   └── html.py              # HTML reports
└── utils/                   # New: Utility functions
    ├── __init__.py
    ├── patterns.py          # Common regex patterns
    ├── kubernetes.py        # K8s helper functions
    └── splunk.py            # Splunk-specific utilities
```

## Phase 1: Foundation & Basic Diagnostic Collection

### 1.1 Base Collector Framework

Create base classes for all collectors:

```python
# kubectl_splunk/collectors/base.py

import logging
from abc import ABC, abstractmethod
from typing import Dict, Any, List

class BaseCollector(ABC):
    """Base class for all diagnostic collectors."""

    def __init__(self, args, pod_name: str):
        self.args = args
        self.pod_name = pod_name
        self.logger = logging.getLogger(self.__class__.__name__)
        self.data = {}

    @abstractmethod
    def collect(self) -> Dict[str, Any]:
        """Collect diagnostic data. Must be implemented by subclasses."""
        pass

    def get_data(self) -> Dict[str, Any]:
        """Return collected data."""
        return self.data

    def cleanup(self):
        """Cleanup resources if needed."""
        pass
```

### 1.2 Kubernetes Resource Collector

```python
# kubectl_splunk/collectors/kubernetes.py

import subprocess
import json
from typing import Dict, Any, List
from .base import BaseCollector

class KubernetesCollector(BaseCollector):
    """Collects Kubernetes resource information."""

    def collect(self) -> Dict[str, Any]:
        """Collect K8s resources related to Splunk pods."""
        self.logger.info(f"Collecting Kubernetes resources for pod: {self.pod_name}")

        self.data = {
            'pod': self._get_pod_info(),
            'events': self._get_pod_events(),
            'logs': self._get_container_logs(),
            'services': self._get_services(),
            'pvcs': self._get_pvcs(),
            'statefulset': self._get_statefulset(),
            'configmaps': self._get_configmaps(),
        }

        return self.data

    def _get_pod_info(self) -> Dict[str, Any]:
        """Get detailed pod information."""
        cmd = ['kubectl', 'get', 'pod', self.pod_name,
               '-n', self.args.namespace, '-o', 'json']

        if self.args.context:
            cmd.extend(['--context', self.args.context])

        try:
            output = subprocess.check_output(cmd, universal_newlines=True)
            pod_data = json.loads(output)

            return {
                'name': pod_data['metadata']['name'],
                'namespace': pod_data['metadata']['namespace'],
                'status': pod_data['status']['phase'],
                'conditions': pod_data['status'].get('conditions', []),
                'containers': self._extract_container_info(pod_data),
                'node': pod_data['spec'].get('nodeName'),
                'ip': pod_data['status'].get('podIP'),
                'start_time': pod_data['status'].get('startTime'),
            }
        except subprocess.CalledProcessError as e:
            self.logger.error(f"Failed to get pod info: {e}")
            return {}

    def _extract_container_info(self, pod_data: Dict) -> List[Dict]:
        """Extract container information from pod data."""
        containers = []

        for container in pod_data['spec'].get('containers', []):
            container_status = self._find_container_status(
                pod_data, container['name']
            )

            containers.append({
                'name': container['name'],
                'image': container['image'],
                'ready': container_status.get('ready', False) if container_status else False,
                'restart_count': container_status.get('restartCount', 0) if container_status else 0,
                'state': container_status.get('state', {}) if container_status else {},
            })

        return containers

    def _find_container_status(self, pod_data: Dict, container_name: str) -> Dict:
        """Find container status by name."""
        for status in pod_data['status'].get('containerStatuses', []):
            if status['name'] == container_name:
                return status
        return {}

    def _get_pod_events(self) -> List[Dict]:
        """Get events related to the pod."""
        cmd = ['kubectl', 'get', 'events', '-n', self.args.namespace,
               '--field-selector', f'involvedObject.name={self.pod_name}',
               '-o', 'json']

        if self.args.context:
            cmd.extend(['--context', self.args.context])

        try:
            output = subprocess.check_output(cmd, universal_newlines=True)
            events_data = json.loads(output)

            return [{
                'type': event.get('type'),
                'reason': event.get('reason'),
                'message': event.get('message'),
                'count': event.get('count'),
                'first_timestamp': event.get('firstTimestamp'),
                'last_timestamp': event.get('lastTimestamp'),
            } for event in events_data.get('items', [])]
        except subprocess.CalledProcessError as e:
            self.logger.error(f"Failed to get events: {e}")
            return []

    def _get_container_logs(self, tail_lines: int = 100) -> Dict[str, str]:
        """Get recent container logs."""
        cmd = ['kubectl', 'logs', self.pod_name, '-n', self.args.namespace,
               '--tail', str(tail_lines)]

        if self.args.context:
            cmd.extend(['--context', self.args.context])

        try:
            output = subprocess.check_output(cmd, universal_newlines=True,
                                            stderr=subprocess.STDOUT)
            return {'logs': output}
        except subprocess.CalledProcessError as e:
            self.logger.error(f"Failed to get logs: {e}")
            return {'logs': '', 'error': str(e)}

    def _get_services(self) -> List[Dict]:
        """Get services related to the pod."""
        # Get pod labels first
        cmd = ['kubectl', 'get', 'pod', self.pod_name,
               '-n', self.args.namespace, '-o', 'json']

        if self.args.context:
            cmd.extend(['--context', self.args.context])

        try:
            pod_output = subprocess.check_output(cmd, universal_newlines=True)
            pod_data = json.loads(pod_output)
            labels = pod_data['metadata'].get('labels', {})

            # Get services matching labels
            svc_cmd = ['kubectl', 'get', 'svc', '-n', self.args.namespace,
                       '-o', 'json']

            if self.args.context:
                svc_cmd.extend(['--context', self.args.context])

            svc_output = subprocess.check_output(svc_cmd, universal_newlines=True)
            services_data = json.loads(svc_output)

            matching_services = []
            for service in services_data.get('items', []):
                selector = service['spec'].get('selector', {})
                if self._labels_match(labels, selector):
                    matching_services.append({
                        'name': service['metadata']['name'],
                        'type': service['spec'].get('type'),
                        'cluster_ip': service['spec'].get('clusterIP'),
                        'ports': service['spec'].get('ports', []),
                    })

            return matching_services
        except subprocess.CalledProcessError as e:
            self.logger.error(f"Failed to get services: {e}")
            return []

    def _labels_match(self, pod_labels: Dict, selector: Dict) -> bool:
        """Check if pod labels match service selector."""
        for key, value in selector.items():
            if pod_labels.get(key) != value:
                return False
        return True

    def _get_pvcs(self) -> List[Dict]:
        """Get PersistentVolumeClaims used by the pod."""
        cmd = ['kubectl', 'get', 'pod', self.pod_name,
               '-n', self.args.namespace, '-o', 'json']

        if self.args.context:
            cmd.extend(['--context', self.args.context])

        try:
            output = subprocess.check_output(cmd, universal_newlines=True)
            pod_data = json.loads(output)

            pvcs = []
            for volume in pod_data['spec'].get('volumes', []):
                if 'persistentVolumeClaim' in volume:
                    pvc_name = volume['persistentVolumeClaim']['claimName']
                    pvc_info = self._get_pvc_details(pvc_name)
                    if pvc_info:
                        pvcs.append(pvc_info)

            return pvcs
        except subprocess.CalledProcessError as e:
            self.logger.error(f"Failed to get PVCs: {e}")
            return []

    def _get_pvc_details(self, pvc_name: str) -> Dict:
        """Get details for a specific PVC."""
        cmd = ['kubectl', 'get', 'pvc', pvc_name,
               '-n', self.args.namespace, '-o', 'json']

        if self.args.context:
            cmd.extend(['--context', self.args.context])

        try:
            output = subprocess.check_output(cmd, universal_newlines=True)
            pvc_data = json.loads(output)

            return {
                'name': pvc_data['metadata']['name'],
                'status': pvc_data['status'].get('phase'),
                'capacity': pvc_data['status'].get('capacity', {}),
                'storage_class': pvc_data['spec'].get('storageClassName'),
                'access_modes': pvc_data['spec'].get('accessModes', []),
            }
        except subprocess.CalledProcessError as e:
            self.logger.error(f"Failed to get PVC details for {pvc_name}: {e}")
            return {}

    def _get_statefulset(self) -> Dict:
        """Get StatefulSet information if pod is part of one."""
        # Check if pod has controller reference to StatefulSet
        cmd = ['kubectl', 'get', 'pod', self.pod_name,
               '-n', self.args.namespace, '-o', 'json']

        if self.args.context:
            cmd.extend(['--context', self.args.context])

        try:
            output = subprocess.check_output(cmd, universal_newlines=True)
            pod_data = json.loads(output)

            owner_refs = pod_data['metadata'].get('ownerReferences', [])
            for ref in owner_refs:
                if ref['kind'] == 'StatefulSet':
                    return self._get_statefulset_details(ref['name'])

            return {}
        except subprocess.CalledProcessError as e:
            self.logger.error(f"Failed to get StatefulSet: {e}")
            return {}

    def _get_statefulset_details(self, sts_name: str) -> Dict:
        """Get StatefulSet details."""
        cmd = ['kubectl', 'get', 'statefulset', sts_name,
               '-n', self.args.namespace, '-o', 'json']

        if self.args.context:
            cmd.extend(['--context', self.args.context])

        try:
            output = subprocess.check_output(cmd, universal_newlines=True)
            sts_data = json.loads(output)

            return {
                'name': sts_data['metadata']['name'],
                'replicas': sts_data['spec'].get('replicas'),
                'ready_replicas': sts_data['status'].get('readyReplicas', 0),
                'current_replicas': sts_data['status'].get('currentReplicas', 0),
                'update_strategy': sts_data['spec'].get('updateStrategy', {}),
            }
        except subprocess.CalledProcessError as e:
            self.logger.error(f"Failed to get StatefulSet details: {e}")
            return {}

    def _get_configmaps(self) -> List[str]:
        """Get ConfigMap names used by the pod."""
        cmd = ['kubectl', 'get', 'pod', self.pod_name,
               '-n', self.args.namespace, '-o', 'json']

        if self.args.context:
            cmd.extend(['--context', self.args.context])

        try:
            output = subprocess.check_output(cmd, universal_newlines=True)
            pod_data = json.loads(output)

            configmaps = set()
            for volume in pod_data['spec'].get('volumes', []):
                if 'configMap' in volume:
                    configmaps.add(volume['configMap']['name'])

            return list(configmaps)
        except subprocess.CalledProcessError as e:
            self.logger.error(f"Failed to get ConfigMaps: {e}")
            return []
```

### 1.3 Splunk Configuration Collector

```python
# kubectl_splunk/collectors/splunk_config.py

import subprocess
import tempfile
import os
from typing import Dict, Any, List
from .base import BaseCollector

class SplunkConfigCollector(BaseCollector):
    """Collects Splunk configuration files."""

    def collect(self) -> Dict[str, Any]:
        """Collect Splunk configuration files."""
        self.logger.info(f"Collecting Splunk configurations from pod: {self.pod_name}")

        self.data = {
            'server_info': self._get_server_info(),
            'system_local': self._get_system_local_configs(),
            'apps': self._get_app_configs(),
            'indexes': self._get_index_info(),
            'cluster_config': self._get_cluster_config(),
        }

        return self.data

    def _get_server_info(self) -> Dict[str, Any]:
        """Get Splunk server information."""
        cmd = [
            'kubectl', 'exec', self.pod_name,
            '-n', self.args.namespace, '--',
            self.args.splunk_path, 'show', 'servername'
        ]

        if self.args.context:
            cmd[1:1] = ['--context', self.args.context]

        try:
            output = subprocess.check_output(cmd, universal_newlines=True)
            return {'servername': output.strip()}
        except subprocess.CalledProcessError as e:
            self.logger.error(f"Failed to get server info: {e}")
            return {}

    def _get_system_local_configs(self) -> Dict[str, str]:
        """Get system/local configuration files."""
        config_files = [
            'server.conf',
            'inputs.conf',
            'outputs.conf',
            'web.conf',
            'authentication.conf',
        ]

        configs = {}
        for config_file in config_files:
            path = f'/opt/splunk/etc/system/local/{config_file}'
            content = self._read_file_from_pod(path)
            if content:
                configs[config_file] = content

        return configs

    def _read_file_from_pod(self, file_path: str) -> str:
        """Read a file from the pod."""
        cmd = [
            'kubectl', 'exec', self.pod_name,
            '-n', self.args.namespace, '--',
            'cat', file_path
        ]

        if self.args.context:
            cmd[1:1] = ['--context', self.args.context]

        try:
            output = subprocess.check_output(cmd, universal_newlines=True,
                                            stderr=subprocess.DEVNULL)
            return output
        except subprocess.CalledProcessError:
            # File might not exist, which is OK
            return ""

    def _get_app_configs(self) -> Dict[str, List[str]]:
        """Get list of installed apps."""
        cmd = [
            'kubectl', 'exec', self.pod_name,
            '-n', self.args.namespace, '--',
            'ls', '/opt/splunk/etc/apps'
        ]

        if self.args.context:
            cmd[1:1] = ['--context', self.args.context]

        try:
            output = subprocess.check_output(cmd, universal_newlines=True)
            apps = output.strip().split('\n')
            return {'installed_apps': apps}
        except subprocess.CalledProcessError as e:
            self.logger.error(f"Failed to get apps: {e}")
            return {}

    def _get_index_info(self) -> List[Dict[str, Any]]:
        """Get index information."""
        # Using btool to get index configurations
        cmd = [
            'kubectl', 'exec', self.pod_name,
            '-n', self.args.namespace, '--',
            self.args.splunk_path, 'cmd', 'btool', 'indexes', 'list',
            '--debug'
        ]

        if self.args.context:
            cmd[1:1] = ['--context', self.args.context]

        try:
            output = subprocess.check_output(cmd, universal_newlines=True,
                                            stderr=subprocess.DEVNULL)
            # Parse btool output
            indexes = self._parse_btool_output(output)
            return indexes
        except subprocess.CalledProcessError as e:
            self.logger.error(f"Failed to get index info: {e}")
            return []

    def _parse_btool_output(self, output: str) -> List[Dict]:
        """Parse btool output into structured data."""
        indexes = []
        current_index = None

        for line in output.split('\n'):
            line = line.strip()
            if line.startswith('[') and line.endswith(']'):
                if current_index:
                    indexes.append(current_index)
                current_index = {'name': line[1:-1], 'settings': {}}
            elif '=' in line and current_index:
                key, value = line.split('=', 1)
                current_index['settings'][key.strip()] = value.strip()

        if current_index:
            indexes.append(current_index)

        return indexes

    def _get_cluster_config(self) -> Dict[str, Any]:
        """Get cluster configuration if applicable."""
        # Check for clustering mode
        cmd = [
            'kubectl', 'exec', self.pod_name,
            '-n', self.args.namespace, '--',
            self.args.splunk_path, 'show', 'cluster-config'
        ]

        if self.args.context:
            cmd[1:1] = ['--context', self.args.context]

        try:
            output = subprocess.check_output(cmd, universal_newlines=True,
                                            stderr=subprocess.DEVNULL)
            return {'cluster_config': output}
        except subprocess.CalledProcessError:
            # Not in a cluster or command not available
            return {}
```

### 1.4 Main Module Integration

Add diagnostic command to main.py:

```python
# Add to kubectl_splunk/main.py

from collectors.kubernetes import KubernetesCollector
from collectors.splunk_config import SplunkConfigCollector
import json
import tarfile
import os
from datetime import datetime

def parse_args(...):
    # ... existing code ...

    # Add diag subparser
    diag_parser = subparsers.add_parser('diag', help='Collect diagnostic information')
    diag_parser.add_argument('--output-dir', default='/tmp',
                            help='Directory to save diagnostic bundle')
    diag_parser.add_argument('--full', action='store_true',
                            help='Collect full diagnostics (may take longer)')
    diag_parser.add_argument('--quick', action='store_true',
                            help='Quick health check only')

    # ... existing code ...

def collect_diagnostics(args, pod_name):
    """Collect diagnostic information from a pod."""
    output_dir = args.output_dir
    timestamp = datetime.now().strftime('%Y-%m-%d-%H%M%S')
    diag_bundle_name = f"splunk-diag-{pod_name}-{timestamp}"
    diag_dir = os.path.join(output_dir, diag_bundle_name)

    os.makedirs(diag_dir, exist_ok=True)

    print(f"Collecting diagnostics for pod: {pod_name}")

    # Collect Kubernetes resources
    print("[1/3] Collecting Kubernetes resources...")
    k8s_collector = KubernetesCollector(args, pod_name)
    k8s_data = k8s_collector.collect()

    with open(os.path.join(diag_dir, 'kubernetes.json'), 'w') as f:
        json.dump(k8s_data, f, indent=2)

    # Collect Splunk configurations
    print("[2/3] Collecting Splunk configurations...")
    splunk_collector = SplunkConfigCollector(args, pod_name)
    splunk_data = splunk_collector.collect()

    with open(os.path.join(diag_dir, 'splunk_config.json'), 'w') as f:
        json.dump(splunk_data, f, indent=2)

    # Create tarball
    print("[3/3] Creating diagnostic bundle...")
    tarball_path = f"{diag_dir}.tar.gz"
    with tarfile.open(tarball_path, "w:gz") as tar:
        tar.add(diag_dir, arcname=diag_bundle_name)

    # Cleanup directory
    import shutil
    shutil.rmtree(diag_dir)

    print(f"\nDiagnostic bundle saved to: {tarball_path}")
    print(f"Bundle size: {os.path.getsize(tarball_path) / (1024*1024):.2f} MB")

def main():
    # ... existing code ...

    # Handle diag mode
    if args.mode == 'diag':
        if len(pods) > 1:
            logging.error("Diag mode requires a single pod. Please specify with --pod")
            sys.exit(1)
        collect_diagnostics(args, pods[0])

    # ... existing code ...
```

## Next Steps

This implementation provides:

1. ✅ Base framework for collectors
2. ✅ Kubernetes resource collection
3. ✅ Splunk configuration collection
4. ✅ Diagnostic bundle creation

**Phase 2** would add:
- Log collectors
- Metrics collectors
- Splunk Operator CRD collectors
- HTML report generation

**Phase 3** would add:
- Log analysis with pattern matching
- Health checks
- Performance analysis
- Recommendation engine

Would you like me to continue with Phase 2 or Phase 3 implementation?
