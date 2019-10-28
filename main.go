package main

import (
	"github.com/zduymz/hpa-operator/pkg/controller"
	"github.com/zduymz/hpa-operator/pkg/signals"
	"github.com/zduymz/hpa-operator/pkg/utils"
	"time"

	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
)



func main() {

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	eksMasterUrl := utils.EnvVar("K8S_MASTER", "")
	eksConfig := utils.EnvVar("K8S_CONFIG", "")

	cfg, err := clientcmd.BuildConfigFromFlags(eksMasterUrl, eksConfig)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	// (client kubernetes.Interface, defaultResync time.Duration)
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)

	ctl, err := controller.NewController(kubeInformerFactory.Apps().V1().Deployments(),
		kubeInformerFactory.Autoscaling().V2beta2().HorizontalPodAutoscalers(),
		kubeClient)
	if err != nil {
		klog.Fatalf("Error building kubernetes controller: %s", err.Error())
	}

	kubeInformerFactory.Start(stopCh)

	if err = ctl.Run(1, stopCh); err != nil {
		klog.Fatalf("Error running controller: %s", err.Error())
	}
}