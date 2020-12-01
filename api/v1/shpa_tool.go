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

import "fmt"

func (shpa SHPA) String() string {
	ret := fmt.Sprintf("Spec:%v Status:%v", shpa.Spec, shpa.Status)
	return ret
}

func (shpa_spec SHPASpec) String() string {
	minReplicas := "<nil>"
	if shpa_spec.MinReplicas != nil {
		minReplicas = fmt.Sprintf("%v", *shpa_spec.MinReplicas)
	}
	ret := fmt.Sprintf("{Ref:%v/%v DFWS:%v UFWS:%v SULF:%v SULM:%v T:%v MinR:%v MaxR:%v M:%v}",
		shpa_spec.ScaleTargetRef.Kind,
		shpa_spec.ScaleTargetRef.Name,
		shpa_spec.DownscaleForbiddenWindowSeconds,
		shpa_spec.UpscaleForbiddenWindowSeconds,
		shpa_spec.ScaleUpLimitFactor,
		shpa_spec.ScaleUpLimitMinimum,
		shpa_spec.Tolerance,
		minReplicas,
		shpa_spec.MaxReplicas,
		shpa_spec.Metrics)
	return ret
}

func (shpa_status SHPAStatus) String() string {
	lastScaleTime := "<nil>"
	if shpa_status.LastScaleTime != nil {
		lastScaleTime = fmt.Sprintf("%v", *shpa_status.LastScaleTime)
	}
	ret := fmt.Sprintf("{LST:%v CR:%v DR:%v}", lastScaleTime, shpa_status.CurrentReplicas, shpa_status.DesiredReplicas)
	return ret
}
