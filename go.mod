module sidecar-hpa

go 1.15

require (
	github.com/go-logr/logr v0.2.0 // indirect
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/onsi/ginkgo v1.11.0
	github.com/onsi/gomega v1.8.1
	k8s.io/api v0.19.4
	k8s.io/apimachinery v0.19.4
	k8s.io/apiserver v0.19.4 // indirect
	k8s.io/client-go v0.19.4
	k8s.io/kubernetes v1.19.4
	k8s.io/metrics v0.19.4
	sigs.k8s.io/controller-runtime v0.5.0
	sigs.k8s.io/kind v0.9.0 // indirect
)

replace (
	k8s.io/api => k8s.io/api v0.0.0-20201114085527-4a626d306b98
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.0.0-20201114091224-a7ee1efe41fc
	k8s.io/apimachinery => k8s.io/apimachinery v0.20.0-beta.2.0.20201118005411-2456ebdaba22
	k8s.io/apiserver => k8s.io/apiserver v0.0.0-20201120050325-0e46f0ea2bdc
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.0.0-20201114091436-bb334014705f
	k8s.io/client-go => k8s.io/client-go v0.0.0-20201121005859-fb61a7c88cb9
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.0.0-20201114092026-235e676f444a
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.0.0-20201114092228-614b98eee358
	k8s.io/code-generator => k8s.io/code-generator v0.20.0-beta.2.0.20201118094405-356aa54a63d2
	k8s.io/component-base => k8s.io/component-base v0.0.0-20201114090208-1e84b325f5ba
	k8s.io/component-helpers => k8s.io/component-helpers v0.20.0-alpha.2.0.20201114090304-7cb42b694587
	k8s.io/controller-manager => k8s.io/controller-manager v0.20.0-alpha.1.0.20201114091934-20fa1a1257aa
	k8s.io/cri-api => k8s.io/cri-api v0.20.0-beta.2
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.0.0-20201114092327-833303372de1
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.0.0-20201126170540-6c47de442a82
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.0.0-20201114092129-18c28a4120de
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.0.0-20201114091637-deb12d4b202f
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.0.0-20201114091838-0f62d3991af1
	k8s.io/kubectl => k8s.io/kubectl v0.0.0-20201114092718-b155278f1f4a
	k8s.io/kubelet => k8s.io/kubelet v0.0.0-20201114091737-92ded5ee6b96
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.0.0-20201125092503-cc0a0abf3d78
	k8s.io/metrics => k8s.io/metrics v0.0.0-20201114091333-d70c0e0c6aa5
	k8s.io/mount-utils => k8s.io/mount-utils v0.20.0-beta.2
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.0.0-20201114090814-1f4e6a92d4b8
	k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.0.0-20201114091536-307712dacc73
	k8s.io/sample-controller => k8s.io/sample-controller v0.0.0-20201114091033-7644cdf6adcd

)
