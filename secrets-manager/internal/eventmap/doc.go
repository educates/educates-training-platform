// Package eventmap turns events on the objects the secrets-manager watches
// into reconciles of the secrets.educates.dev resources they affect. Field
// indexes map a Secret to the rules that name it as a source or as a copy's
// name, and a copy's tracking annotation to the resource whose rule wrote
// it. Namespace events are mapped by evaluating the selectors of the cached
// rules. No event lists the objects of a kind from the API server.
package eventmap
