package controller

import (
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/zduymz/hpa-operator/pkg/utils"
	"k8s.io/api/autoscaling/v2beta2"
	"k8s.io/client-go/kubernetes"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	appsInformer "k8s.io/client-go/informers/apps/v1"
	asInformer "k8s.io/client-go/informers/autoscaling/v2beta2"
	appsLister "k8s.io/client-go/listers/apps/v1"
	asLister "k8s.io/client-go/listers/autoscaling/v2beta2"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

// HPA will be created with similar name as Deployment

const (
	defaultMinReplicas int32 = 1
	defaultMaxReplicas int32 = 3

	// optional annotations
	hpaMinReplicas = "hpa.apixio.com/min"
	hpaMaxReplicas = "hpa.apixio.com/max"

	// we can use multiple template by adding comma separator
	hpaTemplateName = "hpa.apixio.com/template"
)

var (
	IgnoredNamespaces = map[string]bool{
		metav1.NamespaceSystem: true,
		metav1.NamespacePublic: true,
	}
	// place where read template
	templateDir = utils.EnvVar("HPA_TEMPLATES","/template/")
)

type Controller struct {
	deployLister  appsLister.DeploymentLister
	hpaLister     asLister.HorizontalPodAutoscalerLister
	kubeclientset kubernetes.Interface
	hasSynced     cache.InformerSynced
	workqueue     workqueue.RateLimitingInterface
}

func NewController(deployInformer appsInformer.DeploymentInformer, hpaInformer asInformer.HorizontalPodAutoscalerInformer, kubeclientset kubernetes.Interface) (*Controller, error) {

	controller := &Controller{
		deployLister:  deployInformer.Lister(),
		hpaLister:     hpaInformer.Lister(),
		hasSynced:     deployInformer.Informer().HasSynced,
		workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "HPA Operator"),
		kubeclientset: kubeclientset,
	}

	klog.Info("Setting up event handlers")

	deployInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.handleAddObject,
		UpdateFunc: controller.handleUpdateObject,
		// no need handle DeleteFunc, bc HPA is automatically deleted with Deploy by OwnerReference
		//DeleteFunc: controller.handleDeleteObject,
	})

	return controller, nil
}

// Run will set event handler for deployment, syncing informer caches and starting workers.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.Info("Starting controller")

	// Wait for the caches to be synced before starting workers
	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.hasSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Info("Starting workers")
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")

	return nil
}

func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		defer c.workqueue.Done(obj)
		// get deployment object
		namespace, name, err := cache.SplitMetaNamespaceKey(obj.(string))
		if err != nil {
			klog.Errorf("Malformed key %v. Ignored", obj)
			return nil
		}
		// need to check key is valid or not

		klog.Infof("Start processing: %v", obj)
		if err := c.createHPA(namespace, name); err != nil {
			c.workqueue.AddRateLimited(obj)
			return nil
		}

		klog.Infof("Finish: %v", obj)
		c.workqueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

func (c *Controller) createHPA(namespace, name string) error {

	var (
		minReplicas int32
		maxReplicas int32
	)

	deploy, err := c.deployLister.Deployments(namespace).Get(name)
	if err != nil {
		klog.Errorf("Deployment %s not found in namespace. Ignored", name, namespace)
		return nil
	}

	// make sure deployment is successful
	if deploy.Status.ReadyReplicas != deploy.Status.ReadyReplicas {
		return fmt.Errorf("Deployment %s is not ready ", name)
	}

	// get min
	minReplicas, err = utils.StringtoInt32(deploy.Annotations[hpaMinReplicas])
	if err != nil {
		minReplicas = defaultMinReplicas
	}

	// get max
	maxReplicas, err = utils.StringtoInt32(deploy.Annotations[hpaMaxReplicas])
	if err != nil {
		maxReplicas = defaultMaxReplicas
	}

	// get hpa template
	var metrics []v2beta2.MetricSpec

	for _, m := range strings.Split(deploy.Annotations[hpaTemplateName], ",") {
		metric, err := c.loadMetricsTemplate(m)
		if err != nil {
			klog.Errorf("Can not load hpa template %s. Reason: %v", m, err)
			continue
		}
		metrics = append(metrics, *metric)
	}

	if len(metrics) < 1 {
		klog.Errorf("No hpa template is found. Ignored")
		return nil
	}

	hpa := &v2beta2.HorizontalPodAutoscaler{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HorizontalPodAutoscaler",
			APIVersion: "autoscaling/v2beta2",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			OwnerReferences: deploy.GetOwnerReferences(),
		},
		Spec: v2beta2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: v2beta2.CrossVersionObjectReference{
				APIVersion: deploy.GroupVersionKind().GroupVersion().String(),
				Kind:       deploy.GroupVersionKind().Kind,
				Name:       name,
			},
			MinReplicas: &minReplicas,
			MaxReplicas: maxReplicas,
			Metrics:     metrics,
		},
	}

	// update if hpa existed
	if c.isHPAExisted(namespace, name) {
		_, err := c.kubeclientset.AutoscalingV2beta2().HorizontalPodAutoscalers(namespace).Update(hpa)
		if err != nil {
			klog.Errorf("Can not update %s. Reason: %v", name, err)
			return err
		}
	} else {
		_, err := c.kubeclientset.AutoscalingV2beta2().HorizontalPodAutoscalers(namespace).Create(hpa)
		if err != nil {
			klog.Errorf("Can not create %s. Reason: %v", name, err)
			return err
		}
	}

	return nil
}

func (c *Controller) handleAddObject(obj interface{}) {
	var object metav1.Object
	var ok bool
	if object, ok = obj.(metav1.Object); !ok {
		klog.Errorf("Can not convert obj %v", obj)
		return
	}

	klog.Infof("Handle add object :%s ", object.GetName())

	deploy, err := c.deployLister.Deployments(object.GetNamespace()).Get(object.GetName())
	if err != nil {
		klog.Infof("Object %s not found. Ignored", object.GetName())
		return
	}

	// ignore system namespace
	if IgnoredNamespaces[deploy.Namespace] {
		klog.Infof("Object %s found in ignored namespaces. Ignored", object.GetName())
		return
	}

	// check if deployment has hpa annotation
	if deploy.Annotations[hpaTemplateName] == "" {
		klog.Infof("Deployment %s has no hpa annotation. Ignored", deploy.Name)
		return
	}

	// everything is correct, add to workqueue
	key, err := cache.MetaNamespaceKeyFunc(deploy)
	if err != nil {
		klog.Errorf("Can not create key for workqueue. Reason: %v", err)
		return
	}
	c.workqueue.Add(key)
}

func (c *Controller) handleDeleteObject(obj interface{}) {
	// when deployment is deleted. HPA related should be deleted either
	var object metav1.Object
	var ok bool
	if object, ok = obj.(metav1.Object); !ok {
		klog.Infof("Can not convert obj %v", obj)
		return
	}

	// should try to delete 3 times
	try := 0
	for try < 3 {
		err := c.kubeclientset.AutoscalingV2beta2().HorizontalPodAutoscalers(object.GetNamespace()).Delete(object.GetName(), &metav1.DeleteOptions{})
		if err != nil {
			try += 1
			klog.Errorf("Can not delete hpa %s. Try: %d Reason: %v", object.GetName(), try, err)
			continue
		}
		break
	}
}

func (c *Controller) handleUpdateObject(old, new interface{}) {
	// what happen when we update deployment on fly
	oldObject, _ := old.(metav1.Object)
	newObject, _ := new.(metav1.Object)
	klog.Infof("Handle Update object %v. Checking", newObject.GetName())
	if oldObject.GetAnnotations()[hpaTemplateName] != newObject.GetAnnotations()[hpaTemplateName] {
		// annotation was added
		if newObject.GetAnnotations()[hpaTemplateName] != "" {
			klog.Infof("%s annotations was updated. Updating HPA", newObject.GetName())
			key, err := cache.MetaNamespaceKeyFunc(newObject)
			if err != nil {
				klog.Errorf("Can not create a key for workqueue. Reason: %v", err)
				return
			}
			c.workqueue.Add(key)
		} else {
			// annotation was removed
			klog.Infof("%s annotations was removed. Removing HPA", newObject.GetName())
			//TODO: remove hpa
		}
	} else {
		klog.Infof("Handle Update object %v. Nothing changed. Ignored", newObject.GetName())
	}

}

// check HPA existed
func (c *Controller) isHPAExisted(namespace, name string) bool {
	_, err := c.hpaLister.HorizontalPodAutoscalers(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			return false
		}
	}
	return true
}

func (c *Controller) loadMetricsTemplate(name string) (*v2beta2.MetricSpec, error) {
	data, err := ioutil.ReadFile(templateDir + name)
	if err != nil {
		return nil, err
	}

	metrics := &v2beta2.MetricSpec{}
	err = yaml.Unmarshal(data, metrics)
	if err != nil {
		return nil, err
	}

	return metrics, nil
}
