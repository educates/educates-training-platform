package templates

import (
	"bytes"
	"embed"
	"html/template"
	"os"
	"path"
	"regexp"

	"github.com/pkg/errors"
)

//go:embed all:files/*
var workshopTemplates embed.FS

//go:embed all:github/*
var githubTemplates embed.FS

type InternalTemplate string

func ListWorkshopTemplates() []InternalTemplate {
	files, err := workshopTemplates.ReadDir("files")

	if err != nil {
		return []InternalTemplate{}
	}

	var templates []InternalTemplate = make([]InternalTemplate, 0, len(files))

	for _, file := range files {
		templates = append(templates, InternalTemplate(file.Name()))
	}

	return templates
}

func (t InternalTemplate) exists(templateType string) bool {
	templatePath := path.Join(templateType, string(t))

	var files []os.DirEntry
	var err error

	switch templateType {
	case "files":
		files, err = workshopTemplates.ReadDir(templatePath)
	case "github":
		files, err = githubTemplates.ReadDir(templatePath)
	}

	return err == nil && len(files) != 0
}

func (t InternalTemplate) IsValid(templateType string) bool {
	match, _ := regexp.MatchString("^[a-z-]+$", string(t))

	return match && t.exists(templateType)
}

func (t InternalTemplate) ApplyFiles(directory string, parameters map[string]string) error {
	if !t.IsValid("files") {
		return errors.Errorf("internal template %q does not exist", t)
	}

	templatePath := path.Join("files", string(t))

	os.MkdirAll(directory, 0775)

	return copyTemplateDir(workshopTemplates, templatePath, directory, parameters)
}

func (t InternalTemplate) ApplyGitHubAction(directory string, parameters map[string]string) error {
	if !t.IsValid("github") {
		return errors.Errorf("internal template %q does not exist", t)
	}

	templatePath := path.Join("github", string(t))

	os.MkdirAll(directory, 0775)

	return copyTemplateDir(githubTemplates, templatePath, directory, parameters)
}

func copyTemplateDir(fs embed.FS, src string, dst string, parameters map[string]string) error {
	files, err := fs.ReadDir(src)

	if err != nil {
		return errors.Wrapf(err, "unable to open template directory %q", src)
	}

	for _, file := range files {
		srcFile := path.Join(src, file.Name())
		dstFile := path.Join(dst, file.Name())

		if file.IsDir() {
			if err = os.MkdirAll(dstFile, 0775); err != nil {
				return errors.Wrapf(err, "unable to create workshop directory %q", dstFile)
			}

			if err = copyTemplateDir(fs, srcFile, dstFile, parameters); err != nil {
				return err
			}
		} else {
			fileData, err := fs.ReadFile(srcFile)

			if err != nil {
				return errors.Wrapf(err, "unable to read template file %q", srcFile)
			}

			fileTemplate, err := template.New("template-file").Parse(string(fileData))

			if err != nil {
				return errors.Wrapf(err, "failed to parse template file %q", srcFile)
			}

			var fileOutData bytes.Buffer

			err = fileTemplate.Execute(&fileOutData, parameters)

			if err != nil {
				return errors.Wrapf(err, "failed to generate template file %q", srcFile)
			}

			newFile, err := os.Create(dstFile)

			if err != nil {
				return errors.Wrapf(err, "failed to create destination file %q", dstFile)
			}

			_, err = newFile.Write(fileOutData.Bytes())

			if err != nil {
				return errors.Wrapf(err, "unable to write destination file %q", dstFile)
			}

			// TODO Change permissions on files based on extension.
		}
	}

	return nil
}
