// Package schemas embeds the JSON schemas for the cli.educates.dev/v1alpha1
// config kinds. Schemas drive command-time validation, IDE support (via the
// public schemas.educates.dev URL), `local config set` path checks, and
// generated reference docs.
package schemas

import _ "embed"

//go:embed EducatesLocalConfig.schema.json
var EducatesLocalConfig []byte

//go:embed EducatesConfig.schema.json
var EducatesConfig []byte

//go:embed EducatesInlineConfig.schema.json
var EducatesInlineConfig []byte

//go:embed EducatesGKEConfig.schema.json
var EducatesGKEConfig []byte

//go:embed EducatesEKSConfig.schema.json
var EducatesEKSConfig []byte

//go:embed EducatesLocalMirrors.schema.json
var EducatesLocalMirrors []byte
