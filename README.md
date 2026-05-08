# SQL Server Execution Plan Analyzer Library

A Go library for parsing, analyzing, and generating reports from SQL Server execution plans (.sqlplan files).

## Features

- **Parse** SQL Server execution plan XML files
- **Analyze** plans using 14+ rule types (table scans, missing indexes, spills, etc.)
- **Score** plans with a health score (0-100) with Green/Yellow/Red status
- **Explain** with technical DBA explanations and business-friendly narratives
- **Report** in HTML (interactive), Markdown, or JSON formats
- **Database** integration - fetch plans directly from SQL Server or PostgreSQL

## Installation

```bash
go get sqlplan-analyzer
```

## Quick Start

### Basic Usage - Analyze a .sqlplan File

```go
package main

import (
    "fmt"
    "log"
    
    "sqlplan-analyzer"
)

func main() {
    // Parse and analyze a .sqlplan file
    analysis, err := sqlplan.AnalyzeFile("path/to/plan.sqlplan")
    if err != nil {
        log.Fatal(err)
    }
    
    // Print health score
    fmt.Printf("Health Score: %d/100\n", analysis.HealthScore.OverallScore)
    fmt.Printf("Status: %s\n", analysis.ExecutiveSummary.Status)
    
    // Print critical findings
    for _, finding := range analysis.Findings {
        if finding.Severity == sqlplan.SeverityCritical || finding.Severity == sqlplan.SeverityHigh {
            fmt.Printf("[%s] %s\n", finding.Severity, finding.Title)
            fmt.Printf("  Recommendation: %s\n", finding.Recommendation)
        }
    }
}
```

### Generate HTML Report

```go
package main

import (
    "fmt"
    "log"
    "os"
    
    "sqlplan-analyzer"
)

func main() {
    analysis, err := sqlplan.AnalyzeFile("path/to/plan.sqlplan")
    if err != nil {
        log.Fatal(err)
    }
    
    // Generate HTML report
    html, err := sqlplan.GenerateHTML(analysis)
    if err != nil {
        log.Fatal(err)
    }
    
    // Save to file
    err = os.WriteFile("report.html", []byte(html), 0644)
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Println("HTML report saved to report.html")
}
```

### Export as JSON

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"
    
    "sqlplan-analyzer"
)

func main() {
    analysis, err := sqlplan.AnalyzeFile("path/to/plan.sqlplan")
    if err != nil {
        log.Fatal(err)
    }
    
    // Export as JSON
    data, err := sqlplan.ExportJSON(analysis)
    if err != nil {
        log.Fatal(err)
    }
    
    // Pretty print
    var prettyJSON map[string]interface{}
    json.Unmarshal(data, &prettyJSON)
    pretty, _ := json.MarshalIndent(prettyJSON, "", "  ")
    fmt.Println(string(pretty))
}
```

## Advanced Usage

### Using the Analyzer Object

For more control, create an `Analyzer` object:

```go
package main

import (
    "fmt"
    "log"
    
    "sqlplan-analyzer"
)

func main() {
    // Create analyzer with custom configuration
    analyzer := sqlplan.NewAnalyzer(sqlplan.Config{
        EnableRules:        true,  // Enable rule-based analysis
        EnableScoring:      true,  // Enable health scoring
        EnableNarrative:    true,  // Generate business explanations
        EnableCostAnalysis: true,  // Analyze operator costs
    })
    
    // Parse file
    analysis, err := analyzer.ParseFile("path/to/plan.sqlplan")
    if err != nil {
        log.Fatal(err)
    }
    
    // Run analysis
    analysis = analyzer.Analyze(analysis)
    
    // Access results
    fmt.Printf("Query: %s\n", analysis.Metadata.StatementText)
    fmt.Printf("Operators: %d\n", len(analysis.Operators))
    fmt.Printf("Missing Indexes: %d\n", len(analysis.MissingIndexes))
}
```

### Analyzing Bytes or Readers

```go
package main

import (
    "bytes"
    "fmt"
    "log"
    "os"
    
    "sqlplan-analyzer"
)

func main() {
    // Read file into bytes
    data, err := os.ReadFile("path/to/plan.sqlplan")
    if err != nil {
        log.Fatal(err)
    }
    
    // Analyze bytes
    analysis, err := sqlplan.AnalyzeBytes(data)
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Health Score: %d\n", analysis.HealthScore.OverallScore)
    
    // Or use a Reader
    reader := bytes.NewReader(data)
    analysis, err = sqlplan.AnalyzeReader(reader)
    if err != nil {
        log.Fatal(err)
    }
}
```

### Accessing Detailed Analysis Results

```go
package main

import (
    "fmt"
    "log"
    
    "sqlplan-analyzer"
)

func main() {
    analysis, err := sqlplan.AnalyzeFile("path/to/plan.sqlplan")
    if err != nil {
        log.Fatal(err)
    }
    
    // Health score breakdown
    fmt.Printf("Overall Score: %d\n", analysis.HealthScore.OverallScore)
    fmt.Printf("  Access Methods: %d\n", analysis.HealthScore.AccessMethodsScore)
    fmt.Printf("  Memory Usage: %d\n", analysis.HealthScore.MemoryUsageScore)
    fmt.Printf("  Join Strategy: %d\n", analysis.HealthScore.JoinStrategyScore)
    
    // Query metadata
    fmt.Printf("\nQuery Info:\n")
    fmt.Printf("  Database: %s\n", analysis.Metadata.DatabaseName)
    fmt.Printf("  Server: %s\n", analysis.Metadata.ServerName)
    fmt.Printf("  Query Hash: %s\n", analysis.Metadata.QueryHash)
    
    // Findings by severity
    fmt.Printf("\nFindings:\n")
    for _, f := range analysis.Findings {
        fmt.Printf("[%s] %s (Operator: %s)\n", f.Severity, f.Title, f.OperatorName)
        if f.Recommendation != "" {
            fmt.Printf("  → %s\n", f.Recommendation)
        }
    }
    
    // Missing indexes
    if len(analysis.MissingIndexes) > 0 {
        fmt.Printf("\nMissing Indexes:\n")
        for _, idx := range analysis.MissingIndexes {
            fmt.Printf("  Table: %s.%s\n", idx.Schema, idx.Table)
            fmt.Printf("  Columns: %v\n", idx.Columns)
            fmt.Printf("  CREATE: %s\n", idx.CreateIndexStatement)
        }
    }
    
    // Recommendations
    if len(analysis.Recommendations) > 0 {
        fmt.Printf("\nRecommendations (sorted by priority):\n")
        for _, rec := range analysis.Recommendations {
            fmt.Printf("[Priority %d] %s\n", rec.Priority, rec.Title)
            fmt.Printf("  %s\n", rec.Description)
        }
    }
}
```

## Package Structure

The library is organized into the following packages:

| Package | Description |
|---------|-------------|
| `sqlplan` (root) | Main API with convenience functions |
| `analyzer` | Core analysis orchestration |
| `parser` | XML parser for .sqlplan files |
| `rules` | Rule engine with 14+ rule types |
| `scoring` | Health score calculation |
| `narrative` | Business-friendly explanations |
| `reporter` | HTML/Markdown report generation |
| `exporter` | JSON export |
| `models` | Core domain models and types |
| `db` | Database connectivity (SQL Server/PostgreSQL) |

## Rule Types

The library evaluates the following rule categories:

### Access Methods
- Table Scan
- Clustered Index Scan
- Index Scan
- Key Lookup / RID Lookup

### Joins
- Hash Match
- Nested Loops
- Merge Join
- Adaptive Join
- Probe Residual

### Memory
- TempDB Spills
- Excessive Memory Grant
- Insufficient Memory Grant

### Parallelism
- Thread Skew
- Excessive DOP
- Gather/Distribute Streams

### Indexing
- Missing Indexes
- Covering Index
- Bookmark Lookups

### Predicates
- Implicit Conversion
- Non-SARGable predicates
- No Join Predicate

### Cardinality
- Estimation Mismatch
- Stale Statistics
- Parameter Sniffing

## Health Score

The health score (0-100) is calculated based on:

| Category | Max Points |
|----------|------------|
| Access Methods | 30 |
| Cardinality | 25 |
| Memory Usage | 20 |
| Join Strategy/Indexing | 15 |
| Parallelism | 10 |

Status levels:
- **Green**: 80-100 (Healthy)
- **Yellow**: 50-79 (Needs attention)
- **Red**: 0-49 (Critical issues)

## Database Integration

Fetch execution plans directly from a database:

```go
package main

import (
    "context"
    "fmt"
    "log"
    
    "sqlplan-analyzer/db"
    "sqlplan-analyzer/parser"
)

func main() {
    // Create database reader
    reader, err := db.NewReader(db.Config{
        Driver:         db.SQLServer, // or db.PostgreSQL
        ConnectionString: "sqlserver://user:pass@localhost:1433?database=mydb",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer reader.Close()
    
    ctx := context.Background()
    
    // Fetch plan XML from database
    query := `SELECT TOP 1 query_plan 
              FROM sys.dm_exec_query_stats 
              CROSS APPLY sys.dm_exec_query_plan(plan_handle)`
    
    xmlData, err := reader.FetchPlanXML(ctx, query)
    if err != nil {
        log.Fatal(err)
    }
    
    // Parse the plan
    p := parser.New(parser.Config{EnableStreaming: true})
    analysis, err := p.ParseBytes(xmlData)
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Parsed plan with %d operators\n", len(analysis.Operators))
}
```

## Command-Line Tool

The library also includes a CLI tool:

```bash
# Build the CLI
go build -o sqlplan-analyzer.exe ./cmd/cli

# Analyze a file
./sqlplan-analyzer.exe analyze plan.sqlplan -f html -o report.html

# Analyze from database
./sqlplan-analyzer.exe db \
  --driver sqlserver \
  --conn "sqlserver://user:pass@localhost:1433?database=mydb" \
  --query "SELECT plan_xml FROM dbo.QueryPlans WHERE id = 123"
```

## Models

The `models` package contains all the core types:

```go
import "sqlplan-analyzer/models"

// Key types
var (
    analysis  models.PlanAnalysis
    finding   models.Finding
    op        models.Operator
    score     models.HealthScore
    rec       models.Recommendation
)
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

[Add your license here]

## References

This library is based on the book "SQL Server Execution Plans" by Grant Fritchey.
