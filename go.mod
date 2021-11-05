module github.com/splunk/splunk-operator

go 1.13

require (
	github.com/Azure/go-autorest/autorest/adal v0.9.13 // indirect
	github.com/aws/aws-sdk-go v1.37.24
	github.com/go-logr/logr v0.1.0
	github.com/minio/minio-go/v7 v7.0.10
	github.com/onsi/ginkgo v1.12.0
	github.com/onsi/gomega v1.9.0
	github.com/operator-framework/operator-sdk v0.18.2
	github.com/spf13/pflag v1.0.5
	k8s.io/api v0.18.17
	k8s.io/apimachinery v0.18.17
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/kubectl v0.18.17
	k8s.io/kubernetes v1.18.17
	sigs.k8s.io/controller-runtime v0.6.0
)

// Pinned to kubernetes v1.18.17
replace (
	k8s.io/api => k8s.io/api v0.18.17
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.18.17
	k8s.io/apimachinery => k8s.io/apimachinery v0.18.17
	k8s.io/apiserver => k8s.io/apiserver v0.18.17
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.18.17
	k8s.io/client-go => k8s.io/client-go v0.18.17 // Required by prometheus-operator
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.18.17
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.18.17
	k8s.io/code-generator => k8s.io/code-generator v0.18.17
	k8s.io/component-base => k8s.io/component-base v0.18.17
	k8s.io/cri-api => k8s.io/cri-api v0.18.17
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.18.17
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.18.17
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.18.17
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.18.17
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.18.17
	k8s.io/kubectl => k8s.io/kubectl v0.18.17
	k8s.io/kubelet => k8s.io/kubelet v0.18.17
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.18.17
	k8s.io/metrics => k8s.io/metrics v0.18.17
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.18.17
)

replace (
	github.com/docker/docker => github.com/moby/moby v0.7.3-0.20190826074503-38ab9da00309 // Required by Helm

	github.com/evanphx/json-patch => github.com/evanphx/json-patch/v5 v5.5.0 //CVE-2018-14632

	github.com/gogo/protobuf => github.com/gogo/protobuf v1.3.2 //CVE-2021-3121

	github.com/openshift/api => github.com/openshift/api v0.0.0-20190924102528-32369d4db2ad // Required until https://github.com/operator-framework/operator-lifecycle-manager/pull/1241 is resolved

	github.com/smartystreets/goconvey => github.com/smartystreets/goconvey v1.6.6 //CVE-2017-18214

	golang.org/x/crypto => golang.org/x/crypto v0.0.0-20201216223049-8b5274cf687f //CVE-2020-29652

	golang.org/x/net => golang.org/x/net v0.0.0-20210614182718-04defd469f4e // CVE-2021-33194 and CVE-2021-31525

	golang.org/x/text => golang.org/x/text v0.3.5 // Fix CVE-2020-14040 and CVE-2020-28852
)
