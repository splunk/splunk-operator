# Log Analysis Patterns and Health Checks

## Common Splunk Error Patterns

### Critical Errors

#### 1. Indexing Failures
```python
PATTERNS = {
    'indexing_failure': {
        'regex': r'ERROR\s+IndexProcessor\s+-\s+.*failed',
        'severity': 'critical',
        'category': 'indexing',
        'description': 'Indexing pipeline failure detected',
        'recommendation': 'Check disk space, permissions, and index configuration'
    },
    'hot_bucket_failure': {
        'regex': r'ERROR\s+HotBucketRoller\s+-\s+.*failed to roll',
        'severity': 'critical',
        'category': 'indexing',
        'description': 'Hot bucket cannot be rolled to warm',
        'recommendation': 'Check disk space on index volume and verify filesystem health'
    },
    'disk_full': {
        'regex': r'ERROR.*No space left on device|disk.*full',
        'severity': 'critical',
        'category': 'storage',
        'description': 'Disk space exhausted',
        'recommendation': 'Increase PVC size or enable frozen bucket deletion'
    }
}
```

#### 2. Clustering Issues
```python
CLUSTERING_PATTERNS = {
    'peer_disconnected': {
        'regex': r'WARN\s+CMSlave\s+-\s+.*Cluster master is not available',
        'severity': 'critical',
        'category': 'clustering',
        'description': 'Indexer lost connection to cluster manager',
        'recommendation': 'Check network connectivity and cluster manager status'
    },
    'replication_failure': {
        'regex': r'ERROR\s+ReplicationManager\s+-\s+.*failed to replicate',
        'severity': 'critical',
        'category': 'clustering',
        'description': 'Bucket replication failure',
        'recommendation': 'Verify peer connectivity and replication factor settings'
    },
    'search_factor_not_met': {
        'regex': r'WARN.*SearchFactorNotMet',
        'severity': 'warning',
        'category': 'clustering',
        'description': 'Search factor requirement not satisfied',
        'recommendation': 'Add more search peers or reduce search factor'
    },
    'captain_election_failed': {
        'regex': r'ERROR\s+SHCSlaveHandler\s+-\s+.*captain election failed',
        'severity': 'critical',
        'category': 'search_head_clustering',
        'description': 'Search head captain election failure',
        'recommendation': 'Check SHC member connectivity and quorum requirements'
    }
}
```

#### 3. License Issues
```python
LICENSE_PATTERNS = {
    'license_expired': {
        'regex': r'WARN\s+LMTracker\s+-\s+.*license.*expired',
        'severity': 'critical',
        'category': 'licensing',
        'description': 'Splunk license has expired',
        'recommendation': 'Upload a valid license file immediately'
    },
    'license_quota_exceeded': {
        'regex': r'WARN\s+LicenseUsage\s+-\s+.*quota exceeded',
        'severity': 'warning',
        'category': 'licensing',
        'description': 'Daily indexing quota exceeded',
        'recommendation': 'Review data sources or upgrade license'
    },
    'license_pool_exhausted': {
        'regex': r'WARN.*license pool.*exhausted',
        'severity': 'warning',
        'category': 'licensing',
        'description': 'License pool capacity reached',
        'recommendation': 'Adjust pool allocations or add more license capacity'
    }
}
```

#### 4. Authentication/Security
```python
SECURITY_PATTERNS = {
    'auth_failure': {
        'regex': r'ERROR\s+AuthenticationManagerSplunk\s+-\s+.*authentication failed',
        'severity': 'warning',
        'category': 'authentication',
        'description': 'User authentication failure',
        'recommendation': 'Check credentials and LDAP/SAML configuration'
    },
    'ssl_cert_expiring': {
        'regex': r'WARN.*SSL certificate.*expir(ing|es)',
        'severity': 'warning',
        'category': 'security',
        'description': 'SSL certificate approaching expiration',
        'recommendation': 'Renew SSL certificates before expiration'
    },
    'splunkd_ssl_error': {
        'regex': r'ERROR.*SSL.*handshake failed',
        'severity': 'warning',
        'category': 'security',
        'description': 'SSL handshake failure',
        'recommendation': 'Verify SSL configuration and certificate validity'
    }
}
```

#### 5. Search Issues
```python
SEARCH_PATTERNS = {
    'search_timeout': {
        'regex': r'ERROR\s+SearchOrchestrator\s+-\s+.*timed out',
        'severity': 'warning',
        'category': 'search',
        'description': 'Search execution timeout',
        'recommendation': 'Optimize search query or increase timeout limits'
    },
    'search_memory_exceeded': {
        'regex': r'ERROR.*search.*exceeded memory limit',
        'severity': 'warning',
        'category': 'search',
        'description': 'Search process exceeded memory limits',
        'recommendation': 'Optimize search or increase memory allocation'
    },
    'bundle_replication_failed': {
        'regex': r'ERROR\s+DistributedBundleReplicationManager\s+-\s+.*failed',
        'severity': 'warning',
        'category': 'search',
        'description': 'Knowledge bundle replication failure',
        'recommendation': 'Check search head to indexer connectivity'
    }
}
```

#### 6. Performance Issues
```python
PERFORMANCE_PATTERNS = {
    'queue_full': {
        'regex': r'WARN.*queue.*full|blocked',
        'severity': 'warning',
        'category': 'performance',
        'description': 'Processing queue is full',
        'recommendation': 'Review queue settings and system resources'
    },
    'slow_disk_io': {
        'regex': r'WARN.*slow.*disk|I/O.*slow',
        'severity': 'warning',
        'category': 'performance',
        'description': 'Slow disk I/O detected',
        'recommendation': 'Check storage performance and IOPS capacity'
    },
    'high_cpu_usage': {
        'regex': r'WARN.*CPU usage.*high',
        'severity': 'warning',
        'category': 'performance',
        'description': 'High CPU utilization',
        'recommendation': 'Review resource requests/limits and workload'
    }
}
```

## Health Check Definitions

### 1. Cluster Health Checks

```python
HEALTH_CHECKS = {
    'indexer_cluster_health': {
        'name': 'Indexer Cluster Health',
        'command': 'splunk show cluster-status',
        'checks': [
            {
                'name': 'Replication Factor Met',
                'pattern': r'replication_factor_met.*true',
                'severity': 'critical',
                'fail_message': 'Replication factor not met - data at risk'
            },
            {
                'name': 'Search Factor Met',
                'pattern': r'search_factor_met.*true',
                'severity': 'warning',
                'fail_message': 'Search factor not met - search performance impacted'
            },
            {
                'name': 'All Peers Connected',
                'pattern': r'peers_down.*0',
                'severity': 'warning',
                'fail_message': 'One or more indexer peers disconnected'
            }
        ]
    },

    'search_head_cluster_health': {
        'name': 'Search Head Cluster Health',
        'command': 'splunk show shcluster-status',
        'checks': [
            {
                'name': 'Captain Elected',
                'pattern': r'elected_captain.*present',
                'severity': 'critical',
                'fail_message': 'No SHC captain elected'
            },
            {
                'name': 'All Members Connected',
                'pattern': r'members.*ready',
                'severity': 'warning',
                'fail_message': 'Not all search head members are connected'
            },
            {
                'name': 'Service Ready',
                'pattern': r'service_ready_flag.*true',
                'severity': 'warning',
                'fail_message': 'Search head cluster not ready'
            }
        ]
    },

    'license_health': {
        'name': 'License Health',
        'command': 'splunk show license-usage',
        'checks': [
            {
                'name': 'License Valid',
                'pattern': r'status.*valid',
                'severity': 'critical',
                'fail_message': 'License is invalid or expired'
            },
            {
                'name': 'Within Quota',
                'pattern': r'quota_used.*[0-7][0-9]%',  # Less than 80%
                'severity': 'warning',
                'fail_message': 'License quota usage over 80%'
            }
        ]
    }
}
```

### 2. Kubernetes Health Checks

```python
K8S_HEALTH_CHECKS = {
    'pod_health': {
        'name': 'Pod Health',
        'checks': [
            {
                'name': 'Pod Running',
                'field': 'status.phase',
                'expected': 'Running',
                'severity': 'critical',
                'fail_message': 'Pod is not in Running state'
            },
            {
                'name': 'All Containers Ready',
                'field': 'status.containerStatuses[*].ready',
                'expected': True,
                'severity': 'critical',
                'fail_message': 'One or more containers not ready'
            },
            {
                'name': 'Low Restart Count',
                'field': 'status.containerStatuses[*].restartCount',
                'threshold': 5,
                'operator': '<',
                'severity': 'warning',
                'fail_message': 'Container has restarted multiple times'
            }
        ]
    },

    'storage_health': {
        'name': 'Storage Health',
        'checks': [
            {
                'name': 'PVC Bound',
                'field': 'status.phase',
                'expected': 'Bound',
                'severity': 'critical',
                'fail_message': 'PersistentVolumeClaim not bound'
            },
            {
                'name': 'Storage Not Full',
                'check_type': 'exec',
                'command': 'df -h /opt/splunk/var',
                'pattern': r'(\d+)%',
                'threshold': 90,
                'operator': '<',
                'severity': 'warning',
                'fail_message': 'Storage volume over 90% full'
            }
        ]
    },

    'resource_health': {
        'name': 'Resource Health',
        'checks': [
            {
                'name': 'Memory Not Exceeded',
                'check_type': 'metrics',
                'metric': 'container_memory_usage_bytes',
                'threshold_field': 'resources.limits.memory',
                'threshold_percentage': 90,
                'severity': 'warning',
                'fail_message': 'Memory usage over 90% of limit'
            },
            {
                'name': 'CPU Not Throttled',
                'check_type': 'metrics',
                'metric': 'container_cpu_cfs_throttled_seconds_total',
                'rate': '5m',
                'threshold': 0.1,
                'operator': '<',
                'severity': 'warning',
                'fail_message': 'Container is being CPU throttled'
            }
        ]
    }
}
```

### 3. Performance Checks

```python
PERFORMANCE_CHECKS = {
    'indexing_performance': {
        'name': 'Indexing Performance',
        'metrics': [
            {
                'name': 'Indexing Rate',
                'command': 'splunk list index',
                'extract': r'currentDBSizeMB.*?(\d+)',
                'unit': 'MB',
                'threshold': 1000,
                'operator': '>',
                'severity': 'info',
                'message': 'Indexing rate normal'
            },
            {
                'name': 'Index Lag',
                'log_file': 'metrics.log',
                'pattern': r'group=queue.*name=indexQueue.*current_size=(\d+)',
                'threshold': 1000,
                'operator': '<',
                'severity': 'warning',
                'fail_message': 'Index queue filling up'
            }
        ]
    },

    'search_performance': {
        'name': 'Search Performance',
        'metrics': [
            {
                'name': 'Search Concurrency',
                'log_file': 'metrics.log',
                'pattern': r'group=search_concurrency.*active_hist_searches=(\d+)',
                'threshold': 100,
                'operator': '<',
                'severity': 'info',
                'message': 'Search concurrency normal'
            },
            {
                'name': 'Skipped Searches',
                'log_file': 'scheduler.log',
                'pattern': r'skipped.*scheduled search',
                'count_threshold': 10,
                'time_range': '1h',
                'severity': 'warning',
                'fail_message': 'High number of skipped scheduled searches'
            }
        ]
    }
}
```

## Recommendation Engine Rules

```python
RECOMMENDATIONS = {
    'disk_space_low': {
        'condition': 'storage_usage > 80%',
        'recommendations': [
            'Increase PersistentVolumeClaim size',
            'Enable frozen bucket deletion (frozenTimePeriodInSecs)',
            'Archive old buckets to S3/SmartStore',
            'Review data retention policies'
        ],
        'commands': [
            'kubectl patch pvc <pvc-name> -p \'{"spec":{"resources":{"requests":{"storage":"200Gi"}}}}\'',
            'Configure indexes.conf: frozenTimePeriodInSecs = 2592000'
        ]
    },

    'replication_factor_not_met': {
        'condition': 'cluster_status.replication_factor_met == false',
        'recommendations': [
            'Check connectivity between cluster manager and peers',
            'Verify all indexer pods are running',
            'Review cluster manager logs for peer registration issues',
            'Consider adding more indexer peers'
        ],
        'commands': [
            'kubectl get pods -l app=splunk,role=indexer',
            'kubectl logs <cluster-manager-pod> | grep -i peer',
            'kubectl splunk exec show cluster-peers'
        ]
    },

    'high_memory_usage': {
        'condition': 'memory_usage > 90%',
        'recommendations': [
            'Increase memory limits in StatefulSet',
            'Review search complexity and memory usage',
            'Check for memory leaks in custom apps',
            'Enable memory profiling for investigation'
        ],
        'commands': [
            'kubectl patch statefulset <sts-name> -p \'{"spec":{"template":{"spec":{"containers":[{"name":"splunk","resources":{"limits":{"memory":"16Gi"}}}]}}}}\'',
            'Review search.log for memory-intensive searches'
        ]
    },

    'certificate_expiring': {
        'condition': 'ssl_cert_expires_in < 30_days',
        'recommendations': [
            'Renew SSL certificates before expiration',
            'Update certificate in Kubernetes Secret',
            'Restart Splunk pods to load new certificate',
            'Verify certificate chain is complete'
        ],
        'commands': [
            'kubectl create secret tls splunk-tls --cert=server.crt --key=server.key --dry-run=client -o yaml | kubectl apply -f -',
            'kubectl rollout restart statefulset <sts-name>'
        ]
    }
}
```

## Usage Example

```python
# Example analyzer implementation
from analyzers.patterns import PATTERNS, HEALTH_CHECKS, RECOMMENDATIONS

class LogAnalyzer:
    def __init__(self, log_content: str):
        self.log_content = log_content
        self.issues = []
        self.recommendations = []

    def analyze(self):
        """Run all pattern matches and health checks."""
        # Check error patterns
        for pattern_name, pattern_config in PATTERNS.items():
            matches = re.finditer(pattern_config['regex'], self.log_content)
            for match in matches:
                self.issues.append({
                    'type': pattern_name,
                    'severity': pattern_config['severity'],
                    'category': pattern_config['category'],
                    'description': pattern_config['description'],
                    'line': match.group(0),
                    'recommendation': pattern_config['recommendation']
                })

        # Generate recommendations
        self._generate_recommendations()

        return {
            'issues': self.issues,
            'recommendations': self.recommendations,
            'health_score': self._calculate_health_score()
        }

    def _generate_recommendations(self):
        """Generate actionable recommendations based on issues."""
        for issue in self.issues:
            if issue['severity'] == 'critical':
                # Look up recommendations for this issue type
                rec = RECOMMENDATIONS.get(issue['type'])
                if rec:
                    self.recommendations.append(rec)

    def _calculate_health_score(self) -> int:
        """Calculate overall health score (0-100)."""
        score = 100
        for issue in self.issues:
            if issue['severity'] == 'critical':
                score -= 20
            elif issue['severity'] == 'warning':
                score -= 5
        return max(0, score)
```

This provides a comprehensive framework for analyzing Splunk logs and generating actionable recommendations for Kubernetes-based Splunk deployments.
