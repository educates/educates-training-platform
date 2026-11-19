package deployer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/apply"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/helm"
)

// DriftOptions configures a report of the drift of a cluster from a
// revision of its deploy history, which Drift writes.
type DriftOptions struct {
	// Getter is the kubectl-style RESTClientGetter of the cluster.
	Getter genericclioptions.RESTClientGetter

	// Out is where the report is written.
	Out io.Writer

	// Context names the kube context of the cluster, for the report.
	Context string
}

// Drift writes how the cluster has drifted from what the revision rev of
// its deploy history applied, and changes nothing.
//
// For the operator release it compares the chart version and the values
// the revision recorded with the release's. For each platform resource
// it diffs a server-side dry run of the revision's apply, which merges
// the revision's spec into the live resource as a redeploy of it would,
// against the live resource, so that the defaults the API server adds
// show on neither side. A field another tool, such as kubectl edit, added
// survives a redeploy and so shows in no diff: the report lists the spec
// fields each field manager other than the CLI's owns. A resource the
// revision applied that the cluster no longer has, or one it has that the
// revision did not apply, is reported as such.
func Drift(ctx context.Context, rev Revision, opts DriftOptions) error {
	r := &diffReport{w: opts.Out}
	if r.w == nil {
		r.w = io.Discard
	}

	var cfg v1alpha1.EducatesConfig
	if err := yaml.Unmarshal([]byte(rev.Applied), &cfg); err != nil {
		return fmt.Errorf("read what revision %d applied: %w", rev.Number, err)
	}
	recorded := translator.TranslateEscape(&cfg)

	revisionLabel := fmt.Sprintf("revision %d", rev.Number)
	r.printf("Comparing revision %d of the deploy history, which %s Educates on %s,\n"+
		"with the cluster of kube context %s. Nothing is changed.\n",
		rev.Number, rev.Action, rev.Deployed.UTC().Format("2006-01-02 15:04:05 MST"), opts.Context)

	if err := r.operatorDriftSection(opts, rev, recorded, revisionLabel); err != nil {
		return err
	}

	applier, err := apply.New(opts.Getter)
	if err != nil {
		return err
	}
	return r.platformDriftSection(ctx, applier, recorded, revisionLabel)
}

// operatorDriftSection compares the operator release with what the
// revision recorded for it.
func (r *diffReport) operatorDriftSection(opts DriftOptions, rev Revision, recorded *translator.Output, revisionLabel string) error {
	helmClient, err := helm.New(opts.Getter, OperatorNamespace, io.Discard)
	if err != nil {
		return err
	}

	r.section(fmt.Sprintf("Operator release %s in namespace %s", OperatorReleaseName, OperatorNamespace))

	rel, err := helmClient.Release(OperatorReleaseName)
	if errors.Is(err, helm.ErrReleaseNotFound) {
		r.indented(2, "deleted from the cluster")
		return nil
	}
	if err != nil {
		return err
	}

	current := ""
	if rel.Chart != nil && rel.Chart.Metadata != nil {
		current = rel.Chart.Metadata.Version
	}
	switch {
	case rev.ChartVersion == "":
		r.indented(2, fmt.Sprintf("chart version %s; the revision did not record its own", current))
	case rev.ChartVersion == current:
		r.indented(2, fmt.Sprintf("chart version %s, unchanged", current))
	default:
		r.indented(2, fmt.Sprintf("chart version %s at the revision, %s in the cluster", rev.ChartVersion, current))
	}

	before, err := yamlText(recorded.OperatorChartValues)
	if err != nil {
		return err
	}
	after, err := yamlText(rel.Config)
	if err != nil {
		return err
	}
	if before == after {
		r.indented(2, "values unchanged")
	} else {
		r.indented(2, "values:")
		r.indented(4, labelledDiff(before, after, revisionLabel, "cluster"))
	}
	return nil
}

// platformDriftSection compares each platform resource with what the
// revision applied of it.
func (r *diffReport) platformDriftSection(ctx context.Context, applier *apply.Client, recorded *translator.Output, revisionLabel string) error {
	r.section("Platform resources")

	var rows []diffRow
	for _, resource := range []struct {
		kind string
		obj  map[string]interface{}
	}{
		{"EducatesClusterConfig", recorded.EducatesClusterConfig},
		{"SecretsManager", recorded.SecretsManager},
		{"SessionManager", recorded.SessionManager},
		{"LookupService", recorded.LookupService},
	} {
		row, err := resourceDrift(ctx, applier, resource.kind, resource.obj, revisionLabel)
		if err != nil {
			return err
		}
		if row != nil {
			rows = append(rows, *row)
		}
	}
	r.rows(rows)
	return nil
}

// resourceDrift returns the row of the platform resource kind, which the
// revision applied as obj, or nil when neither the revision nor the
// cluster has it.
func resourceDrift(ctx context.Context, applier *apply.Client, kind string, obj map[string]interface{}, revisionLabel string) (*diffRow, error) {
	name := kind + " cluster"

	if obj == nil {
		// Only the LookupService is optional.
		live, err := applier.Get(ctx, lookupServiceGVK, "", "cluster")
		switch {
		case err == nil && live != nil:
			return &diffRow{name: name, change: "added after the revision"}, nil
		case err != nil && !apierrors.IsNotFound(err) && !meta.IsNoMatchError(err):
			return nil, fmt.Errorf("get %s: %w", name, err)
		}
		return nil, nil
	}

	u, err := mapToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", kind, err)
	}

	live, err := applier.Get(ctx, u.GroupVersionKind(), "", u.GetName())
	switch {
	case err == nil:
	case apierrors.IsNotFound(err) || meta.IsNoMatchError(err):
		return &diffRow{name: name, change: "deleted from the cluster"}, nil
	default:
		return nil, fmt.Errorf("get %s: %w", name, err)
	}

	redeployed, err := applier.DryRunApply(ctx, u)
	if err != nil {
		if apierrors.IsInvalid(err) {
			return &diffRow{name: name, change: "refused", detail: "the API server refuses what the revision applied, against the CRDs in the cluster now: " + err.Error()}, nil
		}
		return nil, fmt.Errorf("dry run of %s: %w", name, err)
	}

	before, after, err := comparableTexts(redeployed, live)
	if err != nil {
		return nil, err
	}
	others := otherManagedSpecFields(live)

	row := &diffRow{name: name, change: changeUnchanged}
	var detail []string
	if before != after {
		row.change = changeChanged
		detail = append(detail, labelledDiff(before, after, revisionLabel, "cluster"))
	}
	if len(others) > 0 {
		if row.change == changeUnchanged {
			row.change = "changed by another tool"
		}
		detail = append(detail, "spec fields another field manager than the CLI owns:")
		for _, line := range others {
			detail = append(detail, "  "+line)
		}
	}
	row.detail = strings.Join(detail, "\n")
	return row, nil
}

// otherManagedSpecFields returns, a line each, the spec fields of live
// that a field manager other than the CLI's owns, with the manager, how
// it wrote them and when. The operator writes only status and finalizers,
// and so owns none.
func otherManagedSpecFields(live *unstructured.Unstructured) []string {
	var lines []string
	for _, entry := range live.GetManagedFields() {
		if entry.Manager == apply.FieldManager || entry.Subresource != "" || entry.FieldsV1 == nil {
			continue
		}
		var fields map[string]interface{}
		if err := yaml.Unmarshal(entry.FieldsV1.Raw, &fields); err != nil {
			continue
		}
		spec, ok := toStringMap(fields["f:spec"])
		if !ok {
			continue
		}
		when := ""
		if entry.Time != nil {
			when = " at " + entry.Time.UTC().Format("2006-01-02 15:04:05 MST")
		}
		for _, path := range fieldPaths(spec, "spec") {
			lines = append(lines, fmt.Sprintf("%s, by %s (%s%s)", path, entry.Manager, entry.Operation, when))
		}
	}
	sort.Strings(lines)
	return lines
}

// fieldPaths returns the paths of the fields a managed fields set owns,
// below prefix: f:<name> is a field, k:<key> and v:<value> an item of a
// list, and "." the field itself.
func fieldPaths(fields map[string]interface{}, prefix string) []string {
	var paths []string
	for key, child := range fields {
		var path string
		switch {
		case key == ".":
			continue
		case strings.HasPrefix(key, "f:"):
			path = prefix + "." + key[2:]
		case strings.HasPrefix(key, "k:"), strings.HasPrefix(key, "v:"), strings.HasPrefix(key, "i:"):
			path = prefix + "[" + key[2:] + "]"
		default:
			path = prefix + "." + key
		}
		m, _ := toStringMap(child)
		if len(m) == 0 || (len(m) == 1 && m["."] != nil) {
			paths = append(paths, path)
			continue
		}
		paths = append(paths, fieldPaths(m, path)...)
	}
	sort.Strings(paths)
	return paths
}

// toStringMap returns v as a map with string keys, as yaml.v2 decodes
// JSON objects into maps with interface{} keys.
func toStringMap(v interface{}) (map[string]interface{}, bool) {
	switch m := v.(type) {
	case map[string]interface{}:
		return m, true
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(m))
		for k, val := range m {
			out[fmt.Sprint(k)] = val
		}
		return out, true
	}
	return nil, false
}
