/*
Copyright 2026.

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

package config

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// CRDWatcher gates the registration of controller-runtime watches on
// CRD-defined kinds (e.g., cert-manager.io ClusterIssuer/Certificate)
// behind a discovery probe. Without this, controller-runtime's Kind
// source resolves the GVK via the discovery client at cache-sync
// time; a missing CRD makes the source's internal poll loop retry
// forever (10s intervals), blocking cache sync and preventing the
// controller's workers from ever starting.
//
// Pattern: register a partial set of watches in SetupWithManager
// (only kinds whose CRDs are always present — core kinds + our own
// CRDs); call .Build(r) to obtain the Controller; register this
// runnable, which polls discovery, and on first finding each target
// GVK calls Controller.Watch() to add the deferred source at
// runtime. Verified against controller-runtime v0.23.3:
// Controller.Watch is safe to call post-Start
// (pkg/internal/controller/controller.go:237-250); a source added
// that way is Start()-ed immediately under the controller's mutex.
//
// Lifecycle: the runnable polls until every Target is registered,
// then exits cleanly (Start returns nil). manager.Add'd Runnables
// are not restarted on a nil return.
//
// Activation latency: up to PollInterval between the CRD appearing
// in the cluster and watch events flowing. In Managed mode that
// means a small window after `helm install cert-manager` completes
// during which the reconciler doesn't react to drift on
// ClusterIssuer/Certificate; it does still reconcile via its own
// requeue loop on the EducatesClusterConfig and via the other
// (Deployment/Secret/etc.) watches that fire during cert-manager
// rollout. End-to-end "Ready=True" is not gated on the deferred
// watches activating.
type CRDWatcher struct {
	Manager      ctrl.Manager
	Controller   controller.Controller
	Discovery    discovery.DiscoveryInterface
	Targets      []deferredWatch
	PollInterval time.Duration

	mu         sync.Mutex
	registered map[schema.GroupVersionKind]bool
}

// deferredWatch carries a GVK to probe for + the mapping function to
// use once the watch is registered. The mapper is one of the
// per-kind narrowing funcs from watches.go.
type deferredWatch struct {
	GVK    schema.GroupVersionKind
	Mapper handler.MapFunc
}

// Start polls discovery and registers each Target's watch as soon as
// its CRD becomes available. Returns nil once all Targets are
// registered, or on context cancellation. PollInterval defaults to
// 15s if zero (kept configurable so tests can shorten it).
func (w *CRDWatcher) Start(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("crd-watcher")
	w.mu.Lock()
	if w.registered == nil {
		w.registered = map[schema.GroupVersionKind]bool{}
	}
	if w.PollInterval == 0 {
		w.PollInterval = 15 * time.Second
	}
	w.mu.Unlock()

	if w.tryAll(log) {
		return nil
	}

	t := time.NewTicker(w.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if w.tryAll(log) {
				return nil
			}
		}
	}
}

// tryAll iterates every Target and attempts to register any that
// aren't already. Returns true when every Target is registered.
func (w *CRDWatcher) tryAll(log logr.Logger) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	allDone := true
	for _, dw := range w.Targets {
		if w.registered[dw.GVK] {
			continue
		}
		if !w.gvkAvailable(dw.GVK) {
			allDone = false
			continue
		}
		if err := w.registerWatch(dw); err != nil {
			log.Error(err, "deferred watch registration failed; will retry", "gvk", dw.GVK.String())
			allDone = false
			continue
		}
		log.Info("deferred watch activated", "gvk", dw.GVK.String())
		w.registered[dw.GVK] = true
	}
	return allDone
}

// gvkAvailable reports whether the apiserver currently knows about
// the given GVK. ServerResourcesForGroupVersion is a single discovery
// call. A missing group/version returns an error; a present group
// that doesn't carry this Kind returns an empty match — both map to
// "not yet".
func (w *CRDWatcher) gvkAvailable(gvk schema.GroupVersionKind) bool {
	rl, err := w.Discovery.ServerResourcesForGroupVersion(gvk.GroupVersion().String())
	if err != nil || rl == nil {
		return false
	}
	for _, r := range rl.APIResources {
		if r.Kind == gvk.Kind {
			return true
		}
	}
	return false
}

// registerWatch wraps the GVK in an unstructured object, builds a
// Kind source against the manager's cache, and adds it to the
// controller. Because we've already verified availability via
// discovery, the Source's internal Start path succeeds instead of
// entering its silent retry loop — that's the entire point of
// gating Watch() on discovery.
//
// The obj is typed as client.Object (not *unstructured.Unstructured)
// so source.Kind's generic type parameter infers `object =
// client.Object`, matching the type of handler.EnqueueRequestsFromMapFunc
// (which is TypedEventHandler[client.Object, reconcile.Request]).
// Without that explicit typing, type inference would propose
// `object = *unstructured.Unstructured` and the handler type
// wouldn't match.
//
// Side effect: invalidate the manager's RESTMapper. The operator's
// typed-client SSA/Get calls go through that mapper, whose
// discovery cache was built at pod start (when the CRDs were
// absent). Without a Reset() here, the mapper would happily return
// NoMatchError on every cert-manager.io call until pod restart —
// even though the CRDs are now sitting in the cluster. Resetting
// the deferred discovery mapper forces the next lookup to
// re-discover from the apiserver, picking up the newly-installed
// kinds.
func (w *CRDWatcher) registerWatch(dw deferredWatch) error {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(dw.GVK)
	var obj client.Object = u
	src := source.Kind(
		w.Manager.GetCache(),
		obj,
		handler.EnqueueRequestsFromMapFunc(dw.Mapper),
	)
	if err := w.Controller.Watch(src); err != nil {
		return fmt.Errorf("Controller.Watch(%s): %w", dw.GVK, err)
	}
	if resetter, ok := w.Manager.GetRESTMapper().(interface{ Reset() }); ok {
		resetter.Reset()
	}
	return nil
}
