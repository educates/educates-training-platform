package sessions

import (
	"context"
	"fmt"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	educatesrestapi "github.com/educates/educates-training-platform/client-programs/pkg/educates/restapi"
	educatesTypes "github.com/educates/educates-training-platform/client-programs/pkg/educates/types"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
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

		portal, ok := labels[constants.EducatesTrainingLabelAnnotationPortalName]

		if ok && portal == cfg.Portal {
			if cfg.Environment != "" {
				environment, ok := labels[constants.EducatesTrainingLabelAnnotationEnvironmentName]

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

	var data [][]string
	for _, item := range sessions {
		name := item.GetName()
		labels := item.GetLabels()
		portal := labels[constants.EducatesTrainingLabelAnnotationPortalName]
		environment := labels[constants.EducatesTrainingLabelAnnotationEnvironmentName]

		status, _, _ := unstructured.NestedString(item.Object, "status", "educates", "phase")

		data = append(data, []string{name, portal, environment, status})
	}

	return utils.PrintTable([]string{"NAME", "PORTAL", "ENVIRONMENT", "STATUS"}, data), nil
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
	return utils.PrintKeyValuesTable([][]string{
		{"Started", details.Started},
		{"Expires", details.Expires},
		{"Expiring", fmt.Sprintf("%t", details.Expiring)},
		{"Countdown", fmt.Sprintf("%d", details.Countdown)},
		{"Extendable", fmt.Sprintf("%t", details.Extendable)},
		{"Status", details.Status}},
	)
}
