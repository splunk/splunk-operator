# kubectl-splunk Enhancement Proposal: Diagnostic Collection & Log Analysis

## Executive Summary

This proposal outlines comprehensive enhancements to the `kubectl-splunk` plugin to add diagnostic data collection and log analysis capabilities specifically for Splunk deployments on Kubernetes managed by the Splunk Operator.

## Current State

The plugin currently supports:
- Executing Splunk CLI commands in pods
- REST API calls via port-forwarding
- File copying to/from pods
- Interactive shell access

## Proposed Enhancements

### 1. Diagnostic Collection Mode (`diag`)

A new `diag` subcommand to collect comprehensive diagnostic information about Splunk deployments.

#### 1.1 Kubernetes-Level Diagnostics

**Resource Information:**
- Pod status, events, and resource usage
- StatefulSet/Deployment configurations
- Services and endpoints
- PersistentVolumeClaims and storage
- ConfigMaps and Secrets (names only, not content)
- Custom Resource Definitions (CRDs) for Splunk Operator

**Splunk Operator Resources:**
- Standalone instances
- IndexerClusters
- SearchHeadClusters
- ClusterManagers
- LicenseManagers
- MonitoringConsoles

**Container/Pod Metrics:**
- CPU and memory usage
- Restart counts and reasons
- Image versions (docker-splunk)
- Container logs (recent errors/warnings)

#### 1.2 Splunk-Level Diagnostics

**Configuration Files:**
- `/opt/splunk/etc/system/local/*.conf`
- `/opt/splunk/etc/apps/*/local/*.conf`
- Server class configurations
- Distributed search configurations

**Log Files:**
- `splunkd.log` (recent entries, errors, warnings)
- `metrics.log`
- `audit.log`
- `scheduler.log`
- `python.log`

**Splunk Status Information:**
- License status and usage (`splunk show license-usage`)
- Server info (`splunk show servername`, version, roles)
- Index status and sizes
- Search head clustering status
- Indexer clustering status
- Bucket distribution
- Replication status

**Performance Metrics:**
- Indexing throughput
- Search performance
- Disk I/O statistics
- Queue fill ratios

#### 1.3 Network and Connectivity

- Service discovery (DNS resolution)
- Inter-pod connectivity tests
- Port availability checks
- SSL certificate validation

### 2. Log Analysis Mode (`analyze`)

A new `analyze` subcommand to parse and analyze Splunk logs for common issues.

#### 2.1 Error Detection

**Critical Errors:**
- Indexing failures
- Search errors
- Clustering issues
- License violations
- Authentication failures
- Replication failures

**Warning Patterns:**
- Performance degradation
- Disk space warnings
- High queue fill ratios
- Certificate expiration warnings
- Configuration conflicts

#### 2.2 Health Checks

**Cluster Health:**
- Indexer cluster status (peer connectivity, replication factor met)
- Search head cluster status (captain election, replication)
- Cluster manager connectivity
- License manager connectivity

**Data Health:**
- Missing or stale data
- Uneven bucket distribution
- Failed bucket fixes
- Index size trends

**System Health:**
- Resource constraints (CPU/memory pressure)
- Disk space issues
- Network latency/timeouts
- Process crashes or restarts

#### 2.3 Performance Analysis

- Slow searches identification
- Index-time parsing issues
- Heavy forwarder detection
- Skipped scheduled searches

### 3. Report Generation

**Output Formats:**
- Human-readable text summary
- Structured JSON for automation
- HTML report with charts/graphs
- Kubernetes YAML manifests for issue reproduction

**Report Sections:**
- Executive summary (overall health score)
- Critical issues (immediate attention required)
- Warnings (should be addressed)
- Recommendations (best practices)
- Resource inventory
- Timeline of events
- Troubleshooting steps

### 4. Implementation Design

#### 4.1 New Command Structure

```bash
# Diagnostic collection
kubectl splunk diag [flags]
kubectl splunk diag --output-dir /tmp/splunk-diag
kubectl splunk diag --full  # Collect everything
kubectl splunk diag --quick # Quick health check

# Log analysis
kubectl splunk analyze [flags]
kubectl splunk analyze --logs splunkd
kubectl splunk analyze --time-range 1h
kubectl splunk analyze --severity error

# Combined (collect + analyze)
kubectl splunk troubleshoot [flags]
kubectl splunk troubleshoot --auto-fix  # Suggest fixes
```

#### 4.2 Architecture Components

**DiagnosticCollector:**
- `KubernetesCollector`: Gathers K8s resource information
- `SplunkConfigCollector`: Retrieves Splunk configurations
- `LogCollector`: Fetches and filters log files
- `MetricsCollector`: Gathers performance metrics
- `OperatorCollector`: Collects Splunk Operator CRD status

**LogAnalyzer:**
- `PatternMatcher`: Regex-based error/warning detection
- `HealthChecker`: Runs health validation rules
- `PerformanceAnalyzer`: Analyzes performance metrics
- `RecommendationEngine`: Generates actionable recommendations

**ReportGenerator:**
- `TextFormatter`: Plain text output
- `JSONFormatter`: Structured JSON
- `HTMLFormatter`: Interactive HTML reports
- `ManifestGenerator`: K8s YAML for reproduction

#### 4.3 Configuration

```yaml
# ~/.kubectl_splunk_diag_config
diag:
  collect:
    kubernetes: true
    splunk_configs: true
    logs: true
    metrics: true

  logs:
    files:
      - splunkd.log
      - metrics.log
      - scheduler.log
    tail_lines: 1000
    time_range: 24h

  analyze:
    error_patterns:
      - pattern: "ERROR.*index"
        severity: critical
        category: indexing
      - pattern: "WARN.*license"
        severity: warning
        category: licensing

    health_checks:
      - name: cluster_health
        enabled: true
      - name: disk_space
        enabled: true
        threshold: 80

  report:
    format: html
    include_manifests: true
    include_recommendations: true
```

### 5. Usage Examples

#### 5.1 Quick Health Check

```bash
# Quick health check across all Splunk pods
kubectl splunk diag --quick

# Output:
# ✓ Found 5 Splunk pods (3 indexers, 2 search heads)
# ✓ All pods running and ready
# ⚠ Warning: License usage at 85%
# ✗ Critical: Indexer idx-2 replication lag > 5 minutes
#
# Health Score: 75/100
```

#### 5.2 Full Diagnostic Collection

```bash
# Collect full diagnostics for specific pod
kubectl splunk -P splunk-idx-0 diag --full --output-dir /tmp/diag

# Output:
# Collecting diagnostics for pod: splunk-idx-0
# [1/8] Kubernetes resources... done
# [2/8] Splunk configurations... done
# [3/8] Log files... done (125 MB)
# [4/8] Performance metrics... done
# [5/8] Cluster status... done
# [6/8] License information... done
# [7/8] Network connectivity... done
# [8/8] Generating report... done
#
# Diagnostic bundle saved to: /tmp/diag/splunk-idx-0-2025-01-17-053320.tar.gz
```

#### 5.3 Log Analysis

```bash
# Analyze logs for errors in last 4 hours
kubectl splunk analyze --time-range 4h --severity error

# Output:
# Analyzing logs from 5 Splunk pods...
#
# Critical Errors (3):
#   [idx-1] 2025-01-17 01:23:45 - Index write failed: disk full
#   [idx-2] 2025-01-17 02:15:30 - Replication peer disconnected
#   [shc-1] 2025-01-17 03:45:12 - Search head captain election failed
#
# Recommendations:
#   1. Increase PVC size for idx-1 (current: 100GB, used: 98GB)
#   2. Check network connectivity between idx-2 and cluster manager
#   3. Review search head cluster logs for captain election issues
```

#### 5.4 Troubleshooting Mode

```bash
# Comprehensive troubleshooting with automated analysis
kubectl splunk troubleshoot --namespace splunk-prod

# Output:
# 🔍 Collecting diagnostics...
# 🔍 Analyzing logs...
# 🔍 Running health checks...
# 🔍 Generating recommendations...
#
# === SPLUNK OPERATOR TROUBLESHOOTING REPORT ===
# Generated: 2025-01-17 05:33:20 UTC
# Namespace: splunk-prod
# Pods: 8 (6 running, 2 pending)
#
# CRITICAL ISSUES:
#   1. Pod splunk-idx-2 in CrashLoopBackOff
#      Cause: OOMKilled (memory limit: 4Gi, requested: 6Gi)
#      Fix: Increase memory limit in IndexerCluster CR
#
#   2. License expired for indexer cluster
#      Expires: 2025-01-15
#      Fix: Upload new license via LicenseManager
#
# Full report: /tmp/splunk-troubleshoot-2025-01-17.html
```

### 6. Benefits

1. **Faster Troubleshooting**: Automated collection reduces diagnostic time from hours to minutes
2. **Proactive Monitoring**: Identify issues before they become critical
3. **Knowledge Capture**: Standardized diagnostics improve collaboration with Splunk Support
4. **Best Practices**: Built-in recommendations based on Splunk and Kubernetes best practices
5. **Reduced Downtime**: Quick identification of root causes
6. **Training Tool**: Help operators learn common Splunk issues and resolutions

### 7. Implementation Phases

#### Phase 1: Basic Diagnostic Collection (2-3 weeks)
- Kubernetes resource collection
- Basic log file retrieval
- Simple text report generation

#### Phase 2: Splunk-Specific Diagnostics (3-4 weeks)
- Splunk configuration collection
- Status command execution
- Operator CRD inspection
- JSON/HTML report formats

#### Phase 3: Log Analysis (4-5 weeks)
- Pattern matching for common errors
- Health check framework
- Performance metrics analysis
- Recommendation engine

#### Phase 4: Advanced Features (3-4 weeks)
- Auto-fix suggestions
- Comparative analysis (before/after)
- Integration with Splunk Support
- Custom check plugins

### 8. Testing Strategy

- Unit tests for each collector and analyzer
- Integration tests with mock Splunk deployments
- End-to-end tests in real Kubernetes clusters
- Performance tests with large log volumes
- Validation against known Splunk issues

### 9. Documentation Requirements

- User guide with examples
- Configuration reference
- Troubleshooting playbook
- Custom check development guide
- API documentation for extensibility

### 10. Future Enhancements

- Real-time monitoring dashboard
- Alert integration (PagerDuty, Slack)
- Historical trend analysis
- ML-based anomaly detection
- Integration with Splunk Observability Cloud
- Automated remediation actions

## Conclusion

These enhancements will transform `kubectl-splunk` from a simple command execution tool into a comprehensive diagnostic and troubleshooting platform for Splunk on Kubernetes, significantly improving operator efficiency and reducing mean time to resolution (MTTR) for Splunk-related issues.
