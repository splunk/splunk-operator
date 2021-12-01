module github.com/splunk/splunk-operator

go 1.16

require (
	github.com/aws/aws-sdk-go v1.42.16
	github.com/go-logr/logr v0.4.0
	github.com/minio/minio-go/v7 v7.0.16
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.15.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.11.0
	k8s.io/api v0.22.4
	k8s.io/apimachinery v0.22.4
	k8s.io/client-go v0.22.4
	k8s.io/kubectl v0.22.4
	sigs.k8s.io/controller-runtime v0.10.0
)
