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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/apply"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/helm"
)

// ErrNoPlatform is the error of an export from a cluster Educates is not
// deployed to.
var ErrNoPlatform = errors.New("no Educates platform is deployed to the cluster: it has no EducatesClusterConfig")

// The platform resources, each a singleton named "cluster".
var (
	educatesClusterConfigGVK = schema.GroupVersionKind{Group: "config.educates.dev", Version: "v1alpha1", Kind: "EducatesClusterConfig"}
	secretsManagerGVK        = schema.GroupVersionKind{Group: "platform.educates.dev", Version: "v1alpha1", Kind: "SecretsManager"}
	sessionManagerGVK        = schema.GroupVersionKind{Group: "platform.educates.dev", Version: "v1alpha1", Kind: "SessionManager"}
)

// Export is a configuration of Educates as an EducatesConfig, the CLI
// config kind that deploys the platform resources' specs as they are.
type Export struct {
	// Config is the configuration.
	Config *v1alpha1.EducatesConfig

	// ChartVersion is the version of the operator chart, empty when it is
	// not known.
	ChartVersion string

	// SecretRefs are the Secrets the platform resources reference, which
	// an export holds the names of and never the contents.
	SecretRefs []SecretRef

	// UnmappedValues are the operator chart values, by dotted path, that
	// the operator block of the config cannot express, so that deploying
	// the config would leave them unset.
	UnmappedValues []string

	// NoOperatorRelease reports that the cluster has no operator release,
	// so the config has no operator block.
	NoOperatorRelease bool
}

// SecretRef is a Secret a platform resource references.
type SecretRef struct {
	// Namespace and Name name the Secret; Namespace is empty when the
	// reference names none.
	Namespace string
	Name      string

	// Field is the field that references it, as
	// educatesClusterConfig.ingress.certificates.bundledCertManager.customCA.caKeyPairSecretRef.
	Field string
}

// ExportLive returns the configuration deployed to the cluster getter
// reaches: the spec of each platform resource as the cluster holds it,
// including what the API server defaulted and what another tool set, and
// the values of the operator release mapped to the operator block. It
// returns ErrNoPlatform when the cluster has no EducatesClusterConfig.
func ExportLive(ctx context.Context, getter genericclioptions.RESTClientGetter) (*Export, error) {
	applier, err := apply.New(getter)
	if err != nil {
		return nil, err
	}

	spec := func(gvk schema.GroupVersionKind) (map[string]interface{}, error) {
		obj, err := applier.Get(ctx, gvk, "", "cluster")
		switch {
		case err == nil:
		case apierrors.IsNotFound(err) || meta.IsNoMatchError(err):
			return nil, nil
		default:
			return nil, fmt.Errorf("get %s: %w", gvk.Kind, err)
		}
		s, _ := obj.Object["spec"].(map[string]interface{})
		if s == nil {
			s = map[string]interface{}{}
		}
		return s, nil
	}

	ecc, err := spec(educatesClusterConfigGVK)
	if err != nil {
		return nil, err
	}
	if ecc == nil {
		return nil, ErrNoPlatform
	}
	secretsManager, err := spec(secretsManagerGVK)
	if err != nil {
		return nil, err
	}
	lookupService, err := spec(lookupServiceGVK)
	if err != nil {
		return nil, err
	}
	sessionManager, err := spec(sessionManagerGVK)
	if err != nil {
		return nil, err
	}

	helmClient, err := helm.New(getter, OperatorNamespace, io.Discard)
	if err != nil {
		return nil, err
	}
	var values map[string]interface{}
	chartVersion := ""
	rel, err := helmClient.Release(OperatorReleaseName)
	switch {
	case err == nil:
		values = rel.Config
		if rel.Chart != nil && rel.Chart.Metadata != nil {
			chartVersion = rel.Chart.Metadata.Version
		}
	case errors.Is(err, helm.ErrReleaseNotFound):
	default:
		return nil, err
	}

	e := newExport(values, ecc, secretsManager, lookupService, sessionManager)
	e.ChartVersion = chartVersion
	e.NoOperatorRelease = rel == nil
	return e, nil
}

// ExportOf returns the configuration a deploy of out applies, as an
// EducatesConfig that deploys the same operator chart values and
// platform resource specs.
func ExportOf(out *translator.Output) *Export {
	spec := func(obj map[string]interface{}) map[string]interface{} {
		if obj == nil {
			return nil
		}
		s, _ := obj["spec"].(map[string]interface{})
		if s == nil {
			s = map[string]interface{}{}
		}
		return s
	}
	return newExport(out.OperatorChartValues, spec(out.EducatesClusterConfig), spec(out.SecretsManager), spec(out.LookupService), spec(out.SessionManager))
}

func newExport(values, ecc, secretsManager, lookupService, sessionManager map[string]interface{}) *Export {
	operator, unmapped := translator.OperatorConfigFromChartValues(values)

	cfg := &v1alpha1.EducatesConfig{
		TypeMeta: v1alpha1.TypeMeta{
			APIVersion: v1alpha1.APIVersion,
			Kind:       v1alpha1.KindEducatesConfig,
		},
		Operator:              operator,
		EducatesClusterConfig: ecc,
		SecretsManager:        secretsManager,
		LookupService:         lookupService,
		SessionManager:        sessionManager,
	}

	var refs []SecretRef
	for _, section := range []struct {
		name string
		spec map[string]interface{}
	}{
		{"educatesClusterConfig", ecc},
		{"secretsManager", secretsManager},
		{"lookupService", lookupService},
		{"sessionManager", sessionManager},
	} {
		refs = append(refs, secretRefsIn(section.spec, section.name)...)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Field < refs[j].Field })

	return &Export{Config: cfg, SecretRefs: refs, UnmappedValues: unmapped}
}

// secretRefsIn returns the Secret references in value, found at path: the
// maps under a key ending in SecretRef that name a Secret.
func secretRefsIn(value interface{}, path string) []SecretRef {
	var refs []SecretRef
	switch v := value.(type) {
	case map[string]interface{}:
		for key, child := range v {
			childPath := path + "." + key
			if strings.HasSuffix(key, "SecretRef") {
				if ref, ok := child.(map[string]interface{}); ok {
					if name, _ := ref["name"].(string); name != "" {
						namespace, _ := ref["namespace"].(string)
						refs = append(refs, SecretRef{Namespace: namespace, Name: name, Field: childPath})
						continue
					}
				}
			}
			refs = append(refs, secretRefsIn(child, childPath)...)
		}
	case []interface{}:
		for i, child := range v {
			refs = append(refs, secretRefsIn(child, fmt.Sprintf("%s[%d]", path, i))...)
		}
	}
	return refs
}

// YAML returns the configuration as an EducatesConfig document, after
// header, a comment line each, and the Secrets it references and the
// operator chart values it cannot express.
func (e *Export) YAML(header []string) ([]byte, error) {
	var b strings.Builder

	for _, line := range header {
		b.WriteString(comment(line))
	}
	if len(e.SecretRefs) > 0 {
		b.WriteString(comment("Secrets this configuration references, which are not exported. Create them"))
		b.WriteString(comment("in a cluster before deploying this file there:"))
		for _, ref := range e.SecretRefs {
			name := ref.Name
			if ref.Namespace != "" {
				name = ref.Namespace + "/" + name
			}
			b.WriteString(comment(fmt.Sprintf("  %s (%s)", name, ref.Field)))
		}
	}
	if len(e.UnmappedValues) > 0 {
		b.WriteString(comment("The operator release also sets values the operator block cannot express,"))
		b.WriteString(comment("which a deploy of this file leaves unset: " + strings.Join(e.UnmappedValues, ", ")))
	}
	if e.NoOperatorRelease {
		b.WriteString(comment(fmt.Sprintf("The cluster has no operator release %s in namespace %s, so there is", OperatorReleaseName, OperatorNamespace)))
		b.WriteString(comment("no operator block."))
	}

	data, err := yaml.Marshal(e.Config)
	if err != nil {
		return nil, fmt.Errorf("marshal EducatesConfig: %w", err)
	}
	b.Write(data)

	return []byte(b.String()), nil
}

func comment(line string) string {
	if line == "" {
		return "#\n"
	}
	return "# " + line + "\n"
}
