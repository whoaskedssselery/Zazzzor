package auth

import "testing"

// UseProductionCost hashes new passwords at the server's own cost until t
// ends. Not for parallel tests: the cost is the whole binary's.
func UseProductionCost(t testing.TB) {
	old := currentParams
	currentParams = productionParams
	t.Cleanup(func() { currentParams = old })
}
