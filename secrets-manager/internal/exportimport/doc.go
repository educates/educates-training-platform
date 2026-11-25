// Package exportimport reconciles SecretExporters and SecretImporters.
//
// A SecretExporter offers the Secret of its own name in its own namespace to
// the namespaces its rules select, and copies it where the SecretImporter
// named after the copy accepts it, as package copyrules decides. A
// SecretImporter copies nothing: the reconcilers of the rules that offer it
// a copy accept or refuse it, and its own reconciler works out where it
// stands.
package exportimport
