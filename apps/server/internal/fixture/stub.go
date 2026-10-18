//go:build !verifyfixture

// Package fixture omits the verification data set unless the build passes -tags verifyfixture.
package fixture

import (
	"context"
	"errors"
	"flag"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

// Register adds no flag. Only verifyfixture builds advertise --verify-fixture.
func Register(*flag.FlagSet) *bool {
	off := false
	return &off
}

func Apply(string) (string, string, error) {
	return "", "", errors.New("verify fixture is absent from release builds")
}

func Agents(string) []agentruntime.Definition { return nil }

func HoldStream(context.Context, *store.Store) error { return nil }
