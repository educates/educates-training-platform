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
	"errors"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/crds"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// external-dns install constants. Mirrors the cert-manager / Contour
// shape: dedicated namespace owned by the EducatesClusterConfig,
// helm release named after the chart, single Deployment name
// (verified against the vendored chart's templates: the chart's
// fullname helper resolves release name → "external-dns" since the
// release name contains the chart name).
const (
	externalDNSNamespace        = "external-dns"
	externalDNSReleaseName      = "external-dns"
	externalDNSControllerDeploy = "external-dns"
)

// externalDNSImagePath is the chart's default image repository,
// registry.k8s.io/external-dns/external-dns, without its registry
// host. The chart has no registry override, only a full repository,
// so the image-registry prefix is joined to this path. Verified
// against chart 1.22.0.
const externalDNSImagePath = "external-dns/external-dns"

// errExternalDNSNotReady is the sentinel ensureExternalDNSReady
// returns while the install is in flight. Same pattern as
// errCertManagerNotReady / errContourNotReady.
var errExternalDNSNotReady = errors.New("external-dns Deployment not yet Available")

// reconcileExternalDNSPhase runs the external-dns install pipeline.
// Order of operations:
//
//  1. Validate provider + credentials (per-provider mutex enforced
//     here with friendlier messages than the CEL on the CRD).
//  2. helm install/upgrade from the vendored chart.
//  3. Wait for the external-dns Deployment to report Available.
//
// external-dns has no admission webhook, so there's no cainjector-
// style bootstrap race. The chart doesn't manage CRDs (no
// CRDWatcher additions needed). When provider != BundledExternalDNS,
// the phase early-returns done=true.
func (r *EducatesClusterConfigReconciler) reconcileExternalDNSPhase(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig) (bool, ctrl.Result, error) {
	log := logf.FromContext(ctx)
	phaseStop := func(res ctrl.Result, err error) (bool, ctrl.Result, error) {
		return false, res, err
	}

	if !shouldInstallExternalDNS(obj) {
		return true, ctrl.Result{}, nil
	}

	if err := r.validateBundledExternalDNS(ctx, obj); err != nil {
		if verr, ok := errors.AsType[*validationError](err); ok {
			r.markValidationFailed(obj, verr)
			return phaseStop(ctrl.Result{}, r.updateStatusWithTransitionLog(ctx, obj))
		}
		return phaseStop(ctrl.Result{}, err)
	}

	res, err := r.reconcileExternalDNS(ctx, obj)
	if err != nil {
		log.Error(err, "external-dns reconcile failed")
		r.markDNSProgressing(obj, "InstallFailed", err.Error())
		_ = r.updateStatusWithTransitionLog(ctx, obj)
		return phaseStop(ctrl.Result{}, err)
	}

	if proceed, result, err := r.handleManagedReleaseResult(ctx, obj, "external-dns", res, r.markDNSProgressing); !proceed {
		return false, result, err
	}

	if err := r.ensureExternalDNSReady(ctx); err != nil {
		if errors.Is(err, errExternalDNSNotReady) {
			r.markDNSProgressing(obj, "WaitingForExternalDNS", "external-dns Deployment not yet Available")
			r.markManagedPhase(obj, configv1alpha1.ClusterConfigPhaseInstalling)
			// Same cache-vs-watch race mitigation as Contour: single
			// Deployment means few status transitions, so we self-poll
			// every 15s instead of trusting watch events alone.
			return false, ctrl.Result{RequeueAfter: 15 * time.Second}, r.updateStatusWithTransitionLog(ctx, obj)
		}
		return phaseStop(ctrl.Result{}, err)
	}

	r.markDNSReadyTrue(obj)
	return true, ctrl.Result{}, nil
}

// shouldInstallExternalDNS reports whether the operator owns the
// external-dns install. Manual/None providers short-circuit the
// phase with done=true so the orchestrator moves on; no install,
// no DNSReady condition published (the absence of the condition is
// the "not applicable" signal).
func shouldInstallExternalDNS(obj *configv1alpha1.EducatesClusterConfig) bool {
	if obj.Spec.DNS == nil {
		return false
	}
	return obj.Spec.DNS.Provider == configv1alpha1.DNSProviderBundledExternalDNS
}

// reconcileExternalDNS ensures the helm release exists, installing
// from the vendored tarball on first sight. Mirrors
// reconcileCertManager + reconcileContour, except that the chart ships
// its DNSEndpoint and DNSRecord CRDs in crds/, which Helm applies only on
// a release's first install, so the operator applies them before every
// converge to carry a chart bump's CRD changes to upgraded clusters, and
// installs the release with Helm's own install of them skipped. They are
// cluster-wide: the apply forces ownership, so the bundled version
// replaces any other external-dns installation's.
func (r *EducatesClusterConfigReconciler) reconcileExternalDNS(ctx context.Context, owner *configv1alpha1.EducatesClusterConfig) (helm.Result, error) {
	chrt, err := vendoredcharts.ExternalDNS()
	if err != nil {
		return helm.Result{}, fmt.Errorf("load embedded external-dns chart: %w", err)
	}

	if err := r.ensureNamespace(ctx, externalDNSNamespace, owner, nil); err != nil {
		return helm.Result{}, err
	}

	hc, err := r.HelmClientFor(externalDNSNamespace)
	if err != nil {
		return helm.Result{}, fmt.Errorf("build helm client for %q: %w", externalDNSNamespace, err)
	}

	if err := crds.ApplyChart(ctx, r.Client, chrt); err != nil {
		return helm.Result{}, fmt.Errorf("apply external-dns CRDs: %w", err)
	}

	res, err := hc.EnsureRelease(ctx, externalDNSReleaseName, chrt, renderExternalDNSValues(owner), helm.SkipCRDs())
	if err != nil {
		return helm.Result{}, err
	}

	if owner.Status.BundledChartVersions == nil {
		owner.Status.BundledChartVersions = map[string]string{}
	}
	owner.Status.BundledChartVersions["external-dns"] = vendoredcharts.ExternalDNSChartVersion
	return res, nil
}

// renderExternalDNSValues builds the values map. Choices follow the
// v3 Carvel patterns (carvel-packages/installer/.../infrastructure/
// {eks,gke}/10-default-settings-for-provider.yaml):
//
//   - provider: aws | google.
//   - sources: from spec, default {service} via CRD kubebuilder
//     default — Educates publishes the wildcard via the Envoy
//     Service annotation set in renderContourValues, so service-
//     source is sufficient. Users override to include "ingress"
//     when they want per-workshop Ingress records too.
//   - domainFilters: [<spec.ingress.domain>]. Scopes external-dns
//     to the cluster's wildcard zone (override surface is a
//     follow-up; v3 had the same hard-coded behavior in the
//     ytt overlays).
//   - txtOwnerId: <spec.ingress.domain>. Lets multiple Educates
//     clusters share a DNS zone without fighting each other's
//     TXT records.
//   - policy: sync. v3's setting for cloud providers; "upsert-only"
//     leaves stale records on resource deletion, which is wrong
//     for our lifecycle.
//     The chart has no default for policy (it is a required value),
//     so it is always set here.
//   - registry: txt (chart default).
//   - serviceAccount.annotations for IRSA / Workload Identity, OR
//     env vars referencing the user-provided Secret for static
//     credentials.
//   - spec.imageRegistry.prefix → image.repository, set to
//     <prefix>/external-dns/external-dns (see externalDNSImagePath).
func renderExternalDNSValues(obj *configv1alpha1.EducatesClusterConfig) map[string]any {
	bedns := obj.Spec.DNS.BundledExternalDNS
	domain := obj.Spec.Ingress.Domain

	// Use []any (not []string) for slice values: helm's
	// values.schema.json validator on this chart rejects `[]string`
	// with "invalid jsonType []string" because the JSON-array shape
	// it expects unmarshals to []interface{}.
	sources := []any{}
	if len(bedns.Sources) > 0 {
		for _, s := range bedns.Sources {
			sources = append(sources, s)
		}
	} else {
		// Defensive default if the CRD schema default didn't apply.
		sources = append(sources, "service")
	}

	values := map[string]any{
		"sources":       sources,
		"policy":        "sync",
		"registry":      "txt",
		"txtOwnerId":    domain,
		"domainFilters": []any{domain},
	}

	switch bedns.Provider {
	case configv1alpha1.DNS01ProviderRoute53:
		applyRoute53Values(values, bedns.Route53)
	case configv1alpha1.DNS01ProviderCloudDNS:
		applyCloudDNSValues(values, bedns.CloudDNS)
	}

	// No replica plumbing: the kubernetes-sigs external-dns chart
	// bounds its replicaCount value to 0 or 1 (external-dns has no
	// leader election, so concurrent instances would race on record
	// writes) and we leave it at the chart default of 1.

	if obj.Spec.ImageRegistry != nil && obj.Spec.ImageRegistry.Prefix != "" {
		values["image"] = map[string]any{
			"repository": obj.Spec.ImageRegistry.Prefix + "/" + externalDNSImagePath,
		}
	}

	return values
}

// applyRoute53Values mutates the values map in place with the AWS
// chart-values shape. Splits IRSA (annotation on ServiceAccount)
// from static-credentials (env vars sourcing from the user's
// Secret). One of the two must be set; the validator enforces it.
func applyRoute53Values(values map[string]any, r53 *configv1alpha1.ExternalDNSRoute53Config) {
	values["provider"] = "aws"
	values["zoneIdFilters"] = []any{r53.HostedZoneID}

	if r53.Region != "" {
		values["aws"] = map[string]any{"region": r53.Region}
	}

	switch {
	case r53.IAMRoleARN != "":
		values["serviceAccount"] = map[string]any{
			"annotations": map[string]any{
				"eks.amazonaws.com/role-arn": r53.IAMRoleARN,
			},
		}
	case r53.CredentialsSecretRef != nil:
		values["env"] = []map[string]any{
			{
				"name": "AWS_ACCESS_KEY_ID",
				"valueFrom": map[string]any{
					"secretKeyRef": map[string]any{
						"name": r53.CredentialsSecretRef.Name,
						"key":  "aws_access_key_id",
					},
				},
			},
			{
				"name": "AWS_SECRET_ACCESS_KEY",
				"valueFrom": map[string]any{
					"secretKeyRef": map[string]any{
						"name": r53.CredentialsSecretRef.Name,
						"key":  "aws_secret_access_key",
					},
				},
			},
		}
	}
}

// applyCloudDNSValues mutates the values map in place with the GCP
// chart-values shape. Workload Identity is preferred on GKE
// (annotation on the ServiceAccount); static-credentials passes
// the service-account JSON via a volume-mount + GOOGLE_APPLICATION_
// CREDENTIALS env var.
func applyCloudDNSValues(values map[string]any, cd *configv1alpha1.ExternalDNSCloudDNSConfig) {
	values["provider"] = "google"
	values["google"] = map[string]any{
		"project": cd.Project,
	}

	switch {
	case cd.WorkloadIdentityServiceAccount != "":
		values["serviceAccount"] = map[string]any{
			"annotations": map[string]any{
				"iam.gke.io/gcp-service-account": cd.WorkloadIdentityServiceAccount,
			},
		}
	case cd.CredentialsSecretRef != nil:
		// Mount the secret-supplied service-account JSON and point
		// GOOGLE_APPLICATION_CREDENTIALS at it. The chart's
		// extraVolumes/extraVolumeMounts give us this without
		// touching the Deployment template directly.
		values["env"] = []map[string]any{
			{
				"name":  "GOOGLE_APPLICATION_CREDENTIALS",
				"value": "/etc/secrets/service-account/credentials.json",
			},
		}
		values["extraVolumes"] = []map[string]any{
			{
				"name": "google-service-account",
				"secret": map[string]any{
					"secretName": cd.CredentialsSecretRef.Name,
				},
			},
		}
		values["extraVolumeMounts"] = []map[string]any{
			{
				"name":      "google-service-account",
				"mountPath": "/etc/secrets/service-account",
				"readOnly":  true,
			},
		}
	}
}

// validateBundledExternalDNS enforces the mutual-exclusivity rules
// CEL on the CRD can't friendly-message (one-of cred mechanisms per
// provider, plus "not yet supported" for Cloudflare/AzureDNS even
// though the field type allows them at the moment).
func (r *EducatesClusterConfigReconciler) validateBundledExternalDNS(_ context.Context, obj *configv1alpha1.EducatesClusterConfig) error {
	bedns := obj.Spec.DNS.BundledExternalDNS
	if bedns == nil {
		return &validationError{
			Field:  "spec.dns.bundledExternalDNS",
			Reason: "required when dns.provider is BundledExternalDNS",
		}
	}

	switch bedns.Provider {
	case configv1alpha1.DNS01ProviderRoute53:
		r53 := bedns.Route53
		if r53 == nil {
			return &validationError{
				Field:  "spec.dns.bundledExternalDNS.route53",
				Reason: "required when provider is Route53",
			}
		}
		if (r53.IAMRoleARN == "") == (r53.CredentialsSecretRef == nil) {
			return &validationError{
				Field:  "spec.dns.bundledExternalDNS.route53",
				Reason: "exactly one of iamRoleARN or credentialsSecretRef must be set",
			}
		}
	case configv1alpha1.DNS01ProviderCloudDNS:
		cd := bedns.CloudDNS
		if cd == nil {
			return &validationError{
				Field:  "spec.dns.bundledExternalDNS.cloudDNS",
				Reason: "required when provider is CloudDNS",
			}
		}
		if (cd.WorkloadIdentityServiceAccount == "") == (cd.CredentialsSecretRef == nil) {
			return &validationError{
				Field:  "spec.dns.bundledExternalDNS.cloudDNS",
				Reason: "exactly one of workloadIdentityServiceAccount or credentialsSecretRef must be set",
			}
		}
	default:
		return &validationError{
			Field:  "spec.dns.bundledExternalDNS.provider",
			Reason: fmt.Sprintf("provider %q is not yet supported in v1alpha1 (only Route53 and CloudDNS)", bedns.Provider),
		}
	}
	return nil
}

// ensureExternalDNSReady gates the rest of the pipeline on the
// external-dns Deployment reporting Available=True. Single
// Deployment so the readiness check is simple — no DaemonSet, no
// webhook, no per-CRD readiness.
func (r *EducatesClusterConfigReconciler) ensureExternalDNSReady(ctx context.Context) error {
	dep := &appsv1.Deployment{}
	key := types.NamespacedName{Namespace: externalDNSNamespace, Name: externalDNSControllerDeploy}
	if err := r.Get(ctx, key, dep); err != nil {
		if apierrors.IsNotFound(err) {
			return errExternalDNSNotReady
		}
		return fmt.Errorf("get Deployment %s: %w", key, err)
	}
	if !deploymentAvailable(dep) {
		return errExternalDNSNotReady
	}
	return nil
}

// cleanupExternalDNS unwinds the install in reverse order: helm
// uninstall → external-dns namespace delete. Idempotent.
//
// The cleanup runs only when the DNS provider is BundledExternalDNS.
// For any other provider the operator installed nothing here, and an
// external-dns release or namespace under the same names belongs to the
// administrator, so it must survive the teardown. The provider is
// immutable, so the spec seen at deletion is the one the install ran
// with.
func (r *EducatesClusterConfigReconciler) cleanupExternalDNS(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig) error {
	if !shouldInstallExternalDNS(obj) {
		return nil
	}
	hc, err := r.HelmClientFor(externalDNSNamespace)
	if err != nil {
		return fmt.Errorf("build helm client for cleanup: %w", err)
	}
	if err := hc.Uninstall(externalDNSReleaseName); err != nil {
		return fmt.Errorf("uninstall external-dns release: %w", err)
	}
	if err := r.deleteIfPresent(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: externalDNSNamespace},
	}); err != nil {
		return fmt.Errorf("delete external-dns namespace: %w", err)
	}
	return nil
}
