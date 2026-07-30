# SHC reliability qualification fixtures

The baseline topology uses a `LicenseManager` named `shc82`. The
`ClusterManager`, `IndexerCluster`, and `SearchHeadCluster` each set
`spec.licenseManagerRef.name: shc82`. For the `SearchHeadCluster`, that single
reference configures both the deployer and every Search Head member.

The LicenseManager mounts the `shc82-license` Secret at `/mnt/licenses` and
loads `/mnt/licenses/enterprise.lic`. The license file is deliberately not
stored in Git.

Create or update the Secret before applying the baseline manifest:

```bash
make shc82-license-secret \
  SHC82_NAMESPACE=<qualification-namespace> \
  SHC82_LICENSE_FILE=/absolute/path/to/enterprise.lic
```

The supplied license must support operation as a remote license manager. The
built-in Enterprise trial license does not satisfy this qualification
requirement.
