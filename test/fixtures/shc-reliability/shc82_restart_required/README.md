# SHC-82 restart-required test app

This fixture is used only for controlled App Framework availability
qualification.

The `full` deployer push mode and restart-required app metadata exercise the
Search Head Cluster bundle path. The enabled `splunktcp` input was selected
from the Splunk Enterprise cluster-bundle test case
`test_push_app_with_other_conf_restart_needed`; the listener uses the otherwise
unused port `19997`, and the test does not send traffic to it.

The first EKS update proved that this package causes a Splunk-managed Search
Head rolling restart. It did not cause an indexer restart on the tested Splunk
build: every peer reported `restart_required=0` and reloaded the bundle.
Therefore this fixture must not be used as indexer restart qualification
evidence. SHC-82 still requires a separate indexer package whose live
structured bundle status reports `restart_required=1`.

Build the package from the repository root:

```text
make shc82-app-package
```

Build a subsequent version of the same application without modifying the
checked-in source:

```text
make shc82-app-package SHC82_APP_VERSION=1.0.1
```

The version must use numeric `major.minor.patch` form. Packaging copies the
fixture into a temporary directory and changes only the staged `app.conf`, so
the generated archive keeps the same application directory while presenting a
new application version and digest to the App Framework.

The generated archive is written below `build/_test/shc82`, which is ignored
by Git. The Make target normalizes file order, timestamps, ownership, and gzip
metadata so repeated builds from identical source have the same SHA-256
digest.
