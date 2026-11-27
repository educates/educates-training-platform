package deployer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/apply"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/helm"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/wait"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

// DeleteOptions configures a delete run. Mirrors Options but the inputs
// differ: we don't need translator output here since the resources are
// always the same four CRs at metadata.name=cluster + the helm release.
type DeleteOptions struct {
	Getter   genericclioptions.RESTClientGetter
	Out      io.Writer
	HelmLog  io.Writer
	Timeout  time.Duration
	Progress progress.Reporter

	// Purge, when true, extends the delete pipeline AFTER helm
	// uninstall to remove the four CRDs, the operator namespace with the
	// Secrets synced into it, and the namespace earlier releases synced
	// the local secrets cache into, when it is left. Local
	// <data-home>/config.yaml and the local secrets cache are
	// intentionally preserved: they're user authoring inputs and survive
	// cluster reinstalls, and the next deploy syncs the cache again.
	Purge bool
}

// PurgeTargets is the inventory --purge will delete in addition to the
// standard delete pipeline. Exported so the cmd-layer confirmation
// prompt can render it for the user before they accept.
type PurgeTargets struct {
	CRDs       []string
	Namespaces []string
}

// PurgePlan returns the list of cluster resources Purge would remove
// after helm uninstall. Stable order so the confirmation prompt is
// reproducible.
func PurgePlan() PurgeTargets {
	return PurgeTargets{
		CRDs: []string{
			"educatesclusterconfigs.config.educates.dev",
			"secretsmanagers.platform.educates.dev",
			"lookupservices.platform.educates.dev",
			"sessionmanagers.platform.educates.dev",
		},
		Namespaces: []string{
			OperatorNamespace, // educates-installer
			// Where earlier releases synced the local secrets cache.
			secrets.RetiredSyncNamespace,
		},
	}
}

// Delete executes the uninstall pipeline:
//
//  1. Delete SessionManager → wait gone.
//  2. Delete LookupService → wait gone.
//  3. Delete SecretsManager → wait gone.
//  4. Delete EducatesClusterConfig → wait gone (its finalizer drains
//     kyverno → external-dns → contour → cert-manager → CustomCA
//     copy in reverse install order, each only when the
//     EducatesClusterConfig has Educates install it).
//  5. helm uninstall the operator chart.
//
// Idempotent: a CR that's already gone is a no-op step. Useful for
// re-running after a half-finished delete or for cleaning up state the
// last deploy never created.
//
// Deliberately NOT deleted (user state across reinstalls):
//   - CRDs (helm never owns them; would cascade-delete any CR in the
//     cluster, including ones from other Educates installs).
//   - The operator namespace itself.
//   - The Secrets synced into the operator namespace from the local
//     secrets cache, such as the CA key pair (the next deploy syncs the
//     same CA the user already trusts in their browser).
func Delete(ctx context.Context, opts DeleteOptions) error {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.HelmLog == nil {
		opts.HelmLog = io.Discard
	}
	if opts.Timeout == 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.Progress == nil {
		opts.Progress = progress.New(io.Discard, 0, false)
	}

	applier, err := apply.New(opts.Getter)
	if err != nil {
		return err
	}
	waiter, err := wait.New(opts.Getter)
	if err != nil {
		return err
	}

	for _, st := range deleteOrder() {
		if err := deleteCRStep(ctx, opts, applier, waiter, st.gvk, st.name, st.label); err != nil {
			return err
		}
	}

	// helm uninstall the operator chart.
	step := opts.Progress.Start(fmt.Sprintf("helm uninstall %s", OperatorReleaseName))
	helmClient, err := helm.New(opts.Getter, OperatorNamespace, opts.HelmLog)
	if err != nil {
		step.Fail(err)
		return err
	}
	if err := helmClient.Uninstall(OperatorReleaseName); err != nil {
		// Uninstall already swallows "release not found"; surface other
		// errors as-is.
		step.Fail(err)
		return err
	}
	step.Done("uninstalled")

	if opts.Purge {
		if err := purge(ctx, opts, applier); err != nil {
			return err
		}
	}
	opts.Progress.Note("delete complete")
	return nil
}

// purge removes the four platform CRDs and the operator + secrets
// namespaces. Idempotent: NotFound at any step closes that step with
// "already gone" rather than erroring.
//
// Order matters: CRDs first (their deletion cascade-removes any
// lingering CR instances cluster-wide; we've already deleted ours
// from the operator-owned namespace, but other teams may have ECC
// resources we don't want to leave dangling against deleted CRDs).
// Namespaces last (they cascade Pod/Secret/ConfigMap cleanup; finalizer
// drain on the operator namespace is what waits the longest).
func purge(ctx context.Context, opts DeleteOptions, applier *apply.Client) error {
	plan := PurgePlan()
	crdGVK := schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1",
		Kind:    "CustomResourceDefinition",
	}
	for _, name := range plan.CRDs {
		step := opts.Progress.Start(fmt.Sprintf("purge CRD %s", name))
		if err := applier.Delete(ctx, crdGVK, "", name); err != nil {
			var statusErr *apierrors.StatusError
			if errors.As(err, &statusErr) && apierrors.IsNotFound(err) {
				step.Done("already gone")
				continue
			}
			step.Fail(err)
			return err
		}
		step.Done("removed")
	}
	nsGVK := schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}
	for _, name := range plan.Namespaces {
		step := opts.Progress.Start(fmt.Sprintf("purge namespace %s", name))
		if err := applier.Delete(ctx, nsGVK, "", name); err != nil {
			var statusErr *apierrors.StatusError
			if errors.As(err, &statusErr) && apierrors.IsNotFound(err) {
				step.Done("already gone")
				continue
			}
			step.Fail(err)
			return err
		}
		step.Done("deletion initiated")
	}
	return nil
}

// deleteCRStep removes one CR by GVK + name and waits for it to be
// 404, reporting both halves through a single progress step. NotFound
// at the delete call → step closes with "already gone" + return nil
// (idempotent re-run).
func deleteCRStep(ctx context.Context, opts DeleteOptions, applier *apply.Client, waiter *wait.Client, gvk schema.GroupVersionKind, name, label string) error {
	step := opts.Progress.Start(fmt.Sprintf("delete %s/%s", label, name))
	if err := applier.Delete(ctx, gvk, "", name); err != nil {
		var statusErr *apierrors.StatusError
		if errors.As(err, &statusErr) && apierrors.IsNotFound(err) {
			step.Done("already gone")
			return nil
		}
		step.Fail(err)
		return err
	}
	step.Update("waiting for finalizer drain")
	if err := waiter.WaitGone(ctx, gvk, "", name, opts.Timeout); err != nil {
		step.Fail(err)
		return err
	}
	step.Done("gone")
	return nil
}

type deleteStep struct {
	gvk   schema.GroupVersionKind
	name  string
	label string
}

// deleteOrder is the inverse of the deploy install order. SessionManager
// first so its remote-access subchart goes away while LookupService is
// still present; LookupService next (its subchart's cleanup runs); then
// SecretsManager; then ECC last (its finalizer drains cluster services).
func deleteOrder() []deleteStep {
	return []deleteStep{
		{
			gvk:   schema.GroupVersionKind{Group: "platform.educates.dev", Version: "v1alpha1", Kind: "SessionManager"},
			name:  "cluster",
			label: "SessionManager",
		},
		{
			gvk:   schema.GroupVersionKind{Group: "platform.educates.dev", Version: "v1alpha1", Kind: "LookupService"},
			name:  "cluster",
			label: "LookupService",
		},
		{
			gvk:   schema.GroupVersionKind{Group: "platform.educates.dev", Version: "v1alpha1", Kind: "SecretsManager"},
			name:  "cluster",
			label: "SecretsManager",
		},
		{
			gvk:   schema.GroupVersionKind{Group: "config.educates.dev", Version: "v1alpha1", Kind: "EducatesClusterConfig"},
			name:  "cluster",
			label: "EducatesClusterConfig",
		},
	}
}
