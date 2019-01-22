package k8sclient

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"os"
)

// GetNodeLabels returns node labels.
// NODE_NAME environment variable is used to determine the node
func GetNodeLabels() map[string]string {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		panic("NODE_NAME environment variable missing")
	}
	nodes, err := clientset.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}
	return nodes.ObjectMeta.Labels
}
