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

// Package vendoredcharts embeds the upstream Helm chart tarballs the
// operator drives via the Helm SDK. Tarballs are colocated with this
// Go file (//go:embed cannot reach outside the package directory) and
// kept integrity-pinned via SHA256SUMS in the same directory; refresh
// them with `make vendor-charts` from the operator root.
//
// Each chart has a typed accessor returning a parsed *chart.Chart
// ready to hand to internal/helm.Client. Versions are exported as
// constants so reconcilers can publish them in
// status.bundledChartVersions without re-parsing the chart bytes.
package vendoredcharts

import (
	_ "embed"

	chart "helm.sh/helm/v4/pkg/chart/v2"

	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
)

// CertManagerVersion mirrors the embedded tarball's appVersion and the
// vendored-charts/SHA256SUMS entry; bumped only when the tarball is
// replaced via `make vendor-charts`.
const CertManagerVersion = "v1.21.3"

//go:embed cert-manager-v1.21.3.tgz
var certManagerTarball []byte

// CertManager parses the embedded cert-manager tarball and returns a
// chart ready for the Helm SDK. Each call re-parses the bytes; the
// caller is responsible for caching if the parse cost matters.
func CertManager() (*chart.Chart, error) {
	return helm.LoadArchive(certManagerTarball)
}

// ContourChartVersion is the upstream Helm chart version (semver
// version of the *chart*, distinct from the Contour appVersion).
// Surfaced in status.bundledChartVersions["contour"].
const ContourChartVersion = "0.9.0"

// ContourAppVersion is the Project Contour binary version the
// embedded chart installs. Less load-bearing than ContourChartVersion
// (the chart version is what's pinned in our build), but useful to
// expose in logs/diagnostics so a reader doesn't have to crack the
// tarball to learn which Contour they're running.
const ContourAppVersion = "1.33.8"

//go:embed contour-0.9.0.tgz
var contourTarball []byte

// Contour parses the embedded Project Contour chart tarball and
// returns a chart ready for the Helm SDK. Source:
// https://github.com/projectcontour/helm-charts/releases.
func Contour() (*chart.Chart, error) {
	return helm.LoadArchive(contourTarball)
}

// ExternalDNSChartVersion is the upstream Helm chart version
// (semver of the *chart*, distinct from the external-dns
// appVersion). Surfaced in
// status.bundledChartVersions["external-dns"].
const ExternalDNSChartVersion = "1.22.0"

// ExternalDNSAppVersion is the kubernetes-sigs/external-dns binary
// version the embedded chart installs.
const ExternalDNSAppVersion = "0.22.0"

//go:embed external-dns-1.22.0.tgz
var externalDNSTarball []byte

// ExternalDNS parses the embedded kubernetes-sigs external-dns
// chart tarball and returns a chart ready for the Helm SDK.
// Source: https://github.com/kubernetes-sigs/external-dns
// (helm-chart-1.22.0 release).
func ExternalDNS() (*chart.Chart, error) {
	return helm.LoadArchive(externalDNSTarball)
}

// SecretsManagerChartVersion is the version stamped onto the in-repo
// `secrets-manager` subchart. Distinct from the runtime appVersion the
// subchart deploys (the operator surfaces this in
// status.installedVersion on SecretsManager CRs). Regenerate the
// tarball via `make package-local-charts` after editing the chart.
const SecretsManagerChartVersion = "4.0.0-alpha.1"

//go:embed secrets-manager-4.0.0-alpha.1.tgz
var secretsManagerTarball []byte

// SecretsManager parses the embedded `secrets-manager` subchart
// tarball and returns a chart ready for the Helm SDK. The subchart
// lives at `installer/charts/educates-training-platform/charts/secrets-manager`
// in source form and is repackaged into this directory by
// `make package-local-charts`.
func SecretsManager() (*chart.Chart, error) {
	return helm.LoadArchive(secretsManagerTarball)
}

// LookupServiceChartVersion is the version stamped onto the in-repo
// `lookup-service` subchart. Regenerate the tarball via
// `make package-local-charts` after editing the chart. Surfaced in
// LookupService.status.installedVersion.
const LookupServiceChartVersion = "4.0.0-alpha.1"

//go:embed lookup-service-4.0.0-alpha.1.tgz
var lookupServiceTarball []byte

// LookupService parses the embedded `lookup-service` subchart
// tarball and returns a chart ready for the Helm SDK. The subchart
// lives at `installer/charts/educates-training-platform/charts/lookup-service`
// in source form.
func LookupService() (*chart.Chart, error) {
	return helm.LoadArchive(lookupServiceTarball)
}

// SessionManagerChartVersion is the version stamped onto the in-repo
// `session-manager` subchart. Regenerate the tarball via
// `make package-local-charts` after editing the chart. Surfaced in
// SessionManager.status.installedVersion.
const SessionManagerChartVersion = "4.0.0-alpha.1"

//go:embed session-manager-4.0.0-alpha.1.tgz
var sessionManagerTarball []byte

// SessionManager parses the embedded `session-manager` subchart
// tarball and returns a chart ready for the Helm SDK. The subchart
// lives at `installer/charts/educates-training-platform/charts/session-manager`
// in source form.
func SessionManager() (*chart.Chart, error) {
	return helm.LoadArchive(sessionManagerTarball)
}

// NodeCAInjectorChartVersion is the version stamped onto the in-repo
// `node-ca-injector` subchart. The operator installs this subchart as
// an opt-in extra under SessionManager when the cluster carries a CA
// cert and containerd-level trust is needed.
const NodeCAInjectorChartVersion = "4.0.0-alpha.1"

//go:embed node-ca-injector-4.0.0-alpha.1.tgz
var nodeCAInjectorTarball []byte

// NodeCAInjector parses the embedded `node-ca-injector` subchart
// tarball and returns a chart ready for the Helm SDK.
func NodeCAInjector() (*chart.Chart, error) {
	return helm.LoadArchive(nodeCAInjectorTarball)
}

// RemoteAccessChartVersion is the version stamped onto the in-repo
// `remote-access` subchart. The operator installs this subchart as
// an opt-in extra under SessionManager so external CLIs can reach
// training.educates.dev resources cross-cluster.
const RemoteAccessChartVersion = "4.0.0-alpha.1"

//go:embed remote-access-4.0.0-alpha.1.tgz
var remoteAccessTarball []byte

// RemoteAccess parses the embedded `remote-access` subchart tarball
// and returns a chart ready for the Helm SDK.
func RemoteAccess() (*chart.Chart, error) {
	return helm.LoadArchive(remoteAccessTarball)
}

// KyvernoChartVersion is the upstream Helm chart version
// (semver of the *chart*, distinct from the Kyverno binary
// appVersion). Surfaced in status.bundledChartVersions["kyverno"].
const KyvernoChartVersion = "3.9.2"

// KyvernoAppVersion is the Kyverno binary version the embedded
// chart installs.
const KyvernoAppVersion = "v1.19.2"

//go:embed kyverno-3.9.2.tgz
var kyvernoTarball []byte

// Kyverno parses the embedded Kyverno chart tarball and returns a
// chart ready for the Helm SDK. Source:
// https://kyverno.github.io/kyverno/.
func Kyverno() (*chart.Chart, error) {
	return helm.LoadArchive(kyvernoTarball)
}
