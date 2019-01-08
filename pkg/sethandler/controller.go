package sethandler

import (
  "reflect"
  "k8s.io/api/core/v1"
  "k8s.io/client-go/informers"
  "k8s.io/client-go/kubernetes"
  "k8s.io/client-go/tools/cache"
  "time"
)

func NewController(k8sClient kubernetes.Interface) cache.Controller {
  kubeInformerFactory := informers.NewSharedInformerFactory(k8sClient, time.Second*30)
  controller := kubeInformerFactory.Core().V1().Pods().Informer()
  controller.AddEventHandler(cache.ResourceEventHandlerFuncs{
    AddFunc:  func(obj interface{}) {podAdded(k8sClient, *(reflect.ValueOf(obj).Interface().(*v1.Pod)))},
    DeleteFunc: func(obj interface{}) {},
    UpdateFunc: func(oldObj, newObj interface{}) {},
  })
  return controller
}

func podAdded(k8sClient kubernetes.Interface, podSpec v1.Pod) {
  return
}