package k8sclient

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"encoding/json"
	"context"
	"os"
)

type patch struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage	`json:"value"`
}

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
	nodes, err := clientset.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		return nil,err
	}
	return nodes.ObjectMeta.Labels,nil
}

func SetPodAnnotation(pod v1.Pod, key string, value string) error{
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}
	patchData := make([]patch, 1)
	patchData[0].Op = "add"
	patchData[0].Path = "/metadata/annotations/" + key
	newAnnotation :=  `"` + value + `"`
	patchData[0].Value = json.RawMessage(newAnnotation)
	jsonData, err := json.Marshal(patchData)
	if err != nil{
		return err
	}
	_, err = clientset.CoreV1().Pods(pod.ObjectMeta.Namespace).Patch(context.TODO(), pod.ObjectMeta.Name, types.JSONPatchType, jsonData, metav1.PatchOptions{})
	return err
}
