// Package wait polls a Kubernetes resource until its Ready=True condition
// flips, or a timeout fires. Used by deploy between CR apply calls — each
// platform CR is gated on the previous one being Ready.
package wait

import (
	"context"
	"errors"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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
	dyn          dynamic.Interface
	mapper       *restmapper.DeferredDiscoveryRESTMapper
	pollInterval time.Duration
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
	return &Client{dyn: dyn, mapper: mapper, pollInterval: PollInterval}, nil
}

// PollInterval is how often the waiter re-checks. Kept short to keep
// the CLI feeling responsive on small clusters; large clusters mostly
// pay the cost in apiserver list/watch traffic which is negligible.
const PollInterval = 2 * time.Second

// WaitReady blocks until the Ready condition of the object an apply
// returned is True for the generation that apply produced, or the
// timeout fires. A Ready=True the operator concluded for an earlier
// generation doesn't count: until the operator reconciles the applied
// spec, that condition describes the previous one.
//
// onPhase, when not nil, fires every time the status.phase of a status
// that describes the applied generation changes (or first becomes set),
// so a phase left from the previous spec is not reported as progress.
// It runs on the poll goroutine; reporter implementations are expected
// to be cheap (write a single status line).
//
// Returns the last-observed object on success — callers use it for the
// summary line (status.url, status.observedDomain, etc.).
func (c *Client) WaitReady(ctx context.Context, applied *unstructured.Unstructured, timeout time.Duration, onPhase func(phase string)) (*unstructured.Unstructured, error) {
	gvk := applied.GroupVersionKind()
	namespace, name, generation := applied.GetNamespace(), applied.GetName(), applied.GetGeneration()
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
		reconciled := err == nil && reconciledGeneration(obj) >= generation
		if onPhase != nil && reconciled {
			if phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase"); phase != "" && phase != lastPhase {
				onPhase(phase)
				lastPhase = phase
			}
		}
		if reconciled && isReady(obj) {
			return obj, nil
		}

		if time.Now().After(deadline) {
			return obj, fmt.Errorf("timeout waiting for %s/%s to be Ready (last status: %s)",
				gvk.Kind, name, readyReason(obj, generation))
		}
		select {
		case <-ctx.Done():
			return obj, ctx.Err()
		case <-time.After(c.pollInterval):
		}
	}
}

// deploymentsResource is the apps/v1 Deployment resource. It needs no
// discovery: every API server the CLI supports serves it.
var deploymentsResource = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

// WaitRolledOut blocks until the Deployment namespace/name has rolled
// out its current spec, as `kubectl rollout status` judges it: the
// deployment controller has observed the spec, every replica runs it,
// no replica of an earlier spec is left, and the updated replicas are
// available. It fails at once when the rollout exceeds its progress
// deadline, and when the timeout fires.
//
// onProgress, when not nil, fires every time the rollout's progress
// message changes, on the poll goroutine.
func (c *Client) WaitRolledOut(ctx context.Context, namespace, name string, timeout time.Duration, onProgress func(progress string)) error {
	deployments := c.dyn.Resource(deploymentsResource).Namespace(namespace)
	deadline := time.Now().Add(timeout)
	var lastProgress string
	for {
		obj, err := deployments.Get(ctx, name, metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("get Deployment %s/%s: %w", namespace, name, err)
		}
		progress := "waiting for the Deployment to exist"
		if err == nil {
			var done bool
			done, progress, err = rolloutStatus(obj)
			if err != nil {
				return fmt.Errorf("Deployment %s/%s: %w", namespace, name, err)
			}
			if done {
				return nil
			}
		}
		if onProgress != nil && progress != lastProgress {
			onProgress(progress)
			lastProgress = progress
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for Deployment %s/%s to roll out (last status: %s)", namespace, name, progress)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.pollInterval):
		}
	}
}

// rolloutStatus reports whether the Deployment obj has rolled out its
// current spec, with a short message saying what the rollout waits on
// when it hasn't. It follows the rules of `kubectl rollout status`.
func rolloutStatus(obj *unstructured.Unstructured) (done bool, progress string, err error) {
	d := &appsv1.Deployment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, d); err != nil {
		return false, "", fmt.Errorf("decode: %w", err)
	}
	if d.Status.ObservedGeneration < d.Generation {
		return false, "waiting for the rollout to start", nil
	}
	for _, cond := range d.Status.Conditions {
		if cond.Type == appsv1.DeploymentProgressing && cond.Reason == "ProgressDeadlineExceeded" {
			return false, "", errors.New("rollout exceeded its progress deadline")
		}
	}
	replicas := int32(1)
	if d.Spec.Replicas != nil {
		replicas = *d.Spec.Replicas
	}
	switch {
	case d.Status.UpdatedReplicas < replicas:
		return false, fmt.Sprintf("rolling out, %d of %d replicas updated", d.Status.UpdatedReplicas, replicas), nil
	case d.Status.Replicas > d.Status.UpdatedReplicas:
		return false, fmt.Sprintf("rolling out, %d old replicas pending termination", d.Status.Replicas-d.Status.UpdatedReplicas), nil
	case d.Status.AvailableReplicas < d.Status.UpdatedReplicas:
		return false, fmt.Sprintf("rolling out, %d of %d updated replicas available", d.Status.AvailableReplicas, d.Status.UpdatedReplicas), nil
	}
	return true, "", nil
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
		case <-time.After(c.pollInterval):
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

// reconciledGeneration returns the generation of the spec obj's status
// describes: the observedGeneration of its Ready condition, or
// status.observedGeneration when the condition doesn't record one.
// Zero means the status doesn't say.
func reconciledGeneration(obj *unstructured.Unstructured) int64 {
	if ready := readyCondition(obj); ready != nil {
		if observed, _, _ := unstructured.NestedInt64(ready, "observedGeneration"); observed > 0 {
			return observed
		}
	}
	observed, _, _ := unstructured.NestedInt64(obj.Object, "status", "observedGeneration")
	return observed
}

// readyCondition returns status.conditions[?(@.type=="Ready")], or nil
// when obj has none.
func readyCondition(obj *unstructured.Unstructured) map[string]interface{} {
	if obj == nil {
		return nil
	}
	conds, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conds {
		if m, ok := c.(map[string]interface{}); ok && m["type"] == "Ready" {
			return m
		}
	}
	return nil
}

// isReady returns true when status.conditions[?(@.type=="Ready")].status == "True".
func isReady(obj *unstructured.Unstructured) bool {
	ready := readyCondition(obj)
	return ready != nil && ready["status"] == "True"
}

// readyReason extracts a short status snippet for the timeout error
// message. Returns "(no status yet)" when the object hasn't reconciled
// at all, and says so first when the status describes an earlier
// generation than the applied one.
func readyReason(obj *unstructured.Unstructured, generation int64) string {
	if obj == nil {
		return "(not found)"
	}
	summary := statusSummary(obj)
	observed := reconciledGeneration(obj)
	switch {
	case observed >= generation || summary == noStatusYet:
		return summary
	case observed == 0:
		return fmt.Sprintf("the status doesn't record which generation it describes, so generation %d is not known to be reconciled (%s)",
			generation, summary)
	default:
		return fmt.Sprintf("the operator has not reconciled generation %d yet; the status describes generation %d (%s)",
			generation, observed, summary)
	}
}

// noStatusYet is the summary of an object the operator hasn't written
// a status for.
const noStatusYet = "(no status yet)"

// statusSummary is the phase of obj's status, or its Ready condition
// when it has no phase.
func statusSummary(obj *unstructured.Unstructured) string {
	if phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase"); phase != "" {
		return "phase=" + phase
	}
	if ready := readyCondition(obj); ready != nil {
		status, _ := ready["status"].(string)
		reason, _ := ready["reason"].(string)
		msg, _ := ready["message"].(string)
		return fmt.Sprintf("Ready=%s reason=%q message=%q", status, reason, msg)
	}
	return noStatusYet
}
