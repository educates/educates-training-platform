package deployer

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// HistoryConfigMap is the ConfigMap, in OperatorNamespace, holding a
// revision for each deploy of the platform by the CLI.
const HistoryConfigMap = "educates-deploy-history"

// historyRetained is how many of the latest revisions the history keeps,
// besides revision 1, which it always keeps.
const historyRetained = 10

// historyAttempts bounds the retries of a record that races another
// deploy recording its own revision.
const historyAttempts = 5

// The actions a revision records.
const (
	ActionInstalled = "installed"
	ActionUpdated   = "updated"
)

// Revision is the record of one deploy of the platform.
type Revision struct {
	// Number is the revision's number in the history, from 1.
	Number int `yaml:"revision"`

	// Deployed is when the deploy applied the platform resources.
	Deployed time.Time `yaml:"deployed"`

	// Action is ActionInstalled when the deploy created the platform, and
	// ActionUpdated when the cluster already had it.
	Action string `yaml:"action"`

	// CLIVersion is the version of the CLI that deployed.
	CLIVersion string `yaml:"cliVersion"`

	// KubeContext is the kube context the deploy reached the cluster
	// through.
	KubeContext string `yaml:"kubeContext,omitempty"`

	// Source says where the config came from, such as the file --config
	// named.
	Source string `yaml:"source"`

	// Kind is the kind of the CLI config.
	Kind string `yaml:"kind"`

	// Config is the config file as given.
	Config string `yaml:"config"`

	// Applied is what the deploy applied, as an EducatesConfig.
	Applied string `yaml:"applied"`
}

// RevisionInput is what a deploy records about itself besides what it
// applies.
type RevisionInput struct {
	CLIVersion  string
	KubeContext string
	Source      string
	Kind        string
	Config      []byte
}

// revisionKey is the key of revision n in the history ConfigMap.
func revisionKey(n int) string {
	return fmt.Sprintf("revision-%d.yaml", n)
}

// revisionNumber is the number of the revision a key of the history
// ConfigMap holds, or false for another key.
func revisionNumber(key string) (int, bool) {
	s, ok := strings.CutPrefix(key, "revision-")
	if !ok {
		return 0, false
	}
	s, ok = strings.CutSuffix(s, ".yaml")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil && n > 0
}

// RecordRevision adds rev to the history of the cluster cs reaches, under
// the number after the highest the history holds, and drops the
// revisions it no longer keeps: all but revision 1 and the latest
// historyRetained. It returns the number rev is recorded under. It
// creates the history ConfigMap when the cluster has none, and retries
// when another deploy records a revision at the same time.
func RecordRevision(ctx context.Context, cs kubernetes.Interface, rev Revision) (int, error) {
	configMaps := cs.CoreV1().ConfigMaps(OperatorNamespace)

	var err error
	for attempt := 0; attempt < historyAttempts; attempt++ {
		cm, getErr := configMaps.Get(ctx, HistoryConfigMap, metav1.GetOptions{})
		exists := getErr == nil
		if getErr != nil && !apierrors.IsNotFound(getErr) {
			return 0, fmt.Errorf("read the deploy history: %w", getErr)
		}
		if !exists {
			cm = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
				Name:      HistoryConfigMap,
				Namespace: OperatorNamespace,
				Labels: map[string]string{
					"app.kubernetes.io/part-of":    "educates",
					"app.kubernetes.io/managed-by": "educates-cli",
				},
			}}
		}
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}

		numbers := revisionNumbers(cm.Data)
		rev.Number = 1
		if len(numbers) > 0 {
			rev.Number = numbers[len(numbers)-1] + 1
		}
		data, marshalErr := yaml.Marshal(rev)
		if marshalErr != nil {
			return 0, fmt.Errorf("marshal revision %d: %w", rev.Number, marshalErr)
		}
		cm.Data[revisionKey(rev.Number)] = string(data)
		for _, n := range pruned(append(numbers, rev.Number)) {
			delete(cm.Data, revisionKey(n))
		}

		if exists {
			_, err = configMaps.Update(ctx, cm, metav1.UpdateOptions{})
		} else {
			_, err = configMaps.Create(ctx, cm, metav1.CreateOptions{})
		}
		switch {
		case err == nil:
			return rev.Number, nil
		case apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err):
			// Another deploy recorded a revision first: number this one
			// after it.
			continue
		default:
			return 0, fmt.Errorf("write the deploy history: %w", err)
		}
	}
	return 0, fmt.Errorf("write the deploy history: %w", err)
}

// revisionNumbers returns the numbers of the revisions in data, sorted.
func revisionNumbers(data map[string]string) []int {
	var numbers []int
	for key := range data {
		if n, ok := revisionNumber(key); ok {
			numbers = append(numbers, n)
		}
	}
	sort.Ints(numbers)
	return numbers
}

// pruned returns the revisions of numbers the history drops: all but
// revision 1 and the latest historyRetained.
func pruned(numbers []int) []int {
	sorted := append([]int(nil), numbers...)
	sort.Ints(sorted)
	var drop []int
	for i, n := range sorted {
		if n != 1 && i < len(sorted)-historyRetained {
			drop = append(drop, n)
		}
	}
	return drop
}

// History returns the revisions the cluster cs reaches records, oldest
// first, or none when it has no history.
func History(ctx context.Context, cs kubernetes.Interface) ([]Revision, error) {
	cm, err := cs.CoreV1().ConfigMaps(OperatorNamespace).Get(ctx, HistoryConfigMap, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the deploy history: %w", err)
	}
	var revisions []Revision
	for _, n := range revisionNumbers(cm.Data) {
		var rev Revision
		if err := yaml.Unmarshal([]byte(cm.Data[revisionKey(n)]), &rev); err != nil {
			return nil, fmt.Errorf("read revision %d of the deploy history: %w", n, err)
		}
		rev.Number = n
		revisions = append(revisions, rev)
	}
	return revisions, nil
}

// SelectRevision returns the revision of revisions, oldest first, that
// which names: "latest", "original" for the oldest the history keeps, or
// a number.
func SelectRevision(revisions []Revision, which string) (Revision, error) {
	if len(revisions) == 0 {
		return Revision{}, fmt.Errorf("the cluster has no deploy history: the CLI records a revision in the ConfigMap %s in namespace %s for each deploy it makes", HistoryConfigMap, OperatorNamespace)
	}
	switch which {
	case "latest":
		return revisions[len(revisions)-1], nil
	case "original":
		return revisions[0], nil
	}
	n, err := strconv.Atoi(which)
	if err != nil {
		return Revision{}, fmt.Errorf("revision %q is not latest, original or a number", which)
	}
	for _, rev := range revisions {
		if rev.Number == n {
			return rev, nil
		}
	}
	numbers := make([]string, len(revisions))
	for i, rev := range revisions {
		numbers[i] = strconv.Itoa(rev.Number)
	}
	return Revision{}, fmt.Errorf("the deploy history has no revision %d; it keeps revisions %s", n, strings.Join(numbers, ", "))
}
