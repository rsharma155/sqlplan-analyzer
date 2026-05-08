package analyzer

import (
	"os"
	"testing"

	"github.com/rsharma155/sqlplan-analyzer/models"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.EnableRules {
		t.Error("expected rules enabled by default")
	}
	if !cfg.EnableScoring {
		t.Error("expected scoring enabled by default")
	}
	if !cfg.EnableNarrative {
		t.Error("expected narrative enabled by default")
	}
	if !cfg.EnableCostAnalysis {
		t.Error("expected cost analysis enabled by default")
	}
	if cfg.MaxOperators != 1000 {
		t.Errorf("expected MaxOperators=1000, got %d", cfg.MaxOperators)
	}
}

func TestAnalyzeEmptyPlan(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableRules = false
	a := New(cfg)

	plan := &models.PlanAnalysis{
		Operators: []models.Operator{},
		QueryPlan: &models.QueryPlan{
			DegreeOfParallelism: 1,
		},
	}

	result := a.Analyze(plan)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Timestamp.IsZero() {
		t.Error("expected timestamp to be set")
	}
}

func TestAnalyzeWithFindings(t *testing.T) {
	cfg := DefaultConfig()
	a := New(cfg)

	plan := &models.PlanAnalysis{
		Metadata: models.QueryMetadata{
			QueryText: "SELECT * FROM Orders",
		},
		Operators: []models.Operator{
			{
				ID:                       1,
				PhysicalOp:               "Table Scan",
				LogicalOp:                "Table Scan",
				EstimatedTotalSubtreeCost: 10.5,
				EstimateRows:             10000,
				ActualRows:               5000,
				ActualExecutions:         1,
				TableScan: &models.TableScan{
					Object: models.TableObject{
						Database: "TestDB",
						Schema:  "dbo",
						Table:   "Orders",
					},
				},
			},
		},
		QueryPlan: &models.QueryPlan{
			DegreeOfParallelism: 1,
		},
	}

	result := a.Analyze(plan)
	if len(result.Findings) == 0 {
		t.Log("no findings generated (acceptable for default rules)")
	}
	if result.HealthScore.OverallScore < 0 || result.HealthScore.OverallScore > 100 {
		t.Errorf("HealthScore out of range: %d", result.HealthScore.OverallScore)
	}
}

func TestAnalyzeWithMemoryGrant(t *testing.T) {
	cfg := DefaultConfig()
	a := New(cfg)

	plan := &models.PlanAnalysis{
		Operators: []models.Operator{
			{
				ID:         1,
				PhysicalOp: "Sort",
				ActualRows: 1000,
				ActualExecutions: 1,
				EstimatedTotalSubtreeCost: 0.5,
				Sort: &models.SortInfo{
					EstimateRows: 1000,
				},
			},
		},
		QueryPlan: &models.QueryPlan{
			DegreeOfParallelism: 4,
			HasMemoryGrant:      true,
			MemoryGrantInfo: models.MemoryGrantInfo{
				GrantedMemory: 65536,
				MaxUsedMemory: 49152,
			},
		},
	}

	result := a.Analyze(plan)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestDeduplicateFindingsByGroup(t *testing.T) {
	findings := []models.Finding{
		{
			FindingType:  "TableScanDetection",
			Title:        "Full Table Scan on Orders",
			OperatorID:   1,
			OperatorName: "Table Scan",
			Recommendation: "Create index on Orders",
			EstimatedCost: 10.5,
			Confidence:   0.8,
		},
		{
			FindingType:  "TableScanDetection",
			Title:        "Full Table Scan on Orders",
			OperatorID:   2,
			OperatorName: "Table Scan",
			Recommendation: "Create index on Orders",
			EstimatedCost: 5.2,
			Confidence:   0.6,
		},
	}

	result := deduplicateFindingsByGroup(findings)
	if len(result) != 1 {
		t.Fatalf("expected 1 finding after dedup, got %d", len(result))
	}

	if len(result[0].OperatorIDs) != 2 {
		t.Errorf("expected 2 operator IDs, got %d", len(result[0].OperatorIDs))
	}

	if result[0].EstimatedCost != 10.5 {
		t.Errorf("expected max cost 10.5, got %f", result[0].EstimatedCost)
	}
}

func TestRemoveWeakFindings(t *testing.T) {
	findings := []models.Finding{
		{
			Title:       "Full Table Scan on Orders",
			FindingType: "TableScanDetection",
		},
		{
			Title:       "Generic operator education about scans",
			FindingType: "operator_education",
		},
		{
			Title:       "Serial execution detected for complex query",
			FindingType: "NoParallelismAvailable",
		},
		{
			Title:       "Missing Index on Customers",
			FindingType: "MissingIndexRecommendation",
		},
		{
			Title:       "Hash Match Join",
			FindingType: "JoinDetection",
		},
	}

	result := removeWeakFindings(findings)
	if len(result) != 2 {
		t.Fatalf("expected 2 findings after weak removal, got %d: %v", len(result), getTitles(result))
	}
}

func getTitles(findings []models.Finding) []string {
	titles := make([]string, len(findings))
	for i, f := range findings {
		titles[i] = f.Title
	}
	return titles
}

func TestAnalyzeRealFile(t *testing.T) {
	testFile := "../../examples/exec_plan1.sqlplan"
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Skipf("skipping: cannot read %s", testFile)
	}

	cfg := DefaultConfig()
	a := New(cfg)

	plan, err := a.ParseFile(testFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	plan = a.Analyze(plan)
	if len(plan.Operators) == 0 {
		t.Error("expected at least one operator")
	}
	if plan.HealthScore.OverallScore < 0 || plan.HealthScore.OverallScore > 100 {
		t.Errorf("HealthScore out of range: %d", plan.HealthScore.OverallScore)
	}
}

func TestAnalyzeComplexPlanFile(t *testing.T) {
	testFile := "../../examples/exec_plan2.sqlplan"
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Skipf("skipping: cannot read %s", testFile)
	}

	cfg := DefaultConfig()
	a := New(cfg)

	plan, err := a.ParseFile(testFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	plan = a.Analyze(plan)
	if len(plan.Operators) == 0 {
		t.Error("expected at least one operator")
	}
}
