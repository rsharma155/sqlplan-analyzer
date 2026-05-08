package parser

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/rsharma155/sqlplan-analyzer/models"
)

func TestParserCreation(t *testing.T) {
	cfg := Config{EnableStreaming: true}
	p := New(cfg)
	if p == nil {
		t.Fatal("expected non-nil parser")
	}
}

func TestParseSimplePlan(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="UTF-8"?>
<ShowPlanXML Version="1.5" Build="14.0.1000">
 <BatchSequence>
  <Batch>
   <Statements>
    <StmtSimple StatementText="SELECT * FROM Orders" StatementType="SELECT">
     <QueryPlan DegreeOfParallelism="4" CachedPlanSize="32" CompileTimeMS="15" CompileCPU="12" CompileMemory="256" OptimizationLevel="FULL">
      <RelOp NodeId="0" PhysicalOp="Clustered Index Scan" LogicalOp="Clustered Index Scan" EstimateRows="1000" EstimatedTotalSubtreeCost="0.5" EstimateCPU="0.3" EstimateIO="0.2">
       <OutputList>
        <ColumnReference Column="OrderID" />
       </OutputList>
       <IndexScan ScanType="Full" IndexKind="ClusteredIndex">
        <Object Database="TestDB" Schema="dbo" Table="Orders" Index="PK_Orders" />
       </IndexScan>
      </RelOp>
     </QueryPlan>
    </StmtSimple>
   </Statements>
  </Batch>
 </BatchSequence>
</ShowPlanXML>`

	p := New(Config{})
	plan, err := p.ParseBytes([]byte(xmlData))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if plan.Version != "1.5" {
		t.Errorf("expected version 1.5, got %s", plan.Version)
	}
	if plan.Build != "14.0.1000" {
		t.Errorf("expected build 14.0.1000, got %s", plan.Build)
	}
	if plan.Metadata.StatementText != "SELECT * FROM Orders" {
		t.Errorf("expected SELECT * FROM Orders, got %s", plan.Metadata.StatementText)
	}
	if plan.QueryPlan.DegreeOfParallelism != 4 {
		t.Errorf("expected DOP 4, got %d", plan.QueryPlan.DegreeOfParallelism)
	}
	if plan.QueryPlan.CachedPlanSize != 32 {
		t.Errorf("expected cached plan size 32, got %d", plan.QueryPlan.CachedPlanSize)
	}
	if plan.QueryPlan.OptimizationLevel != "FULL" {
		t.Errorf("expected optimization level FULL, got %s", plan.QueryPlan.OptimizationLevel)
	}
	if len(plan.Operators) != 1 {
		t.Errorf("expected 1 operator, got %d", len(plan.Operators))
	}
	if len(plan.Operators) > 0 {
		op := plan.Operators[0]
		if op.PhysicalOp != "Clustered Index Scan" {
			t.Errorf("expected Clustered Index Scan, got %s", op.PhysicalOp)
		}
		if op.EstimatedTotalSubtreeCost != 0.5 {
			t.Errorf("expected cost 0.5, got %f", op.EstimatedTotalSubtreeCost)
		}
	}
}

func TestParseWithRuntimeCounters(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="UTF-8"?>
<ShowPlanXML Version="1.5" Build="14.0.1000">
 <BatchSequence>
  <Batch>
   <Statements>
    <StmtSimple StatementText="SELECT * FROM Orders" StatementType="SELECT">
     <QueryPlan DegreeOfParallelism="4" CachedPlanSize="32" CompileTimeMS="15">
      <RelOp NodeId="0" PhysicalOp="Clustered Index Scan" LogicalOp="Clustered Index Scan" EstimateRows="1000" EstimatedTotalSubtreeCost="0.5">
       <RunTimeInformation>
        <RunTimeCountersPerThread Thread="1" ActualRows="500" ActualExecutions="1" ActualElapsedms="120" ActualCPUms="100" ActualLogicalReads="5000" ActualPhysicalReads="10" />
        <RunTimeCountersPerThread Thread="2" ActualRows="500" ActualExecutions="1" ActualElapsedms="150" ActualCPUms="110" ActualLogicalReads="5200" ActualPhysicalReads="8" />
       </RunTimeInformation>
       <IndexScan ScanType="Full" IndexKind="ClusteredIndex">
        <Object Database="TestDB" Schema="dbo" Table="Orders" Index="PK_Orders" />
       </IndexScan>
      </RelOp>
     </QueryPlan>
    </StmtSimple>
   </Statements>
  </Batch>
 </BatchSequence>
</ShowPlanXML>`

	p := New(Config{})
	plan, err := p.ParseBytes([]byte(xmlData))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(plan.Operators) == 0 {
		t.Fatal("expected at least one operator")
	}

	op := plan.Operators[0]
	if op.ActualRows != 1000 {
		t.Errorf("expected ActualRows=1000 (SUM), got %d", op.ActualRows)
	}
	if op.ActualLogicalReads != 10200 {
		t.Errorf("expected ActualLogicalReads=10200 (SUM), got %d", op.ActualLogicalReads)
	}
	if op.ActualExecutions != 2 {
		t.Errorf("expected ActualExecutions=2, got %d", op.ActualExecutions)
	}

	if op.ActualPhysicalReads != 18 {
		t.Errorf("expected ActualPhysicalReads=18 (SUM), got %d", op.ActualPhysicalReads)
	}
}

func TestParseWithMemoryGrant(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="UTF-8"?>
<ShowPlanXML Version="1.5" Build="14.0.1000">
 <BatchSequence>
  <Batch>
   <Statements>
    <StmtSimple StatementText="SELECT * FROM Orders ORDER BY Total" StatementType="SELECT">
     <QueryPlan DegreeOfParallelism="2" MemoryGrant="65536" CachedPlanSize="48" CompileTimeMS="25">
      <MemoryGrantInfo SerialRequiredMemory="4096" SerialDesiredMemory="16384" RequestedMemory="32768" GrantedMemory="65536" MaxUsedMemory="49152" IdealMemory="32768" />
      <RelOp NodeId="0" PhysicalOp="Sort" LogicalOp="Sort" EstimateRows="1000" EstimatedTotalSubtreeCost="0.8">
       <RelOp NodeId="1" PhysicalOp="Clustered Index Scan" LogicalOp="Clustered Index Scan" EstimateRows="1000" EstimatedTotalSubtreeCost="0.3">
        <IndexScan ScanType="Full" IndexKind="ClusteredIndex">
         <Object Database="TestDB" Schema="dbo" Table="Orders" Index="PK_Orders" />
        </IndexScan>
       </RelOp>
      </RelOp>
     </QueryPlan>
    </StmtSimple>
   </Statements>
  </Batch>
 </BatchSequence>
</ShowPlanXML>`

	p := New(Config{})
	plan, err := p.ParseBytes([]byte(xmlData))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if !plan.QueryPlan.HasMemoryGrant {
		t.Error("expected HasMemoryGrant to be true")
	}
	if plan.QueryPlan.MemoryGrantInfo.SerialRequiredMemory != 4096 {
		t.Errorf("expected SerialRequiredMemory=4096, got %d", plan.QueryPlan.MemoryGrantInfo.SerialRequiredMemory)
	}
	if plan.QueryPlan.MemoryGrantInfo.RequestedMemory != 32768 {
		t.Errorf("expected RequestedMemory=32768, got %d", plan.QueryPlan.MemoryGrantInfo.RequestedMemory)
	}
	if plan.QueryPlan.MemoryGrantInfo.GrantedMemory != 65536 {
		t.Errorf("expected GrantedMemory=65536, got %d", plan.QueryPlan.MemoryGrantInfo.GrantedMemory)
	}
	if plan.QueryPlan.MemoryGrantInfo.MaxUsedMemory != 49152 {
		t.Errorf("expected MaxUsedMemory=49152, got %d", plan.QueryPlan.MemoryGrantInfo.MaxUsedMemory)
	}

	if plan.QueryPlan.MemoryGrantInfo.CalculateUtilization() != 0.75 {
		t.Errorf("expected utilization 0.75, got %f", plan.QueryPlan.MemoryGrantInfo.CalculateUtilization())
	}
}

func TestDOPFallback(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="UTF-8"?>
<ShowPlanXML Version="1.5" Build="14.0.1000">
 <BatchSequence>
  <Batch>
   <Statements>
    <StmtSimple StatementText="SELECT * FROM Orders" StatementType="SELECT">
     <QueryPlan CachedPlanSize="16" CompileTimeMS="5">
      <RelOp NodeId="0" PhysicalOp="Clustered Index Scan" LogicalOp="Clustered Index Scan" EstimateRows="100" EstimatedTotalSubtreeCost="0.1">
       <IndexScan ScanType="Full">
        <Object Database="TestDB" Schema="dbo" Table="Orders" />
       </IndexScan>
      </RelOp>
     </QueryPlan>
    </StmtSimple>
   </Statements>
  </Batch>
 </BatchSequence>
</ShowPlanXML>`

	p := New(Config{})
	plan, err := p.ParseBytes([]byte(xmlData))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if plan.QueryPlan.DegreeOfParallelism == 0 {
		t.Error("DOP should never be 0 - expected fallback to 1")
	}
	if plan.QueryPlan.DegreeOfParallelism != 1 {
		t.Errorf("expected DOP fallback to 1, got %d", plan.QueryPlan.DegreeOfParallelism)
	}
}

func TestParseMissingIndexes(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="UTF-8"?>
<ShowPlanXML Version="1.5" Build="14.0.1000">
 <BatchSequence>
  <Batch>
   <Statements>
    <StmtSimple StatementText="SELECT * FROM Orders WHERE CustomerID = 100" StatementType="SELECT">
     <QueryPlan DegreeOfParallelism="1">
      <RelOp NodeId="0" PhysicalOp="Table Scan" LogicalOp="Table Scan" EstimateRows="10000" EstimatedTotalSubtreeCost="10.5">
       <TableScan>
        <Object Database="TestDB" Schema="dbo" Table="Orders" />
       </TableScan>
      </RelOp>
      <MissingIndexes>
       <MissingIndexGroup Impact="95.5">
        <MissingIndex Database="TestDB" Schema="dbo" Table="Orders">
         <ColumnGroup Usage="EQUALITY">
          <Column Name="CustomerID" />
         </ColumnGroup>
         <ColumnGroup Usage="INCLUDE">
          <Column Name="OrderDate" />
          <Column Name="TotalAmount" />
         </ColumnGroup>
        </MissingIndex>
       </MissingIndexGroup>
      </MissingIndexes>
     </QueryPlan>
    </StmtSimple>
   </Statements>
  </Batch>
 </BatchSequence>
</ShowPlanXML>`

	p := New(Config{})
	plan, err := p.ParseBytes([]byte(xmlData))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(plan.MissingIndexes) != 1 {
		t.Fatalf("expected 1 missing index, got %d", len(plan.MissingIndexes))
	}

	mi := plan.MissingIndexes[0]
	if mi.Database != "TestDB" {
		t.Errorf("expected TestDB, got %s", mi.Database)
	}
	if mi.Table != "Orders" {
		t.Errorf("expected Orders, got %s", mi.Table)
	}
	if len(mi.Columns) != 1 {
		t.Fatalf("expected 1 column, got %d", len(mi.Columns))
	}
	if !mi.Columns[0].Equality {
		t.Error("expected equality column")
	}
	if len(mi.IncludedColumns) != 2 {
		t.Errorf("expected 2 included columns, got %d", len(mi.IncludedColumns))
	}
	if mi.Score != 95 {
		t.Errorf("expected score 95, got %d", mi.Score)
	}
}

func TestParseWarnings(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="UTF-8"?>
<ShowPlanXML Version="1.5" Build="14.0.1000">
 <BatchSequence>
  <Batch>
   <Statements>
    <StmtSimple StatementText="SELECT * FROM Orders" StatementType="SELECT">
     <QueryPlan DegreeOfParallelism="1">
      <RelOp NodeId="0" PhysicalOp="Hash Match" LogicalOp="Join" EstimateRows="1000" EstimatedTotalSubtreeCost="0.5">
       <Warnings NoJoinPredicate="true" />
       <RelOp NodeId="1" PhysicalOp="Table Scan" LogicalOp="Table Scan" EstimateRows="100">
        <TableScan>
         <Object Database="TestDB" Schema="dbo" Table="Orders" />
        </TableScan>
       </RelOp>
       <RelOp NodeId="2" PhysicalOp="Table Scan" LogicalOp="Table Scan" EstimateRows="200">
        <TableScan>
         <Object Database="TestDB" Schema="dbo" Table="Customers" />
        </TableScan>
       </RelOp>
      </RelOp>
     </QueryPlan>
    </StmtSimple>
   </Statements>
  </Batch>
 </BatchSequence>
</ShowPlanXML>`

	p := New(Config{})
	plan, err := p.ParseBytes([]byte(xmlData))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(plan.Warnings) == 0 {
		t.Fatal("expected at least one warning")
	}

	foundNoJoin := false
	for _, w := range plan.Warnings {
		if w.Type == models.WarningTypeNoJoinPredicate {
			foundNoJoin = true
			break
		}
	}
	if !foundNoJoin {
		t.Error("expected NoJoinPredicate warning")
	}
}

func TestParseRealFile(t *testing.T) {
	testFile := "../../examples/exec_plan1.sqlplan"
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Skipf("skipping: cannot read test file %s: %v", testFile, err)
	}

	p := New(Config{})
	plan, err := p.ParseBytes(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if plan.Version == "" {
		t.Error("expected version to be set")
	}
	if len(plan.Operators) == 0 {
		t.Error("expected at least one operator")
	}
	if plan.QueryPlan.DegreeOfParallelism <= 0 {
		t.Error("DOP should be >= 1")
	}
}

func TestParseComplexPlan(t *testing.T) {
	testFile := "../../examples/exec_plan2.sqlplan"
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Skipf("skipping: cannot read test file %s: %v", testFile, err)
	}

	p := New(Config{})
	plan, err := p.ParseBytes(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(plan.Operators) == 0 {
		t.Error("expected at least one operator")
	}
	if plan.QueryPlan.DegreeOfParallelism <= 0 {
		t.Error("DOP should be >= 1")
	}
}

func TestParseFileMethod(t *testing.T) {
	testFile := "../../examples/exec_plan1.sqlplan"
	p := New(Config{EnableStreaming: true})
	plan, err := p.ParseFile(testFile)
	if err != nil {
		t.Skipf("skipping: cannot read file %s: %v", testFile, err)
	}

	if plan == nil {
		t.Fatal("expected non-nil plan")
	}
	if plan.Metadata.StatementText == "" {
		t.Log("warning: empty statement text (real plans may vary)")
	}
}

func TestUTF16Conversion(t *testing.T) {
	utf16BOM := []byte{0xFF, 0xFE}
	longXML := "<?xml version=\"1.0\" encoding=\"UTF-16\"?><ShowPlanXML Version=\"1.5\" Build=\"14.0.1000\"><BatchSequence><Batch><Statements><StmtSimple StatementText=\"SELECT * FROM Orders\" StatementType=\"SELECT\"><QueryPlan DegreeOfParallelism=\"1\"><RelOp NodeId=\"0\" PhysicalOp=\"Clustered Index Scan\" LogicalOp=\"Clustered Index Scan\" EstimateRows=\"1000\" EstimatedTotalSubtreeCost=\"0.5\"><IndexScan ScanType=\"Full\" IndexKind=\"ClusteredIndex\"><Object Database=\"TestDB\" Schema=\"dbo\" Table=\"Orders\" Index=\"PK_Orders\" /></IndexScan></RelOp></QueryPlan></StmtSimple></Statements></Batch></BatchSequence></ShowPlanXML>"

	result := make([]byte, 0, len(utf16BOM)+len(longXML)*2)
	result = append(result, utf16BOM...)
	for _, b := range []byte(longXML) {
		result = append(result, b, 0)
	}

	converted := convertToUTF8(result)
	if len(converted) == 0 {
		t.Fatal("expected non-empty result")
	}

	output := string(converted)
	if !strings.Contains(output, "<?xml") {
		t.Errorf("expected XML declaration, got %s", output[:min(50, len(output))])
	}
	if !strings.Contains(output, "ShowPlanXML") {
		t.Errorf("expected ShowPlanXML, got %s", output[:min(50, len(output))])
	}
}

func TestStreamParse(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="UTF-8"?>
<ShowPlanXML Version="1.5" Build="14.0.1000">
 <BatchSequence>
  <Batch>
   <Statements>
    <StmtSimple StatementText="SELECT * FROM Orders" StatementType="SELECT">
     <QueryPlan DegreeOfParallelism="1">
      <RelOp NodeId="0" PhysicalOp="Index Seek" LogicalOp="Index Seek" EstimateRows="50" EstimatedTotalSubtreeCost="0.05">
       <IndexScan ScanType="Seek" IndexKind="NonClustered">
        <Object Database="TestDB" Schema="dbo" Table="Orders" Index="IX_Orders_CustomerID" />
        <SeekPredicates>
         <SeekPredicateNew SeekType="EQ">
          <Prefix>
           <RangeColumns>
            <ColumnReference Column="CustomerID" />
           </RangeColumns>
           <RangeExpressions>
            <ScalarOperator ScalarString="[TestDB].[dbo].[Orders].[CustomerID]=(100)" />
           </RangeExpressions>
          </Prefix>
         </SeekPredicateNew>
        </SeekPredicates>
       </IndexScan>
      </RelOp>
     </QueryPlan>
    </StmtSimple>
   </Statements>
  </Batch>
 </BatchSequence>
</ShowPlanXML>`

	p := New(Config{EnableStreaming: true})
	plan, err := p.ParseBytes([]byte(xmlData))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(plan.Operators) != 1 {
		t.Fatalf("expected 1 operator, got %d", len(plan.Operators))
	}

	op := plan.Operators[0]
	if op.PhysicalOp != "Index Seek" {
		t.Errorf("expected Index Seek, got %s", op.PhysicalOp)
	}
	if op.IndexScan == nil {
		t.Fatal("expected IndexScan detail")
	}
	if op.IndexScan.Object.Index != "IX_Orders_CustomerID" {
		t.Errorf("expected IX_Orders_CustomerID, got %s", op.IndexScan.Object.Index)
	}
}

func TestMemoryGrantUtilization(t *testing.T) {
	mgi := models.MemoryGrantInfo{
		GrantedMemory: 65536,
		MaxUsedMemory: 49152,
	}
	util := mgi.CalculateUtilization()
	if util != 0.75 {
		t.Errorf("expected 0.75, got %f", util)
	}

	mgi2 := models.MemoryGrantInfo{
		GrantedMemory: 0,
		MaxUsedMemory: 0,
	}
	util2 := mgi2.CalculateUtilization()
	if util2 != 0 {
		t.Errorf("expected 0, got %f", util2)
	}
}

func BenchmarkParseSmall(b *testing.B) {
	xmlData := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<ShowPlanXML Version="1.5" Build="14.0.1000">
 <BatchSequence>
  <Batch>
   <Statements>
    <StmtSimple StatementText="SELECT * FROM Orders" StatementType="SELECT">
     <QueryPlan DegreeOfParallelism="1">
      <RelOp NodeId="0" PhysicalOp="Clustered Index Scan" LogicalOp="Clustered Index Scan" EstimateRows="1000" EstimatedTotalSubtreeCost="0.5">
       <IndexScan ScanType="Full" IndexKind="ClusteredIndex">
        <Object Database="TestDB" Schema="dbo" Table="Orders" Index="PK_Orders" />
       </IndexScan>
      </RelOp>
     </QueryPlan>
    </StmtSimple>
   </Statements>
  </Batch>
 </BatchSequence>
</ShowPlanXML>`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New(Config{})
		_, err := p.ParseBytes(xmlData)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestParseHashJoin(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="UTF-8"?>
<ShowPlanXML Version="1.5" Build="14.0.1000">
 <BatchSequence>
  <Batch>
   <Statements>
    <StmtSimple StatementText="SELECT * FROM Orders o JOIN Customers c ON o.CustomerID = c.CustomerID" StatementType="SELECT">
     <QueryPlan DegreeOfParallelism="4">
      <RelOp NodeId="0" PhysicalOp="Hash Match" LogicalOp="Inner Join" EstimateRows="1000" EstimatedTotalSubtreeCost="0.5">
       <Warnings NoJoinPredicate="false" />
       <Hash>
        <DefinedValues />
        <HashKeysBuild>
         <ColumnReference Database="[TestDB]" Schema="[dbo]" Table="[Customers]" Alias="[c]" Column="CustomerID" />
        </HashKeysBuild>
        <HashKeysProbe>
         <ColumnReference Database="[TestDB]" Schema="[dbo]" Table="[Orders]" Alias="[o]" Column="CustomerID" />
        </HashKeysProbe>
        <ProbeResidual>
         <ScalarOperator ScalarString="[TestDB].[dbo].[Customers].[CustomerID] as [c].[CustomerID]=[TestDB].[dbo].[Orders].[CustomerID] as [o].[CustomerID]">
          <Compare CompareOp="EQ">
           <ScalarOperator>
            <Identifier>
             <ColumnReference Database="[TestDB]" Schema="[dbo]" Table="[Customers]" Alias="[c]" Column="CustomerID" />
            </Identifier>
           </ScalarOperator>
           <ScalarOperator>
            <Identifier>
             <ColumnReference Database="[TestDB]" Schema="[dbo]" Table="[Orders]" Alias="[o]" Column="CustomerID" />
            </Identifier>
           </ScalarOperator>
          </Compare>
         </ScalarOperator>
        </ProbeResidual>
        <RelOp NodeId="1" PhysicalOp="Clustered Index Scan" LogicalOp="Clustered Index Scan" EstimateRows="500" EstimatedTotalSubtreeCost="0.2">
         <IndexScan ScanType="Full">
          <Object Database="TestDB" Schema="dbo" Table="Customers" Index="PK_Customers" />
         </IndexScan>
        </RelOp>
        <RelOp NodeId="2" PhysicalOp="Index Scan" LogicalOp="Index Scan" EstimateRows="1000" EstimatedTotalSubtreeCost="0.3">
         <IndexScan ScanType="Full">
          <Object Database="TestDB" Schema="dbo" Table="Orders" Index="IX_Orders_CustomerID" />
         </IndexScan>
        </RelOp>
       </Hash>
      </RelOp>
     </QueryPlan>
    </StmtSimple>
   </Statements>
  </Batch>
 </BatchSequence>
</ShowPlanXML>`

	p := New(Config{})
	plan, err := p.ParseBytes([]byte(xmlData))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(plan.Operators) == 0 {
		t.Fatal("expected at least one operator")
	}

	hashOp := plan.Operators[0]
	if hashOp.PhysicalOp != "Hash Match" {
		t.Fatalf("expected Hash Match, got %s", hashOp.PhysicalOp)
	}
	if hashOp.Hash == nil {
		t.Fatal("expected Hash struct to be populated")
	}
	if len(hashOp.Hash.HashKeysBuild) != 1 {
		t.Fatalf("expected 1 hash build key, got %d", len(hashOp.Hash.HashKeysBuild))
	}
	if hashOp.Hash.HashKeysBuild[0].Column != "CustomerID" {
		t.Errorf("expected build key CustomerID, got %s", hashOp.Hash.HashKeysBuild[0].Column)
	}
	if hashOp.Hash.HashKeysBuild[0].Table != "Customers" {
		t.Errorf("expected build key table Customers, got %s", hashOp.Hash.HashKeysBuild[0].Table)
	}
	if len(hashOp.Hash.HashKeysProbe) != 1 {
		t.Fatalf("expected 1 hash probe key, got %d", len(hashOp.Hash.HashKeysProbe))
	}
	if hashOp.Hash.HashKeysProbe[0].Column != "CustomerID" {
		t.Errorf("expected probe key CustomerID, got %s", hashOp.Hash.HashKeysProbe[0].Column)
	}
	if hashOp.Hash.HashKeysProbe[0].Table != "Orders" {
		t.Errorf("expected probe key table Orders, got %s", hashOp.Hash.HashKeysProbe[0].Table)
	}
	if hashOp.Hash.ProbeResidual == "" {
		t.Error("expected ProbeResidual to be populated")
	}
	if !strings.Contains(hashOp.Hash.ProbeResidual, "CustomerID") {
		t.Errorf("expected ProbeResidual to contain CustomerID, got %s", hashOp.Hash.ProbeResidual)
	}
}

func TestParseHashJoinMultiKey(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="UTF-8"?>
<ShowPlanXML Version="1.5" Build="14.0.1000">
 <BatchSequence>
  <Batch>
   <Statements>
    <StmtSimple StatementText="SELECT * FROM T1 JOIN T2 ON T1.a = T2.x AND T1.b = T2.y" StatementType="SELECT">
     <QueryPlan DegreeOfParallelism="1">
      <RelOp NodeId="0" PhysicalOp="Hash Match" LogicalOp="Inner Join" EstimateRows="500" EstimatedTotalSubtreeCost="0.4">
       <Hash>
        <DefinedValues />
        <HashKeysBuild>
         <ColumnReference Database="[DB]" Schema="[dbo]" Table="[T1]" Alias="[T1]" Column="a" />
         <ColumnReference Database="[DB]" Schema="[dbo]" Table="[T1]" Alias="[T1]" Column="b" />
        </HashKeysBuild>
        <HashKeysProbe>
         <ColumnReference Database="[DB]" Schema="[dbo]" Table="[T2]" Alias="[T2]" Column="x" />
         <ColumnReference Database="[DB]" Schema="[dbo]" Table="[T2]" Alias="[T2]" Column="y" />
        </HashKeysProbe>
       </Hash>
       <RelOp NodeId="1" PhysicalOp="Table Scan" LogicalOp="Table Scan" EstimateRows="500">
        <TableScan>
         <Object Database="DB" Schema="dbo" Table="T1" />
        </TableScan>
       </RelOp>
       <RelOp NodeId="2" PhysicalOp="Table Scan" LogicalOp="Table Scan" EstimateRows="500">
        <TableScan>
         <Object Database="DB" Schema="dbo" Table="T2" />
        </TableScan>
       </RelOp>
      </RelOp>
     </QueryPlan>
    </StmtSimple>
   </Statements>
  </Batch>
 </BatchSequence>
</ShowPlanXML>`

	p := New(Config{})
	plan, err := p.ParseBytes([]byte(xmlData))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(plan.Operators) == 0 {
		t.Fatal("expected at least one operator")
	}

	hashOp := plan.Operators[0]
	if hashOp.Hash == nil {
		t.Fatal("expected Hash struct to be populated")
	}
	if len(hashOp.Hash.HashKeysBuild) != 2 {
		t.Fatalf("expected 2 hash build keys, got %d", len(hashOp.Hash.HashKeysBuild))
	}
	if hashOp.Hash.HashKeysBuild[0].Column != "a" || hashOp.Hash.HashKeysBuild[1].Column != "b" {
		t.Errorf("expected build keys [a,b], got [%s,%s]", hashOp.Hash.HashKeysBuild[0].Column, hashOp.Hash.HashKeysBuild[1].Column)
	}
	if len(hashOp.Hash.HashKeysProbe) != 2 {
		t.Fatalf("expected 2 hash probe keys, got %d", len(hashOp.Hash.HashKeysProbe))
	}
	if hashOp.Hash.HashKeysProbe[0].Column != "x" || hashOp.Hash.HashKeysProbe[1].Column != "y" {
		t.Errorf("expected probe keys [x,y], got [%s,%s]", hashOp.Hash.HashKeysProbe[0].Column, hashOp.Hash.HashKeysProbe[1].Column)
	}
}

func TestParseSeekPredicateMultiColumn(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="UTF-8"?>
<ShowPlanXML Version="1.5" Build="14.0.1000">
 <BatchSequence>
  <Batch>
   <Statements>
    <StmtSimple StatementText="SELECT * FROM T WHERE a = 1 AND b = 2" StatementType="SELECT">
     <QueryPlan DegreeOfParallelism="1">
      <RelOp NodeId="0" PhysicalOp="Index Seek" LogicalOp="Index Seek" EstimateRows="1" EstimatedTotalSubtreeCost="0.05">
       <IndexScan ScanType="Seek" IndexKind="NonClustered">
        <Object Database="DB" Schema="dbo" Table="T" Index="IX_T_AB" />
        <SeekPredicates>
         <SeekPredicateNew>
          <SeekKeys>
           <Prefix ScanType="EQ">
            <RangeColumns>
             <ColumnReference Column="a" />
             <ColumnReference Column="b" />
            </RangeColumns>
            <RangeExpressions>
             <ScalarOperator ScalarString="[DB].[dbo].[T].[a]=(1)" />
             <ScalarOperator ScalarString="[DB].[dbo].[T].[b]=(2)" />
            </RangeExpressions>
           </Prefix>
          </SeekKeys>
         </SeekPredicateNew>
        </SeekPredicates>
       </IndexScan>
      </RelOp>
     </QueryPlan>
    </StmtSimple>
   </Statements>
  </Batch>
 </BatchSequence>
</ShowPlanXML>`

	p := New(Config{})
	plan, err := p.ParseBytes([]byte(xmlData))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(plan.Operators) == 0 {
		t.Fatal("expected at least one operator")
	}

	op := plan.Operators[0]
	if len(op.SeekPredicates) != 1 {
		t.Fatalf("expected 1 SeekPredicate, got %d", len(op.SeekPredicates))
	}

	sp := op.SeekPredicates[0]
	if len(sp.PrefixPredicate) != 2 {
		t.Fatalf("expected 2 PrefixPredicates, got %d", len(sp.PrefixPredicate))
	}
	if sp.PrefixPredicate[0].ScalarString != "[DB].[dbo].[T].[a]=(1)" {
		t.Errorf("expected first scalar string, got %s", sp.PrefixPredicate[0].ScalarString)
	}
	if sp.PrefixPredicate[1].ScalarString != "[DB].[dbo].[T].[b]=(2)" {
		t.Errorf("expected second scalar string, got %s", sp.PrefixPredicate[1].ScalarString)
	}
}

func BenchmarkParseLargeReal(b *testing.B) {
	testFile := "../../examples/exec_plan1.sqlplan"
	data, err := os.ReadFile(testFile)
	if err != nil {
		b.Skipf("skipping: cannot read %s: %v", testFile, err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New(Config{EnableStreaming: true})
		_, err := p.Parse(bytes.NewReader(data))
		if err != nil {
			b.Fatal(err)
		}
	}
}
