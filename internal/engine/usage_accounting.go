package engine

import (
	"context"
	"gemmadesktop.local/app/internal/inference"
	"sync"
)

type usageLedgerKey struct{}
type usageLedger struct {
	mu         sync.Mutex
	usage      inference.Metrics
	accounting *inference.TokenAccounting
}

func newUsageLedger() *usageLedger {
	return &usageLedger{accounting: inference.NewTokenAccounting(0, 0, 0)}
}
func recordObservedUsage(ctx context.Context, usage inference.Metrics, accounting *inference.TokenAccounting) {
	if ledger, ok := ctx.Value(usageLedgerKey{}).(*usageLedger); ok {
		ledger.mu.Lock()
		defer ledger.mu.Unlock()
		ledger.usage = addGenerationUsage(ledger.usage, usage)
		ledger.accounting = inference.MergeTokenAccounting(ledger.accounting, accounting)
	}
}

// Register the attempt before dispatch, so cancellation cannot return a falsely
// complete aggregate while a provider request is still unwinding. A terminal
// receipt is committed immediately in the callback, ahead of payload validation.
func beginProviderUsage(ctx context.Context) func(inference.Metrics, bool, bool) {
	ledger, ok := ctx.Value(usageLedgerKey{}).(*usageLedger)
	if !ok {
		return func(inference.Metrics, bool, bool) {}
	}
	ledger.mu.Lock()
	ledger.accounting = inference.MergeTokenAccounting(ledger.accounting, inference.NewTokenAccounting(1, 0, 0))
	ledger.mu.Unlock()
	var once sync.Once
	return func(usage inference.Metrics, terminal, measured bool) {
		once.Do(func() {
			ledger.mu.Lock()
			defer ledger.mu.Unlock()
			if usage.PromptEvalCount < 0 {
				usage.PromptEvalCount = 0
			}
			if usage.EvalCount < 0 {
				usage.EvalCount = 0
			}
			ledger.usage = addGenerationUsage(ledger.usage, usage)
			a := ledger.accounting
			t, n := 0, 0
			if terminal {
				t = 1
			}
			if measured {
				n = 1
			}
			out := inference.NewTokenAccounting(a.Attempts, a.TerminalResponses+t, a.MeasuredResponses+n)
			if a.UnknownContributions > 0 {
				unknown := inference.UnknownTokenAccounting()
				unknown.UnknownContributions = a.UnknownContributions
				out = inference.MergeTokenAccounting(out, unknown)
			}
			ledger.accounting = out
		})
	}
}
