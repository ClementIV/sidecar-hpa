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

package v1

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// CrossVersionObjectReference contains enough information to let you identify the referred resource.
type CrossVersionObjectReference struct {
	// Kind of the referent; More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds"
	Kind string `json:"kind"`
	// Name of the referent; More info: http://kubernetes.io/docs/user-guide/identifiers#names
	Name string `json:"name"`
	// API version of the referent
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`
}

// SHPASpec defines the desired state of SHPA
type SHPASpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// part of HorizontalController, see comments in the k8s repo: pkg/controller/podautoscaler/horizontal.go
	DownscaleForbiddenWindowSeconds int32 `json:"downscaleForbiddenWindowSeconds,omitempty"`
	UpscaleForbiddenWindowSeconds   int32 `json:"upscaleForbiddenWindowSeconds,omitempty"`
	// See the comment about this parameter above
	ScaleUpLimitFactor float32 `json:"scaleUpLimitFactor,omitempty"`
	// See the comment about this parameter above
	ScaleUpLimitMinimum int32   `json:"scaleUpLimitMinimum,omitempty"`
	Tolerance           float32 `json:"tolerance,omitempty"`

	// part of HorizontalPodAutoscalerSpec, see comments in the k8s-1.10.8 repo: staging/src/k8s.io/api/autoscaling/v1/types.go
	// reference to scaled resource; horizontal pod autoscaler will learn the current resource consumption
	// and will set the desired number of pods by using its Scale subresource.
	ScaleTargetRef CrossVersionObjectReference `json:"scaleTargetRef"`
	// specifications that will be used to calculate the desired replica count
	Metrics []autoscalingv2.MetricSpec `json:"metrics,omitempty"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	MinReplicas *int32 `json:"minReplicas,omitempty"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	MaxReplicas int32 `json:"maxReplicas"`
}

// SHPAStatus defines the observed state of SHPA
type SHPAStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	ObservedGeneration *int64                                           `json:"observedGeneration,omitempty"`
	LastScaleTime      *metav1.Time                                     `json:"lastScaleTime,omitempty"`
	CurrentReplicas    int32                                            `json:"currentReplicas"`
	DesiredReplicas    int32                                            `json:"desiredReplicas"`
	CurrentMetrics     []autoscalingv2.MetricStatus                     `json:"currentMetrics"`
	Conditions         []autoscalingv2.HorizontalPodAutoscalerCondition `json:"conditions"`
}

// +kubebuilder:object:root=true

// SHPA is the Schema for the shpas API
type SHPA struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SHPASpec   `json:"spec,omitempty"`
	Status SHPAStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SHPAList contains a list of SHPA
type SHPAList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SHPA `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SHPA{}, &SHPAList{})
}
