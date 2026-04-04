package router

type BudgetManager struct {
	enabled       bool
	defaultBudget float64
}

func NewBudgetManager(enabled bool, defaultBudget float64) *BudgetManager {
	return &BudgetManager{
		enabled:       enabled,
		defaultBudget: defaultBudget,
	}
}

func (b *BudgetManager) IsEnabled() bool {
	return b.enabled
}

func (b *BudgetManager) DefaultBudget() float64 {
	return b.defaultBudget
}

func (b *BudgetManager) ShouldDowngrade(budgetLeft float64, totalBudget float64) bool {
	if !b.enabled || totalBudget <= 0 {
		return false
	}
	ratio := budgetLeft / totalBudget
	return ratio < 0.15
}
