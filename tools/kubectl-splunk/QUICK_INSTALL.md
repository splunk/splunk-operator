# Quick Installation Guide for kubectl-splunk

## The Problem

If you tried to install with:
```bash
pipx install kubectl-splunk
```

And got this error:
```
ERROR: Could not find a version that satisfies the requirement kubectl-splunk (from versions: none)
ERROR: No matching distribution found for kubectl-splunk
```

**This is expected!** The `kubectl-splunk` package is **NOT published to PyPI**. It must be installed from the source code in the splunk-operator repository.

## Solution: Install from Source

### Option 1: Using pipx (Recommended - Isolated Environment)

```bash
# 1. Clone the repository (if you haven't already)
git clone https://github.com/splunk/splunk-operator.git
cd splunk-operator/tools/kubectl-splunk

# 2. Set Python version (if using asdf/mise/similar)
echo "python 3.13.7t" > .tool-versions
# Note: Use any Python 3.6+ version available on your system

# 3. Install with pipx
pipx install --index-url https://pypi.org/simple .
```

### Option 2: Using pip (Simple Installation)

```bash
# 1. Clone the repository (if you haven't already)
git clone https://github.com/splunk/splunk-operator.git
cd splunk-operator/tools/kubectl-splunk

# 2. Set Python version (if using asdf/mise/similar)
echo "python 3.13.7t" > .tool-versions
# Note: Use any Python 3.6+ version available on your system

# 3. Install with pip
pip install --index-url https://pypi.org/simple .
```

### Option 3: Using pip in editable mode (For Development)

```bash
# 1. Clone the repository (if you haven't already)
git clone https://github.com/splunk/splunk-operator.git
cd splunk-operator/tools/kubectl-splunk

# 2. Set Python version (if using asdf/mise/similar)
echo "python 3.13.7t" > .tool-versions

# 3. Install in editable mode
pip install --index-url https://pypi.org/simple -e .
```

## Verify Installation

After installation, verify it works:

```bash
# Check version
kubectl-splunk --version
# Should output: kubectl-splunk 1.6

# Check help
kubectl splunk --help
# Should display the help message

# Verify kubectl integration
kubectl splunk --version
# Should also work
```

## Common Installation Issues

### Issue 1: Python Version Manager (asdf/mise)

**Error:**
```
No version is set for command python3
```

**Fix:**
```bash
cd splunk-operator/tools/kubectl-splunk
echo "python 3.13.7t" > .tool-versions
# Or use: echo "python 3.11.0" > .tool-versions
# Any Python 3.6+ version will work
```

### Issue 2: Custom Splunk PyPI Repository

**Error:**
```
401 Error, Credentials not correct for https://repo.splunkdev.net
```

**Fix:**
Add `--index-url https://pypi.org/simple` to your install command:
```bash
pip install --index-url https://pypi.org/simple .
```

This forces pip to use the public PyPI instead of the custom Splunk repository.

### Issue 3: Permission Denied

**Error:**
```
ERROR: Could not install packages due to an OSError: [Errno 13] Permission denied
```

**Fix:**
Either:
- Use `pipx` (recommended - no sudo needed)
- Use a virtual environment
- Add `--user` flag: `pip install --user --index-url https://pypi.org/simple .`

## Installation Complete!

Once installed, you can use kubectl-splunk to:

```bash
# Execute Splunk commands
kubectl splunk -n splunk-namespace exec status

# Get server info via REST API
kubectl splunk rest GET /services/server/info --insecure

# Copy files to/from pods
kubectl splunk cp /local/file.txt :/remote/file.txt

# Start interactive shell
kubectl splunk --interactive

# Specify a specific pod
kubectl splunk -P splunk-idx-0 exec list user
```

## Need Help?

- Check the full [README.md](README.md) for detailed documentation
- Run `kubectl splunk --help` for command options
- View examples: `kubectl splunk exec --help`

## For Your Colleague (qingw)

The installation command you tried won't work because the package isn't on PyPI. Instead:

```bash
# Navigate to your splunk-operator directory
cd ~/path/to/splunk-operator/tools/kubectl-splunk

# Install from the current directory (note the "." at the end)
pipx install --index-url https://pypi.org/simple .
```

The `.` tells pipx/pip to install from the current directory, not from PyPI!
