package adapters

import (
	"github.com/neprel/git-a2a/adapters/golang"
	"github.com/neprel/git-a2a/adapters/npm"
	"github.com/neprel/git-a2a/adapters/pypi"
	"github.com/neprel/git-a2a/internal/adapter"
)

func All() []adapter.Adapter {
	return []adapter.Adapter{npm.Adapter{}, pypi.Adapter{}, golang.Adapter{}}
}
