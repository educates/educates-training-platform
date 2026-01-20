package sessions

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	educatesrestapi "github.com/educates/educates-training-platform/client-programs/pkg/educates/restapi"
	educatesTypes "github.com/educates/educates-training-platform/client-programs/pkg/educates/types"
	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

type SessionManager struct {
}

func NewSessionManager() *SessionManager {
	return &SessionManager{}
}


type ListSessionsConfig struct {
	Client dynamic.Interface
	Portal string
	Environment string
}

type ExtendSessionConfig struct {
	ClusterConfig *cluster.ClusterConfig
	Portal string
	Name string
}

type SessionStatusConfig struct {
	ClusterConfig *cluster.ClusterConfig
	Portal string
	Name string
}

type TerminateSessionConfig struct {
	ClusterConfig *cluster.ClusterConfig
	Portal string
	Name string
}

func (m *SessionManager) ListSessions(cfg ListSessionsConfig) (string, error) {
	workshopSessionClient := cfg.Client.Resource(educatesTypes.WorkshopsessionsResource)

	workshopSessions, err := workshopSessionClient.List(context.TODO(), metav1.ListOptions{})

	if k8serrors.IsNotFound(err) {
		return "No sessions found.", nil
	}

	var sessions []unstructured.Unstructured

	for _, item := range workshopSessions.Items {
		labels := item.GetLabels()

		portal, ok := labels["training.educates.dev/portal.name"]

		if ok && portal == cfg.Portal {
			if cfg.Environment != "" {
				environment, ok := labels["training.educates.dev/environment.name"]

				if ok && environment == cfg.Environment {
					sessions = append(sessions, item)
				}
			} else {
				sessions = append(sessions, item)
			}

		}
	}

	if len(sessions) == 0 {
		return "No sessions found.", nil
	}

	var buf strings.Builder
	w := new(tabwriter.Writer)
	w.Init(&buf, 8, 8, 3, ' ', 0)

	fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", "NAME", "PORTAL", "ENVIRONMENT", "STATUS")

	for i, item := range sessions {
		name := item.GetName()
		labels := item.GetLabels()

		portal := labels["training.educates.dev/portal.name"]
		environment := labels["training.educates.dev/environment.name"]

		status, _, _ := unstructured.NestedString(item.Object, "status", "educates", "phase")

		fmt.Fprintf(w, "%s\t%s\t%s\t%s", name, portal, environment, status)
		if i < len(sessions) - 1 {
			fmt.Fprintf(w, "\n")
		}
	}

	w.Flush()

	return buf.String(), nil
}

func (m *SessionManager) ExtendSession(cfg ExtendSessionConfig) (string, error) {
	catalogApiRequester := educatesrestapi.NewWorkshopsCatalogRequester(
		cfg.ClusterConfig,
		cfg.Portal,
	)
	logout, err := catalogApiRequester.Login()
	defer logout()
	if err != nil {
		return "", errors.Wrap(err, "failed to login to training portal")
	}

	details, err := catalogApiRequester.ExtendWorkshopSession(cfg.Name)
	if err != nil {
		return "", err
	}

	return printStatus(details), nil
}

func (m *SessionManager) SessionStatus(cfg SessionStatusConfig) (string, error) {
	catalogApiRequester := educatesrestapi.NewWorkshopsCatalogRequester(
		cfg.ClusterConfig,
		cfg.Portal,
	)
	logout, err := catalogApiRequester.Login()
	defer logout()
	if err != nil {
		return "", errors.Wrap(err, "failed to login to training portal")
	}

	details, err := catalogApiRequester.GetWorkshopSession(cfg.Name)
	if err != nil {
		return "", err
	}

	return printStatus(details), nil
}

func (m *SessionManager) TerminateSession(cfg TerminateSessionConfig) (string, error) {
	catalogApiRequester := educatesrestapi.NewWorkshopsCatalogRequester(
		cfg.ClusterConfig,
		cfg.Portal,
	)
	logout, err := catalogApiRequester.Login()
	defer logout()
	if err != nil {
		return "", errors.Wrap(err, "failed to login to training portal")
	}

	details, err := catalogApiRequester.TerminateWorkshopSession(cfg.Name)
	if err != nil {
		return "", err
	}

	return printStatus(details), nil

}

func printStatus(details *educatesrestapi.WorkshopSessionDetails) string {
	var buf strings.Builder

	fmt.Fprintf(&buf, "Started: %s\n", details.Started)
	fmt.Fprintf(&buf, "Expires: %s\n", details.Expires)
	fmt.Fprintf(&buf, "Expiring: %t\n", details.Expiring)
	fmt.Fprintf(&buf, "Countdown: %d\n", details.Countdown)
	fmt.Fprintf(&buf, "Extendable: %t\n", details.Extendable)
	fmt.Fprintf(&buf, "Status: %s", details.Status)

	return buf.String()
}
