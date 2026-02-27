# Operations and Debug

## Quick Capture Commands
`kubectl get <kind> <name> -n <ns> -o yaml`
`kubectl describe <kind> <name> -n <ns>`
`kubectl get events -n <ns> --sort-by=.lastTimestamp`
`kubectl logs -n splunk-operator deploy/splunk-operator-controller-manager -c manager --since=30m`

## Operator Metrics and pprof
The operator registers pprof handlers on the metrics server when `--pprof=true` (default).
Default metrics bind address is `:8080` with `--metrics-secure=false`.

Example local access:
`kubectl -n splunk-operator port-forward deploy/splunk-operator-controller-manager 8080:8080`
Then open `/debug/pprof` on `http://127.0.0.1:8080`.

## Where Debug Endpoints Are Wired
- Registration: `internal/controller/debug/register.go`
- Flags and setup: `cmd/main.go`
