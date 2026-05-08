# SQL Server Execution Plan Analyzer - How To Use

## Table of Contents
1. [Quick Start](#quick-start)
2. [CLI Usage](#cli-usage)
3. [Integration Guide](#integration-guide)
4. [Database Integration](#database-integration)
5. [Web Embedding](#web-embedding)
6. [API Reference](#api-reference)
7. [Output Formats](#output-formats)
8. [Configuration](#configuration)

---

## Quick Start

### Prerequisites
- Go 1.21 or later installed
- A SQL Server `.sqlplan` execution plan file, or
- SQL plan XML stored in a SQL Server / PostgreSQL table column

### Run the Binary

```bash
# Check help
.\sqlplan-analyzer.exe --help

# Analyze a plan file (generates HTML report)
.\sqlplan-analyzer.exe analyze your_plan.sqlplan

# Specify output format
.\sqlplan-analyzer.exe analyze your_plan.sqlplan -f html

# Output to a file
.\sqlplan-analyzer.exe analyze your_plan.sqlplan -o report.html

# Analyze from a database table column (SQL Server)
.\sqlplan-analyzer.exe db --driver sqlserver --conn "sqlserver://user:pass@host:1433?database=mydb" --query "SELECT plan_xml FROM dbo.QueryPlans WHERE plan_id = 123"

# Save DB result to file (PostgreSQL)
.\sqlplan-analyzer.exe db --driver postgres --conn "postgres://user:pass@host:5432/mydb?sslmode=disable" --query "SELECT plan_xml FROM query_plans WHERE id = 1" -o report.html
```

### Build from Source

```bash
# Clone or navigate to the project directory
cd sql_server_execution_plan_viewer

# Build the binary
go build -o sqlplan-analyzer.exe ./cmd/cli

# Run
.\sqlplan-analyzer.exe --help
```

---

## CLI Usage

### Command: `analyze` (from file)

| Flag | Short | Description | Default |
|------|-------|-------------|---------|
| `--format` | `-f` | Output format: `html`, `markdown`, `json` | `html` |
| `--output` | `-o` | Output file path | stdout |
| `--verbose` | `-v` | Enable verbose output | false |
| `--no-rules` | - | Disable rule evaluation | false |
| `--no-scoring` | - | Disable health scoring | false |
| `--no-narrative` | - | Disable narrative generation | false |

### Command: `db` (from database table column)

| Flag | Description | Default | Required |
|------|-------------|---------|----------|
| `--driver` | Database driver: `sqlserver` or `postgres` | `sqlserver` | No |
| `--conn` | Database connection string | - | Yes |
| `--query` | SQL query returning plan XML in first column of first row | - | Yes |
| `--output` / `-o` | Output file path (omit to print HTML to stdout) | - | No |
| `--no-rules` | Disable rule evaluation | false | No |
| `--no-scoring` | Disable health scoring | false | No |
| `--no-narrative` | Disable narrative generation | false | No |

### Examples

```bash
# From file - HTML report to stdout
.\sqlplan-analyzer.exe analyze plan.sqlplan

# From file - Save HTML report
.\sqlplan-analyzer.exe analyze plan.sqlplan -o report.html

# From file - Generate Markdown report
.\sqlplan-analyzer.exe analyze plan.sqlplan -f markdown -o report.md

# From file - Generate JSON for API/integration
.\sqlplan-analyzer.exe analyze plan.sqlplan -f json -o report.json

# From SQL Server - Fetch plan from table column and print HTML to stdout
.\sqlplan-analyzer.exe db --driver sqlserver --conn "sqlserver://user:pass@host:1433?database=mydb" --query "SELECT plan_xml FROM dbo.QueryPlans WHERE id = 1"

# From SQL Server - ADO-style connection string
.\sqlplan-analyzer.exe db --driver sqlserver --conn "server=host;user id=user;password=pass;database=mydb;" --query "SELECT showplan_xml FROM sys.dm_exec_query_stats CROSS APPLY sys.dm_exec_query_plan(plan_handle) WHERE sql_handle = 0x..."

# From SQL Server - Save HTML to file
.\sqlplan-analyzer.exe db --driver sqlserver --conn "sqlserver://user:pass@host:1433?database=mydb" --query "SELECT plan_xml FROM dbo.QueryPlans WHERE id = 1" -o report.html

# From PostgreSQL - Fetch plan and print HTML to stdout
.\sqlplan-analyzer.exe db --driver postgres --conn "postgres://user:pass@host:5432/mydb?sslmode=disable" --query "SELECT plan_xml FROM query_plans WHERE id = 1"

# From PostgreSQL - Save HTML to file
.\sqlplan-analyzer.exe db --driver postgres --conn "postgres://user:pass@host:5432/mydb?sslmode=disable" --query "SELECT plan_xml FROM query_plans WHERE id = 1" -o report.html
```

---

## Integration Guide

### Option 1: Database Integration with HTML Output (Recommended)

Fetch plan XML from a SQL Server or PostgreSQL table column and get HTML report as a string — no files written. Ideal for embedding in existing web applications.

```go
package main

import (
    "context"
    "fmt"

    "sqlplan-analyzer/internal/sqlplan_plugin"
)

func main() {
    ctx := context.Background()

    p := plugin.New(plugin.Config{
        EnableRules:        true,
        EnableScoring:      true,
        EnableNarrative:    true,
        EnableCostAnalysis: true,
    })

    // One call: fetch from DB → analyze → return HTML string
    // SQL Server
    html, err := p.AnalyzeFromDB(ctx, db.DriverSQLServer,
        "sqlserver://user:pass@host:1433?database=mydb",
        "SELECT plan_xml FROM dbo.QueryPlans WHERE plan_id = 123",
    )

    // PostgreSQL
    // html, err := p.AnalyzeFromDB(ctx, db.DriverPostgres,
    //     "postgres://user:pass@host:5432/mydb?sslmode=disable",
    //     "SELECT plan_xml FROM query_plans WHERE id = 1",
    // )

    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        return
    }

    // Output HTML directly (no file written)
    // Use in webpage: w.Write([]byte(html)) or embed in iframe
    fmt.Print(html)
}

    // Output HTML directly (no file written)
    // Use in webpage: w.Write([]byte(html)) or embed in iframe
    fmt.Print(html)
}
```

See [Database Integration](#database-integration) and [Web Embedding](#web-embedding) for more examples.

### Option 2: Import as Go Module

Add to your `go.mod`:

```
require sqlplan-analyzer v0.1.0
```

Then import in your code:

```go
package main

import (
    "context"
    "fmt"

    "sqlplan-analyzer/internal/analyzer"
    "sqlplan-analyzer/internal/plugin"
    "sqlplan-analyzer/internal/models"
)

func main() {
    // Create analyzer with default config
    cfg := analyzer.DefaultConfig()
    a := analyzer.New(cfg)

    // Parse and analyze a file
    plan, err := a.ParseFile("plan.sqlplan")
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }

    result := a.Analyze(plan)

    // Access results
    fmt.Printf("Health Score: %d/100\n", result.HealthScore.OverallScore)
    fmt.Printf("Status: %s\n", result.ExecutiveSummary.TrafficLight)

    // Iterate through findings
    for _, finding := range result.Findings {
        fmt.Printf("- [%s] %s\n", finding.Severity, finding.Title)
    }
}
```

### Option 3: Use Plugin Interface

The plugin package provides a clean interface for integration:

```go
package main

import (
    "context"
    "fmt"

    "sqlplan-analyzer/internal/plugin"
    "sqlplan-analyzer/internal/models"
)

func main() {
    ctx := context.Background()

    // Create plugin with custom config
    cfg := plugin.Config{
        EnableRules:        true,
        EnableScoring:    true,
        EnableNarrative:  true,
        EnableCostAnalysis: true,
    }

    p := plugin.New(cfg)

    // Analyze from file
    plan, err := p.AnalyzeFile(ctx, "plan.sqlplan")
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        return
    }

    fmt.Printf("Health Score: %d/100\n", plan.HealthScore.OverallScore)
    fmt.Printf("Traffic Light: %s\n", plan.ExecutiveSummary.TrafficLight)
}
```

### Option 4: Direct Parser Usage

For custom analysis pipelines:

```go
package main

import (
    "encoding/json"
    "fmt"
    "os"

    "sqlplan-analyzer/internal/analyzer"
    "sqlplan-analyzer/internal/parser"
    "sqlplan-analyzer/internal/reporter"
)

func main() {
    // Step 1: Parse the XML
    p := parser.New(parser.Config{EnableStreaming: true})
    plan, err := p.ParseFile("plan.sqlplan")
    if err != nil {
        fmt.Fprintf(os.Stderr, "Parse error: %v\n", err)
        os.Exit(1)
    }

    // Step 2: Analyze with custom config
    cfg := analyzer.Config{
        EnableRules:        true,
        EnableScoring:    true,
        EnableNarrative:  true,
        EnableCostAnalysis: true,
    }
    a := analyzer.New(cfg)
    plan = a.Analyze(plan)

    // Step 3: Generate output in your preferred format

    // As JSON
    jsonData, _ := json.MarshalIndent(plan, "", "  ")
    os.WriteFile("output.json", jsonData, 0644)

    // Or use reporter directly
    htmlReporter := reporter.NewHTMLReporter()
    htmlOutput := htmlReporter.Generate(plan)
    os.WriteFile("output.html", []byte(htmlOutput), 0644)

    mdReporter := reporter.NewMarkdownReporter()
    mdOutput := mdReporter.Generate(plan)
    os.WriteFile("output.md", []byte(mdOutput), 0644)
}
```

### Option 5: Use in HTTP API

```go
package main

import (
    "io/ioutil"
    "net/http"

    "sqlplan-analyzer/internal/plugin"
)

type Handler struct {
    plugin *plugin.Plugin
}

func (h *Handler) AnalyzePlanHTML(w http.ResponseWriter, r *http.Request) {
    // Read the uploaded .sqlplan file
    r.ParseMultipartForm(32 << 20)
    file, _, err := r.FormFile("plan")
    if err != nil {
        http.Error(w, err.Error(), 400)
        return
    }
    defer file.Close()

    data, err := ioutil.ReadAll(file)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }

    // Analyze and get HTML string (no file written)
    html, err := h.plugin.AnalyzeBytesForHTML(r.Context(), data)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }

    // Return HTML directly — embed in existing webpage via iframe
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    w.Write([]byte(html))
}

func main() {
    p := plugin.New(plugin.Config{
        EnableRules:        true,
        EnableScoring:      true,
        EnableNarrative:    true,
        EnableCostAnalysis: true,
    })
    h := &Handler{plugin: p}

    http.HandleFunc("/analyze", h.AnalyzePlanHTML)
    http.ListenAndServe(":8080", nil)
}
```

---

## Database Integration

Fetch a SQL Server or PostgreSQL execution plan directly from a database table column for analysis.

### Option 1: Using the Plugin API (Recommended)

The plugin provides `AnalyzeFromDB` which connects, fetches, analyzes, and returns HTML — all in one call. Set `db.DriverSQLServer` for SQL Server or `db.DriverPostgres` for PostgreSQL.

```go
package main

import (
    "context"
    "fmt"
    "net/http"

    "sqlplan-analyzer/internal/sqlplan_plugin"
)

func main() {
    ctx := context.Background()

    p := plugin.New(plugin.Config{
        EnableRules:        true,
        EnableScoring:      true,
        EnableNarrative:    true,
        EnableCostAnalysis: true,
    })

    // SQL Server
    html, err := p.AnalyzeFromDB(ctx, db.DriverSQLServer,
        "sqlserver://user:pass@host:1433?database=mydb",
        "SELECT plan_xml FROM dbo.QueryPlans WHERE plan_id = 123",
    )

    // Or PostgreSQL:
    // html, err := p.AnalyzeFromDB(ctx, db.DriverPostgres,
    //     "postgres://user:pass@host:5432/mydb?sslmode=disable",
    //     "SELECT plan_xml FROM query_plans WHERE id = 1",
    // )
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        return
    }

    fmt.Print(html) // Output to stdout, HTTP response, or embed in webpage
}
```

### Option 2: Using the Database Reader Directly

For custom workflows where you need more control over the database interaction:

```go
package main

import (
    "context"
    "fmt"
    "time"

    "sqlplan-analyzer/internal/sqlplan_analyzer"
    db "sqlplan-analyzer/internal/sqlplan_db"
    "sqlplan-analyzer/internal/sqlplan_reporter"
)

func main() {
    ctx := context.Background()

    // Step 1: Connect to database and fetch plan XML
    // SQL Server
    reader, err := db.NewReader(db.Config{
        Driver:           db.DriverSQLServer,
        ConnectionString: "sqlserver://user:pass@host:1433?database=mydb",
        QueryTimeout:     30 * time.Second,
    })

    // Or PostgreSQL:
    // reader, err := db.NewReader(db.Config{
    //     Driver:           db.DriverPostgres,
    //     ConnectionString: "postgres://user:pass@host:5432/mydb?sslmode=disable",
    //     QueryTimeout:     30 * time.Second,
    // })

    if err != nil {
        fmt.Fprintf(os.Stderr, "Failed to connect: %v\n", err)
        return
    }
    defer reader.Close()

    xmlData, err := reader.FetchPlanXML(ctx, "SELECT plan_xml FROM dbo.QueryPlans WHERE id = 1")
    if err != nil {
        fmt.Fprintf(os.Stderr, "Failed to fetch: %v\n", err)
        return
    }

    // Step 2: Parse and analyze
    a := analyzer.New(analyzer.DefaultConfig())
    plan, err := a.ParsePlan(xmlData)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Failed to parse: %v\n", err)
        return
    }
    result := a.Analyze(plan)

    // Step 3: Generate HTML report string (no file written)
    htmlReporter := reporter.NewHTMLReporter()
    html := htmlReporter.Generate(result)

    fmt.Print(html)
}
```

---

## Web Embedding

The HTML report is returned as a string — no files are written. Embed it directly into any existing webpage.

### Embed Full Report (via iframe)

```go
package main

import (
    "context"
    "fmt"
    "net/http"

    "sqlplan-analyzer/internal/sqlplan_models"
    "sqlplan-analyzer/internal/sqlplan_plugin"
)

func planReportHandler(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    p := plugin.New(plugin.Config{
        EnableRules:        true,
        EnableScoring:      true,
        EnableNarrative:    true,
        EnableCostAnalysis: true,
    })

    // Option A: From raw XML bytes (e.g., fetched from DB by your system)
    var planXML []byte // your XML data
    html, err := p.AnalyzeBytesForHTML(ctx, planXML)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }

    // Option B: From an already-analyzed PlanAnalysis object
    var plan *models.PlanAnalysis // your analyzed plan
    html = p.GenerateHTMLReport(plan)

    // Return HTML directly (existing webpage embeds via iframe)
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    w.Write([]byte(html))
}

func main() {
    http.HandleFunc("/plan-report", planReportHandler)
    http.ListenAndServe(":8080", nil)
}
```

### Key Methods

| Method | Description |
|--------|-------------|
| `plugin.AnalyzeFromDB(ctx, driver, connString, query)` | Fetches plan from DB (`db.DriverSQLServer` / `db.DriverPostgres`), analyzes, returns HTML string |
| `plugin.AnalyzeBytesForHTML(ctx, xmlData)` | Parses XML bytes, analyzes, returns HTML string |
| `plugin.GenerateHTMLReport(plan)` | Returns HTML string from an already-analyzed plan (no file I/O) |
| `plugin.AnalyzeBytes(ctx, xmlData)` | Parses and analyzes, returns `*PlanAnalysis` (no HTML generation) |

---

## API Reference

### analyzer.Config

```go
type Config struct {
    EnableRules        bool  // Enable rule engine
    EnableScoring    bool  // Enable health scoring
    EnableNarrative  bool  // Enable narrative generation
    EnableCostAnalysis bool // Enable cost analysis
    MaxOperators     int   // Max operators to process
}
```

### analyzer.New(cfg Config) *Analyzer

Creates a new analyzer instance.

### a.ParseFile(path string) (*models.PlanAnalysis, error)

Parses a .sqlplan file.

### a.ParsePlan(data []byte) (*models.PlanAnalysis, error)

Parses plan from byte slice.

### a.Analyze(plan *models.PlanAnalysis) *models.PlanAnalysis

Analyzes the plan and returns enriched results.

### models.PlanAnalysis

```go
type PlanAnalysis struct {
    Version          string              // Plan version
    Timestamp       time.Time           // Analysis timestamp
    Metadata        QueryMetadata     // Query metadata
    QueryPlan       *QueryPlan       // Parsed query plan
    Operators      []Operator      // All operators
    Warnings       []Warning      // Warnings found
    MissingIndexes []MissingIndex  // Missing indexes
    Findings       []Finding     // Findings from rules
    Recommendations  []Recommendation // Actionable recommendations
    HealthScore    HealthScore   // 0-100 score
    TechnicalSummary TechnicalSummary // DBA details
    ExecutiveSummary ExecutiveSummary // Business summary
    CostSummary   CostSummary  // Cost analysis
    QueryNarrative []string    // Step-by-step narrative
}
```

### models.Finding

```go
type Finding struct {
    FindingType            string         // Type identifier
    Severity              Severity       // Critical/High/Medium/Low/Info
    OperatorID            int           // Associated operator ID
    OperatorName          string        // Operator name
    Title                 string        // Finding title
    TechnicalExplanation string         // DBA explanation
    BusinessExplanation string        // Plain English explanation
    Recommendation       string        // How to fix
    Impact               string        // Business impact
    Confidence           float64       // 0.0-1.0 confidence
    NumericImpact        float64       // Numeric metric
    EstimatedCost        float64       // Estimated cost
    Category             string        // Category
    SubCategory          string        // Sub-category
}
```

### Severity Constants

```go
const (
    SeverityCritical = "Critical"
    SeverityHigh   = "High"
    SeverityMedium = "Medium"
    SeverityLow   = "Low"
    SeverityInfo  = "Info"
)
```

---

## Output Formats

### HTML Output

The HTML report includes:
- Traffic light status (Green/Yellow/Red)
- Health score (0-100) with breakdown
- Score distribution charts
- Findings table with severity badges
- Recommendations with priority
- Missing index suggestions with SQL
- Operator tree visualization
- Warnings table

### Markdown Output

Suitable for:
- GitHub readmes
- Confluence
- Jira
- Email sharing

### JSON Output

Machine-readable format for:
- API responses
- Dashboards
- Automation pipelines
- AI/LLM integration

---

## Configuration

### Disable Specific Features

```go
cfg := analyzer.Config{
    EnableRules:        false, // Disable rule engine
    EnableScoring:      true,
    EnableNarrative:    false, // Disable narrative
    EnableCostAnalysis: true,
}
a := analyzer.New(cfg)
```

### Enable Specific Rules

```go
// Access rule engine
engine := rules.NewEngine()

// Enable/disable specific rules
registry := engine.Registry()
tableScanRule, _ := registry.Get("TableScanDetection")
_ = tableScanRule // Use rule
```

### Custom Reporter

```go
// HTML with custom theme
htmlReporter := reporter.NewHTMLReporter()
htmlReporter.theme = "dark" // Currently only "light" supported

// Markdown
mdReporter := reporter.NewMarkdownReporter()
```

---

## Troubleshooting

### Memory Issues

For very large plans (50MB+), the parser uses streaming by default. If you still encounter memory issues:

```go
cfg := parser.Config{
    EnableStreaming: true,
    MaxMemoryMB:   100, // Limit memory usage
}
p := parser.New(cfg)
```

### Common Errors

| Error | Solution |
|-------|---------|
| `XML syntax error` | Invalid XML - verify the .sqlplan file is valid |
| `file not found` | Check the path is correct |
| `out of memory` | Enable streaming or reduce MaxMemoryMB |

---

## Next Steps

- Review the generated reports
- Check findings for Critical/High severity items
- Implement recommendations
- Re-run after optimization
- Compare before/after health scores