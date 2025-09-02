package main

import (
	"context"
	"fmt"
	"time"

	samplecomv1alpha1 "github.com/istiak/sample-controller/pkg/apis/sample.com/v1alpha1"
	"golang.org/x/time/rate"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	clientset "github.com/istiak/sample-controller/pkg/generated/clientset/versioned"
	informers "github.com/istiak/sample-controller/pkg/generated/informers/externalversions/sample.com/v1alpha1"
	listers "github.com/istiak/sample-controller/pkg/generated/listers/sample.com/v1alpha1"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	appslisters "k8s.io/client-go/listers/apps/v1"
)

type Controller struct {
	kubeclientset     kubernetes.Interface
	sampleclientset   clientset.Interface
	deploymentsLister appslisters.DeploymentLister
	deploymentsSynced cache.InformerSynced
	foosLister        listers.FooLister
	foosSynced        cache.InformerSynced
	workqueue         workqueue.TypedRateLimitingInterface[cache.ObjectName]
}

// NewController returns a new sample controller
func NewController(
	ctx context.Context,
	kubeclientset kubernetes.Interface,
	sampleclientset clientset.Interface,
	deploymentInformer appsinformers.DeploymentInformer,
	fooInformer informers.FooInformer) *Controller {

	ratelimiter := workqueue.NewTypedMaxOfRateLimiter(
		workqueue.NewTypedItemExponentialFailureRateLimiter[cache.ObjectName](5*time.Millisecond, 1000*time.Second),
		&workqueue.TypedBucketRateLimiter[cache.ObjectName]{Limiter: rate.NewLimiter(rate.Limit(50), 300)},
	)

	controller := &Controller{
		kubeclientset:     kubeclientset,
		sampleclientset:   sampleclientset,
		foosLister:        fooInformer.Lister(),
		foosSynced:        fooInformer.Informer().HasSynced,
		deploymentsLister: deploymentInformer.Lister(),
		deploymentsSynced: deploymentInformer.Informer().HasSynced,
		workqueue:         workqueue.NewTypedRateLimitingQueue(ratelimiter),
	}

	fooInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueFoo,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueFoo(new)
		},
		DeleteFunc: controller.enqueueFoo,
	})

	deploymentInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.handleObject,
		UpdateFunc: func(old, new interface{}) {
			newDepl := new.(*appsv1.Deployment)
			oldDepl := old.(*appsv1.Deployment)

			if newDepl.ResourceVersion == oldDepl.ResourceVersion {
				return
			}

			controller.handleObject(new)
		},
		DeleteFunc: controller.handleObject,
	})

	return controller
}

func (c *Controller) Run(ctx context.Context, workers int) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()
	logger := klog.FromContext(ctx)

	logger.Info("Starting Foo controller")

	logger.Info("Waiting for informer caches to sync")

	if ok := cache.WaitForCacheSync(ctx.Done(), c.foosSynced, c.deploymentsSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	logger.Info("Starting workers", "count", workers)

	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}

	logger.Info("Started workers")
	<-ctx.Done()
	logger.Info("Shutting down workers")

	return nil
}

func (c *Controller) runWorker(ctx context.Context) {
	for c.processNextWorkItem(ctx) {
	}
}

func (c *Controller) processNextWorkItem(ctx context.Context) bool {
	objRef, shutdown := c.workqueue.Get()
	logger := klog.FromContext(ctx)

	if shutdown {
		return false
	}

	defer c.workqueue.Done(objRef)

	err := c.syncHandler(ctx, objRef)
	if err == nil {
		c.workqueue.Forget(objRef)
		logger.Info("Successfully synced", "objectName", objRef)
		return true
	}

	utilruntime.HandleErrorWithContext(ctx, err, "Error syncing; requeuing for later retry", "objectReference", objRef)
	c.workqueue.AddRateLimited(objRef)
	return true
}

func (c *Controller) syncHandler(ctx context.Context, objectRef cache.ObjectName) error {
	logger := klog.LoggerWithValues(klog.FromContext(ctx), "objectRef", objectRef)

	foo, err := c.foosLister.Foos(objectRef.Namespace).Get(objectRef.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			utilruntime.HandleErrorWithContext(ctx, err, "Foo referenced by item in work queue no longer exists", "objectReference", objectRef)
			return nil
		}
		return err
	}

	deploymentName := foo.Spec.DeploymentName

	if deploymentName == "" {
		utilruntime.HandleErrorWithContext(ctx, nil, "Deployment name missing from object reference", "objectReference", objectRef)
		return nil
	}

	deployment, err := c.deploymentsLister.Deployments(foo.Namespace).Get(deploymentName)

	if errors.IsNotFound(err) {
		deployment, err = c.kubeclientset.AppsV1().Deployments(foo.Namespace).Create(ctx, newDeployment(foo), metav1.CreateOptions{})
	}

	if err != nil {
		return err
	}

	if foo.Spec.Replicas != nil && *foo.Spec.Replicas != *deployment.Spec.Replicas {
		logger.V(4).Info("Update deployment resource", "currentReplicas", *deployment.Spec.Replicas, "desiredReplicas", *foo.Spec.Replicas)
		deployment, err = c.kubeclientset.AppsV1().Deployments(foo.Namespace).Update(ctx, newDeployment(foo), metav1.UpdateOptions{})
	}

	if err != nil {
		return err
	}

	if deployment.Spec.Template.Spec.Containers[0].Image != foo.Spec.Image {
		logger.V(4).Info("Updating deployment image",
			"currentImage", deployment.Spec.Template.Spec.Containers[0].Image,
			"desiredImage", foo.Spec.Image)
		_, err = c.kubeclientset.AppsV1().Deployments(foo.Namespace).Update(ctx, newDeployment(foo), metav1.UpdateOptions{})
	}

	if err != nil {
		return err
	}

	err = c.updateFooStatus(ctx, foo, deployment)

	if err != nil {
		return err
	}

	logger.Info("Successfully processed Foo", "foo", klog.KObj(foo))
	return nil
}

func (c *Controller) updateFooStatus(ctx context.Context, foo *samplecomv1alpha1.Foo, deployment *appsv1.Deployment) error {
	fooCopy := foo.DeepCopy()
	fooCopy.Status.AvailableReplicas = deployment.Status.AvailableReplicas

	_, err := c.sampleclientset.SampleV1alpha1().Foos(foo.Namespace).UpdateStatus(ctx, fooCopy, metav1.UpdateOptions{})

	return err
}

func (c *Controller) enqueueFoo(obj interface{}) {
	if objectRef, err := cache.ObjectToName(obj); err != nil {
		utilruntime.HandleError(err)
		return
	} else {
		c.workqueue.Add(objectRef)
	}
}

func (c *Controller) handleObject(obj interface{}) {
	var object metav1.Object
	var ok bool
	logger := klog.FromContext(context.Background())
	if object, ok = obj.(metav1.Object); !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleErrorWithContext(context.Background(), nil, "Error decoding object, invalid type", "type", fmt.Sprintf("%T", obj))
			return
		}
		object, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			utilruntime.HandleErrorWithContext(context.Background(), nil, "Error decoding object tombstone, invalid type", "type", fmt.Sprintf("%T", tombstone.Obj))
			return
		}
		logger.V(4).Info("Recovered deleted object", "resourceName", object.GetName())
	}
	logger.V(4).Info("Processing object", "object", klog.KObj(object))
	if ownerRef := metav1.GetControllerOf(object); ownerRef != nil {
		if ownerRef.Kind != "Foo" {
			return
		}

		foo, err := c.foosLister.Foos(object.GetNamespace()).Get(ownerRef.Name)
		if err != nil {
			logger.V(4).Info("Ignore orphaned object", "object", klog.KObj(object), "foo", ownerRef.Name)
			return
		}

		c.enqueueFoo(foo)
		return
	}
}

func newDeployment(foo *samplecomv1alpha1.Foo) *appsv1.Deployment {
	labels := map[string]string{
		"app": "foo",
	}

	image := foo.Spec.Image
	if image == "" {
		image = "istiaka2i/bookapp:v1.0"
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: foo.Spec.DeploymentName,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(foo, samplecomv1alpha1.SchemeGroupVersion.WithKind("Foo")),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: foo.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "main",
							Image: image,
						},
					},
				},
			},
		},
	}
}
