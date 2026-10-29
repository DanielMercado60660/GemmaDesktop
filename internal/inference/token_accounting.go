package inference

// TokenAccounting describes observed provider receipts, independently of whether
// the solver completed its task. Nil/legacy data is never evidence of completeness.
type TokenAccounting struct {
	Version              int    `json:"version"`
	Source               string `json:"source"`
	Status               string `json:"status"`
	Attempts             int    `json:"attempts"`
	TerminalResponses    int    `json:"terminalResponses"`
	MeasuredResponses    int    `json:"measuredResponses"`
	UnknownContributions int    `json:"unknownContributions"`
}

const tokenAccountingSource = "provider_terminal_counts"

func NewTokenAccounting(attempts, terminal, measured int) *TokenAccounting {
	a := &TokenAccounting{Version: 1, Source: tokenAccountingSource, Attempts: attempts, TerminalResponses: terminal, MeasuredResponses: measured}
	a.Status = a.status()
	return a
}
func (a *TokenAccounting) status() string {
	if a.UnknownContributions == 0 && a.Attempts > 0 && a.Attempts == a.MeasuredResponses {
		return "complete"
	}
	if a.MeasuredResponses > 0 {
		return "partial"
	}
	return "unknown"
}
func (a *TokenAccounting) Valid() bool {
	return a != nil && a.Version == 1 && a.Source == tokenAccountingSource && a.Attempts >= 0 && a.TerminalResponses >= 0 && a.MeasuredResponses >= 0 && a.UnknownContributions >= 0 && a.MeasuredResponses <= a.TerminalResponses && a.TerminalResponses <= a.Attempts && a.Status == a.status()
}
func (a *TokenAccounting) Complete() bool {
	return a.Valid() && a.Attempts > 0 && a.Status == "complete"
}
func UnknownTokenAccounting() *TokenAccounting {
	a := NewTokenAccounting(0, 0, 0)
	a.UnknownContributions = 1
	a.Status = "unknown"
	return a
}
func MergeTokenAccounting(a, b *TokenAccounting) *TokenAccounting {
	if !a.Valid() {
		a = UnknownTokenAccounting()
	}
	if !b.Valid() {
		b = UnknownTokenAccounting()
	}
	// Invalid imported counters must not wrap back into an apparently complete
	// aggregate. Zero-attempt ledgers remain a known empty merge identity.
	max := int(^uint(0) >> 1)
	if b.Attempts > max-a.Attempts || b.TerminalResponses > max-a.TerminalResponses || b.MeasuredResponses > max-a.MeasuredResponses || b.UnknownContributions > max-a.UnknownContributions {
		return UnknownTokenAccounting()
	}
	out := NewTokenAccounting(a.Attempts+b.Attempts, a.TerminalResponses+b.TerminalResponses, a.MeasuredResponses+b.MeasuredResponses)
	out.UnknownContributions = a.UnknownContributions + b.UnknownContributions
	out.Status = out.status()
	return out
}
