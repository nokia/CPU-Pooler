package k8sclient

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"os"
)

// GetNodeLabels returns node labels.
// NODE_NAME environment variable is used to determine the node
func GetNodeLabels() (map[string]string,error) {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil,err
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil,err
	}
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		return nil,nil
	}
	nodes, err := clientset.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
	if err != nil {
		return nil,err
	}
	return nodes.ObjectMeta.Labels,nil
}
