# splunk-universalforwarder Helm Chart

Deploys a Splunk Universal Forwarder (UF) on Kubernetes as a stateless-by-default Deployment.

> **Splunk General Terms:** Use of the Splunk Universal Forwarder image requires acceptance of the Splunk General Terms. See [Splunk General Terms Acceptance](https://splunk.github.io/splunk-operator/#splunk-general-terms-acceptance) in the Splunk Operator documentation for the required `SPLUNK_GENERAL_TERMS` env var and the legal language you must accept before setting it.

## Documentation

Full deployment guide, configuration reference, forwarding setup, SSL, storage modes, and troubleshooting:

📄 **[docs/uf-helm-chart.md](../../docs/uf-helm-chart.md)**

## Quick Install

```sh
helm install my-uf ./helm-chart/splunk-universalforwarder \
  --namespace my-namespace \
  --create-namespace \
  --set splunkConfig.forwardServer=indexer.example.com:9997 \
  --set splunkConfig.password=MySecurePassword1
```

> You must set `SPLUNK_GENERAL_TERMS` after reading the [Splunk General Terms Acceptance](https://splunk.github.io/splunk-operator/#splunk-general-terms-acceptance) section of the Splunk Operator docs.

See `values.yaml` for all configurable options.
