package workshops

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/educates/educates-training-platform/client-programs/pkg/templates"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
)

type WorkshopNewOptions struct {
	Template              string
	Name                  string
	Title                 string
	Description           string
	Image                 string
	TargetDirectory       string
	Overwrite             bool
	WithKubernetesAccess  bool
	WithGitHubAction      bool
	WithVirtualCluster    bool
	WithDockerDaemon      bool
	WithImageRegistry     bool
	WithKubernetesConsole bool
	WithEditor            bool
	WithTerminal          bool
}

// If o.TargetDirectory is provided, we will use that as the directory to be used, otherwise a new one will be created
func (o *WorkshopNewOptions) Run(args []string) error {
	var err error

	workshopDir := filepath.Clean(args[0])
	if o.TargetDirectory != "" {
		workshopDir = o.TargetDirectory
	}

	if workshopDir, err = filepath.Abs(workshopDir); err != nil {
		return errors.Wrapf(err, "could not convert path name %q to absolute path", workshopDir)
	}

	if o.TargetDirectory == "" {
		if _, err = os.Stat(workshopDir); err == nil {
			return errors.Errorf("target path name %q already exists", workshopDir)
		}
	} else {
		// Check if target directory already exist and prompt the user to confirm that they want to overwrite the files in it
		if _, err = os.Stat(workshopDir); err == nil {
			ok := o.Overwrite
			if !o.Overwrite {
				ok = utils.YesNoPrompt(fmt.Sprintf("the directory %q already exists. All files will be overwritten. Do you want to use it?", workshopDir), true)
			}
			if !ok {
				return errors.Errorf("operation cancelled")
			}
		}

	}

	name := o.Name

	if name == "" {
		name = filepath.Base(workshopDir)
	}

	if match, _ := regexp.MatchString("^[a-z0-9-]+$", name); !match {
		return errors.Errorf("invalid workshop name %q. It can only contain lowercase letters, numbers, and hyphens", name)
	}

	parameters := map[string]string{
		"WorkshopName":          name,
		"WorkshopTitle":         o.Title,
		"WorkshopDescription":   o.Description,
		"WorkshopImage":         o.Image,
		"WithKubernetesAccess":  strconv.FormatBool(o.WithKubernetesAccess),
		"WithVirtualCluster":    strconv.FormatBool(o.WithVirtualCluster),
		"WithDockerDaemon":      strconv.FormatBool(o.WithDockerDaemon),
		"WithImageRegistry":     strconv.FormatBool(o.WithImageRegistry),
		"WithKubernetesConsole": strconv.FormatBool(o.WithKubernetesConsole),
		"WithEditor":            strconv.FormatBool(o.WithEditor),
		"WithTerminal":          strconv.FormatBool(o.WithTerminal),
	}

	template := templates.InternalTemplate(o.Template)

	err = template.ApplyFiles(workshopDir, parameters)
	if err != nil {
		return err
	}

	if o.WithGitHubAction {
		template := templates.InternalTemplate("single")
		err = template.ApplyGitHubAction(workshopDir, parameters)
	}

	return err
}
