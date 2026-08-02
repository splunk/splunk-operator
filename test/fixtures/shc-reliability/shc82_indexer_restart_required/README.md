# SHC-82 indexer restart-required test app

This controlled qualification fixture is separate from the Search Head app.
Its `health.conf` change was observed on the live Splunk 10.5 qualification
build with `restart_required_for_apply_bundle=true`, causing Splunk's
searchable indexer rolling-restart path. The Search Head fixture instead
reloaded on indexers and cannot prove indexer restart behavior.

Build a deterministic archive from the repository root:

```text
make shc82-indexer-app-package SHC82_INDEXER_APP_VERSION=1.0.0
```

For every qualified Splunk build, accept the fixture only after the Cluster
Manager's structured bundle status confirms that the change is restart
required. A filename or a prior-version result is not sufficient evidence.
