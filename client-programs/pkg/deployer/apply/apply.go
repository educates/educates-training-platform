// Package apply server-side-applies arbitrary unstructured Kubernetes
// objects from the CLI. Used by deploy to push the four platform CRs
// after the operator chart is installed.
//
// Server-side apply is preferred over kubectl-style client-side apply:
// it converges multiple CLI runs cleanly, surfaces conflict errors with
// the field-owning manager, and matches how the operator itself writes
// back to .status (so co-ownership of .spec stays clean).
package apply

import (
	"context"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
)

// FieldManager is the SSA owner the CLI claims for fields it sets.
const FieldManager = "educates-cli"

// Client wraps dynamic.Interface + RESTMapper for server-side apply.
// One Client per `educates admin platform deploy` run.
type Client struct {
	dyn    dynamic.Interface
	cache  discovery.CachedDiscoveryInterface
	mapper *restmapper.DeferredDiscoveryRESTMapper
}

// New builds a Client from a kubectl-style RESTClientGetter.
func New(getter genericclioptions.RESTClientGetter) (*Client, error) {
	cfg, err := getter.ToRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("REST config: %w", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("discovery client: %w", err)
	}
	// memory.NewMemCacheClient is the standard wrapper for the REST
	// mapper. A fresh cache per deploy run avoids the trap where CRDs
	// installed earlier in this run aren't seen by later apply calls.
	cache := memory.NewMemCacheClient(dc)
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(cache)
	return &Client{dyn: dyn, cache: cache, mapper: mapper}, nil
}

// InvalidateDiscovery clears the cached discovery snapshot and resets the
// deferred mapper. Call this after applying CRDs so the next RESTMapping
// lookup re-fetches `/apis` and picks up the newly registered kinds.
// Without it, the mapper's reset-on-NoMatchError retry path rebuilds from
// the same stale cache and the new GVK stays invisible.
func (c *Client) InvalidateDiscovery() {
	c.cache.Invalidate()
	c.mapper.Reset()
}

// Apply server-side-applies one Unstructured. force=true so re-runs
// after the operator stamps defaults into .spec don't deadlock on field
// conflicts the operator caused.
func (c *Client) Apply(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	return c.apply(ctx, obj, false)
}

// DryRunApply is Apply as a server-side dry run: the API server validates
// and defaults obj, and merges it into the live object as Apply would,
// and returns the object Apply would leave, persisting nothing.
func (c *Client) DryRunApply(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	return c.apply(ctx, obj, true)
}

func (c *Client) apply(ctx context.Context, obj *unstructured.Unstructured, dryRun bool) (*unstructured.Unstructured, error) {
	gvk := obj.GroupVersionKind()
	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("REST mapping for %s: %w", gvk, err)
	}
	data, err := json.Marshal(obj.Object)
	if err != nil {
		return nil, fmt.Errorf("marshal %s/%s: %w", gvk.Kind, obj.GetName(), err)
	}

	resource := c.dyn.Resource(mapping.Resource)
	var typed dynamic.ResourceInterface = resource
	if ns := obj.GetNamespace(); ns != "" {
		typed = resource.Namespace(ns)
	}

	force := true
	options := metav1.PatchOptions{
		FieldManager: FieldManager,
		Force:        &force,
	}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	applied, err := typed.Patch(ctx, obj.GetName(), types.ApplyPatchType, data, options)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("apply %s/%s: target not found (CRD not installed?): %w", gvk.Kind, obj.GetName(), err)
		}
		return nil, fmt.Errorf("apply %s/%s: %w", gvk.Kind, obj.GetName(), err)
	}
	return applied, nil
}

// Get returns the live object for a GVK + name, or NotFound. Used by
// callers that need to poll a status field (e.g. CRD Established) without
// also pulling in the wait package.
func (c *Client) Get(ctx context.Context, gvk schema.GroupVersionKind, namespace, name string) (*unstructured.Unstructured, error) {
	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("REST mapping for %s: %w", gvk, err)
	}
	resource := c.dyn.Resource(mapping.Resource)
	var typed dynamic.ResourceInterface = resource
	if namespace != "" {
		typed = resource.Namespace(namespace)
	}
	return typed.Get(ctx, name, metav1.GetOptions{})
}

// Delete removes one object by GVK + name. Idempotent: missing → nil.
func (c *Client) Delete(ctx context.Context, gvk schema.GroupVersionKind, namespace, name string) error {
	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("REST mapping for %s: %w", gvk, err)
	}
	resource := c.dyn.Resource(mapping.Resource)
	var typed dynamic.ResourceInterface = resource
	if namespace != "" {
		typed = resource.Namespace(namespace)
	}
	if err := typed.Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete %s/%s: %w", gvk.Kind, name, err)
	}
	return nil
}
