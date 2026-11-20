package placement_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/educates/educates-training-platform/lookup-service/internal/inventory"
	"github.com/educates/educates-training-platform/lookup-service/internal/placement"
)

// portal returns a training portal whose session maximum is capacity, 0
// for none, holding allocated sessions and inFlight requests.
func portal(name string, capacity int64, allocated, inFlight int) *inventory.Portal {
	return &inventory.Portal{Name: name, Capacity: capacity, Allocated: allocated, InFlight: inFlight}
}

// environment returns a workshop environment of p with that capacity, nil
// for none, holding allocated and available sessions and inFlight requests.
func environment(name string, p *inventory.Portal, capacity *int64, allocated, available, inFlight int,
) *inventory.Environment {
	return &inventory.Environment{
		Portal: p, Name: name, Capacity: capacity,
		Allocated: allocated, Available: available, InFlight: inFlight,
	}
}

func names(environments []*inventory.Environment) []string {
	result := make([]string, 0, len(environments))
	for _, e := range environments {
		result = append(result, e.Name)
	}
	return result
}

// runs is how often a case is ordered, so that every order of candidates
// that rank equally is seen.
const runs = 200

func TestOrdering(t *testing.T) {
	unlimited := (*int64)(nil)
	cases := []struct {
		name       string
		candidates func() []*inventory.Environment
		// ranks are the names of the candidates in the order they must be
		// tried, those of one rank in any order.
		ranks [][]string
	}{
		{
			name: "a portal with room comes before a full one",
			candidates: func() []*inventory.Environment {
				return []*inventory.Environment{
					environment("full", portal("a", 2, 2, 0), unlimited, 0, 0, 0),
					environment("room", portal("b", 2, 1, 0), unlimited, 0, 0, 0),
				}
			},
			ranks: [][]string{{"room"}, {"full"}},
		},
		{
			name: "an environment with room comes before a full one, whatever the portals' remaining capacity",
			candidates: func() []*inventory.Environment {
				return []*inventory.Environment{
					environment("full", portal("a", 10, 1, 0), new(int64(1)), 1, 0, 0),
					environment("room", portal("b", 3, 0, 0), new(int64(1)), 0, 0, 0),
				}
			},
			ranks: [][]string{{"room"}, {"full"}},
		},
		{
			name: "#975, equal capacity: equal portals rank equally",
			candidates: func() []*inventory.Environment {
				return []*inventory.Environment{
					environment("a", portal("a", 3, 0, 0), new(int64(5)), 0, 0, 0),
					environment("b", portal("b", 3, 0, 0), new(int64(5)), 0, 0, 0),
				}
			},
			ranks: [][]string{{"a", "b"}},
		},
		{
			name: "#975, equal capacity: the portal that took the last session comes second",
			candidates: func() []*inventory.Environment {
				return []*inventory.Environment{
					environment("a", portal("a", 3, 1, 0), new(int64(5)), 1, 0, 0),
					environment("b", portal("b", 3, 0, 0), new(int64(5)), 0, 0, 0),
				}
			},
			ranks: [][]string{{"b"}, {"a"}},
		},
		{
			name: "#975, unequal capacity: more remaining portal capacity comes first",
			candidates: func() []*inventory.Environment {
				return []*inventory.Environment{
					environment("small", portal("a", 2, 0, 0), new(int64(5)), 0, 0, 0),
					environment("large", portal("b", 4, 1, 0), new(int64(5)), 1, 0, 0),
				}
			},
			ranks: [][]string{{"large"}, {"small"}},
		},
		{
			name: "#975, unequal capacity: equal remaining portal capacity ranks equally",
			candidates: func() []*inventory.Environment {
				return []*inventory.Environment{
					environment("small", portal("a", 2, 0, 0), unlimited, 0, 0, 0),
					environment("large", portal("b", 4, 2, 0), unlimited, 2, 0, 0),
				}
			},
			ranks: [][]string{{"small", "large"}},
		},
		{
			name: "an environment with reserved sessions comes first among equal portals",
			candidates: func() []*inventory.Environment {
				return []*inventory.Environment{
					environment("none", portal("a", 4, 1, 0), new(int64(3)), 1, 0, 0),
					environment("reserved", portal("b", 4, 1, 0), new(int64(3)), 0, 1, 0),
				}
			},
			ranks: [][]string{{"reserved"}, {"none"}},
		},
		{
			name: "reserved sessions rank after the portals' remaining capacity",
			candidates: func() []*inventory.Environment {
				return []*inventory.Environment{
					environment("reserved", portal("a", 4, 2, 0), new(int64(3)), 0, 2, 0),
					environment("none", portal("b", 4, 1, 0), new(int64(3)), 1, 0, 0),
				}
			},
			ranks: [][]string{{"none"}, {"reserved"}},
		},
		{
			name: "more remaining environment capacity comes first among equal portals",
			candidates: func() []*inventory.Environment {
				p := portal("a", 0, 1, 0)
				return []*inventory.Environment{
					environment("two", p, new(int64(3)), 1, 0, 0),
					environment("four", p, new(int64(4)), 0, 0, 0),
					environment("one", p, new(int64(2)), 1, 0, 0),
				}
			},
			ranks: [][]string{{"four"}, {"two"}, {"one"}},
		},
		{
			name: "a portal with no maximum ranks as one with one session left",
			candidates: func() []*inventory.Environment {
				return []*inventory.Environment{
					environment("unlimited", portal("a", 0, 7, 0), new(int64(10)), 0, 0, 0),
					environment("two-left", portal("b", 3, 1, 0), new(int64(10)), 0, 0, 0),
					environment("one-left", portal("c", 3, 2, 0), new(int64(10)), 0, 0, 0),
				}
			},
			ranks: [][]string{{"two-left"}, {"unlimited", "one-left"}},
		},
		{
			name: "a portal maximum of 0 is unlimited, so the portal always has room",
			candidates: func() []*inventory.Environment {
				return []*inventory.Environment{
					environment("full", portal("a", 2, 2, 0), unlimited, 0, 0, 0),
					environment("unlimited", portal("b", 0, 50, 0), unlimited, 0, 0, 0),
				}
			},
			ranks: [][]string{{"unlimited"}, {"full"}},
		},
		{
			name: "an environment with no capacity ranks as one with one session left",
			candidates: func() []*inventory.Environment {
				p := portal("a", 0, 0, 0)
				return []*inventory.Environment{
					environment("unlimited", p, unlimited, 9, 0, 0),
					environment("two-left", p, new(int64(2)), 0, 0, 0),
					environment("one-left", p, new(int64(2)), 1, 0, 0),
				}
			},
			ranks: [][]string{{"two-left"}, {"unlimited", "one-left"}},
		},
		{
			name: "an environment with capacity 0 has no room",
			candidates: func() []*inventory.Environment {
				p := portal("a", 0, 0, 0)
				return []*inventory.Environment{
					environment("none", p, new(int64(0)), 0, 0, 0),
					environment("one", p, new(int64(1)), 0, 0, 0),
				}
			},
			ranks: [][]string{{"one"}, {"none"}},
		},
		{
			name: "requests in flight to a portal count against it",
			candidates: func() []*inventory.Environment {
				return []*inventory.Environment{
					environment("in-flight", portal("a", 3, 0, 1), new(int64(5)), 0, 0, 1),
					environment("idle", portal("b", 3, 0, 0), new(int64(5)), 0, 0, 0),
				}
			},
			ranks: [][]string{{"idle"}, {"in-flight"}},
		},
		{
			name: "requests in flight to an environment count against it",
			candidates: func() []*inventory.Environment {
				p := portal("a", 0, 0, 2)
				return []*inventory.Environment{
					environment("in-flight", p, new(int64(2)), 0, 0, 2),
					environment("idle", p, new(int64(1)), 0, 0, 0),
				}
			},
			ranks: [][]string{{"idle"}, {"in-flight"}},
		},
		{
			name: "requests in flight and sessions together fill a portal",
			candidates: func() []*inventory.Environment {
				return []*inventory.Environment{
					environment("filled", portal("a", 2, 1, 1), unlimited, 1, 0, 1),
					environment("room", portal("b", 1, 0, 0), unlimited, 0, 0, 0),
				}
			},
			ranks: [][]string{{"room"}, {"filled"}},
		},
	}

	ordering := placement.NewOrdering()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// firsts records, for each rank, which candidates came first
			// in it.
			firsts := make([]map[string]bool, len(c.ranks))
			for i := range firsts {
				firsts[i] = map[string]bool{}
			}
			for range runs {
				ordered := names(ordering.Order(c.candidates()))
				position := 0
				for i, rank := range c.ranks {
					if position+len(rank) > len(ordered) {
						t.Fatalf("ordered %v, want ranks %v", ordered, c.ranks)
					}
					got := ordered[position : position+len(rank)]
					if !sameElements(got, rank) {
						t.Fatalf("ordered %v, want ranks %v", ordered, c.ranks)
					}
					firsts[i][got[0]] = true
					position += len(rank)
				}
				if position != len(ordered) {
					t.Fatalf("ordered %v, want ranks %v", ordered, c.ranks)
				}
			}
			for i, rank := range c.ranks {
				for _, name := range rank {
					if !firsts[i][name] {
						t.Errorf("%s never came first among %v in %d runs", name, rank, runs)
					}
				}
			}
		})
	}
}

func sameElements(a, b []string) bool {
	return len(a) == len(b) && slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

// TestOrderingSpreadsSessions places one session after another on the
// first candidate the ordering gives, as session requests do, counting each
// as in flight until the next is placed.
func TestOrderingSpreadsSessions(t *testing.T) {
	cases := []struct {
		name string
		// capacities are the session maximums of the two clusters' portals.
		capacities [2]int64
		// first is how many sessions the second, larger, portal must take
		// before both have as many sessions left.
		first int
	}{
		{name: "#975, equal capacity: the clusters alternate", capacities: [2]int64{3, 3}},
		{name: "#975, unequal capacity: the larger cluster takes sessions until both have as many left, then they alternate",
			capacities: [2]int64{2, 4}, first: 2},
	}

	ordering := placement.NewOrdering()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for range runs / 10 {
				portals := []*inventory.Portal{portal("a", c.capacities[0], 0, 0), portal("b", c.capacities[1], 0, 0)}
				candidates := []*inventory.Environment{
					environment("a", portals[0], new(int64(10)), 0, 0, 0),
					environment("b", portals[1], new(int64(10)), 0, 0, 0),
				}
				var placed []string
				total := int(c.capacities[0] + c.capacities[1])
				for i := range total {
					chosen := ordering.Order(candidates)[0]
					chosen.InFlight++
					chosen.Portal.InFlight++
					placed = append(placed, chosen.Name)

					left := int(portals[0].Capacity) - portals[0].InFlight - (int(portals[1].Capacity) - portals[1].InFlight)
					want := 0
					switch {
					case i < c.first:
						want = c.first - 1 - i
					case (i-c.first)%2 == 0:
						want = 1
					}
					if abs(left) != want {
						t.Fatalf("after %d sessions, placed %v: the portals have %d sessions left between them, want %d",
							i+1, placed, abs(left), want)
					}
				}
				for i := range c.first {
					if placed[i] != "b" {
						t.Fatalf("placed %v: the larger portal must take the first %d", placed, c.first)
					}
				}
				if portals[0].InFlight != int(c.capacities[0]) || portals[1].InFlight != int(c.capacities[1]) {
					t.Fatalf("placed %v: each portal must be filled", placed)
				}
			}
		})
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func ExampleOrdering() {
	p := portal("portal-a", 0, 0, 0)
	ordered := placement.NewOrdering().Order([]*inventory.Environment{
		environment("full", p, new(int64(1)), 1, 0, 0),
		environment("reserved", p, new(int64(2)), 0, 1, 0),
	})
	fmt.Println(names(ordered))
	// Output: [reserved full]
}
