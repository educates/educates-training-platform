package restapi

// WorkshopCatalog
// --------------------------------------------

type WorkshopsCatalogResponse struct {
	Portal       PortalDetails        `json:"portal"`
	Environments []EnvironmentDetails `json:"environments"`
}

type PortalDetails struct {
	Name string `json:"name"`
	// Labels     []string         `json:"labels"`
	UID        string         `json:"uid"`
	Generation int64          `json:"generation"`
	URL        string         `json:"url"`
	Sessions   SessionDetails `json:"sessions"`
}

type SessionDetails struct {
	Maximum    int64 `json:"maximum"`
	Registered int64 `json:"registered"`
	Anonymous  int64 `json:"anonymous"`
	Allocated  int64 `json:"allocated"`
}

type WorkshopDetails struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Vendor      string   `json:"vendor"`
	Authors     []string `json:"authors"`
	Difficulty  string   `json:"difficulty"`
	Duration    string   `json:"duration"`
	Tags        []string `json:"tags"`
	// Labels      []string `json:"labels"`
	Logo string `json:"logo"`
	URL  string `json:"url"`
}

type EnvironmentDetails struct {
	Name      string          `json:"name"`
	State     string          `json:"state"`
	Duration  int64           `json:"duration"`
	Capacity  int64           `json:"capacity"`
	Reserved  int64           `json:"reserved"`
	Allocated int64           `json:"allocated"`
	Available int64           `json:"available"`
	Workshop  WorkshopDetails `json:"workshop"`
}

// RequestWorkshop
// --------------------------------------------
type RequestWorkshopRequest struct {
	Parameters []Parameter `json:"parameters"`
}

type Parameter struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type RequestWorkshopResponse struct {
	Name        string `json:"name"`
	User        string `json:"user"`
	URL         string `json:"url"`
	Workshop    string `json:"workshop"`
	Environment string `json:"environment"`
	Namespace   string `json:"namespace"`
}

// WorkshopSessionDetails
// --------------------------------------------

type WorkshopSessionDetails struct {
	Started    string `json:"started"`
	Expires    string `json:"expires"`
	Expiring   bool   `json:"expiring"`
	Countdown  int    `json:"countdown"`
	Extendable bool   `json:"extendable"`
	Status     string `json:"status"`
}
