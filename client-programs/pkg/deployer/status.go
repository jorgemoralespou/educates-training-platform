package deployer

import (
	"context"
	stderrors "errors"
	"net/http"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// PlatformComponentStatus is the observed status of one Educates platform CR.
type PlatformComponentStatus struct {
	Kind     string // e.g. "EducatesClusterConfig"
	Optional bool   // absent is acceptable (only LookupService)
	Present  bool   // the CR (and its CRD) exists in the cluster
	Ready    bool   // status.conditions[type=Ready].status == "True"
	Phase    string // status.phase, for extra detail when not Ready
}

// platformCRs lists the Educates platform singletons (all named "cluster")
// in install order. LookupService is optional — it is only present when
// lookupService is enabled — so an absent LookupService is not a fault.
var platformCRs = []struct {
	kind     string
	gvr      schema.GroupVersionResource
	optional bool
}{
	{"EducatesClusterConfig", schema.GroupVersionResource{Group: "config.educates.dev", Version: "v1alpha1", Resource: "educatesclusterconfigs"}, false},
	{"SecretsManager", schema.GroupVersionResource{Group: "platform.educates.dev", Version: "v1alpha1", Resource: "secretsmanagers"}, false},
	{"LookupService", schema.GroupVersionResource{Group: "platform.educates.dev", Version: "v1alpha1", Resource: "lookupservices"}, true},
	{"SessionManager", schema.GroupVersionResource{Group: "platform.educates.dev", Version: "v1alpha1", Resource: "sessionmanagers"}, false},
}

// PlatformStatus reads the status of the Educates platform CRs from the
// cluster. A CR whose CRD or "cluster" instance is absent is reported with
// Present=false — that is how a cluster-only install (no Educates) is
// detected. A connectivity error (unreachable API) is returned as an error.
func PlatformStatus(ctx context.Context, dyn dynamic.Interface) ([]PlatformComponentStatus, error) {
	result := make([]PlatformComponentStatus, 0, len(platformCRs))

	for _, cr := range platformCRs {
		st := PlatformComponentStatus{Kind: cr.kind, Optional: cr.optional}

		obj, err := dyn.Resource(cr.gvr).Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			// A missing CRD or a missing "cluster" instance both come back
			// as 404 — either way the component is not installed.
			if notInstalled(err) {
				result = append(result, st)
				continue
			}
			return nil, errors.Wrapf(err, "unable to read %s status", cr.kind)
		}

		st.Present = true
		st.Ready = crReady(obj)
		st.Phase, _, _ = unstructured.NestedString(obj.Object, "status", "phase")
		result = append(result, st)
	}

	return result, nil
}

// crReady reports whether the object has status.conditions[type=Ready] set
// to "True".
func crReady(obj *unstructured.Unstructured) bool {
	conds, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}
	for _, c := range conds {
		m, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if m["type"] == "Ready" && m["status"] == "True" {
			return true
		}
	}
	return false
}

// notInstalled reports whether err means the resource type or instance does
// not exist (a missing "cluster" CR reports NotFound; a missing CRD reports a
// bare 404).
func notInstalled(err error) bool {
	if apierrors.IsNotFound(err) {
		return true
	}
	var status apierrors.APIStatus
	if stderrors.As(err, &status) {
		return status.Status().Code == http.StatusNotFound
	}
	return false
}
