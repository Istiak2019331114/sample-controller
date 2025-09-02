package main

import (
	"flag"
	"path/filepath"
	"time"

	"github.com/istiak/sample-controller/pkg/signals"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"k8s.io/klog/v2"

	clientset "github.com/istiak/sample-controller/pkg/generated/clientset/versioned"
	informers "github.com/istiak/sample-controller/pkg/generated/informers/externalversions"
	kubeinformers "k8s.io/client-go/informers"
)

func main() {
	ctx := signals.SetupSignalHandler()
	logger := klog.FromContext(ctx)

	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}

	flag.Parse()

	cfg, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		logger.Error(err, "Error building kubeconfig")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		logger.Error(err, "Error building kubernetes clientset")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	sampleClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		logger.Error(err, "Error building kubernetes clientset")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	sampleInformerFactory := informers.NewSharedInformerFactory(sampleClient, time.Second*30)

	controller := NewController(ctx, kubeClient, sampleClient, kubeInformerFactory.Apps().V1().Deployments(), sampleInformerFactory.Sample().V1alpha1().Foos())

	kubeInformerFactory.Start(ctx.Done())
	sampleInformerFactory.Start(ctx.Done())

	if err = controller.Run(ctx, 2); err != nil {
		logger.Error(err, "Error running controller")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
}
