package k8sclient

import (
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"os"
)

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
	fmt.Printf("NODES labels: %v\n", nodes.ObjectMeta.Labels)
	return nodes.ObjectMeta.Labels
}

func PrintPod(podName string, podNameSpace string) {
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
	pod, err := clientset.CoreV1().Pods(podNameSpace).Get(podName, metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}
	fmt.Printf("NODES labels: %v\n", pod)
}
