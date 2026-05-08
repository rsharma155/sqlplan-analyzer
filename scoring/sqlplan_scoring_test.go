package scoring

import (
	"testing"

	"github.com/rsharma155/sqlplan-analyzer/models"
)

func TestCalculateHealthScore(t *testing.T) {
	calc := NewCalculator()

	plan := &models.PlanAnalysis{
		Warnings: []models.Warning{},
		Findings: []models.Finding{},
		QueryPlan: &models.QueryPlan{
			DegreeOfParallelism: 1,
			HasMemoryGrant:     true,
			MemoryGrantInfo: models.MemoryGrantInfo{
				GrantedMemory: 65536,
				MaxUsedMemory: 16384,
			},
		},
		Operators: []models.Operator{
			{
				PhysicalOp: "Clustered Index Seek",
				IndexScan: &models.IndexScan{
					ScanType: "Seek",
				},
			},
		},
	}

	score := calc.Calculate(plan)

	if score.OverallScore < 0 || score.OverallScore > 100 {
		t.Errorf("OverallScore out of range [0,100]: %d", score.OverallScore)
	}

	if score.AccessMethodsScore < 0 || score.AccessMethodsScore > 30 {
		t.Errorf("AccessMethodsScore out of range [0,30]: %d", score.AccessMethodsScore)
	}
}

func TestCalculateHealthScoreOptimalPlan(t *testing.T) {
	calc := NewCalculator()

	plan := &models.PlanAnalysis{
		Operators: []models.Operator{
			{
				PhysicalOp:                "Clustered Index Seek",
				ActualRows:               100,
				ActualExecutions:         1,
				EstimatedTotalSubtreeCost: 0.1,
				IndexScan: &models.IndexScan{
					ScanType: "Seek",
					Object:   models.IndexObject{},
				},
			},
		},
		QueryPlan: &models.QueryPlan{
			DegreeOfParallelism: 4,
			HasMemoryGrant:     true,
			MemoryGrantInfo: models.MemoryGrantInfo{
				GrantedMemory: 65536,
				MaxUsedMemory: 32768,
			},
		},
		MissingIndexes: []models.MissingIndex{},
		Warnings:       []models.Warning{},
		Findings:       []models.Finding{},
	}

	score := calc.Calculate(plan)

	if score.OverallScore < 70 {
		t.Errorf("Optimal plan should score >=70, got %d", score.OverallScore)
	}
}

func TestCalculateWithSpillWarning(t *testing.T) {
	calc := NewCalculator()

	plan := &models.PlanAnalysis{
		Operators: []models.Operator{
			{
				PhysicalOp:                "Hash Match",
				ActualRows:               5000,
				ActualExecutions:         1,
				EstimatedTotalSubtreeCost: 0.5,
				Hash: &models.HashMatch{},
			},
		},
		QueryPlan: &models.QueryPlan{
			DegreeOfParallelism: 1,
		},
		Warnings: []models.Warning{
			{Type: models.WarningTypeSpillToTempDB},
		},
		Findings: []models.Finding{
			{Category: "Memory", Severity: models.SeverityHigh},
		},
	}

	score := calc.Calculate(plan)

	if score.MemoryUsageScore > 15 {
		t.Errorf("MemoryUsageScore should be penalized for spill, got %d", score.MemoryUsageScore)
	}
}

func TestCalculateCardinalityIssues(t *testing.T) {
	calc := NewCalculator()

	plan := &models.PlanAnalysis{
		Operators: []models.Operator{
			{
				PhysicalOp:                "Index Seek",
				ActualRows:               1000,
				ActualExecutions:         1,
				EstimatedTotalSubtreeCost: 0.5,
				EstimateRows:             10,
			},
		},
		QueryPlan: &models.QueryPlan{
			DegreeOfParallelism: 1,
		},
		Findings: []models.Finding{
			{Category: "Cardinality", Severity: models.SeverityCritical},
			{Category: "Cardinality", Severity: models.SeverityHigh},
		},
	}

	score := calc.Calculate(plan)

	if score.CardinalityScore > 10 {
		t.Errorf("CardinalityScore should be penalized, got %d", score.CardinalityScore)
	}
}

func TestDetermineStatus(t *testing.T) {
	calc := NewCalculator()

	tests := []struct {
		score    int
		expected string
	}{
		{85, "Green"},
		{80, "Green"},
		{60, "Yellow"},
		{50, "Yellow"},
		{40, "Red"},
		{49, "Red"},
	}

	for _, tt := range tests {
		hs := models.HealthScore{OverallScore: tt.score}
		status := calc.DetermineStatus(hs)
		if status != tt.expected {
			t.Errorf("score %d: expected %s, got %s", tt.score, tt.expected, status)
		}
	}
}

func TestEvidenceCoverageCheck(t *testing.T) {
	calc := NewCalculator()

	plan := &models.PlanAnalysis{
		Operators: []models.Operator{
			{
				PhysicalOp:                "Clustered Index Scan",
				ActualRows:               0,
				ActualExecutions:         0,
				EstimatedTotalSubtreeCost: 0,
			},
		},
		QueryPlan: &models.QueryPlan{
			DegreeOfParallelism: 1,
		},
	}

	score := calc.Calculate(plan)

	result := calc.DetermineStatus(score)
	if result == "" {
		t.Error("expected non-empty status")
	}
}

func TestScoreConstants(t *testing.T) {
	if MaxAccessMethodsScore != 30 {
		t.Errorf("MaxAccessMethodsScore should be 30, got %d", MaxAccessMethodsScore)
	}
	if MaxCardinalityScore != 25 {
		t.Errorf("MaxCardinalityScore should be 25, got %d", MaxCardinalityScore)
	}
	if MaxMemoryUsageScore != 20 {
		t.Errorf("MaxMemoryUsageScore should be 20, got %d", MaxMemoryUsageScore)
	}
	if MaxParallelismScore != 10 {
		t.Errorf("MaxParallelismScore should be 10, got %d", MaxParallelismScore)
	}
	if MaxIndexingScore != 15 {
		t.Errorf("MaxIndexingScore should be 15, got %d", MaxIndexingScore)
	}
}

func BenchmarkCalculate(b *testing.B) {
	calc := NewCalculator()
	plan := &models.PlanAnalysis{
		Operators: make([]models.Operator, 100),
		QueryPlan: &models.QueryPlan{
			DegreeOfParallelism: 4,
		},
		Warnings: make([]models.Warning, 5),
		Findings: make([]models.Finding, 20),
	}

	for i := range plan.Operators {
		plan.Operators[i] = models.Operator{
			PhysicalOp:                "Index Seek",
			ActualRows:               100,
			ActualExecutions:         1,
			EstimatedTotalSubtreeCost: 0.1,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		calc.Calculate(plan)
	}
}
