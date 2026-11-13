package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
)

// fakeResolverContainer is a local DNS resolver container that exists
// as exists says, records the configuration it is updated with, and fails
// its update with updateErr. With withoutIncludeFolder set it was created
// before the include folder, so it does not mount it.
type fakeResolverContainer struct {
	exists               bool
	existsErr            error
	updateErr            error
	withoutIncludeFolder bool
	updated              []byte
}

func (f *fakeResolverContainer) container() resolverContainer {
	return resolverContainer{
		exists: func() (bool, error) { return f.exists, f.existsErr },
		update: func(content []byte) (bool, error) {
			f.updated = content
			return !f.withoutIncludeFolder, f.updateErr
		},
	}
}

// refreshForTest refreshes the resolver container c with what render
// returns, and returns what the refresh reported.
func refreshForTest(c *fakeResolverContainer, render func() ([]byte, []error, error)) string {
	var out bytes.Buffer
	refreshResolverContainer(progress.New(&out, 0, false), c.container(), render)
	return out.String()
}

func TestRefreshLocalResolverServesTheRenderedConfig(t *testing.T) {
	c := &fakeResolverContainer{exists: true}

	out := refreshForTest(c, func() ([]byte, []error, error) { return []byte("address=/educates.test/10.10.10.1\n"), nil, nil })

	if got := string(c.updated); got != "address=/educates.test/10.10.10.1\n" {
		t.Errorf("resolver updated with %q, want the rendered config", got)
	}
	if strings.Contains(out, "⚠") {
		t.Errorf("refresh warned:\n%s", out)
	}
}

func TestRefreshLocalResolverWarnsAboutAContainerWithoutTheIncludeFolder(t *testing.T) {
	c := &fakeResolverContainer{exists: true, withoutIncludeFolder: true}

	out := refreshForTest(c, func() ([]byte, []error, error) { return []byte("# rendered\n"), nil, nil })

	if string(c.updated) != "# rendered\n" {
		t.Errorf("resolver updated with %q, want the rendered config", c.updated)
	}
	if got := strings.Count(out, "⚠"); got != 1 {
		t.Errorf("refresh reported %d warnings, want one:\n%s", got, out)
	}
	for _, part := range []string{"dnsmasq.d", "educates local resolver delete", "educates local resolver deploy"} {
		if !strings.Contains(out, part) {
			t.Errorf("refresh reported:\n%s\nwant a warning mentioning %q", out, part)
		}
	}
	if strings.Contains(out, "skipped") {
		t.Errorf("refresh reported it skipped the update:\n%s", out)
	}
}

func TestRefreshLocalResolverDoesNothingWithoutAResolver(t *testing.T) {
	c := &fakeResolverContainer{}
	rendered := false

	out := refreshForTest(c, func() ([]byte, []error, error) { rendered = true; return nil, nil, nil })

	if rendered || c.updated != nil || out != "" {
		t.Errorf("rendered %t, updated %q, reported %q; want nothing done", rendered, c.updated, out)
	}
}

func TestRefreshLocalResolverWarnsAboutTheConfigsItLeavesOut(t *testing.T) {
	c := &fakeResolverContainer{exists: true}

	out := refreshForTest(c, func() ([]byte, []error, error) {
		return []byte("# rendered\n"), []error{errors.New(`local cluster "test2" left out: /data/clusters/test2/config.yaml: loopback`)}, nil
	})

	if c.updated == nil {
		t.Errorf("resolver not updated, want it served the configs that load")
	}
	if !strings.Contains(out, "⚠") || !strings.Contains(out, "/data/clusters/test2/config.yaml") {
		t.Errorf("refresh reported:\n%s\nwant a warning naming the config left out", out)
	}
}

func TestRefreshLocalResolverWarnsWhenItCannotRefresh(t *testing.T) {
	cases := []struct {
		name   string
		c      *fakeResolverContainer
		render func() ([]byte, []error, error)
		cause  string
	}{
		{
			name:   "the resolver can't be looked up",
			c:      &fakeResolverContainer{existsErr: errors.New("docker is not running")},
			render: func() ([]byte, []error, error) { return nil, nil, nil },
			cause:  "docker is not running",
		},
		{
			name: "the config can't be rendered",
			c:    &fakeResolverContainer{exists: true},
			render: func() ([]byte, []error, error) {
				return nil, nil, errors.New(`the default local cluster and local cluster "test2" both claim the domain "educates.test"`)
			},
			cause: "both claim",
		},
		{
			name:   "the resolver can't be restarted",
			c:      &fakeResolverContainer{exists: true, updateErr: errors.New("failed to restart resolver")},
			render: func() ([]byte, []error, error) { return []byte("# rendered\n"), nil, nil },
			cause:  "failed to restart resolver",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := refreshForTest(tc.c, tc.render)

			for _, part := range []string{"⚠", tc.cause, "educates local resolver update"} {
				if !strings.Contains(out, part) {
					t.Errorf("refresh reported:\n%s\nwant a warning mentioning %q", out, part)
				}
			}
			if strings.Contains(out, "✗") {
				t.Errorf("refresh reported a failure, want a warning:\n%s", out)
			}
		})
	}
}
