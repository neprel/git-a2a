package manifest

// Manifest is the schema 2 repository declaration. It contains one component,
// one responsible external agent, and direct dependencies only.
type Manifest struct {
	Schema       int          `yaml:"schema" json:"schema"`
	Component    Component    `yaml:"component" json:"component"`
	Agent        *Agent       `yaml:"agent,omitempty" json:"agent,omitempty"`
	Dependencies []Dependency `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
}

type Component struct {
	ID          string   `yaml:"id" json:"id"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Repository  string   `yaml:"repository,omitempty" json:"repository,omitempty"`
	Exports     []Export `yaml:"exports,omitempty" json:"exports,omitempty"`
	Surface     string   `yaml:"surface,omitempty" json:"surface,omitempty"`
}

type Export struct {
	Adapter  string `yaml:"adapter" json:"adapter"`
	Name     string `yaml:"name" json:"name"`
	Path     string `yaml:"path,omitempty" json:"path,omitempty"`
	Checksum string `yaml:"checksum,omitempty" json:"checksum,omitempty"`
}

type Agent struct {
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	Card string `yaml:"card" json:"card"`
}

// LockedAgent records both the upstream declaration and the reference a local
// consumer can actually use. For repository-relative cards, Card points below
// .git-a2a while Commit identifies the Git object the bytes came from.
type LockedAgent struct {
	Name         string `yaml:"name,omitempty" json:"name,omitempty"`
	DeclaredCard string `yaml:"declaredCard" json:"declaredCard"`
	Card         string `yaml:"card" json:"card"`
	Commit       string `yaml:"commit" json:"commit"`
}

// Dependency.Name is the consumer-local alias. Component identity is learned
// from the upstream manifest and is never used as the local map key.
type Dependency struct {
	Name     string    `yaml:"name" json:"name"`
	Git      string    `yaml:"git" json:"git"`
	Ref      string    `yaml:"ref,omitempty" json:"ref,omitempty"`
	Path     string    `yaml:"path,omitempty" json:"path,omitempty"`
	Bindings []Binding `yaml:"bindings,omitempty" json:"bindings,omitempty"`
}

type Binding struct {
	Adapter string `yaml:"adapter" json:"adapter"`
	Variant string `yaml:"variant" json:"variant"`
	Export  string `yaml:"export,omitempty" json:"export,omitempty"`
	Path    string `yaml:"path,omitempty" json:"path,omitempty"`
}

type Lock struct {
	Schema       int                         `yaml:"schema" json:"schema"`
	Dependencies map[string]LockedDependency `yaml:"dependencies" json:"dependencies"`
}

type LockedDependency struct {
	Component string      `yaml:"component" json:"component"`
	Git       string      `yaml:"git" json:"git"`
	Ref       string      `yaml:"ref" json:"ref"`
	Path      string      `yaml:"path,omitempty" json:"path,omitempty"`
	Commit    string      `yaml:"commit" json:"commit"`
	Manifest  string      `yaml:"manifest" json:"manifest"`
	Bindings  []Binding   `yaml:"bindings" json:"bindings"`
	Agent     LockedAgent `yaml:"agent" json:"agent"`
	Surface   string      `yaml:"surface,omitempty" json:"surface,omitempty"`
}
