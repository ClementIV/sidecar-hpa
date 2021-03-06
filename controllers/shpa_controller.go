/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"

	//"github.com/go-logr/logr"
	autoscaling "k8s.io/api/autoscaling/v2beta2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"
	"math"
	ctrl "sigs.k8s.io/controller-runtime"
	"time"

	"github.com/golang/glog"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2beta1"
	v1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubernetes/pkg/controller/podautoscaler/metrics"
	resourceclient "k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"
	"k8s.io/metrics/pkg/client/custom_metrics"
	"k8s.io/metrics/pkg/client/external_metrics"
	webappv1 "sidecar-hpa/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	defaultSyncPeriod                            = time.Second * 15
	defaultTargetCPUUtilizationPercentage  int32 = 80
	defaultTolerance                             = 0.1
	defaultDownscaleForbiddenWindowSeconds       = 300
	defaultUpscaleForbiddenWindowSeconds         = 300
	defaultScaleUpLimitMinimum                   = 4.0
	defaultScaleUpLimitFactor                    = 2.0
)

// Add creates a new SHPA Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	clientConfig := mgr.GetConfig()

	metricsClient := metrics.NewRESTMetricsClient(
		resourceclient.NewForConfigOrDie(clientConfig),
		//TODO:这里需要重新写这里的逻辑 需要看HPA
		custom_metrics.NewForConfig(clientConfig, mgr.GetRESTMapper(), nil),
		external_metrics.NewForConfigOrDie(clientConfig),
	)
	clientSet, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		log.Fatal(err)
	}

	evtNamespacer := clientSet.CoreV1()
	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(glog.Infof)
	broadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: evtNamespacer.Events("")})
	recorder := broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "horizontal-pod-autoscaler"})

	replicaCalc := NewReplicaCalculator(metricsClient, clientSet.CoreV1(), defaultTolerance)
	return &SHPAReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		clientSet:     clientSet,
		replicaCalc:   replicaCalc,
		eventRecorder: recorder,
		syncPeriod:    defaultSyncPeriod,
	}
}

// When the SHPA is changed (status is changed, edited by the user, etc),
// a new "UpdateEvent" is generated and passed to the "updatePredicate" function.
// If the function returns "true", the event is added to the "Reconcile" queue,
// If the function returns "false", the event is skipped.
func updatePredicate(ev event.UpdateEvent) bool {
	oldObject := ev.ObjectOld.(*webappv1.SHPA)
	newObject := ev.ObjectNew.(*webappv1.SHPA)
	// Add the chpa object to the queue only if the spec has changed.
	// Status change should not lead to a requeue.
	if !apiequality.Semantic.DeepEqual(newObject.Spec, oldObject.Spec) {
		return true
	}
	return false
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("chpa-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to CHPA
	predicate := predicate.Funcs{UpdateFunc: updatePredicate}
	err = c.Watch(&source.Kind{Type: &webappv1.SHPA{}}, &handler.EnqueueRequestForObject{}, predicate)
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &SHPAReconciler{}

// SHPAReconciler reconciles a SHPA object
type SHPAReconciler struct {
	Log logr.gLogger
	client.Client
	//replicaCalculator *podautoscaler.ReplicaCalculator
	Scheme        *runtime.Scheme
	clientSet     kubernetes.Interface
	syncPeriod    time.Duration
	eventRecorder record.EventRecorder
	replicaCalc   *ReplicaCalculator
}

// Reconcile reads that state of the cluster for a SHPA object and makes changes based on the state read
// and what is in the SHPA.Spec
// The implementation repeats kubernetes hpa implementation from v1.10.8

// Automatically generate RBAC rules to allow the Controller to read and write Deployments
// TODO: decide, what to use: patch or update in rbac
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;update;patch
// +kubebuilder:rbac:groups=,resources=pods,verbs=get;list
// +kubebuilder:rbac:groups=autoscalers.postmates.com,resources=chpas,verbs=get;list;watch;create;update;patch;delete
func (r *SHPAReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	log.Printf("") // to have clear separation between previous and current reconcile run
	log.Printf("")
	log.Printf("Reconcile request: %v\n", request)

	// resRepeat will be returned if we want to re-run reconcile process
	// NB: we can't return non-nil err, as the "reconcile" msg will be added to the rate-limited queue
	// so that it'll slow down if we have several problems in a row
	resRepeat := reconcile.Result{RequeueAfter: r.syncPeriod}
	// resStop will be returned in case if we found some problem that can't be fixed, and we want to stop repeating reconcile process
	resStop := reconcile.Result{}

	shpa := &webappv1.SHPA{}
	err := r.Get(context.TODO(), request.NamespacedName, shpa)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Do not repeat the Reconcile again
			return resStop, nil
		}
		// Error reading the object (intermittent problems?) - requeue the request,
		log.Printf("Can't get CHPA object '%v': %v", request.NamespacedName, err)
		return resRepeat, nil
	}

	setSHPADefaults(shpa)

	if err := checkSHPAValidity(shpa); err != nil {
		log.Printf("Got an invalid CHPA spec '%v': %v", request.NamespacedName, err)
		// The chpa spec is incorrect (most likely, in "metrics" section) stop processing it
		// When the spec is updated, the chpa will be re-added to the reconcile queue
		r.eventRecorder.Event(shpa, v1.EventTypeWarning, "FailedSpecCheck", err.Error())
		setCondition(shpa, autoscalingv2.AbleToScale, v1.ConditionFalse, "FailedSpecCheck", "Invalid CHPA specification: %s", err)
		return resStop, nil
	}
	log.Printf("-> chpa: %v\n", shpa.String())

	// kind := shpa.Spec.ScaleTargetRef.Kind
	namespace := shpa.Namespace
	name := shpa.Spec.ScaleTargetRef.Name
	namespacedName := types.NamespacedName{Namespace: namespace, Name: name}

	deploy := &appsv1.Deployment{}
	if err := r.Get(context.TODO(), namespacedName, deploy); err != nil {
		// Error reading the object, repeat later
		log.Printf("Error reading Deployment '%v': %v", namespacedName, err)
		return resRepeat, nil
	}
	if err := controllerutil.SetControllerReference(shpa, deploy, r.Scheme); err != nil {
		// Error communicating with apiserver, repeat later
		log.Printf("Can't set the controller reference for the deployment %v: %v", namespacedName, err)
		return resRepeat, nil
	}

	if err := r.reconcileCHPA(shpa, deploy); err != nil {
		// Should never happen, actually.
		log.Printf(err.Error())
		r.eventRecorder.Event(shpa, v1.EventTypeWarning, "FailedProcessCHPA", err.Error())
		setCondition(shpa, autoscalingv2.AbleToScale, v1.ConditionTrue, "FailedProcessCHPA", "Error happened while processing the CHPA")
		return resStop, nil
	}
	return resRepeat, nil
}

// Function returns an error only when we need to stop working with the CHPA spec
func (r *SHPAReconciler) reconcileCHPA(shpa *webappv1.SHPA, deploy *appsv1.Deployment) (err error) {
	defer func() {
		if err1 := recover(); err1 != nil {
			err = fmt.Errorf("RunTime error in reconcileCHPA: %s", err1)
		}
	}()
	currentReplicas := deploy.Status.Replicas
	log.Printf("-> deploy: {%v/%v replicas:%v}\n", deploy.Namespace, deploy.Name, currentReplicas)
	chpaStatusOriginal := shpa.Status.DeepCopy()

	reference := fmt.Sprintf("%s/%s/%s", shpa.Spec.ScaleTargetRef.Kind, shpa.Namespace, shpa.Spec.ScaleTargetRef.Name)

	setCondition(shpa, autoscalingv2.AbleToScale, v1.ConditionTrue, "SucceededGetScale", "the HPA controller was able to get the target's current scale")

	var metricStatuses []autoscalingv2.MetricStatus
	metricDesiredReplicas := int32(0)
	metricName := ""
	metricTimestamp := time.Time{}

	desiredReplicas := int32(0)
	rescaleReason := ""
	timestamp := time.Now()

	rescale := true

	if *deploy.Spec.Replicas == 0 {
		// Autoscaling is disabled for this resource
		desiredReplicas = 0
		rescale = false
		setCondition(shpa, autoscalingv2.ScalingActive, v1.ConditionFalse, "ScalingDisabled", "scaling is disabled since the replica count of the target is zero")
	} else if currentReplicas > shpa.Spec.MaxReplicas {
		rescaleReason = "Current number of replicas above Spec.MaxReplicas"
		desiredReplicas = shpa.Spec.MaxReplicas
	} else if shpa.Spec.MinReplicas != nil && currentReplicas < *shpa.Spec.MinReplicas {
		rescaleReason = "Current number of replicas below Spec.MinReplicas"
		desiredReplicas = *shpa.Spec.MinReplicas
	} else if currentReplicas == 0 {
		rescaleReason = "Current number of replicas must be greater than 0"
		desiredReplicas = 1
	} else {
		metricDesiredReplicas, metricName, metricStatuses, metricTimestamp, err = r.computeReplicasForMetrics(shpa, deploy, shpa.Spec.Metrics)
		if err != nil {
			r.setCurrentReplicasInStatus(shpa, currentReplicas)
			if err := r.updateStatusIfNeeded(chpaStatusOriginal, shpa); err != nil {
				r.eventRecorder.Event(shpa, v1.EventTypeWarning, "FailedUpdateReplicas", err.Error())
				setCondition(shpa, autoscalingv2.AbleToScale, v1.ConditionFalse, "FailedUpdateReplicas", "the CHPA controller was unable to update the number of replicas: %v", err)
				log.Printf("the CHPA controller was unable to update the number of replicas: %v", err)
				return nil
			}
			r.eventRecorder.Event(shpa, v1.EventTypeWarning, "FailedComputeMetricsReplicas", err.Error())
			log.Printf("failed to compute desired number of replicas based on listed metrics for %s: %v", reference, err)
			return nil
		}
		log.Printf("proposing %v desired replicas (based on %s from %s) for %s", metricDesiredReplicas, metricName, timestamp, reference)

		rescaleMetric := ""
		if metricDesiredReplicas > desiredReplicas {
			desiredReplicas = metricDesiredReplicas
			timestamp = metricTimestamp
			rescaleMetric = metricName
		}
		if desiredReplicas > currentReplicas {
			rescaleReason = fmt.Sprintf("%s above target", rescaleMetric)
		}
		if desiredReplicas < currentReplicas {
			rescaleReason = "All metrics below target"
		}

		desiredReplicas = r.normalizeDesiredReplicas(shpa, currentReplicas, desiredReplicas)
		log.Printf(" -> after normalization: %v", desiredReplicas)

		rescale = r.shouldScale(shpa, currentReplicas, desiredReplicas, timestamp)
		backoffDown := false
		backoffUp := false
		if shpa.Status.LastScaleTime != nil {
			downscaleForbiddenWindow := time.Duration(shpa.Spec.DownscaleForbiddenWindowSeconds) * time.Second
			if !shpa.Status.LastScaleTime.Add(downscaleForbiddenWindow).Before(timestamp) {
				setCondition(shpa, autoscalingv2.AbleToScale, v1.ConditionFalse, "BackoffDownscale", "the time since the previous scale is still within the downscale forbidden window")
				backoffDown = true
			}

			upscaleForbiddenWindow := time.Duration(shpa.Spec.UpscaleForbiddenWindowSeconds) * time.Second
			if !shpa.Status.LastScaleTime.Add(upscaleForbiddenWindow).Before(timestamp) {
				backoffUp = true
				if backoffDown {
					setCondition(shpa, autoscalingv2.AbleToScale, v1.ConditionFalse, "BackoffBoth", "the time since the previous scale is still within both the downscale and upscale forbidden windows")
				} else {
					setCondition(shpa, autoscalingv2.AbleToScale, v1.ConditionFalse, "BackoffUpscale", "the time since the previous scale is still within the upscale forbidden window")
				}
			}
		}

		if !backoffDown && !backoffUp {
			// mark that we're not backing off
			setCondition(shpa, autoscalingv2.AbleToScale, v1.ConditionTrue, "ReadyForNewScale", "the last scale time was sufficiently old as to warrant a new scale")
		}
	}

	if rescale {
		deploy.Spec.Replicas = &desiredReplicas
		if err := r.Update(context.TODO(), deploy); err != nil {
			r.eventRecorder.Eventf(shpa, v1.EventTypeWarning, "FailedRescale", "New size: %d; reason: %s; error: %v", desiredReplicas, rescaleReason, err.Error())
			setCondition(shpa, autoscalingv2.AbleToScale, v1.ConditionFalse, "FailedUpdateScale", "the HPA controller was unable to update the target scale: %v", err)
			r.setCurrentReplicasInStatus(shpa, currentReplicas)
			if err := r.updateStatusIfNeeded(chpaStatusOriginal, shpa); err != nil {
				r.eventRecorder.Event(shpa, v1.EventTypeWarning, "FailedUpdateReplicas", err.Error())
				setCondition(shpa, autoscalingv2.AbleToScale, v1.ConditionFalse, "FailedUpdateReplicas", "the CHPA controller was unable to update the number of replicas: %v", err)
				return nil
			}
			return nil
		}
		setCondition(shpa, autoscalingv2.AbleToScale, v1.ConditionTrue, "SucceededRescale", "the HPA controller was able to update the target scale to %d", desiredReplicas)
		r.eventRecorder.Eventf(shpa, v1.EventTypeNormal, "SuccessfulRescale", "New size: %d; reason: %s", desiredReplicas, rescaleReason)
		log.Printf("Successful rescale of %s, old size: %d, new size: %d, reason: %s",
			shpa.Name, currentReplicas, desiredReplicas, rescaleReason)
	} else {
		log.Printf("decided not to scale %s to %v (last scale time was %s)", reference, desiredReplicas, shpa.Status.LastScaleTime)
		desiredReplicas = currentReplicas
	}

	r.setStatus(shpa, currentReplicas, desiredReplicas, metricStatuses, rescale)
	r.updateStatusIfNeeded(chpaStatusOriginal, shpa)

	return nil
}

func (r *SHPAReconciler) computeReplicasForMetrics(shpa *webappv1.SHPA, deploy *appsv1.Deployment, metricSpecs []autoscalingv2.MetricSpec) (replicas int32, metric string, statuses []autoscalingv2.MetricStatus, timestamp time.Time, err error) {
	currentReplicas := deploy.Status.Replicas

	statuses = make([]autoscalingv2.MetricStatus, len(metricSpecs))

	for i, metricSpec := range metricSpecs {
		if deploy.Spec.Selector == nil {
			errMsg := "selector is required"
			r.eventRecorder.Event(shpa, v1.EventTypeWarning, "SelectorRequired", errMsg)
			setCondition(shpa, autoscalingv2.ScalingActive, v1.ConditionFalse, "InvalidSelector", "the CHPA target's deploy is missing a selector")
			return 0, "", nil, time.Time{}, fmt.Errorf(errMsg)
		}

		selector, err := metav1.LabelSelectorAsSelector(deploy.Spec.Selector)
		if err != nil {
			errMsg := fmt.Sprintf("couldn't convert selector into a corresponding internal selector object: %v", err)
			r.eventRecorder.Event(shpa, v1.EventTypeWarning, "InvalidSelector", errMsg)
			setCondition(shpa, autoscalingv2.ScalingActive, v1.ConditionFalse, "InvalidSelector", errMsg)
			return 0, "", nil, time.Time{}, fmt.Errorf(errMsg)
		}

		var replicaCountProposal int32
		var utilizationProposal int64
		var timestampProposal time.Time
		var metricNameProposal string

		switch metricSpec.Type {
		case autoscalingv2.ObjectMetricSourceType:
			replicaCountProposal, utilizationProposal, timestampProposal, err = r.replicaCalc.GetObjectMetricReplicas(currentReplicas, metricSpec.Object.TargetValue.MilliValue(), metricSpec.Object.MetricName, shpa.Namespace, (*autoscaling.CrossVersionObjectReference)(&metricSpec.Object.Target))
			if err != nil {
				r.eventRecorder.Event(shpa, v1.EventTypeWarning, "FailedGetObjectMetric", err.Error())
				setCondition(shpa, autoscalingv2.ScalingActive, v1.ConditionFalse, "FailedGetObjectMetric", "the HPA was unable to compute the replica count: %v", err)
				return 0, "", nil, time.Time{}, fmt.Errorf("failed to get object metric value: %v", err)
			}
			metricNameProposal = fmt.Sprintf("%s metric %s", metricSpec.Object.Target.Kind, metricSpec.Object.MetricName)
			statuses[i] = autoscalingv2.MetricStatus{
				Type: autoscalingv2.ObjectMetricSourceType,
				Object: &autoscalingv2.ObjectMetricStatus{
					Target:       metricSpec.Object.Target,
					MetricName:   metricSpec.Object.MetricName,
					CurrentValue: *resource.NewMilliQuantity(utilizationProposal, resource.DecimalSI),
				},
			}
		case autoscalingv2.PodsMetricSourceType:
			replicaCountProposal, utilizationProposal, timestampProposal, err = r.replicaCalc.GetMetricReplicas(currentReplicas, metricSpec.Pods.TargetAverageValue.MilliValue(), metricSpec.Pods.MetricName, shpa.Namespace, selector)
			if err != nil {
				r.eventRecorder.Event(shpa, v1.EventTypeWarning, "FailedGetPodsMetric", err.Error())
				setCondition(shpa, autoscalingv2.ScalingActive, v1.ConditionFalse, "FailedGetPodsMetric", "the HPA was unable to compute the replica count: %v", err)
				return 0, "", nil, time.Time{}, fmt.Errorf("failed to get pods metric value: %v", err)
			}
			metricNameProposal = fmt.Sprintf("pods metric %s", metricSpec.Pods.MetricName)
			statuses[i] = autoscalingv2.MetricStatus{
				Type: autoscalingv2.PodsMetricSourceType,
				Pods: &autoscalingv2.PodsMetricStatus{
					MetricName:          metricSpec.Pods.MetricName,
					CurrentAverageValue: *resource.NewMilliQuantity(utilizationProposal, resource.DecimalSI),
				},
			}
		case autoscalingv2.ResourceMetricSourceType:
			if metricSpec.Resource.TargetAverageValue != nil {
				var rawProposal int64
				replicaCountProposal, rawProposal, timestampProposal, err = r.replicaCalc.GetRawResourceReplicas(currentReplicas, metricSpec.Resource.TargetAverageValue.MilliValue(), metricSpec.Resource.Name, shpa.Namespace, selector)
				if err != nil {
					r.eventRecorder.Event(shpa, v1.EventTypeWarning, "FailedGetResourceMetric", err.Error())
					setCondition(shpa, autoscalingv2.ScalingActive, v1.ConditionFalse, "FailedGetResourceMetric", "the HPA was unable to compute the replica count: %v", err)
					return 0, "", nil, time.Time{}, fmt.Errorf("failed to get %s utilization: %v", metricSpec.Resource.Name, err)
				}
				metricNameProposal = fmt.Sprintf("%s resource", metricSpec.Resource.Name)
				statuses[i] = autoscalingv2.MetricStatus{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricStatus{
						Name:                metricSpec.Resource.Name,
						CurrentAverageValue: *resource.NewMilliQuantity(rawProposal, resource.DecimalSI),
					},
				}
			} else {
				// set a default utilization percentage if none is set
				if metricSpec.Resource.TargetAverageUtilization == nil {
					errMsg := "invalid resource metric source: neither a utilization target nor a value target was set"
					r.eventRecorder.Event(shpa, v1.EventTypeWarning, "FailedGetResourceMetric", errMsg)
					setCondition(shpa, autoscalingv2.ScalingActive, v1.ConditionFalse, "FailedGetResourceMetric", "the HPA was unable to compute the replica count: %s", errMsg)
					return 0, "", nil, time.Time{}, fmt.Errorf(errMsg)
				}

				targetUtilization := *metricSpec.Resource.TargetAverageUtilization

				var percentageProposal int32
				var rawProposal int64
				replicaCountProposal, percentageProposal, rawProposal, timestampProposal, err = r.replicaCalc.GetResourceReplicas(currentReplicas, targetUtilization, metricSpec.Resource.Name, shpa.Namespace, selector)
				if err != nil {
					r.eventRecorder.Event(shpa, v1.EventTypeWarning, "FailedGetResourceMetric", err.Error())
					setCondition(shpa, autoscalingv2.ScalingActive, v1.ConditionFalse, "FailedGetResourceMetric", "the HPA was unable to compute the replica count: %v", err)
					return 0, "", nil, time.Time{}, fmt.Errorf("failed to get %s utilization: %v", metricSpec.Resource.Name, err)
				}
				metricNameProposal = fmt.Sprintf("%s resource utilization (percentage of request)", metricSpec.Resource.Name)
				statuses[i] = autoscalingv2.MetricStatus{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricStatus{
						Name:                      metricSpec.Resource.Name,
						CurrentAverageUtilization: &percentageProposal,
						CurrentAverageValue:       *resource.NewMilliQuantity(rawProposal, resource.DecimalSI),
					},
				}
			}
		case autoscalingv2.ExternalMetricSourceType:
			if metricSpec.External.TargetAverageValue != nil {
				replicaCountProposal, utilizationProposal, timestampProposal, err = r.replicaCalc.GetExternalPerPodMetricReplicas(currentReplicas, metricSpec.External.TargetAverageValue.MilliValue(), metricSpec.External.MetricName, shpa.Namespace, metricSpec.External.MetricSelector)
				if err != nil {
					r.eventRecorder.Event(shpa, v1.EventTypeWarning, "FailedGetExternalMetric", err.Error())
					setCondition(shpa, autoscalingv2.ScalingActive, v1.ConditionFalse, "FailedGetExternalMetric", "the HPA was unable to compute the replica count: %v", err)
					return 0, "", nil, time.Time{}, fmt.Errorf("failed to get %s external metric: %v", metricSpec.External.MetricName, err)
				}
				metricNameProposal = fmt.Sprintf("external metric %s(%+v)", metricSpec.External.MetricName, metricSpec.External.MetricSelector)
				statuses[i] = autoscalingv2.MetricStatus{
					Type: autoscalingv2.ExternalMetricSourceType,
					External: &autoscalingv2.ExternalMetricStatus{
						MetricSelector:      metricSpec.External.MetricSelector,
						MetricName:          metricSpec.External.MetricName,
						CurrentAverageValue: resource.NewMilliQuantity(utilizationProposal, resource.DecimalSI),
					},
				}
			} else if metricSpec.External.TargetValue != nil {
				replicaCountProposal, utilizationProposal, timestampProposal, err = r.replicaCalc.GetExternalMetricReplicas(currentReplicas, metricSpec.External.TargetValue.MilliValue(), metricSpec.External.MetricName, shpa.Namespace, metricSpec.External.MetricSelector)
				if err != nil {
					r.eventRecorder.Event(shpa, v1.EventTypeWarning, "FailedGetExternalMetric", err.Error())
					setCondition(shpa, autoscalingv2.ScalingActive, v1.ConditionFalse, "FailedGetExternalMetric", "the HPA was unable to compute the replica count: %v", err)
					return 0, "", nil, time.Time{}, fmt.Errorf("failed to get external metric %s: %v", metricSpec.External.MetricName, err)
				}
				metricNameProposal = fmt.Sprintf("external metric %s(%+v)", metricSpec.External.MetricName, metricSpec.External.MetricSelector)
				statuses[i] = autoscalingv2.MetricStatus{
					Type: autoscalingv2.ExternalMetricSourceType,
					External: &autoscalingv2.ExternalMetricStatus{
						MetricSelector: metricSpec.External.MetricSelector,
						MetricName:     metricSpec.External.MetricName,
						CurrentValue:   *resource.NewMilliQuantity(utilizationProposal, resource.DecimalSI),
					},
				}
			} else {
				errMsg := "invalid external metric source: neither a value target nor an average value target was set"
				r.eventRecorder.Event(shpa, v1.EventTypeWarning, "FailedGetExternalMetric", errMsg)
				setCondition(shpa, autoscalingv2.ScalingActive, v1.ConditionFalse, "FailedGetExternalMetric", "the HPA was unable to compute the replica count: %v", err)
				return 0, "", nil, time.Time{}, fmt.Errorf(errMsg)
			}
		default:
			errMsg := fmt.Sprintf("unknown metric source type %q", string(metricSpec.Type))
			r.eventRecorder.Event(shpa, v1.EventTypeWarning, "InvalidMetricSourceType", errMsg)
			setCondition(shpa, autoscalingv2.ScalingActive, v1.ConditionFalse, "InvalidMetricSourceType", "the HPA was unable to compute the replica count: %s", errMsg)
			return 0, "", nil, time.Time{}, fmt.Errorf(errMsg)
		}
		if replicas == 0 || replicaCountProposal > replicas {
			timestamp = timestampProposal
			replicas = replicaCountProposal
			metric = metricNameProposal
		}
	}

	setCondition(shpa, autoscalingv2.ScalingActive, v1.ConditionTrue, "ValidMetricFound", "the HPA was able to successfully calculate a replica count from %s", metric)
	return replicas, metric, statuses, timestamp, nil
}

func (r *SHPAReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&webappv1.SHPA{}).
		Complete(r)
}

// normalizeDesiredReplicas takes the metrics desired replicas value and normalizes it based on the appropriate conditions (i.e. < maxReplicas, >
// minReplicas, etc...)
func (r *SHPAReconciler) normalizeDesiredReplicas(shpa *webappv1.SHPA, currentReplicas int32, prenormalizedDesiredReplicas int32) int32 {
	var minReplicas int32
	if shpa.Spec.MinReplicas != nil {
		minReplicas = *shpa.Spec.MinReplicas
	} else {
		minReplicas = 0
	}

	desiredReplicas, condition, reason := convertDesiredReplicasWithRules(shpa, currentReplicas, prenormalizedDesiredReplicas, minReplicas, shpa.Spec.MaxReplicas)

	if desiredReplicas == prenormalizedDesiredReplicas {
		setCondition(shpa, autoscalingv2.ScalingLimited, v1.ConditionFalse, condition, reason)
	} else {
		setCondition(shpa, autoscalingv2.ScalingLimited, v1.ConditionTrue, condition, reason)
	}

	return desiredReplicas
}

// convertDesiredReplicas performs the actual normalization, without depending on `HorizontalController` or `HorizontalPodAutoscaler`
func convertDesiredReplicasWithRules(shpa *webappv1.SHPA, currentReplicas, desiredReplicas, hpaMinReplicas, hpaMaxReplicas int32) (int32, string, string) {

	var minimumAllowedReplicas int32
	var maximumAllowedReplicas int32

	var possibleLimitingCondition string
	var possibleLimitingReason string

	if hpaMinReplicas == 0 {
		minimumAllowedReplicas = 1
		possibleLimitingReason = "the desired replica count is zero"
	} else {
		minimumAllowedReplicas = hpaMinReplicas
		possibleLimitingReason = "the desired replica count is less than the minimum replica count"
	}

	// Do not upscale too much to prevent incorrect rapid increase of the number of master replicas caused by
	// bogus CPU usage report from heapster/kubelet (like in issue #32304).
	scaleUpLimit := calculateScaleUpLimit(shpa, currentReplicas)

	if hpaMaxReplicas > scaleUpLimit {
		maximumAllowedReplicas = scaleUpLimit

		possibleLimitingCondition = "ScaleUpLimit"
		possibleLimitingReason = "the desired replica count is increasing faster than the maximum scale rate"
	} else {
		maximumAllowedReplicas = hpaMaxReplicas

		possibleLimitingCondition = "TooManyReplicas"
		possibleLimitingReason = "the desired replica count is more than the maximum replica count"
	}

	if desiredReplicas < minimumAllowedReplicas {
		possibleLimitingCondition = "TooFewReplicas"

		return minimumAllowedReplicas, possibleLimitingCondition, possibleLimitingReason
	} else if desiredReplicas > maximumAllowedReplicas {
		return maximumAllowedReplicas, possibleLimitingCondition, possibleLimitingReason
	}

	return desiredReplicas, "DesiredWithinRange", "the desired count is within the acceptable range"
}

// setCondition sets the specific condition type on the given HPA to the specified value with the given reason
// and message.  The message and args are treated like a format string.  The condition will be added if it is
// not present.
func setCondition(shpa *webappv1.SHPA, conditionType autoscalingv2.HorizontalPodAutoscalerConditionType, status v1.ConditionStatus, reason, message string, args ...interface{}) {
	shpa.Status.Conditions = setConditionInList(shpa.Status.Conditions, conditionType, status, reason, message, args...)
}

// setConditionInList sets the specific condition type on the given HPA to the specified value with the given
// reason and message.  The message and args are treated like a format string.  The condition will be added if
// it is not present.  The new list will be returned.
func setConditionInList(inputList []autoscalingv2.HorizontalPodAutoscalerCondition, conditionType autoscalingv2.HorizontalPodAutoscalerConditionType, status v1.ConditionStatus, reason, message string, args ...interface{}) []autoscalingv2.HorizontalPodAutoscalerCondition {
	resList := inputList
	var existingCond *autoscalingv2.HorizontalPodAutoscalerCondition
	for i, condition := range resList {
		if condition.Type == conditionType {
			// can't take a pointer to an iteration variable
			existingCond = &resList[i]
			break
		}
	}

	if existingCond == nil {
		resList = append(resList, autoscalingv2.HorizontalPodAutoscalerCondition{
			Type: conditionType,
		})
		existingCond = &resList[len(resList)-1]
	}

	if existingCond.Status != status {
		existingCond.LastTransitionTime = metav1.Now()
	}

	existingCond.Status = status
	existingCond.Reason = reason
	existingCond.Message = fmt.Sprintf(message, args...)

	return resList
}

func setSHPADefaults(shpa *webappv1.SHPA) {
	if shpa.Spec.DownscaleForbiddenWindowSeconds == 0 {
		shpa.Spec.DownscaleForbiddenWindowSeconds = defaultDownscaleForbiddenWindowSeconds
	}
	if shpa.Spec.UpscaleForbiddenWindowSeconds == 0 {
		shpa.Spec.UpscaleForbiddenWindowSeconds = defaultUpscaleForbiddenWindowSeconds
	}
	if shpa.Spec.ScaleUpLimitFactor == 0.0 {
		shpa.Spec.ScaleUpLimitFactor = defaultScaleUpLimitFactor
	}
	if shpa.Spec.ScaleUpLimitMinimum == 0 {
		shpa.Spec.ScaleUpLimitMinimum = defaultScaleUpLimitMinimum
	}
	if shpa.Spec.Tolerance == 0 {
		shpa.Spec.Tolerance = defaultTolerance
	}
}

// setCurrentReplicasInStatus sets the current replica count in the status of the HPA.
func (r *SHPAReconciler) setCurrentReplicasInStatus(shpa *webappv1.SHPA, currentReplicas int32) {
	r.setStatus(shpa, currentReplicas, shpa.Status.DesiredReplicas, shpa.Status.CurrentMetrics, false)
}

// setStatus recreates the status of the given HPA, updating the current and
// desired replicas, as well as the metric statuses
func (r *SHPAReconciler) setStatus(chpa *webappv1.SHPA, currentReplicas, desiredReplicas int32, metricStatuses []autoscalingv2.MetricStatus, rescale bool) {
	chpa.Status = webappv1.SHPAStatus{
		CurrentReplicas: currentReplicas,
		DesiredReplicas: desiredReplicas,
		LastScaleTime:   chpa.Status.LastScaleTime,
		CurrentMetrics:  metricStatuses,
		Conditions:      chpa.Status.Conditions,
	}

	if rescale {
		now := metav1.NewTime(time.Now())
		chpa.Status.LastScaleTime = &now
	}
}
func checkSHPAValidity(shpa *webappv1.SHPA) error {
	if shpa.Spec.ScaleTargetRef.Kind != "Deployment" {
		msg := fmt.Sprintf("configurable chpa doesn't support %s kind, use Deployment instead", shpa.Spec.ScaleTargetRef.Kind)
		log.Printf(msg)
		return fmt.Errorf(msg)
	}
	return checkSHPAMetricsValidity(shpa.Spec.Metrics)
}

func checkSHPAMetricsValidity(metrics []autoscalingv2.MetricSpec) (err error) {
	// This function will not be needed for the vanilla k8s.
	// For now we check only nil pointers here as they crash the default controller algorithm
	for _, metric := range metrics {
		switch metric.Type {
		case "Object":
			if metric.Object == nil {
				return fmt.Errorf("metric.Object is nil while metric.Type is '%s'", metric.Type)
			}
		case "Pods":
			if metric.Pods == nil {
				return fmt.Errorf("metric.Pods is nil while metric.Type is '%s'", metric.Type)
			}
		case "Resource":
			if metric.Resource == nil {
				return fmt.Errorf("metric.Resource is nil while metric.Type is '%s'", metric.Type)
			}
		case "External":
			if metric.External == nil {
				return fmt.Errorf("metric.External is nil while metric.Type is '%s'", metric.Type)
			}
		default:
			return fmt.Errorf("incorrect metric.Type: '%s'", metric.Type)
		}

	}
	return nil
}

func calculateScaleUpLimit(shpa *webappv1.SHPA, currentReplicas int32) int32 {
	return int32(math.Max(shpa.Spec.ScaleUpLimitFactor*float64(currentReplicas), float64(shpa.Spec.ScaleUpLimitMinimum)))
}

func (r *SHPAReconciler) shouldScale(chpa *webappv1.SHPA, currentReplicas, desiredReplicas int32, timestamp time.Time) bool {
	if desiredReplicas == currentReplicas {
		log.Printf("Will not scale: number of replicas is not changed")
		return false
	}

	if chpa.Status.LastScaleTime == nil {
		return true
	}

	// Scale down only if the usageRatio dropped significantly below the target
	// and there was no rescaling in the last downscaleForbiddenWindow.
	downscaleForbiddenWindow := time.Duration(chpa.Spec.DownscaleForbiddenWindowSeconds) * time.Second
	if desiredReplicas < currentReplicas {
		if chpa.Status.LastScaleTime.Add(downscaleForbiddenWindow).Before(timestamp) {
			return true
		}
		log.Printf("Too early to scale. Last scale was at %s, next scale will be at %s, last metrics timestamp: %s", chpa.Status.LastScaleTime, chpa.Status.LastScaleTime.Add(downscaleForbiddenWindow), timestamp)
	}

	// Scale up only if the usage ratio increased significantly above the target
	// and there was no rescaling in the last upscaleForbiddenWindow.
	upscaleForbiddenWindow := time.Duration(chpa.Spec.UpscaleForbiddenWindowSeconds) * time.Second
	if desiredReplicas > currentReplicas {
		if chpa.Status.LastScaleTime.Add(upscaleForbiddenWindow).Before(timestamp) {
			return true
		}
		log.Printf("Too early to scale. Last scale was at %s, next scale will be at %s, last metrics timestamp: %s", chpa.Status.LastScaleTime, chpa.Status.LastScaleTime.Add(upscaleForbiddenWindow), timestamp)
	}
	return false
}

// updateStatusIfNeeded calls updateStatus only if the status of the new HPA is not the same as the old status
func (r *SHPAReconciler) updateStatusIfNeeded(oldStatus *webappv1.SHPAStatus, newCHPA *webappv1.SHPA) error {
	// skip a write if we wouldn't need to update
	if apiequality.Semantic.DeepEqual(oldStatus, &newCHPA.Status) {
		return nil
	}
	return r.updateCHPA(newCHPA)
}

func (r *SHPAReconciler) updateCHPA(shpa *webappv1.SHPA) error {
	return r.Update(context.TODO(), shpa)
}
