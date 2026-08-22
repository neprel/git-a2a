package manifest

type Manifest struct {
	Schema       int            `yaml:"schema" json:"schema"`
	Module       Module         `yaml:"module" json:"module"`
	Agents       []Agent        `yaml:"agents,omitempty" json:"agents,omitempty"`
	Policy       *Policy        `yaml:"policy,omitempty" json:"policy,omitempty"`
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
	Intents       []string       `yaml:"intents" json:"intents"`
	Kind          string         `yaml:"kind" json:"kind"`
	Note          string         `yaml:"note,omitempty" json:"note,omitempty"`
	URL           string         `yaml:"url,omitempty" json:"url,omitempty"`
	Skill         string         `yaml:"skill,omitempty" json:"skill,omitempty"`
	Address       string         `yaml:"address,omitempty" json:"address,omitempty"`
	SubjectPrefix string         `yaml:"subject-prefix,omitempty" json:"subject-prefix,omitempty"`
	Repo          string         `yaml:"repo,omitempty" json:"repo,omitempty"`
	Labels        []string       `yaml:"labels,omitempty" json:"labels,omitempty"`
	Template      string         `yaml:"template,omitempty" json:"template,omitempty"`
	Project       string         `yaml:"project,omitempty" json:"project,omitempty"`
	IssueType     string         `yaml:"issue-type,omitempty" json:"issue-type,omitempty"`
	Channel       string         `yaml:"channel,omitempty" json:"channel,omitempty"`
	Handle        string         `yaml:"handle,omitempty" json:"handle,omitempty"`
	Server        string         `yaml:"server,omitempty" json:"server,omitempty"`
	Extensions    map[string]any `yaml:",inline" json:"-"`
}

type Trust struct {
	Signatures      bool           `yaml:"signatures,omitempty" json:"signatures,omitempty"`
	AcceptsExternal *bool          `yaml:"accepts-external,omitempty" json:"accepts-external,omitempty"`
	Extensions      map[string]any `yaml:",inline" json:"-"`
}

type Policy struct {
	Intents    map[string]string `yaml:"intents,omitempty" json:"intents,omitempty"`
	Consumers  *Consumers        `yaml:"consumers,omitempty" json:"consumers,omitempty"`
	Notes      string            `yaml:"notes,omitempty" json:"notes,omitempty"`
	Extensions map[string]any    `yaml:",inline" json:"-"`
}

type Consumers struct {
	May        []string       `yaml:"may,omitempty" json:"may,omitempty"`
	MayNot     []string       `yaml:"may-not,omitempty" json:"may-not,omitempty"`
	Extensions map[string]any `yaml:",inline" json:"-"`
}

type Dependency struct {
	ID         string         `yaml:"id,omitempty" json:"id,omitempty"`
	Git        string         `yaml:"git" json:"git"`
	Ref        string         `yaml:"ref,omitempty" json:"ref,omitempty"`
	Path       string         `yaml:"path,omitempty" json:"path,omitempty"`
	Track      string         `yaml:"track,omitempty" json:"track,omitempty"`
	Wire       *[]string      `yaml:"wire,omitempty" json:"wire,omitempty"`
	Extensions map[string]any `yaml:",inline" json:"-"`
}

type Lock struct {
	Schema       int                         `yaml:"schema" json:"schema"`
	Dependencies map[string]LockedDependency `yaml:"dependencies" json:"dependencies"`
	Extensions   map[string]any              `yaml:",inline" json:"-"`
}

type LockedDependency struct {
	Git        string            `yaml:"git" json:"git"`
	Ref        string            `yaml:"ref" json:"ref"`
	Path       string            `yaml:"path" json:"path"`
	Commit     string            `yaml:"commit" json:"commit"`
	Manifest   string            `yaml:"manifest" json:"manifest"`
	Cards      map[string]string `yaml:"cards,omitempty" json:"cards,omitempty"`
	Surface    string            `yaml:"surface,omitempty" json:"surface,omitempty"`
	Extensions map[string]any    `yaml:",inline" json:"-"`
}
