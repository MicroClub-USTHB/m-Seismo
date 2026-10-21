package main

import (
	"testing"

	"go.uber.org/fx"
)

// TestGraph checks that every dependency in the app graph is provided,
// without running constructors (so no database, Redis or NATS is needed).
func TestGraph(t *testing.T) {
	if err := fx.ValidateApp(Options()); err != nil {
		t.Fatal(err)
	}
}
