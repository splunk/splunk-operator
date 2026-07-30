# SHC-82 restart-required test app

This fixture is used only for controlled App Framework availability
qualification.

The enabled `splunktcp` input is a restart-required configuration based on the
Splunk Enterprise cluster-bundle test case
`test_push_app_with_other_conf_restart_needed`. The `full` deployer push mode
provides the corresponding Search Head Cluster bundle path. The listener uses
the otherwise unused port `19997`; the test does not send traffic to it.

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
