// File: internal/rules/rules_test.go
// Purpose: Unit tests for rules engine
// Package: github.com/rsharma155/sqlplan-analyzer/internal/rules
package rules

import (
	"testing"

	"github.com/rsharma155/sqlplan-analyzer/models"
)

func TestTableScanRule(t *testing.T) {
	rule := NewTableScanRule()

	plan := &models.PlanAnalysis{
		Operators: []models.Operator{
			{
				ID:        1,
				PhysicalOp: "Table Scan",
				TableScan: &models.TableScan{
					Object: models.TableObject{
						Table: "Orders",
					},
				},
				EstimatedTotalSubtreeCost: 0.5,
			},
		},
	}

	findings := rule.Evaluate(plan)
	if len(findings) == 0 {
		t.Error("Expected to find a table scan finding")
	}

	if findings[0].FindingType != "TableScan" {
		t.Errorf("Expected FindingType TableScan, got %s", findings[0].FindingType)
	}
}

func TestIndexScanRule(t *testing.T) {
	rule := NewIndexScanRule()

	plan := &models.PlanAnalysis{
		Operators: []models.Operator{
			{
				ID:         1,
				PhysicalOp: "Index Scan",
				IndexScan: &models.IndexScan{
					Object: models.IndexObject{
						Table: "Customers",
					},
				},
				EstimatedTotalSubtreeCost: 0.3,
			},
		},
	}

	findings := rule.Evaluate(plan)
	if len(findings) == 0 {
		t.Error("Expected to find index scan")
	}
}

func TestKeyLookupRule(t *testing.T) {
	rule := NewKeyLookupRule()

	plan := &models.PlanAnalysis{
		Operators: []models.Operator{
			{
				ID:                   1,
				PhysicalOp:           "Clustered Index Seek",
				EstimatedTotalSubtreeCost: 0.8,
			},
		},
	}

	findings := rule.Evaluate(plan)
	if len(findings) == 0 {
		t.Error("Expected to find key lookup")
	}
}

func TestSpillRule(t *testing.T) {
	rule := NewSpillRule()

	plan := &models.PlanAnalysis{
		Warnings: []models.Warning{
			{
				Type:     models.WarningTypeSpillToTempDB,
				Severity: models.SeverityHigh,
				Message:  "Operation spilled to tempdb",
			},
		},
		Operators: []models.Operator{
			{
				ID:              1,
				PhysicalOp:     "Sort",
				ActualSpills:    1000,
			},
		},
	}

	findings := rule.Evaluate(plan)
	if len(findings) == 0 {
		t.Error("Expected to find spill warning")
	}
}

func TestImplicitConversionRule(t *testing.T) {
	rule := NewImplicitConversionRule()

	plan := &models.PlanAnalysis{
		Warnings: []models.Warning{
			{
				Type:     models.WarningTypeTypeConversion,
				Severity: models.SeverityMedium,
				Message:  "Implicit type conversion",
			},
		},
	}

	findings := rule.Evaluate(plan)
	if len(findings) == 0 {
		t.Error("Expected to find implicit conversion")
	}
}

func TestMissingIndexRule(t *testing.T) {
	rule := NewMissingIndexRule()

	plan := &models.PlanAnalysis{
		MissingIndexes: []models.MissingIndex{
			{
				ID:       1,
				Database: "AdventureWorks",
				Table:    "Orders",
			},
		},
	}

	findings := rule.Evaluate(plan)
	if len(findings) == 0 {
		t.Error("Expected to find missing index")
	}
}

func TestParallelismSkewRule(t *testing.T) {
	rule := NewParallelismSkewRule()

	plan := &models.PlanAnalysis{
		QueryPlan: &models.QueryPlan{
			DegreeOfParallelism: 4,
		},
		Operators: []models.Operator{
			{
				ID:         1,
				Parallel:  true,
				ActualRows: 1000,
			},
			{
				ID:         2,
				Parallel:  true,
				ActualRows: 10000,
			},
		},
	}

	findings := rule.Evaluate(plan)
	_ = findings
}

func TestCardinalityEstimationRule(t *testing.T) {
	rule := NewCardinalityEstimationRule()

	plan := &models.PlanAnalysis{
		Operators: []models.Operator{
			{
				ID:            1,
				PhysicalOp:    "Table Scan",
				EstimateRows:  1,
				ActualRows:    100000,
				EstimatedTotalSubtreeCost: 0.5,
			},
		},
	}

	findings := rule.Evaluate(plan)
	_ = findings
}

func TestEngineRegister(t *testing.T) {
	registry := NewRegistry()

	rule := NewTableScanRule()
	registry.Register(rule)

	got, ok := registry.Get(rule.Name())
	if !ok {
		t.Error("Expected rule to be registered")
	}

	if got.Name() != rule.Name() {
		t.Errorf("Expected %s, got %s", rule.Name(), got.Name())
	}
}

func TestEngineEvaluateAll(t *testing.T) {
	engine := NewEngine()

	plan := &models.PlanAnalysis{
		Operators: []models.Operator{
			{
				ID:        1,
				PhysicalOp: "Table Scan",
				TableScan: &models.TableScan{
					Object: models.TableObject{
						Table: "Orders",
					},
				},
			},
		},
		Warnings: []models.Warning{},
	}

	findings := engine.Evaluate(plan)
	if len(findings) == 0 {
		t.Error("Expected findings from rule engine")
	}
}

func TestRuleNameAndDescription(t *testing.T) {
	rule := NewTableScanRule()

	if rule.Name() != "TableScanDetection" {
		t.Errorf("Expected TableScanDetection, got %s", rule.Name())
	}

	if rule.Description() == "" {
		t.Error("Expected non-empty description")
	}
}

func BenchmarkTableScanRule(b *testing.B) {
	rule := NewTableScanRule()

	plan := &models.PlanAnalysis{
		Operators: make([]models.Operator, 100),
	}

	for i := 0; i < 100; i++ {
		plan.Operators[i] = models.Operator{
			ID:        i,
			PhysicalOp: "Table Scan",
			TableScan: &models.TableScan{
				Object: models.TableObject{
					Table: "Table",
				},
			},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rule.Evaluate(plan)
	}
}
