package deployer

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
	helmchart "helm.sh/helm/v4/pkg/chart/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/apply"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/chart"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/crds"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/helm"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

// DiffOptions configures a preview of a deploy, which Diff shows.
type DiffOptions struct {
	// Getter is the kubectl-style RESTClientGetter of the cluster, as for
	// Deploy.
	Getter genericclioptions.RESTClientGetter

	// Out is where the preview is written.
	Out io.Writer

	// Context names the kube context of the cluster, for the first line
	// of the preview.
	Context string

	// Verbose shows the full diff of each CRD and of each object of the
	// operator chart, which are otherwise summarised.
	Verbose bool

	// Notes are lines saying how the deploy decided what it pushes, such
	// as the notices of a local certificate plan, shown first.
	Notes []string

	// SyncLocalSecrets and LocalSecretNames are as for Deploy.
	SyncLocalSecrets bool
	LocalSecretNames []string

	// PendingSecrets are Secrets a deploy writes to the local secrets
	// cache before it syncs them, by name, such as a CA key pair a local
	// certificate plan minted, which the preview takes in place of the
	// cache.
	PendingSecrets map[string]*corev1.Secret
}

// The changes the preview reports for an object.
const (
	changeCreated   = "created"
	changeChanged   = "changed"
	changeUnchanged = "unchanged"
)

// lookupServiceGVK is the kind of the LookupService singleton, which a
// config without the lookup service has no object for.
var lookupServiceGVK = schema.GroupVersionKind{Group: "platform.educates.dev", Version: "v1alpha1", Kind: "LookupService"}

// Diff shows what Deploy would change in the cluster with out and the
// matching options, and changes nothing. It reads the cluster, and has the
// API server and Helm do dry runs of what Deploy applies, in the order
// Deploy applies it: the cached Secrets it syncs, the CRDs, the operator
// release, and the platform resources. The CRDs and the objects of the
// operator release are summarised unless opts.Verbose; the values of the
// operator release and the platform resources are shown as unified diffs
// of the cluster against what the deploy would leave.
func Diff(ctx context.Context, out *translator.Output, opts DiffOptions) error {
	r := &diffReport{w: opts.Out, verbose: opts.Verbose}
	if r.w == nil {
		r.w = io.Discard
	}

	kubeContext := opts.Context
	if kubeContext == "" {
		kubeContext = "(current)"
	}
	r.printf("Comparing with the cluster of kube context %s. Nothing is changed.\n", kubeContext)

	if len(opts.Notes) > 0 || len(opts.PendingSecrets) > 0 {
		r.section("Local certificates")
		for _, note := range opts.Notes {
			r.indented(2, note)
		}
		for _, name := range sortedKeys(opts.PendingSecrets) {
			r.indented(2, fmt.Sprintf("would mint CA key pair %q into the local secrets cache", name))
		}
	}

	if opts.SyncLocalSecrets {
		if err := r.secretsSection(opts); err != nil {
			return err
		}
	}

	chrt, err := chart.Load()
	if err != nil {
		return fmt.Errorf("load embedded chart: %w", err)
	}

	applier, err := apply.New(opts.Getter)
	if err != nil {
		return err
	}

	crdsChange, err := r.crdsSection(ctx, applier, chrt)
	if err != nil {
		return err
	}

	if err := r.operatorSection(ctx, opts, out, chrt); err != nil {
		return err
	}

	if err := r.platformSection(ctx, applier, out, crdsChange); err != nil {
		return err
	}

	r.printf("\nThe operator then reconciles the resources that change. What it changes in the\n" +
		"components and cluster services it installs is not shown.\n")

	return nil
}

// diffReport writes the sections of a deploy preview.
type diffReport struct {
	w       io.Writer
	verbose bool
}

func (r *diffReport) printf(format string, args ...any) {
	fmt.Fprintf(r.w, format, args...)
}

func (r *diffReport) section(title string) {
	r.printf("\n%s\n", title)
}

// indented writes every line of text indented by indent spaces.
func (r *diffReport) indented(indent int, text string) {
	prefix := strings.Repeat(" ", indent)
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		r.printf("%s%s\n", prefix, line)
	}
}

// rows writes each row as its name and change, the changes aligned, each
// followed by its detail, indented below it.
func (r *diffReport) rows(rows []diffRow) {
	width := 0
	for _, row := range rows {
		width = max(width, len(row.name))
	}
	for _, row := range rows {
		r.printf("  %-*s  %s\n", width, row.name, row.change)
		if row.detail != "" {
			r.indented(4, row.detail)
		}
	}
}

// diffRow is one object of a section of the preview.
type diffRow struct {
	name   string
	change string
	detail string
}

// secretsSection previews the sync of the cached Secrets.
func (r *diffReport) secretsSection(opts DiffOptions) error {
	cfg, err := opts.Getter.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("REST config for secrets sync: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("kubernetes client for secrets sync: %w", err)
	}
	namespaceCreated, changes, err := secrets.PreviewSyncLocalCachedSecretsToCluster(cs, opts.LocalSecretNames, opts.PendingSecrets)
	if err != nil {
		return fmt.Errorf("preview local secrets sync: %w", err)
	}

	r.section("Secrets in namespace " + secrets.LocalSecretsSyncNamespace)
	var rows []diffRow
	if namespaceCreated {
		rows = append(rows, diffRow{name: "namespace " + secrets.LocalSecretsSyncNamespace, change: changeCreated})
	}
	for _, change := range changes {
		rows = append(rows, diffRow{name: change.Name, change: change.Change})
	}
	if len(rows) == 0 {
		r.indented(2, "none to sync")
		return nil
	}
	r.rows(rows)
	return nil
}

// crdsSection previews the apply of the CRDs and reports whether any is
// created or changed.
func (r *diffReport) crdsSection(ctx context.Context, applier *apply.Client, chrt *helmchart.Chart) (bool, error) {
	objects, err := crds.Objects(chrt)
	if err != nil {
		return false, err
	}

	r.section("CRDs")
	var rows []diffRow
	unchanged := 0
	for _, u := range objects {
		live, err := applier.Get(ctx, crds.GVK, "", u.GetName())
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return false, fmt.Errorf("get CRD %s: %w", u.GetName(), err)
			}
			live = nil
		}
		next, err := applier.DryRunApply(ctx, u)
		if err != nil {
			return false, fmt.Errorf("dry run of CRD %s: %w", u.GetName(), err)
		}
		before, after, err := comparableTexts(live, next)
		if err != nil {
			return false, err
		}
		change := classify(before, after)
		if change == changeUnchanged {
			unchanged++
			continue
		}
		row := diffRow{name: u.GetName(), change: change}
		if r.verbose {
			row.detail = unifiedDiff(before, after)
		}
		rows = append(rows, row)
	}
	r.rows(rows)
	if unchanged > 0 {
		label := "CRDs"
		if unchanged == 1 {
			label = "CRD"
		}
		r.indented(2, fmt.Sprintf("(%d %s unchanged)", unchanged, label))
	}
	return len(rows) > 0, nil
}

// operatorSection previews the install or upgrade of the operator release.
func (r *diffReport) operatorSection(ctx context.Context, opts DiffOptions, out *translator.Output, chrt *helmchart.Chart) error {
	helmClient, err := helm.New(opts.Getter, OperatorNamespace, io.Discard)
	if err != nil {
		return err
	}
	current, next, err := helmClient.Preview(ctx, OperatorReleaseName, chrt, out.OperatorChartValues)
	if err != nil {
		return err
	}

	r.section(fmt.Sprintf("Operator release %s in namespace %s", OperatorReleaseName, OperatorNamespace))

	nextVersion := next.Chart.Metadata.Version
	currentValues, currentManifest := "", ""
	if current == nil {
		r.indented(2, fmt.Sprintf("installed, chart version %s", nextVersion))
	} else {
		currentVersion := current.Chart.Metadata.Version
		if currentVersion == nextVersion {
			r.indented(2, fmt.Sprintf("chart version %s, unchanged", nextVersion))
		} else {
			r.indented(2, fmt.Sprintf("chart version %s -> %s", currentVersion, nextVersion))
		}
		if currentValues, err = yamlText(current.Config); err != nil {
			return err
		}
		currentManifest = current.Manifest
	}

	nextValues, err := yamlText(next.Config)
	if err != nil {
		return err
	}
	if currentValues == nextValues {
		r.indented(2, "values unchanged")
	} else {
		r.indented(2, "values:")
		r.indented(4, unifiedDiff(currentValues, nextValues))
	}

	before, err := manifestObjects(currentManifest)
	if err != nil {
		return fmt.Errorf("read the manifest of release %s: %w", OperatorReleaseName, err)
	}
	after, err := manifestObjects(next.Manifest)
	if err != nil {
		return fmt.Errorf("render the manifest of release %s: %w", OperatorReleaseName, err)
	}

	var created, changed, removed []string
	for _, key := range sortedKeys(after) {
		switch text, ok := before[key]; {
		case !ok:
			created = append(created, key)
		case text != after[key]:
			changed = append(changed, key)
		}
	}
	for _, key := range sortedKeys(before) {
		if _, ok := after[key]; !ok {
			removed = append(removed, key)
		}
	}

	if len(created)+len(changed)+len(removed) == 0 {
		r.indented(2, "objects unchanged")
		return nil
	}
	for _, group := range []struct {
		label string
		keys  []string
	}{
		{"objects created", created},
		{"objects changed", changed},
		{"objects removed", removed},
	} {
		if len(group.keys) == 0 {
			continue
		}
		r.indented(2, group.label+": "+strings.Join(group.keys, ", "))
		if r.verbose {
			for _, key := range group.keys {
				r.indented(4, key+":")
				r.indented(6, unifiedDiff(before[key], after[key]))
			}
		}
	}
	return nil
}

// platformSection previews the apply of the platform resources, in the
// order Deploy applies them. crdsChange says whether the deploy also
// creates or changes CRDs, against whose current version the resources
// are dry-run.
func (r *diffReport) platformSection(ctx context.Context, applier *apply.Client, out *translator.Output, crdsChange bool) error {
	r.section("Platform resources")

	var rows []diffRow
	crdMissing := false
	refused := false

	for _, resource := range []struct {
		kind string
		obj  map[string]interface{}
	}{
		{"EducatesClusterConfig", out.EducatesClusterConfig},
		{"SecretsManager", out.SecretsManager},
		{"SessionManager", out.SessionManager},
		{"LookupService", out.LookupService},
	} {
		if resource.obj == nil {
			// Deploy applies no LookupService for a config without the
			// lookup service, and deletes none it finds.
			live, err := applier.Get(ctx, lookupServiceGVK, "", "cluster")
			switch {
			case err == nil && live != nil:
				rows = append(rows, diffRow{name: "LookupService cluster", change: "not in the config, so left as it is"})
			case err != nil && !apierrors.IsNotFound(err) && !meta.IsNoMatchError(err):
				return fmt.Errorf("get LookupService: %w", err)
			}
			continue
		}

		u, err := mapToUnstructured(resource.obj)
		if err != nil {
			return fmt.Errorf("%s: %w", resource.kind, err)
		}
		name := resource.kind + " " + u.GetName()

		live, err := applier.Get(ctx, u.GroupVersionKind(), "", u.GetName())
		switch {
		case err == nil:
		case meta.IsNoMatchError(err):
			// The CRD is not installed yet, so the API server cannot dry
			// run the apply: show the resource as the deploy writes it.
			crdMissing = true
			before, after, err := comparableTexts(nil, u)
			if err != nil {
				return err
			}
			rows = append(rows, diffRow{name: name, change: classify(before, after), detail: unifiedDiff(before, after)})
			continue
		case apierrors.IsNotFound(err):
			live = nil
		default:
			return fmt.Errorf("get %s: %w", name, err)
		}

		next, err := applier.DryRunApply(ctx, u)
		if err != nil {
			if apierrors.IsInvalid(err) {
				refused = true
				rows = append(rows, diffRow{name: name, change: "refused", detail: explainApplyRejection(err).Error()})
				continue
			}
			return fmt.Errorf("dry run of %s: %w", name, err)
		}
		before, after, err := comparableTexts(live, next)
		if err != nil {
			return err
		}
		row := diffRow{name: name, change: classify(before, after)}
		if row.change != changeUnchanged {
			row.detail = unifiedDiff(before, after)
		}
		rows = append(rows, row)
	}

	r.rows(rows)

	if refused {
		r.indented(2, "The deploy stops at a resource the API server refuses, after the steps above,\n"+
			"and applies none of the resources after it.")
	}

	switch {
	case crdMissing:
		r.indented(2, "The CRDs are not installed yet, so a resource shows as the deploy writes it,\n"+
			"without the defaults the API server adds.")
	case crdsChange:
		r.indented(2, "The CRDs change too, and the resources were dry-run against the CRDs in the\n"+
			"cluster, so a field only the new CRDs define may not show.")
	}
	return nil
}

// comparableTexts returns live and next as the YAML the preview compares,
// each without the fields the API server sets on every write or reports
// as status, or empty when nil.
func comparableTexts(live, next *unstructured.Unstructured) (string, string, error) {
	before, err := comparableText(live)
	if err != nil {
		return "", "", err
	}
	after, err := comparableText(next)
	if err != nil {
		return "", "", err
	}
	return before, after, nil
}

// serverSetMetadata are the metadata fields the API server sets on every
// write, which differ between a live object and a dry run of an apply
// that leaves it as it is.
var serverSetMetadata = []string{"managedFields", "resourceVersion", "generation", "uid", "creationTimestamp", "selfLink"}

func comparableText(obj *unstructured.Unstructured) (string, error) {
	if obj == nil {
		return "", nil
	}
	c := obj.DeepCopy().Object
	delete(c, "status")
	if md, ok := c["metadata"].(map[string]interface{}); ok {
		for _, field := range serverSetMetadata {
			delete(md, field)
		}
	}
	return yamlText(c)
}

// yamlText returns m as YAML with its keys sorted, or empty when m is.
func yamlText(m map[string]interface{}) (string, error) {
	if len(m) == 0 {
		return "", nil
	}
	data, err := sigsyaml.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// classify says whether an object whose text goes from before to after is
// created, changed or unchanged.
func classify(before, after string) string {
	switch {
	case before == after:
		return changeUnchanged
	case before == "":
		return changeCreated
	default:
		return changeChanged
	}
}

// unifiedDiff returns the unified diff of before, the cluster, against
// after, what the deploy would leave.
func unifiedDiff(before, after string) string {
	return labelledDiff(before, after, "cluster", "deploy")
}

// labelledDiff returns the unified diff of before, labelled from, against
// after, labelled to.
func labelledDiff(before, after, from, to string) string {
	text, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        splitLines(before),
		B:        splitLines(after),
		FromFile: from,
		ToFile:   to,
		Context:  3,
	})
	if err != nil {
		return err.Error()
	}
	return text
}

// splitLines returns the lines of s for a diff, each ending in a newline.
// difflib.SplitLines adds an empty line after a final newline, so s is
// split without it.
func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return difflib.SplitLines(s)
}

// manifestObjects returns the objects of a Helm release manifest as the
// YAML the preview compares, by kind and name, with the namespace when
// the manifest sets one.
func manifestObjects(manifest string) (map[string]string, error) {
	objects := map[string]string{}
	reader := k8syaml.NewYAMLReader(bufio.NewReader(strings.NewReader(manifest)))
	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return objects, nil
		}
		if err != nil {
			return nil, err
		}
		var obj map[string]interface{}
		if err := sigsyaml.Unmarshal(doc, &obj); err != nil {
			return nil, err
		}
		if len(obj) == 0 {
			continue
		}
		u := unstructured.Unstructured{Object: obj}
		name := u.GetName()
		if ns := u.GetNamespace(); ns != "" {
			name = ns + "/" + name
		}
		text, err := yamlText(obj)
		if err != nil {
			return nil, err
		}
		objects[u.GetKind()+" "+name] = text
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
