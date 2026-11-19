package diagnostics

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type ClusterDiagnostics struct {
	clusterConfig *cluster.ClusterConfig
	dest          string
	verbose       bool
}

func NewClusterDiagnostics(clusterConfig *cluster.ClusterConfig, dest string, verbose bool) *ClusterDiagnostics {
	return &ClusterDiagnostics{clusterConfig, dest, verbose}
}

func (c *ClusterDiagnostics) Run() error {
	// Check if the cluster is available
	if err := cluster.IsClusterAvailableCheck(c.clusterConfig); err != nil {
		return err
	}

	tempDir, err := createTempDir()
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	clusterDiagnosticsFetcher := &ClusterDiagnosticsFetcher{c.clusterConfig, tempDir, c.verbose}

	// Fetch the Educates platform resources the operator reconciles, whose
	// status conditions say which part of an install is not ready and why.
	for _, platform := range []struct {
		resource schema.GroupVersionResource
		fileName string
		kind     string
	}{
		{educatesClusterConfigResource, "educates-cluster-configs.yaml", "EducatesClusterConfig"},
		{secretsManagerResource, "secrets-managers.yaml", "SecretsManager"},
		{lookupServiceResource, "lookup-services.yaml", "LookupService"},
		{sessionManagerResource, "session-managers.yaml", "SessionManager"},
	} {
		if err = clusterDiagnosticsFetcher.fetchDynamicallyResources(platform.resource, platform.fileName); err != nil {
			fmt.Printf("Error fetching %s resources: %v\n", platform.kind, err)
		}
	}

	// Fetch all Educates training related resources
	if err = clusterDiagnosticsFetcher.fetchDynamicallyResources(trainingportalResource, "training-portals.yaml"); err != nil {
		fmt.Println("Error fetching training portals: ", err)
	}
	if err = clusterDiagnosticsFetcher.fetchDynamicallyResources(workshopResource, "workshops.yaml"); err != nil {
		fmt.Println("Error fetching workshops: ", err)
	}
	if err = clusterDiagnosticsFetcher.fetchDynamicallyResources(workshopsessionsResource, "workshop-sessions.yaml"); err != nil {
		fmt.Println("Error fetching workshop sessions: ", err)
	}
	if err = clusterDiagnosticsFetcher.fetchDynamicallyResources(workshoprequestsResource, "workshop-requests.yaml"); err != nil {
		fmt.Println("Error fetching workshop requests: ", err)
	}
	if err = clusterDiagnosticsFetcher.fetchDynamicallyResources(workshopenvironmentsResource, "workshop-environments.yaml"); err != nil {
		fmt.Println("Error fetching workshop environments: ", err)
	}
	if err = clusterDiagnosticsFetcher.fetchDynamicallyResources(workshopallocationsResource, "workshop-allocations.yaml"); err != nil {
		fmt.Println("Error fetching workshop allocations: ", err)
	}

	//	getEducatesNamespaces
	if err = clusterDiagnosticsFetcher.getEducatesNamespaces("educates-namespaces.yaml"); err != nil {
		fmt.Println("Error fetching educates namespaces: ", err)
	}

	// Fetch all Educates secrets related resources
	if err = clusterDiagnosticsFetcher.fetchDynamicallyResources(secretcopierResource, "secret-copiers.yaml"); err != nil {
		fmt.Println("Error fetching secret copiers: ", err)
	}
	if err = clusterDiagnosticsFetcher.fetchDynamicallyResources(secretinjectorsResource, "secret-injectors.yaml"); err != nil {
		fmt.Println("Error fetching secret injectors: ", err)
	}
	if err = clusterDiagnosticsFetcher.fetchDynamicallyResources(secretexportersResource, "secret-exporters.yaml"); err != nil {
		fmt.Println("Error fetching secret injectors: ", err)
	}
	if err = clusterDiagnosticsFetcher.fetchDynamicallyResources(secretimportersResource, "secret-importers.yaml"); err != nil {
		fmt.Println("Error fetching secret injectors: ", err)
	}

	// Fetch the logs of the Educates operator. The CLI installs it in the
	// educates-installer namespace, but a helm install of its chart may use
	// another, so its pods are found by their labels in every namespace.
	if err = clusterDiagnosticsFetcher.fetchLogsForDeployment("app.kubernetes.io/name=educates-installer,app.kubernetes.io/component=operator", metav1.NamespaceAll, "educates-installer.log"); err != nil {
		fmt.Println("Error fetching logs for the Educates operator: ", err)
	}

	// fetch logs for the session-manager, secret-manager and lookup-service deployments
	if err = clusterDiagnosticsFetcher.fetchLogsForDeployment("app=lookup-service", "educates", "lookup-service.log"); err != nil {
		fmt.Println("Error fetching logs for lookup-service: ", err)
	}
	if err = clusterDiagnosticsFetcher.fetchLogsForDeployment("deployment=session-manager", "educates", "session-manager.log"); err != nil {
		fmt.Println("Error fetching logs for session-manager: ", err)
	}
	if err = clusterDiagnosticsFetcher.fetchLogsForDeployment("deployment=secrets-manager", "educates", "secrets-manager.log"); err != nil {
		fmt.Println("Error fetching logs for secrets-manager: ", err)
	}
	// dump logs for all training-portal deployments
	if err = clusterDiagnosticsFetcher.fetchLogsForDeployment("deployment=training-portal", "training.educates.dev/component=portal", "training-portal-%v.log"); err != nil {
		fmt.Println("Error fetching logs for secrets-manager: ", err)
	}
	// Fetch workshop_list from Rest API for each training-portal
	if err = clusterDiagnosticsFetcher.fetchTrainingPortalDetailsAtRest("training-portal-catalog-%v.json"); err != nil {
		fmt.Println("Error fetching training portal details at Rest: ", err)
	}

	// Fetch Educates related events
	if err = clusterDiagnosticsFetcher.getEducatesNamespacesEvents("educates-events.yaml"); err != nil {
		fmt.Println("Error fetching educates namespaces: ", err)
	}

	// If directory is provided, check that it exists otherwise create it
	dir, file, err := getDestDirAndFile(c.dest)
	if err != nil {
		return err
	}

	// if file is provided, compress the directory and save it to the file
	// else, copy all the files from the tempDir to the provided directory
	if file != "" {
		err = CompressDirToFile(tempDir, c.dest)
		if err != nil {
			return err
		}
		fmt.Println("Diagnostics files saved to file: ", c.dest)
	} else {
		copyAllFilesInDir(tempDir, dir)
		fmt.Println("Diagnostics files saved to dir: ", dir)
	}

	fmt.Println("Diagnostics completed successfully")
	return nil
}

// func (c *ClusterDiagnostics) checkDestination() error {
// 	if c.dir == "" {
// 		// Create a temporary directory
// 		tempDir, err := os.MkdirTemp("", "educates-diagnostics")
// 		if err != nil {
// 			return err
// 		}
// 		fmt.Println("Created temp dir: ", tempDir)
// 		c.dir = tempDir
// 	} else {
// 		// Check if the directory exists
// 		_, err := os.Stat(c.dir)
// 		if os.IsNotExist(err) {
// 			// Create the directory
// 			err := os.MkdirAll(c.dir, 0755)
// 			if err != nil {
// 				return err
// 			}
// 		}
// 	}

// 	return nil
// }

func createTempDir() (string, error) {
	tempDir, err := os.MkdirTemp("", "educates-diagnostics")
	if err != nil {
		return "", err
	}
	return tempDir, nil
}

func getDestDirAndFile(dest string) (string, string, error) {
	if dest == "" {
		return "", "", fmt.Errorf("dest is required")
	}
	if strings.HasSuffix(dest, ".tar.gz") {
		return filepath.Dir(dest), filepath.Base(dest), nil
	} else if filepath.Ext(dest) == "" {
		return dest, "", nil
	} else {
		return "", "", fmt.Errorf("dest must be a directory or a .tar.gz file")
	}
}

func copyAllFilesInDir(src string, dest string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// We only copy the files in the temp directory, if it's a dir, we skip it
		if info.IsDir() {
			return nil
		}
		// Get the relative path of the file
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		// Create the destination path
		destPath := filepath.Join(dest, relPath)

		// Create the directory if it doesn't exist
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}

		// Copy the file
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()
		destFile, err := os.Create(destPath)
		if err != nil {
			return err
		}
		defer destFile.Close()
		_, err = io.Copy(destFile, srcFile)
		return err
	})
}
