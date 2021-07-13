package k8sclient

import (
	"context"
	"encoding/json"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"os"
)

type meta struct {
	Annotations map[string]json.RawMessage `json:"annotations"`
}

type update struct {
	Metadata meta `json:"metadata"`
}

// GetNodeLabels returns node labels.
// NODE_NAME environment variable is used to determine the node
func GetNodeLabels() (map[string]string, error) {
	cSet, err := createClientSet()
	if err != nil {
		return nil, err
	}
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		return nil, nil
	}
	nodes, err := cSet.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return nodes.ObjectMeta.Labels, nil
}

// SetPodAnnotation adds or modifies annotation for pod
func SetPodAnnotation(pod v1.Pod, key string, value string) error {
	cSet, err := createClientSet()
	if err != nil {
		return err
	}
	merge := update{}
	merge.Metadata.Annotations = make(map[string]json.RawMessage)
	merge.Metadata.Annotations[key] = json.RawMessage(`"` + value + `"`)

	jsonData, err := json.Marshal(merge)
	if err != nil {
		return err
	}
	_, err = cSet.CoreV1().Pods(pod.ObjectMeta.Namespace).Patch(context.TODO(), pod.ObjectMeta.Name, types.MergePatchType, jsonData, metav1.PatchOptions{})
	return err
}

//RefreshPod takes an existing Pod object as an input, and re-reads it from the K8s API
//Returns the refreshed Pod descriptor in case of success, or an error
func RefreshPod(pod v1.Pod) (*v1.Pod, error) {
	cSet, err := createClientSet()
	if err != nil {
		return nil, err
	}
	return cSet.CoreV1().Pods(pod.ObjectMeta.Namespace).Get(context.TODO(), pod.ObjectMeta.Name, metav1.GetOptions{})
}

func createClientSet() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}
