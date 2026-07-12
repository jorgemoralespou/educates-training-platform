// Package wait polls a Kubernetes resource until its Ready=True condition
// flips, or a timeout fires. Used by deploy between CR apply calls — each
// platform CR is gated on the previous one being Ready.
package wait

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
)

// Client polls a resource via dynamic+REST mapping. One Client per
// deploy run.
type Client struct {
	dyn    dynamic.Interface
	mapper *restmapper.DeferredDiscoveryRESTMapper
}

func New(getter genericclioptions.RESTClientGetter) (*Client, error) {
	cfg, err := getter.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))
	return &Client{dyn: dyn, mapper: mapper}, nil
}

// PollInterval is how often the waiter re-checks. Kept short to keep
// the CLI feeling responsive on small clusters; large clusters mostly
// pay the cost in apiserver list/watch traffic which is negligible.
const PollInterval = 2 * time.Second

// WaitReady blocks until the object's status.conditions[?(@.type=="Ready")].status
// is "True", or ctx times out. namespace="" for cluster-scoped resources.
//
// Returns the last-observed object on success — callers use it for the
// summary line (status.url, status.observedDomain, etc.).
func (c *Client) WaitReady(ctx context.Context, gvk schema.GroupVersionKind, namespace, name string, timeout time.Duration) (*unstructured.Unstructured, error) {
	return c.WaitReadyWithPhase(ctx, gvk, namespace, name, timeout, nil)
}

// WaitReadyWithPhase is WaitReady with a callback that fires every
// time the observed phase changes (or first becomes set). The callback
// runs on the poll goroutine; reporter implementations are expected to
// be cheap (write a single status line). A nil callback is equivalent
// to WaitReady.
func (c *Client) WaitReadyWithPhase(ctx context.Context, gvk schema.GroupVersionKind, namespace, name string, timeout time.Duration, onPhase func(phase string)) (*unstructured.Unstructured, error) {
	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("REST mapping for %s: %w", gvk, err)
	}
	resource := c.dyn.Resource(mapping.Resource)
	var typed dynamic.ResourceInterface = resource
	if namespace != "" {
		typed = resource.Namespace(namespace)
	}

	deadline := time.Now().Add(timeout)
	var lastPhase string
	for {
		obj, err := typed.Get(ctx, name, metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("get %s/%s: %w", gvk.Kind, name, err)
		}
		if onPhase != nil && obj != nil {
			if phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase"); phase != "" && phase != lastPhase {
				onPhase(phase)
				lastPhase = phase
			}
		}
		if err == nil && isReady(obj) {
			return obj, nil
		}

		if time.Now().After(deadline) {
			return obj, fmt.Errorf("timeout waiting for %s/%s to be Ready (last status: %s)",
				gvk.Kind, name, readyReason(obj))
		}
		select {
		case <-ctx.Done():
			return obj, ctx.Err()
		case <-time.After(PollInterval):
		}
	}
}

// WaitGone polls until the resource is 404 or ctx times out. Used by the
// delete command to gate on a finalizer-driven drain completing before
// moving to the next CR in reverse-install order.
//
// "Already gone" at first poll is success: idempotent re-runs of delete
// (or deleting state the deploy never created) shouldn't fail.
func (c *Client) WaitGone(ctx context.Context, gvk schema.GroupVersionKind, namespace, name string, timeout time.Duration) error {
	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("REST mapping for %s: %w", gvk, err)
	}
	resource := c.dyn.Resource(mapping.Resource)
	var typed dynamic.ResourceInterface = resource
	if namespace != "" {
		typed = resource.Namespace(namespace)
	}

	deadline := time.Now().Add(timeout)
	for {
		obj, err := typed.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("get %s/%s: %w", gvk.Kind, name, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for %s/%s to be deleted (last phase: %s, finalizers: %v)",
				gvk.Kind, name, lastPhase(obj), obj.GetFinalizers())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(PollInterval):
		}
	}
}

// lastPhase pulls .status.phase from an object for the timeout error
// message. Returns "(none)" when unset.
func lastPhase(obj *unstructured.Unstructured) string {
	if p, _, _ := unstructured.NestedString(obj.Object, "status", "phase"); p != "" {
		return p
	}
	return "(none)"
}

// isReady returns true when status.conditions[?(@.type=="Ready")].status == "True".
func isReady(obj *unstructured.Unstructured) bool {
	if obj == nil {
		return false
	}
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

// readyReason extracts a short status snippet for the timeout error
// message. Returns "(no status yet)" when the object hasn't reconciled
// at all.
func readyReason(obj *unstructured.Unstructured) string {
	if obj == nil {
		return "(not found)"
	}
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	if phase != "" {
		return "phase=" + phase
	}
	conds, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conds {
		m, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if m["type"] == "Ready" {
			status, _ := m["status"].(string)
			reason, _ := m["reason"].(string)
			msg, _ := m["message"].(string)
			return fmt.Sprintf("Ready=%s reason=%q message=%q", status, reason, msg)
		}
	}
	return "(no status yet)"
}
