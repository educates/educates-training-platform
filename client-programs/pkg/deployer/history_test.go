package deployer

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func testRevision(source string) Revision {
	return Revision{
		Deployed:   time.Date(2026, 11, 18, 19, 23, 40, 0, time.UTC),
		Action:     ActionUpdated,
		CLIVersion: "develop",
		Source:     source,
		Kind:       "EducatesLocalConfig",
		Config:     "# my config\napiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n\ningress:\n  domain: educates.test\n",
		Applied:    "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesConfig\n",
	}
}

func revisionNumbersOf(revisions []Revision) []int {
	numbers := make([]int, len(revisions))
	for i, rev := range revisions {
		numbers[i] = rev.Number
	}
	return numbers
}

// TestRecordRevisionNumbersAndKeepsEachDeploy checks that revisions are
// numbered from 1 in the history ConfigMap, labelled as the CLI's, and
// read back as recorded, the config's comments and blank lines included.
func TestRecordRevisionNumbersAndKeepsEachDeploy(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewClientset()

	for i, source := range []string{"one", "two", "three"} {
		n, err := RecordRevision(ctx, cs, testRevision(source))
		if err != nil {
			t.Fatalf("RecordRevision: %v", err)
		}
		if n != i+1 {
			t.Errorf("revision %q recorded as %d, want %d", source, n, i+1)
		}
	}

	cm, err := cs.CoreV1().ConfigMaps(OperatorNamespace).Get(ctx, HistoryConfigMap, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get history ConfigMap: %v", err)
	}
	if cm.Labels["app.kubernetes.io/managed-by"] != "educates-cli" {
		t.Errorf("history ConfigMap labels = %v", cm.Labels)
	}

	revisions, err := History(ctx, cs)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if got := revisionNumbersOf(revisions); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("revisions = %v, want 1, 2, 3", got)
	}
	want := testRevision("two")
	want.Number = 2
	if !reflect.DeepEqual(revisions[1], want) {
		t.Errorf("revision 2 =\n%+v\nwant\n%+v", revisions[1], want)
	}
}

// TestRecordRevisionKeepsTheOriginalAndTheLatest checks that the history
// keeps revision 1 and the latest historyRetained revisions.
func TestRecordRevisionKeepsTheOriginalAndTheLatest(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewClientset()

	for i := 0; i < 15; i++ {
		if _, err := RecordRevision(ctx, cs, testRevision("deploy")); err != nil {
			t.Fatalf("RecordRevision: %v", err)
		}
	}

	revisions, err := History(ctx, cs)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	want := []int{1, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	if got := revisionNumbersOf(revisions); !reflect.DeepEqual(got, want) {
		t.Errorf("revisions = %v, want %v", got, want)
	}
}

// TestRecordRevisionRetriesARace checks that a revision whose update of
// the history conflicts, as another deploy recorded its revision first,
// is recorded after that one.
func TestRecordRevisionRetriesARace(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewClientset()
	if _, err := RecordRevision(ctx, cs, testRevision("first")); err != nil {
		t.Fatalf("RecordRevision: %v", err)
	}

	configMaps := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	conflicted := false
	cs.PrependReactor("update", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		if conflicted {
			return false, nil, nil
		}
		conflicted = true
		// The other deploy records revision 2 between this one's read of
		// the history and its update.
		obj, err := cs.Tracker().Get(configMaps, OperatorNamespace, HistoryConfigMap)
		if err != nil {
			return true, nil, err
		}
		cm := obj.(*corev1.ConfigMap).DeepCopy()
		cm.Data[revisionKey(2)] = "revision: 2\naction: updated\n"
		if err := cs.Tracker().Update(configMaps, cm, OperatorNamespace); err != nil {
			return true, nil, err
		}
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, HistoryConfigMap, nil)
	})

	n, err := RecordRevision(ctx, cs, testRevision("second"))
	if err != nil {
		t.Fatalf("RecordRevision: %v", err)
	}
	if !conflicted {
		t.Fatalf("the update never conflicted")
	}
	if n != 3 {
		t.Errorf("revision recorded as %d, want 3, after the other deploy's 2", n)
	}

	revisions, err := History(ctx, cs)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if got := revisionNumbersOf(revisions); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Errorf("revisions = %v, want 1, 2, 3", got)
	}
}

func TestSelectRevision(t *testing.T) {
	revisions := []Revision{{Number: 1}, {Number: 6}, {Number: 7}}

	for which, want := range map[string]int{"latest": 7, "original": 1, "6": 6} {
		rev, err := SelectRevision(revisions, which)
		if err != nil || rev.Number != want {
			t.Errorf("SelectRevision(%q) = %d, %v, want %d", which, rev.Number, err, want)
		}
	}

	if _, err := SelectRevision(revisions, "3"); err == nil || !strings.Contains(err.Error(), "keeps revisions 1, 6, 7") {
		t.Errorf("SelectRevision(3) error = %v, want one naming the revisions kept", err)
	}
	if _, err := SelectRevision(revisions, "newest"); err == nil {
		t.Errorf("SelectRevision(newest) gave no error")
	}
	if _, err := SelectRevision(nil, "latest"); err == nil || !strings.Contains(err.Error(), "no deploy history") {
		t.Errorf("SelectRevision with no history error = %v", err)
	}
}
