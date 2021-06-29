module github.com/nokia/CPU-Pooler

go 1.15

require (
	github.com/fsnotify/fsnotify v1.4.9
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/stretchr/testify v1.4.0
	golang.org/x/net v0.0.0-20201110031124-69a78807bb2b
	golang.org/x/sys v0.0.0-20201112073958-5cba982894dd
	google.golang.org/grpc v1.27.0
	gopkg.in/yaml.v2 v2.2.8
	k8s.io/api v0.19.12
	k8s.io/apimachinery v0.19.12
	k8s.io/client-go v0.19.12
	k8s.io/kubelet v0.19.12
	k8s.io/kubernetes v1.19.12
)

replace (
	k8s.io/api => k8s.io/api v0.19.12
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.19.12
	k8s.io/apimachinery => k8s.io/apimachinery v0.19.12
	k8s.io/apiserver => k8s.io/apiserver v0.19.12
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.19.12
	k8s.io/client-go => k8s.io/client-go v0.19.12
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.19.12
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.19.12
	k8s.io/code-generator => k8s.io/code-generator v0.19.12
	k8s.io/component-base => k8s.io/component-base v0.19.12
	k8s.io/cri-api => k8s.io/cri-api v0.19.12
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.19.12
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.19.12
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.19.12
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.19.12
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.19.12
	k8s.io/kubectl => k8s.io/kubectl v0.19.12
	k8s.io/kubelet => k8s.io/kubelet v0.19.12
	k8s.io/kubernetes => k8s.io/kubernetes v1.19.12
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.19.12
	k8s.io/metrics => k8s.io/metrics v0.19.12
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.19.12
)
