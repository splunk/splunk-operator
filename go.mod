module github.com/splunk/splunk-operator

go 1.16

require (
	github.com/aws/aws-sdk-go v1.43.40
	github.com/fatih/color v1.13.0 // indirect
	github.com/go-logr/logr v0.4.0
	github.com/goccy/go-yaml v1.9.5 // indirect
	github.com/google/go-cmp v0.5.6
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-runewidth v0.0.13 // indirect
	github.com/mikefarah/yq/v3 v3.0.0-20201202084205-8846255d1c37 // indirect
	github.com/minio/minio-go/v7 v7.0.16
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.17.0
	github.com/peterh/liner v1.2.2 // indirect
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.11.0
	github.com/spf13/cobra v1.4.0 // indirect
	go.uber.org/zap v1.19.0
	golang.org/x/net v0.0.0-20220412020605-290c469a71a5 // indirect
	golang.org/x/sys v0.0.0-20220412211240-33da011f77ad // indirect
	golang.org/x/tools v0.1.8 // indirect
	golang.org/x/xerrors v0.0.0-20220411194840-2f41105eb62f // indirect
	k8s.io/api v0.22.4
	k8s.io/apiextensions-apiserver v0.22.1
	k8s.io/apimachinery v0.22.4
	k8s.io/client-go v0.22.4
	k8s.io/kubectl v0.22.4
	sigs.k8s.io/controller-runtime v0.10.0
)

replace golang.org/x/crypto => golang.org/x/crypto v0.0.0-20210921155107-089bfa567519
