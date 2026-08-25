package manifest

import "encoding/json"

type Manifest struct {
	Schema       int            `yaml:"schema" json:"schema"`
	Module       Module         `yaml:"module" json:"module"`
	Agents       []Agent        `yaml:"agents,omitempty" json:"agents,omitempty"`
	Policy       *Policy        `yaml:"policy,omitempty" json:"policy,omitempty"`
	Settings     *Settings      `yaml:"settings,omitempty" json:"settings,omitempty"`
	Dependencies []Dependency   `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
	Extensions   map[string]any `yaml:",inline" json:"-"`
}

type Module struct {
	ID          string         `yaml:"id" json:"id"`
	Name        string         `yaml:"name,omitempty" json:"name,omitempty"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Languages   []string       `yaml:"languages,omitempty" json:"languages,omitempty"`
	Surface     string         `yaml:"surface,omitempty" json:"surface,omitempty"`
	Repository  string         `yaml:"repository,omitempty" json:"repository,omitempty"`
	MovedTo     *MovedTo       `yaml:"moved-to,omitempty" json:"moved-to,omitempty"`
	Docs        string         `yaml:"docs,omitempty" json:"docs,omitempty"`
	Release     *Release       `yaml:"release,omitempty" json:"release,omitempty"`
	Exports     []Export       `yaml:"exports,omitempty" json:"exports,omitempty"`
	Extensions  map[string]any `yaml:",inline" json:"-"`
}

// MovedTo is the owner's announcement, left at the old location, that the module now lives
// elsewhere. Consumers are told on update and follow it only on explicit request.
type MovedTo struct {
	Git        string         `yaml:"git" json:"git"`
	Path       string         `yaml:"path,omitempty" json:"path,omitempty"`
	Notes      string         `yaml:"notes,omitempty" json:"notes,omitempty"`
	Extensions map[string]any `yaml:",inline" json:"-"`
}

type Release struct {
	Channel    string         `yaml:"channel,omitempty" json:"channel,omitempty"`
	Tags       bool           `yaml:"tags,omitempty" json:"tags,omitempty"`
	Notes      string         `yaml:"notes,omitempty" json:"notes,omitempty"`
	Extensions map[string]any `yaml:",inline" json:"-"`
}

type Export struct {
	Ecosystem  string         `yaml:"ecosystem" json:"ecosystem"`
	Name       string         `yaml:"name" json:"name"`
	Path       string         `yaml:"path,omitempty" json:"path,omitempty"`
	Notes      string         `yaml:"notes,omitempty" json:"notes,omitempty"`
	Extensions map[string]any `yaml:",inline" json:"-"`
}

type Agent struct {
	Name        string         `yaml:"name" json:"name"`
	Role        string         `yaml:"role" json:"role"`
	Scope       []string       `yaml:"scope,omitempty" json:"scope,omitempty"`
	Card        string         `yaml:"card,omitempty" json:"card,omitempty"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Contacts    []Contact      `yaml:"contacts,omitempty" json:"contacts,omitempty"`
	Trust       *Trust         `yaml:"trust,omitempty" json:"trust,omitempty"`
	Extensions  map[string]any `yaml:",inline" json:"-"`
}

type Contact struct {
	Intents       []string          `yaml:"intents" json:"intents"`
	Kind          string            `yaml:"kind" json:"kind"`
	Note          string            `yaml:"note,omitempty" json:"note,omitempty"`
	URL           string            `yaml:"url,omitempty" json:"url,omitempty"`
	Skill         string            `yaml:"skill,omitempty" json:"skill,omitempty"`
	Address       string            `yaml:"address,omitempty" json:"address,omitempty"`
	SubjectPrefix string            `yaml:"subject-prefix,omitempty" json:"subject-prefix,omitempty"`
	Repo          string            `yaml:"repo,omitempty" json:"repo,omitempty"`
	Labels        []string          `yaml:"labels,omitempty" json:"labels,omitempty"`
	Template      string            `yaml:"template,omitempty" json:"template,omitempty"`
	Project       string            `yaml:"project,omitempty" json:"project,omitempty"`
	Organization  string            `yaml:"organization,omitempty" json:"organization,omitempty"`
	IssueType     string            `yaml:"issue-type,omitempty" json:"issue-type,omitempty"`
	Channel       string            `yaml:"channel,omitempty" json:"channel,omitempty"`
	Handle        string            `yaml:"handle,omitempty" json:"handle,omitempty"`
	Server        string            `yaml:"server,omitempty" json:"server,omitempty"`
	Method        string            `yaml:"method,omitempty" json:"method,omitempty"`
	Headers       map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	ContentType   string            `yaml:"content-type,omitempty" json:"content-type,omitempty"`
	Body          string            `yaml:"body,omitempty" json:"body,omitempty"`
	Command       []string          `yaml:"command,omitempty" json:"command,omitempty"`
	Args          []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Stdin         string            `yaml:"stdin,omitempty" json:"stdin,omitempty"`
	Extensions    map[string]any    `yaml:",inline" json:"-"`
}

func (c Contact) MarshalJSON() ([]byte, error) {
	type contact Contact
	base, err := json.Marshal(contact(c))
	if err != nil {
		return nil, err
	}
	var values map[string]any
	if err := json.Unmarshal(base, &values); err != nil {
		return nil, err
	}
	for key, value := range c.Extensions {
		if _, exists := values[key]; !exists {
			values[key] = value
		}
	}
	return json.Marshal(values)
}

type Trust struct {
	Signatures      bool           `yaml:"signatures,omitempty" json:"signatures,omitempty"`
	AcceptsExternal *bool          `yaml:"accepts-external,omitempty" json:"accepts-external,omitempty"`
	JWKS            []string       `yaml:"jwks,omitempty" json:"jwks,omitempty"`
	Keys            []string       `yaml:"keys,omitempty" json:"keys,omitempty"`
	Origins         []string       `yaml:"origins,omitempty" json:"origins,omitempty"`
	JWKSMaxAge      string         `yaml:"jwks-max-age,omitempty" json:"jwks-max-age,omitempty"`
	Extensions      map[string]any `yaml:",inline" json:"-"`
}

type Policy struct {
	Intents       map[string]string `yaml:"intents,omitempty" json:"intents,omitempty"`
	Consumers     *Consumers        `yaml:"consumers,omitempty" json:"consumers,omitempty"`
	ContactBudget map[string]string `yaml:"contact-budget,omitempty" json:"contact-budget,omitempty"`
	Notes         string            `yaml:"notes,omitempty" json:"notes,omitempty"`
	Extensions    map[string]any    `yaml:",inline" json:"-"`
}

type Consumers struct {
	May        []string       `yaml:"may,omitempty" json:"may,omitempty"`
	MayNot     []string       `yaml:"may-not,omitempty" json:"may-not,omitempty"`
	Extensions map[string]any `yaml:",inline" json:"-"`
}

type Settings struct {
	VendorDir    string           `yaml:"vendor-dir,omitempty" json:"vendor-dir,omitempty"`
	SyncTargets  []string         `yaml:"sync-targets,omitempty" json:"sync-targets,omitempty"`
	Organisation []string         `yaml:"organisation,omitempty" json:"organisation,omitempty"`
	Contact      *ContactSettings `yaml:"contact,omitempty" json:"contact,omitempty"`
	Extensions   map[string]any   `yaml:",inline" json:"-"`
}

type ContactSettings struct {
	AllowHTTP  []string       `yaml:"allow-http,omitempty" json:"allow-http,omitempty"`
	AllowExec  []string       `yaml:"allow-exec,omitempty" json:"allow-exec,omitempty"`
	Extensions map[string]any `yaml:",inline" json:"-"`
}

type Vendor struct {
	Mode       string         `yaml:"mode,omitempty" json:"mode,omitempty"`
	Path       string         `yaml:"path,omitempty" json:"path,omitempty"`
	Recursive  bool           `yaml:"recursive,omitempty" json:"recursive,omitempty"`
	Extensions map[string]any `yaml:",inline" json:"-"`
}

type Dependency struct {
	ID         string         `yaml:"id,omitempty" json:"id,omitempty"`
	Git        string         `yaml:"git" json:"git"`
	Ref        string         `yaml:"ref,omitempty" json:"ref,omitempty"`
	Path       string         `yaml:"path,omitempty" json:"path,omitempty"`
	Track      string         `yaml:"track,omitempty" json:"track,omitempty"`
	Wire       *[]string      `yaml:"wire,omitempty" json:"wire,omitempty"`
	Vendor     *Vendor        `yaml:"vendor,omitempty" json:"vendor,omitempty"`
	Require    *Require       `yaml:"require,omitempty" json:"require,omitempty"`
	Extensions map[string]any `yaml:",inline" json:"-"`
}

type Require struct {
	Commits    string         `yaml:"commits,omitempty" json:"commits,omitempty"`
	Signers    string         `yaml:"signers,omitempty" json:"signers,omitempty"`
	Cards      string         `yaml:"cards,omitempty" json:"cards,omitempty"`
	CardOrigin bool           `yaml:"card-origin,omitempty" json:"card-origin,omitempty"`
	Extensions map[string]any `yaml:",inline" json:"-"`
}

type Lock struct {
	Schema       int                         `yaml:"schema" json:"schema"`
	Dependencies map[string]LockedDependency `yaml:"dependencies" json:"dependencies"`
	Extensions   map[string]any              `yaml:",inline" json:"-"`
}

type LockedDependency struct {
	Git        string                   `yaml:"git" json:"git"`
	Ref        string                   `yaml:"ref" json:"ref"`
	Path       string                   `yaml:"path" json:"path"`
	Commit     string                   `yaml:"commit" json:"commit"`
	Manifest   string                   `yaml:"manifest" json:"manifest"`
	Cards      map[string]string        `yaml:"cards,omitempty" json:"cards,omitempty"`
	CardsKeys  map[string]LockedCardKey `yaml:"cards-keys,omitempty" json:"cards-keys,omitempty"`
	Verified   string                   `yaml:"verified,omitempty" json:"verified,omitempty"`
	Surface    string                   `yaml:"surface,omitempty" json:"surface,omitempty"`
	Vendor     *LockedVendor            `yaml:"vendor,omitempty" json:"vendor,omitempty"`
	Extensions map[string]any           `yaml:",inline" json:"-"`
}

type LockedCardKey struct {
	KeyID      string         `yaml:"kid" json:"kid"`
	Thumbprint string         `yaml:"thumbprint" json:"thumbprint"`
	JWKS       string         `yaml:"jwks,omitempty" json:"jwks,omitempty"`
	FirstSeen  string         `yaml:"first-seen" json:"first-seen"`
	Extensions map[string]any `yaml:",inline" json:"-"`
}

type LockedVendor struct {
	Mode       string         `yaml:"mode" json:"mode"`
	Path       string         `yaml:"path" json:"path"`
	Tree       string         `yaml:"tree,omitempty" json:"tree,omitempty"`
	Extensions map[string]any `yaml:",inline" json:"-"`
}
