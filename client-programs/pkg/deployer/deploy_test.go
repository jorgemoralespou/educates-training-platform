package deployer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/apply"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/wait"
)

// providerImmutableRejection is the response a Kubernetes API server
// returns when an apply changes spec.ingress.certificates.provider of
// an existing EducatesClusterConfig, recorded from the CRD's CEL rule.
const providerImmutableRejection = `{"kind":"Status","apiVersion":"v1","metadata":{},"status":"Failure","message":"EducatesClusterConfig.config.educates.dev \"cluster\" is invalid: spec: Invalid value: spec.ingress.certificates.provider is immutable; delete and recreate the resource to change the certificate provider","reason":"Invalid","details":{"name":"cluster","group":"config.educates.dev","kind":"EducatesClusterConfig","causes":[{"reason":"FieldValueInvalid","message":"Invalid value: spec.ingress.certificates.provider is immutable; delete and recreate the resource to change the certificate provider","field":"spec"}]},"code":422}`

// protocolRejection is a different validation failure of the same
// resource, which the CLI must pass through unexplained.
const protocolRejection = `{"kind":"Status","apiVersion":"v1","metadata":{},"status":"Failure","message":"EducatesClusterConfig.config.educates.dev \"cluster\" is invalid: spec.ingress: Invalid value: ingress.protocol http is only valid when ingress.certificates.provider is None","reason":"Invalid","details":{"name":"cluster","group":"config.educates.dev","kind":"EducatesClusterConfig","causes":[{"reason":"FieldValueInvalid","message":"Invalid value: ingress.protocol http is only valid when ingress.certificates.provider is None","field":"spec.ingress"}]},"code":422}`

// rejectingAPIServer serves just enough discovery for the CLI to map
// EducatesClusterConfig, and answers every apply of it with rejection.
func rejectingAPIServer(t *testing.T, rejection string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api":
			_, _ = io.WriteString(w, `{"kind":"APIVersions","versions":["v1"]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1":
			_, _ = io.WriteString(w, `{"kind":"APIResourceList","groupVersion":"v1","resources":[]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/apis":
			_, _ = io.WriteString(w, `{"kind":"APIGroupList","apiVersion":"v1","groups":[{"name":"config.educates.dev","versions":[{"groupVersion":"config.educates.dev/v1alpha1","version":"v1alpha1"}],"preferredVersion":{"groupVersion":"config.educates.dev/v1alpha1","version":"v1alpha1"}}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/apis/config.educates.dev/v1alpha1":
			_, _ = io.WriteString(w, `{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"config.educates.dev/v1alpha1","resources":[{"name":"educatesclusterconfigs","singularName":"educatesclusterconfig","namespaced":false,"kind":"EducatesClusterConfig","verbs":["get","list","watch","create","update","patch","delete"]}]}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/apis/config.educates.dev/v1alpha1/educatesclusterconfigs/cluster":
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = io.WriteString(w, rejection)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// restConfigGetter hands the deployer a fixed REST config; apply and
// wait only ever ask for that.
type restConfigGetter struct{ cfg *rest.Config }

func (g restConfigGetter) ToRESTConfig() (*rest.Config, error) { return g.cfg, nil }
func (g restConfigGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	return nil, errors.New("not used")
}
func (g restConfigGetter) ToRESTMapper() (meta.RESTMapper, error)        { return nil, errors.New("not used") }
func (g restConfigGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig { return nil }

// applyClusterConfig runs the deploy step that applies the
// EducatesClusterConfig against srv and returns its error.
func applyClusterConfig(t *testing.T, srv *httptest.Server) error {
	t.Helper()
	opts := Options{
		Getter:   restConfigGetter{cfg: &rest.Config{Host: srv.URL}},
		Progress: progress.New(io.Discard, 0, false),
		Timeout:  DefaultTimeout,
	}
	applier, err := apply.New(opts.Getter)
	if err != nil {
		t.Fatalf("apply.New: %v", err)
	}
	waiter, err := wait.New(opts.Getter)
	if err != nil {
		t.Fatalf("wait.New: %v", err)
	}
	ecc := map[string]interface{}{
		"apiVersion": "config.educates.dev/v1alpha1",
		"kind":       "EducatesClusterConfig",
		"metadata":   map[string]interface{}{"name": "cluster"},
		"spec": map[string]interface{}{
			"mode": "Managed",
			"ingress": map[string]interface{}{
				"domain":           "educates.test",
				"ingressClassName": "contour",
				"controller":       map[string]interface{}{"provider": "BundledContour"},
				"certificates":     map[string]interface{}{"provider": "None"},
			},
		},
	}
	return installStep(context.Background(), opts, applier, waiter, ecc, "EducatesClusterConfig")
}

func TestDeploy_CertificateProviderChangeIsExplained(t *testing.T) {
	err := applyClusterConfig(t, rejectingAPIServer(t, providerImmutableRejection))
	if err == nil {
		t.Fatal("apply succeeded, want the provider change rejected")
	}
	msg := err.Error()
	for _, want := range []string{
		"certificate provider of an existing Educates install cannot change",
		"'educates admin platform delete'",
		"recreate the cluster",
		// The API server's own reason stays visible.
		"spec.ingress.certificates.provider is immutable",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not contain %q", msg, want)
		}
	}
	if !apierrors.IsInvalid(err) {
		t.Errorf("error no longer wraps the API server's Invalid status: %v", err)
	}
}

func TestDeploy_OtherApplyRejectionsAreNotExplained(t *testing.T) {
	err := applyClusterConfig(t, rejectingAPIServer(t, protocolRejection))
	if err == nil {
		t.Fatal("apply succeeded, want the protocol rejection")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ingress.protocol http is only valid") {
		t.Errorf("error %q lost the API server's reason", msg)
	}
	if strings.Contains(msg, "admin platform delete") {
		t.Errorf("error %q explains a provider change for an unrelated rejection", msg)
	}
}
