package router

// BudgetManager tracks per-workflow budget and triggers tier downgrades
// when remaining budget falls below configurable thresholds.
type BudgetManager struct {
	enabled       bool
	defaultBudget float64
}

// NewBudgetManager creates a BudgetManager with the given enable flag and default budget.
func NewBudgetManager(enabled bool, defaultBudget float64) *BudgetManager {
	return &BudgetManager{
		enabled:       enabled,
		defaultBudget: defaultBudget,
	}
}

// IsEnabled reports whether budget-based routing is active.
func (b *BudgetManager) IsEnabled() bool {
	return b.enabled
}

// DefaultBudget returns the default per-workflow budget amount.
func (b *BudgetManager) DefaultBudget() float64 {
	return b.defaultBudget
}

// ShouldDowngrade returns true when the remaining budget ratio is below 15%,
// indicating the router should prefer cheaper model tiers.
func (b *BudgetManager) ShouldDowngrade(budgetLeft float64, totalBudget float64) bool {
	if !b.enabled || totalBudget <= 0 {
		return false
	}
	ratio := budgetLeft / totalBudget
	return ratio < 0.15
}
