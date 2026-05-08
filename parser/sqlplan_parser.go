// File: internal/parser/parser.go
// Purpose: SQL Server execution plan XML parser with streaming support
// Package: github.com/rsharma155/sqlplan-analyzer/internal/parser
package parser

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/rsharma155/sqlplan-analyzer/models"
)

type Config struct {
	EnableStreaming bool
	MaxMemoryMB    int
	ValidateSchema bool
}

type Parser struct {
	config         Config
	operatorStack []*models.Operator
	planAnalysis  *models.PlanAnalysis
	warnings     []models.Warning
	missingIdx   []models.MissingIndex
	depth        int
	opIDCounter  int
	rootOp              *models.Operator
	currentMissingIdx       *models.MissingIndex
	currentColUsage       string
	missingIdxImpact      float64
	predicateType         string
	insideHashBuild       bool
	insideHashProbe       bool
	insideProbeResidual   bool
	insideBuildResidual   bool
	insideSeekKeys        bool
	insidePrefix          bool
}

func New(cfg Config) *Parser {
	return &Parser{
		config:    cfg,
		planAnalysis: &models.PlanAnalysis{
			Metadata:     models.QueryMetadata{},
			Operators:    []models.Operator{},
			Warnings:     []models.Warning{},
			Findings:    []models.Finding{},
			QueryNarrative: []string{},
		},
		operatorStack: make([]*models.Operator, 0),
		depth:         0,
	}
}

func (p *Parser) ParseFile(filepath string) (*models.PlanAnalysis, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	return p.Parse(file)
}

func (p *Parser) Parse(r io.Reader) (*models.PlanAnalysis, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}

	data = convertToUTF8(data)

	reader := bytes.NewReader(data)
	decoder := xml.NewDecoder(reader)
	decoder.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		charset = strings.ToLower(charset)
		if strings.Contains(charset, "utf-16") || charset == "" {
			if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE {
				converted, err := convertUTF16LEToUTF8(data)
				if err == nil && len(converted) > 100 {
					return bytes.NewReader(converted), nil
				}
			}
		}
		return input, nil
	}
	p.operatorStack = make([]*models.Operator, 0)
	p.depth = 0
	p.rootOp = nil
	p.predicateType = ""
	p.insideHashBuild = false
	p.insideHashProbe = false
	p.insideProbeResidual = false
	p.insideBuildResidual = false
	p.insideSeekKeys = false
	p.insidePrefix = false

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to decode XML: %w", err)
		}

		err = p.processToken(token)
		if err != nil {
			return nil, err
		}
	}

	if p.rootOp == nil && len(p.operatorStack) > 0 {
		p.rootOp = p.operatorStack[0]
	}

	if p.rootOp != nil {
		if p.planAnalysis.QueryPlan == nil {
			p.planAnalysis.QueryPlan = &models.QueryPlan{}
		}
		p.planAnalysis.QueryPlan.RelOp = p.rootOp
		p.aggregateRuntimeCounters(p.rootOp)
	}

	// Never emit DOP=0; fallback to DOP=1
	if p.planAnalysis.QueryPlan != nil && p.planAnalysis.QueryPlan.DegreeOfParallelism == 0 {
		p.planAnalysis.QueryPlan.DegreeOfParallelism = 1
	}

	// Calculate memory utilization
	if p.planAnalysis.QueryPlan != nil && p.planAnalysis.QueryPlan.HasMemoryGrant {
		p.planAnalysis.QueryPlan.MemoryGrantInfo.MemoryUtilization = p.planAnalysis.QueryPlan.MemoryGrantInfo.CalculateUtilization()
	}

	p.planAnalysis.Operators = p.collectOperators(p.rootOp)
	p.planAnalysis.Warnings = p.warnings
	p.planAnalysis.MissingIndexes = p.missingIdx

	return p.planAnalysis, nil
}

func (p *Parser) aggregateRuntimeCounters(op *models.Operator) {
	if op == nil {
		return
	}

	if len(op.RuntimeCounters) > 0 {
		var totalRows int64
		var maxElapsed float64
		var maxCPU float64
		var totalLogicalReads int64
		var totalPhysicalReads int64
		var totalExecutions int
		var maxThread int

		for _, rc := range op.RuntimeCounters {
			totalRows += rc.ActualRows
			totalLogicalReads += rc.ActualLogicalReads
			totalPhysicalReads += rc.ActualPhysicalReads
			totalExecutions += rc.ActualExecutions

			// SUM for CPU - total CPU across all threads
			maxCPU += rc.ActualCPUms

			// MAX for elapsed - wall clock time is the longest thread
			if rc.ActualElapsedms > maxElapsed {
				maxElapsed = rc.ActualElapsedms
			}

			if rc.Thread > maxThread {
				maxThread = rc.Thread
			}
		}

		if op.ActualRows == 0 {
			op.ActualRows = totalRows
		}
		if op.ActualElapsedms == 0 {
			op.ActualElapsedms = maxElapsed
		}
		if op.ActualCPUms == 0 {
			op.ActualCPUms = maxCPU
		}
		if op.ActualLogicalReads == 0 {
			op.ActualLogicalReads = totalLogicalReads
		}
		if op.ActualPhysicalReads == 0 {
			op.ActualPhysicalReads = totalPhysicalReads
		}
		if op.ActualExecutions == 0 {
			op.ActualExecutions = totalExecutions
		}
	}

	for _, child := range op.Children {
		p.aggregateRuntimeCounters(child)
	}
}

func (p *Parser) ParseBytes(data []byte) (*models.PlanAnalysis, error) {
	return p.Parse(bytes.NewReader(data))
}

func (p *Parser) processToken(token xml.Token) error {
	switch t := token.(type) {
	case xml.StartElement:
		return p.handleStartElement(t)
	case xml.EndElement:
		return p.handleEndElement(t)
	case xml.CharData:
		return p.handleCharData(t)
	}
	return nil
}

func (p *Parser) handleStartElement(el xml.StartElement) error {
	localName := el.Name.Local

	if strings.HasPrefix(localName, "{") {
		idx := strings.LastIndex(localName, "}")
		if idx > 0 {
			localName = localName[idx+1:]
		}
	}

	switch localName {
	case "ShowPlanXML":
		return p.parseShowPlanXML(el)
	case "QueryPlan":
		return p.parseQueryPlan(el)
	case "RelOp":
		return p.parseRelOp(el)
	case "IndexScan", "IndexSeek":
		return p.parseIndexScan(el)
	case "Object":
		return p.parseObject(el)
	case "OptimizerHardwareDependentProperties":
		return p.parseOptimizerHardware(el)
	case "TableScan":
		return p.parseTableScan(el)
	case "Warnings":
		return p.parseWarnings(el)
	case "MissingIndexes":
		return p.parseMissingIndexes(el)
	case "MissingIndexGroup":
		return p.parseMissingIndexGroup(el)
	case "MissingIndex":
		return p.parseMissingIndex(el)
	case "ColumnGroup":
		return p.parseColumnGroup(el)
	case "Column":
		return p.parseMissingIndexColumn(el)
	case "PlanAffectingConvert":
		return p.parsePlanAffectingConvert(el)
	case "RuntimeCountersPerThread", "RunTimeCountersPerThread":
		return p.parseRuntimeCounters(el)
	case "MemoryGrantInfo":
		return p.parseMemoryGrant(el)
	case "QueryTimeStats":
		return p.parseQueryTimeStats(el)
	case "ThreadStat":
		return p.parseThreadStat(el)
	case "WaitStats":
		return p.parseWaitStats(el)
	case "Wait":
		return p.parseWait(el)
	case "OptimizerStatsUsage":
		return p.parseOptimizerStats(el)
	case "RunTimeInformation":
		return nil
	case "SpillToTempDb":
		return p.parseSpillWarning(el)
	case "ColumnReference":
		return p.parseColumnReference(el)
	case "StmtSimple":
		return p.parseStatement(el)
	case "Top":
		return p.parseTop(el)
	case "ScalarOperator":
		return p.parseScalarOperator(el)
	case "SeekPredicateNew", "SeekPredicate":
		return p.parseSeekPredicates(el)
	case "SeekKeys":
		p.insideSeekKeys = true
		return nil
	case "Prefix":
		p.insidePrefix = true
		return nil
	case "MemoryFractions":
		return p.parseMemoryFractions(el)
	case "HashKeysBuild":
		return p.parseHashKeysBuild(el)
	case "HashKeysProbe":
		return p.parseHashKeysProbe(el)
	case "Hash":
		return p.parseHash(el)
	case "Predicate":
		return p.parsePredicate(el)
	case "ProbeResidual", "BuildResidual":
		return p.parseJoinResidual(el)
	default:
		return p.parseGenericOperator(el)
	}
}

func (p *Parser) handleEndElement(el xml.EndElement) error {
	switch el.Name.Local {
	case "RelOp":
		if len(p.operatorStack) > 0 {
			popped := p.operatorStack[len(p.operatorStack)-1]
			p.operatorStack = p.operatorStack[:len(p.operatorStack)-1]
			if len(p.operatorStack) > 0 {
				p.operatorStack[len(p.operatorStack)-1].Children = append(p.operatorStack[len(p.operatorStack)-1].Children, popped)
			} else {
				p.rootOp = popped
			}
		}
	case "MissingIndex":
		if p.currentMissingIdx != nil {
			p.currentMissingIdx.Score = int(p.missingIdxImpact)
			p.missingIdx = append(p.missingIdx, *p.currentMissingIdx)
			p.currentMissingIdx = nil
		}
	case "MissingIndexGroup":
		p.missingIdxImpact = 0
	case "MissingIndexes":
		p.currentMissingIdx = nil
		p.currentColUsage = ""
		p.missingIdxImpact = 0
	case "Predicate":
		if p.predicateType == "residual" {
			p.predicateType = ""
		}
	case "SeekPredicateNew", "SeekPredicate":
		if p.predicateType == "seek" {
			p.predicateType = ""
		}
	case "SeekKeys":
		p.insideSeekKeys = false
	case "Prefix":
		p.insidePrefix = false
	case "HashKeysBuild":
		p.insideHashBuild = false
	case "HashKeysProbe":
		p.insideHashProbe = false
	case "ProbeResidual":
		p.insideProbeResidual = false
	case "BuildResidual":
		p.insideBuildResidual = false
	}
	p.depth--
	return nil
}

func (p *Parser) handleCharData(data xml.CharData) error {
	return nil
}

func (p *Parser) parseQueryPlan(el xml.StartElement) error {
	if p.planAnalysis.QueryPlan == nil {
		p.planAnalysis.QueryPlan = &models.QueryPlan{}
	}

	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "DegreeOfParallelism":
			p.planAnalysis.QueryPlan.DegreeOfParallelism = parseInt(attr.Value)
		case "MaxDegreeOfParallelism":
			p.planAnalysis.QueryPlan.MaxDegreeOfParallelism = parseInt(attr.Value)
		case "MemoryGrant":
			p.planAnalysis.QueryPlan.MemoryGrant = parseInt(attr.Value)
		case "CachedPlanSize":
			p.planAnalysis.QueryPlan.CachedPlanSize = parseInt(attr.Value)
		case "CompileTimeMS", "CompileTime":
			p.planAnalysis.QueryPlan.CompileTimeMs = parseInt(attr.Value)
		case "CompileCPU":
			p.planAnalysis.QueryPlan.CompileCPU = parseInt(attr.Value)
		case "CompileMemory":
			p.planAnalysis.QueryPlan.CompileMemory = parseInt(attr.Value)
		case "OptimizationLevel":
			p.planAnalysis.QueryPlan.OptimizationLevel = attr.Value
		case "UseParallelBatches":
			p.planAnalysis.QueryPlan.UseParallelBatches = attr.Value == "1"
		}
	}
	return nil
}

func (p *Parser) parseOptimizerHardware(el xml.StartElement) error {
	if p.planAnalysis.QueryPlan == nil {
		p.planAnalysis.QueryPlan = &models.QueryPlan{}
	}

	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "EstimatedAvailableDegreeOfParallelism":
			fmt.Sscanf(attr.Value, "%d", &p.planAnalysis.QueryPlan.EstimatedAvailableDOP)
		case "EstimatedAvailableMemoryGrant":
			fmt.Sscanf(attr.Value, "%d", &p.planAnalysis.QueryPlan.EstimatedAvailableMemoryGrant)
		case "MaxCompileMemory":
			fmt.Sscanf(attr.Value, "%d", &p.planAnalysis.QueryPlan.MaxCompileMemory)
		}
	}
	return nil
}

func parseFloat(value string) float64 {
	if value == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(value, 64)
	return f
}

func parseInt64(value string) int64 {
	if value == "" {
		return 0
	}
	v, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return 0
		}
		return int64(f)
	}
	return v
}

func parseInt(value string) int {
	if value == "" {
		return 0
	}
	v, err := strconv.Atoi(value)
	if err != nil {
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return 0
		}
		return int(f)
	}
	return v
}

func (p *Parser) parseRelOp(el xml.StartElement) error {
	p.opIDCounter++
	op := &models.Operator{
		ID:          p.opIDCounter,
		Parallel:    false,
		Children:    make([]*models.Operator, 0),
		Warnings:    make([]models.Warning, 0),
		Depth:       p.depth,
	}

	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "PhysicalOp":
			op.PhysicalOp = attr.Value
		case "LogicalOp":
			op.LogicalOp = attr.Value
		case "NodeId":
			op.NodeID = parseInt(attr.Value)
		case "ParentNodeId":
			op.ParentNodeID = parseInt(attr.Value)
		case "EstimatedTotalSubtreeCost":
			op.EstimatedTotalSubtreeCost = parseFloat(attr.Value)
		case "EstimateCost":
			op.EstimateCost = parseFloat(attr.Value)
		case "EstimateRows":
			op.EstimateRows = parseInt64(attr.Value)
		case "EstimatedRowsRead":
			op.EstimatedRowsRead = parseInt64(attr.Value)
		case "EstimateCPU", "EstimateCPUms", "CPUCost":
			op.EstimateCPUms = parseFloat(attr.Value)
		case "EstimateIO":
			op.EstimatedIOs = parseFloat(attr.Value)
		case "AvgRowSize":
			op.AvgRowSize = parseFloat(attr.Value)
		case "EstimatedRebinds":
			op.EstimateRebinds = parseInt64(attr.Value)
		case "EstimatedRewinds":
			op.EstimateRewinds = parseInt64(attr.Value)
		case "Parallel":
			op.Parallel = attr.Value == "1" || attr.Value == "true"
		case "ParallelThreadCount":
			op.ParallelThreadCount = parseInt(attr.Value)
		case "ActualRows":
			op.ActualRows = parseInt64(attr.Value)
		case "ActualExecutions":
			op.ActualExecutions = parseInt(attr.Value)
		case "ActualCPUms":
			op.ActualCPUms = parseFloat(attr.Value)
		case "ActualElapsedms":
			op.ActualElapsedms = parseFloat(attr.Value)
		case "ActualRebinds":
			op.ActualRebinds = parseInt64(attr.Value)
		case "ActualRewinds":
			op.ActualRewinds = parseInt64(attr.Value)
		case "ActualSpills":
			op.ActualSpills = parseInt64(attr.Value)
		case "ActualLogicalReads":
			op.ActualLogicalReads = parseInt64(attr.Value)
		case "ActualPhysicalReads":
			op.ActualPhysicalReads = parseInt64(attr.Value)
		case "TableCardinality":
			op.TableCardinality = parseInt64(attr.Value)
		case "EstimatedExecutionMode":
			op.EstimatedExecutionMode = attr.Value
		}
	}

	p.operatorStack = append(p.operatorStack, op)
	p.depth++
	return nil
}

func (p *Parser) parseIndexScan(el xml.StartElement) error {
	if len(p.operatorStack) == 0 {
		return nil
	}

	op := p.operatorStack[len(p.operatorStack)-1]
	op.IndexScan = &models.IndexScan{}

	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "Object":
			objectStr := attr.Value
			objectStr = strings.Trim(objectStr, "[]")
			parts := strings.Split(objectStr, "].[")
			if len(parts) >= 2 {
				op.IndexScan.Object.Schema = strings.Trim(parts[0], "[]")
				op.IndexScan.Object.Table = strings.Trim(parts[1], "[]")
				if len(parts) >= 3 {
					op.IndexScan.Object.Index = strings.Trim(parts[2], "[]")
				}
			} else if len(parts) == 1 {
				op.IndexScan.Object.Table = strings.Trim(parts[0], "[]")
			}
		case "ScanType":
			op.IndexScan.ScanType = attr.Value
		case "IndexKind":
			op.IndexScan.IndexKind = attr.Value
		case "Index":
			op.IndexScan.Object.Index = attr.Value
		case "Database":
			op.IndexScan.Object.Table = attr.Value
		case "Schema":
			op.IndexScan.Object.Schema = attr.Value
		case "Table":
			op.IndexScan.Object.Table = attr.Value
		case "Alias":
			op.IndexScan.Object.Alias = attr.Value
		}
	}
	return nil
}

func (p *Parser) parseTableScan(el xml.StartElement) error {
	if len(p.operatorStack) == 0 {
		return nil
	}

	op := p.operatorStack[len(p.operatorStack)-1]
	op.TableScan = &models.TableScan{}

	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "Object":
			objectStr := attr.Value
			objectStr = strings.Trim(objectStr, "[]")
			parts := strings.Split(objectStr, "].[")
			if len(parts) >= 2 {
				op.TableScan.Object.Schema = strings.Trim(parts[0], "[]")
				op.TableScan.Object.Table = strings.Trim(parts[1], "[]")
			} else if len(parts) == 1 {
				op.TableScan.Object.Table = strings.Trim(parts[0], "[]")
			}
		case "Database":
			op.TableScan.Object.Table = attr.Value
		case "Schema":
			op.TableScan.Object.Schema = attr.Value
		case "Table":
			op.TableScan.Object.Table = attr.Value
		case "Alias":
			op.TableScan.Object.Alias = attr.Value
		}
	}
	return nil
}

func (p *Parser) parseObject(el xml.StartElement) error {
	if len(p.operatorStack) == 0 {
		return nil
	}

	op := p.operatorStack[len(p.operatorStack)-1]

	var object *models.IndexObject
	var tableScanObj *models.TableObject

	if op.IndexScan != nil {
		object = &op.IndexScan.Object
	} else if op.TableScan != nil {
		tableScanObj = &op.TableScan.Object
	} else {
		object = &models.IndexObject{}
	}

	for _, attr := range el.Attr {
		if object != nil {
			switch attr.Name.Local {
			case "Database":
				object.Database = strings.Trim(attr.Value, "[]")
			case "Schema":
				object.Schema = strings.Trim(attr.Value, "[]")
			case "Table":
				object.Table = strings.Trim(attr.Value, "[]")
			case "Index":
				object.Index = strings.Trim(attr.Value, "[]")
			case "Alias":
				object.Alias = strings.Trim(attr.Value, "[]")
			case "IndexKind":
				if op.IndexScan != nil {
					op.IndexScan.IndexKind = attr.Value
				}
			case "Storage":
				if op.IndexScan != nil {
					op.IndexScan.Storage = attr.Value
				}
			}
		} else if tableScanObj != nil {
			switch attr.Name.Local {
			case "Database":
				tableScanObj.Database = strings.Trim(attr.Value, "[]")
			case "Schema":
				tableScanObj.Schema = strings.Trim(attr.Value, "[]")
			case "Table":
				tableScanObj.Table = strings.Trim(attr.Value, "[]")
			case "Alias":
				tableScanObj.Alias = strings.Trim(attr.Value, "[]")
			}
		}
	}

	if op.IndexScan != nil && op.IndexScan.Object.Table == "" && object != nil {
		op.IndexScan.Object = *object
	} else if op.TableScan != nil && op.TableScan.Object.Table == "" && tableScanObj != nil {
		op.TableScan.Object = *tableScanObj
	}

	return nil
}

func (p *Parser) parseColumnReference(el xml.StartElement) error {
	if len(p.operatorStack) == 0 {
		return nil
	}

	if p.insideHashBuild || p.insideHashProbe {
		return p.parseHashKeyColumnReference(el)
	}

	op := p.operatorStack[len(p.operatorStack)-1]
	var outCol models.OutputColumn

	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "Schema":
			if outCol.Schema == "" {
				outCol.Schema = strings.Trim(attr.Value, "[]")
			}
		case "Alias":
			outCol.Schema = strings.Trim(attr.Value, "[]")
		case "Column":
			outCol.Column = strings.Trim(attr.Value, "[]")
		}
	}

	if outCol.Column != "" {
		if op.OutputList == nil {
			op.OutputList = []models.OutputColumn{}
		}
		key := outCol.Column + "|" + outCol.Schema
		duplicate := false
		for _, existing := range op.OutputList {
			if existing.Column == outCol.Column && existing.Schema == outCol.Schema {
				duplicate = true
				break
			}
		}
		if !duplicate {
			_ = key
			op.OutputList = append(op.OutputList, outCol)
		}
	}
	return nil
}

func (p *Parser) parseHashKeyColumnReference(el xml.StartElement) error {
	if len(p.operatorStack) == 0 {
		return nil
	}
	op := p.operatorStack[len(p.operatorStack)-1]
	if op.Hash == nil {
		return nil
	}

	var cr models.ColumnReference
	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "Database":
			cr.Database = strings.Trim(attr.Value, "[]")
		case "Schema":
			cr.Schema = strings.Trim(attr.Value, "[]")
		case "Table":
			cr.Table = strings.Trim(attr.Value, "[]")
		case "Alias":
			cr.Alias = strings.Trim(attr.Value, "[]")
		case "Column":
			cr.Column = strings.Trim(attr.Value, "[]")
		}
	}

	if cr.Column == "" {
		return nil
	}

	if p.insideHashBuild {
		op.Hash.HashKeysBuild = append(op.Hash.HashKeysBuild, cr)
	} else if p.insideHashProbe {
		op.Hash.HashKeysProbe = append(op.Hash.HashKeysProbe, cr)
	}
	return nil
}

func (p *Parser) parseWarnings(el xml.StartElement) error {
	var warning models.Warning
	hasWarning := false

	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "NoJoinPredicate":
			if attr.Value == "true" || attr.Value == "1" {
				warning.Type = models.WarningTypeNoJoinPredicate
				warning.Message = "Cartesian product detected - no join predicate"
				warning.Severity = models.SeverityHigh
				hasWarning = true
			}
		case "SpillToTempDb":
			warning.Type = models.WarningTypeSpillToTempDB
			warning.Message = "Operation spilled to tempdb"
			warning.Severity = models.SeverityHigh
			hasWarning = true
		case "NoStatistics":
			warning.Type = models.WarningTypeNoStatistics
			warning.Message = "Statistics not available"
			warning.Severity = models.SeverityMedium
			hasWarning = true
		case "TypeConversion":
			warning.Type = models.WarningTypeTypeConversion
			warning.Message = "Implicit type conversion detected"
			warning.Severity = models.SeverityMedium
			hasWarning = true
		case "CardinalityEstimateIssue":
			warning.Type = models.WarningTypeCardinalityEst
			warning.Message = "Cardinality estimation issue"
			warning.Severity = models.SeverityMedium
			hasWarning = true
		case "MissingIndex":
			warning.Type = models.WarningTypeMissingIndex
			warning.Message = "Missing index suggestion"
			warning.Severity = models.SeverityMedium
			hasWarning = true
		}
	}

	if hasWarning {
		p.warnings = append(p.warnings, warning)
	}
	return nil
}

func (p *Parser) parseMissingIndexes(el xml.StartElement) error {
	return nil
}

func (p *Parser) parseMissingIndexGroup(el xml.StartElement) error {
	for _, attr := range el.Attr {
		if attr.Name.Local == "Impact" {
			p.missingIdxImpact = parseFloat(attr.Value)
		}
	}
	return nil
}

func (p *Parser) parseMissingIndex(el xml.StartElement) error {
	mi := models.MissingIndex{
		ID: len(p.missingIdx) + 1,
	}
	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "Database":
			mi.Database = attr.Value
		case "Schema":
			mi.Schema = attr.Value
		case "Table":
			mi.Table = attr.Value
		}
	}
	p.currentMissingIdx = &mi
	return nil
}

func (p *Parser) parseColumnGroup(el xml.StartElement) error {
	for _, attr := range el.Attr {
		if attr.Name.Local == "Usage" {
			p.currentColUsage = attr.Value
		}
	}
	return nil
}

func (p *Parser) parseMissingIndexColumn(el xml.StartElement) error {
	if p.currentMissingIdx == nil {
		return nil
	}

	var colName string
	for _, attr := range el.Attr {
		if attr.Name.Local == "Name" {
			colName = attr.Value
		}
	}
	if colName == "" {
		return nil
	}

	switch p.currentColUsage {
	case "EQUALITY":
		p.currentMissingIdx.Columns = append(p.currentMissingIdx.Columns, models.MissingIndexColumn{
			Column:   colName,
			Equality: true,
		})
	case "INEQUALITY":
		p.currentMissingIdx.Columns = append(p.currentMissingIdx.Columns, models.MissingIndexColumn{
			Column:     colName,
			Inequality: true,
		})
	case "INCLUDE":
		p.currentMissingIdx.IncludedColumns = append(p.currentMissingIdx.IncludedColumns, colName)
	}
	return nil
}

func (p *Parser) parsePlanAffectingConvert(el xml.StartElement) error {
	convertIssue := ""
	expression := ""
	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "ConvertIssue":
			convertIssue = attr.Value
		case "Expression":
			expression = attr.Value
		}
	}

	msg := "Plan-affecting convert"
	if convertIssue != "" {
		msg += ": " + convertIssue
	}
	if expression != "" {
		msg += " - " + expression
	}

	p.warnings = append(p.warnings, models.Warning{
		Type:     models.WarningTypeCardinalityEst,
		Severity: models.SeverityMedium,
		Message:  msg,
	})
	return nil
}

func (p *Parser) parseRuntimeCounters(el xml.StartElement) error {
	if len(p.operatorStack) == 0 {
		return nil
	}

	counter := models.RuntimeCounter{}
	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "Thread":
			counter.Thread = parseInt(attr.Value)
		case "RowEstimate":
			counter.RowEstimate = parseInt64(attr.Value)
		case "EstimateRows":
			counter.RowEstimate = parseInt64(attr.Value)
		case "ActualRows":
			counter.ActualRows = parseInt64(attr.Value)
		case "ActualRowsRead":
			counter.ActualRowsRead = parseInt64(attr.Value)
		case "ActualExecutions":
			counter.ActualExecutions = parseInt(attr.Value)
		case "ActualElapsedms":
			counter.ActualElapsedms = parseFloat(attr.Value)
		case "ActualCPUms":
			counter.ActualCPUms = parseFloat(attr.Value)
		case "ActualLogicalReads":
			counter.ActualLogicalReads = parseInt64(attr.Value)
		case "ActualPhysicalReads":
			counter.ActualPhysicalReads = parseInt64(attr.Value)
		case "ActiveParallelThread":
			counter.ActiveParallelThread = parseInt(attr.Value)
		case "Batches":
			counter.Batches = parseInt(attr.Value)
		case "ActualEndOfScans":
			counter.ActualEndOfScans = parseInt(attr.Value)
		case "ActualScans":
			counter.ActualScans = parseInt(attr.Value)
		case "ActualRebinds":
			counter.ActualRebinds = parseInt64(attr.Value)
		case "ActualRewinds":
			counter.ActualRewinds = parseInt64(attr.Value)
		}
	}

	if counter.Thread > 0 || counter.ActualRows > 0 || counter.ActualExecutions > 0 {
		p.operatorStack[len(p.operatorStack)-1].RuntimeCounters = append(
			p.operatorStack[len(p.operatorStack)-1].RuntimeCounters,
			counter,
		)
	}
	return nil
}

func (p *Parser) parseMemoryGrant(el xml.StartElement) error {
	if p.planAnalysis.QueryPlan == nil {
		p.planAnalysis.QueryPlan = &models.QueryPlan{}
	}

	p.planAnalysis.QueryPlan.HasMemoryGrant = true

	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "GrantedMemory":
			p.planAnalysis.QueryPlan.MemoryGrantInfo.GrantedMemory = parseInt(attr.Value)
		case "MaxUsedMemory":
			p.planAnalysis.QueryPlan.MemoryGrantInfo.MaxUsedMemory = parseInt(attr.Value)
		case "IdealMemory":
			p.planAnalysis.QueryPlan.MemoryGrantInfo.IdealMemory = parseInt(attr.Value)
		case "TransferMode":
			p.planAnalysis.QueryPlan.MemoryGrantInfo.TransferMode = attr.Value
		case "SerialRequiredMemory":
			p.planAnalysis.QueryPlan.MemoryGrantInfo.SerialRequiredMemory = parseInt(attr.Value)
		case "SerialDesiredMemory":
			p.planAnalysis.QueryPlan.MemoryGrantInfo.SerialDesiredMemory = parseInt64(attr.Value)
		case "MemoryGrant":
			p.planAnalysis.QueryPlan.MemoryGrant = parseInt(attr.Value)
		case "RequestedMemory":
			p.planAnalysis.QueryPlan.MemoryGrantInfo.RequestedMemory = parseInt(attr.Value)
		}
	}
	return nil
}

func (p *Parser) parseGenericOperator(el xml.StartElement) error {
	return nil
}

func (p *Parser) parseTop(el xml.StartElement) error {
	if len(p.operatorStack) == 0 {
		return nil
	}

	op := p.operatorStack[len(p.operatorStack)-1]
	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "RowCount":
			op.TopRowCount = attr.Value
		case "IsPercent":
			op.TopIsPercent = attr.Value == "true" || attr.Value == "1"
		case "WithTies":
			op.TopWithTies = attr.Value == "true" || attr.Value == "1"
		}
	}
	return nil
}

func (p *Parser) parseSeekPredicates(el xml.StartElement) error {
	p.predicateType = "seek"
	if len(p.operatorStack) == 0 {
		return nil
	}
	op := p.operatorStack[len(p.operatorStack)-1]
	sp := models.SeekPredicate{}
	for _, attr := range el.Attr {
		if attr.Name.Local == "SeekType" {
			sp.SeekType = attr.Value
		}
	}
	op.SeekPredicates = append(op.SeekPredicates, sp)
	return nil
}

func (p *Parser) parsePredicate(el xml.StartElement) error {
	p.predicateType = "residual"
	return nil
}

func (p *Parser) parseScalarOperator(el xml.StartElement) error {
	if p.predicateType == "" && !p.insideProbeResidual && !p.insideBuildResidual {
		return nil
	}
	if len(p.operatorStack) == 0 {
		return nil
	}
	op := p.operatorStack[len(p.operatorStack)-1]

	var scalarString string
	for _, attr := range el.Attr {
		if attr.Name.Local == "ScalarString" {
			scalarString = attr.Value
			break
		}
	}

	if p.insideProbeResidual || p.insideBuildResidual {
		if scalarString == "" {
			return nil
		}
		if op.Hash == nil {
			op.Hash = &models.HashMatch{}
		}
		if p.insideProbeResidual && op.Hash.ProbeResidual == "" {
			op.Hash.ProbeResidual = scalarString
		} else if p.insideBuildResidual && op.Hash.BuildResidual == "" {
			op.Hash.BuildResidual = scalarString
		}
		return nil
	}

	if scalarString == "" {
		return nil
	}

	switch p.predicateType {
	case "residual":
		if op.Predicate == nil {
			op.Predicate = &models.Predicate{}
		}
		op.Predicate.ScalarString = scalarString
	case "seek":
		if len(op.SeekPredicates) > 0 {
			lastIdx := len(op.SeekPredicates) - 1
			op.SeekPredicates[lastIdx].PrefixPredicate = append(
				op.SeekPredicates[lastIdx].PrefixPredicate,
				models.PrefixPredicate{ScalarString: scalarString},
			)
		} else {
			sp := models.SeekPredicate{
				SeekType: "Seek",
				PrefixPredicate: []models.PrefixPredicate{
					{ScalarString: scalarString},
				},
			}
			op.SeekPredicates = append(op.SeekPredicates, sp)
		}
	}
	return nil
}

func (p *Parser) parseMemoryFractions(el xml.StartElement) error {
	if len(p.operatorStack) == 0 {
		return nil
	}

	op := p.operatorStack[len(p.operatorStack)-1]
	mf := models.MemoryFractions{}

	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "Input":
			mf.Input = parseFloat(attr.Value)
		case "Output":
			mf.Output = parseFloat(attr.Value)
		}
	}

	if op.MemoryFractions == nil {
		op.MemoryFractions = &mf
	}
	return nil
}

func (p *Parser) parseHash(el xml.StartElement) error {
	if len(p.operatorStack) == 0 {
		return nil
	}
	op := p.operatorStack[len(p.operatorStack)-1]
	if op.Hash == nil {
		op.Hash = &models.HashMatch{}
	}
	return nil
}

func (p *Parser) parseHashKeysBuild(el xml.StartElement) error {
	if len(p.operatorStack) == 0 {
		return nil
	}
	op := p.operatorStack[len(p.operatorStack)-1]
	if op.Hash == nil {
		op.Hash = &models.HashMatch{}
	}
	p.insideHashBuild = true
	return nil
}

func (p *Parser) parseHashKeysProbe(el xml.StartElement) error {
	if len(p.operatorStack) == 0 {
		return nil
	}
	op := p.operatorStack[len(p.operatorStack)-1]
	if op.Hash == nil {
		op.Hash = &models.HashMatch{}
	}
	p.insideHashProbe = true
	return nil
}

func (p *Parser) parseJoinResidual(el xml.StartElement) error {
	switch el.Name.Local {
	case "ProbeResidual":
		p.insideProbeResidual = true
	case "BuildResidual":
		p.insideBuildResidual = true
	}
	return nil
}

func (p *Parser) collectOperators(root *models.Operator) []models.Operator {
	if root == nil {
		return []models.Operator{}
	}

	result := []models.Operator{}
	queue := []*models.Operator{root}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		result = append(result, *current)
		queue = append(queue, current.Children...)
	}

	return result
}

func (p *Parser) parseShowPlanXML(el xml.StartElement) error {
	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "Version":
			p.planAnalysis.Version = attr.Value
		case "Build":
			p.planAnalysis.Build = attr.Value
		}
	}
	return nil
}

func (p *Parser) parseQueryTimeStats(el xml.StartElement) error {
	if p.planAnalysis.QueryPlan == nil {
		p.planAnalysis.QueryPlan = &models.QueryPlan{}
	}

	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "ElapsedMs", "ElapsedTime":
			p.planAnalysis.QueryPlan.QueryTimeStats.ElapsedMs = parseInt(attr.Value)
		case "CpuMs", "CpuTime":
			p.planAnalysis.QueryPlan.QueryTimeStats.CpuMs = parseInt(attr.Value)
		case "RowCount":
			p.planAnalysis.QueryPlan.QueryTimeStats.RowCount = parseInt(attr.Value)
		}
	}
	return nil
}

func (p *Parser) parseThreadStat(el xml.StartElement) error {
	if p.planAnalysis.QueryPlan == nil {
		p.planAnalysis.QueryPlan = &models.QueryPlan{}
	}

	threadStat := models.ThreadStat{}
	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "Thread":
			fmt.Sscanf(attr.Value, "%d", &threadStat.ThreadCount)
		case "Activity":
			threadStat.Activity = attr.Value
		}
	}
	p.planAnalysis.QueryPlan.ThreadStats = append(p.planAnalysis.QueryPlan.ThreadStats, threadStat)
	return nil
}

func (p *Parser) parseWaitStats(el xml.StartElement) error {
	if p.planAnalysis.QueryPlan == nil {
		p.planAnalysis.QueryPlan = &models.QueryPlan{}
	}
	return nil
}

func (p *Parser) parseWait(el xml.StartElement) error {
	if p.planAnalysis.QueryPlan == nil {
		p.planAnalysis.QueryPlan = &models.QueryPlan{}
	}

	var waitStat models.WaitStat
	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "WaitType":
			waitStat.WaitType = attr.Value
		case "WaitTimeMs":
			waitStat.WaitTimeMs = parseInt(attr.Value)
		case "WaitCount":
			waitStat.WaitCount = parseInt(attr.Value)
		}
	}
	if waitStat.WaitType != "" {
		p.planAnalysis.QueryPlan.WaitStats = append(p.planAnalysis.QueryPlan.WaitStats, waitStat)
	}
	return nil
}

func (p *Parser) parseOptimizerStats(el xml.StartElement) error {
	if p.planAnalysis.QueryPlan == nil {
		p.planAnalysis.QueryPlan = &models.QueryPlan{}
	}

	stats := models.OptimizerStatsUsage{}
	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "Predicate":
			stats.Predicate = attr.Value
		case "TableCardinality":
			fmt.Sscanf(attr.Value, "%d", &stats.TableCard)
		}
	}
	p.planAnalysis.QueryPlan.OptimizerStatsUsage = append(p.planAnalysis.QueryPlan.OptimizerStatsUsage, stats)
	return nil
}

func (p *Parser) parseSpillWarning(el xml.StartElement) error {
	warning := models.Warning{
		Type:     models.WarningTypeSpillToTempDB,
		Severity:  models.SeverityHigh,
		Message:  "Operation spilled to tempdb",
	}

	p.warnings = append(p.warnings, warning)
	return nil
}

func (p *Parser) parseStatement(el xml.StartElement) error {
	if p.planAnalysis.QueryPlan == nil {
		p.planAnalysis.QueryPlan = &models.QueryPlan{}
	}

	for _, attr := range el.Attr {
		switch attr.Name.Local {
		case "StatementText":
			p.planAnalysis.Metadata.StatementText = attr.Value
			p.planAnalysis.Metadata.QueryText = attr.Value
		case "StatementType":
			p.planAnalysis.Metadata.StatementType = attr.Value
		case "StatementEstRows":
			p.planAnalysis.Metadata.StatementEstLines = int(parseInt64(attr.Value))
		case "StatementSubTreeCost":
			cost := parseFloat(attr.Value)
			if p.planAnalysis.QueryPlan != nil && p.planAnalysis.QueryPlan.RelOp != nil {
				p.planAnalysis.QueryPlan.RelOp.EstimatedTotalSubtreeCost = cost
			}
		case "StatementOptmLevel":
			p.planAnalysis.Metadata.StatementOpti = attr.Value
		case "StatementCompId":
			p.planAnalysis.Metadata.StatementCompO = parseInt(attr.Value)
		case "StatementId":
			p.planAnalysis.Metadata.StatementId = attr.Value
		case "QueryHash":
			p.planAnalysis.Metadata.QueryHash = attr.Value
		case "QueryPlanHash":
			p.planAnalysis.Metadata.QueryPlanHash = attr.Value
		case "CardinalityEstimationModelVersion":
			p.planAnalysis.Metadata.CEVersion = attr.Value
		case "RetrievedFromCache":
			p.planAnalysis.Metadata.RetrievedFromCache = attr.Value == "true" || attr.Value == "1"
		case "DatabaseContextSettingsId":
			p.planAnalysis.Metadata.DatabaseContextSettingsId = attr.Value
		case "ParentObjectId":
			p.planAnalysis.Metadata.ParentObjectId = parseInt(attr.Value)
		case "StatementParameterizationType":
			p.planAnalysis.Metadata.StatementParameterizationType = attr.Value
		}
	}
	return nil
}

func convertToUTF8(data []byte) []byte {
	if len(data) >= 2 {
		if data[0] == 0xFF && data[1] == 0xFE {
			result, _ := convertUTF16LEToUTF8(data[2:])
			if len(result) > 100 {
				return result
			}
		}

		if data[0] == 0xFE && data[1] == 0xFF {
			result, _ := convertUTF16BEToUTF8(data[2:])
			if len(result) > 100 {
				return result
			}
		}
	}

	first := string(data[:min(200, len(data))])
	first = strings.TrimSpace(first)

	for _, line := range strings.Split(first, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "<?xml") && strings.Contains(line, "encoding=") {
			enc := extractEncoding(line)
			if enc != "" && strings.ToUpper(enc) != "UTF-8" {
				isoData := convertEncoding(data, enc)
				if isoData != nil {
					return isoData
				}
			}
			break
		}
	}

	dataStr := string(data)
	if !strings.Contains(dataStr, "\x00") {
		return data
	}

	result, _ := convertUTF16LEToUTF8(data)
	if len(result) > 100 {
		return result
	}
	return data
}

func extractEncoding(xmlDecl string) string {
	idx := strings.Index(xmlDecl, "encoding=")
	if idx == -1 {
		return ""
	}
	rest := xmlDecl[idx+len("encoding="):]
	rest = strings.TrimPrefix(rest, `"`)
	rest = strings.TrimPrefix(rest, "'")
	endIdx := len(rest)
	for i, c := range rest {
		if c == '"' || c == '\'' {
			endIdx = i
			break
		}
	}
	return rest[:endIdx]
}

func convertEncoding(data []byte, encoding string) []byte {
	encoding = strings.ToUpper(encoding)

	switch encoding {
	case "UTF-16", "UTF-16LE", "UTF-16BE":
		utf16Data, err := convertUTF16ToUTF8(data, encoding)
		if err == nil {
			return utf16Data
		}
	case "WINDOWS-1252", "ISO-8859-1":
		return windows1252ToUTF8(data)
	}

	return nil
}

func convertUTF16ToUTF8(data []byte, encoding string) ([]byte, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("data too short for UTF-16")
	}

	isLE := true
	if encoding == "UTF-16BE" {
		isLE = false
	} else if len(data) >= 4 {
		if data[0] == 0xFE && data[1] == 0xFF {
			isLE = false
		} else if data[0] == 0xFF && data[1] == 0xFE {
			isLE = true
		}
	}

	if isLE {
		return convertUTF16LEToUTF8(data[2:])
	}
	return convertUTF16BEToUTF8(data[2:])
}

func convertUTF16LEToUTF8(data []byte) ([]byte, error) {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}

	result := make([]byte, 0, len(data))
	for i := 0; i < len(data); i += 2 {
		if i+1 >= len(data) {
			break
		}
		lo := data[i]
		hi := data[i+1]
		char := uint16(lo) | (uint16(hi) << 8)

		if char < 0x80 {
			result = append(result, byte(char))
		} else if char < 0x800 {
			result = append(result, 0xC0|byte(char>>6), 0x80|byte(char&0x3F))
		} else if char < 0xFFFF {
			result = append(result, 0xE0|byte(char>>12), 0x80|byte((char>>6)&0x3F), 0x80|byte(char&0x3F))
		}
	}

	return result, nil
}

func convertUTF16BEToUTF8(data []byte) ([]byte, error) {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}

	result := make([]byte, 0, len(data))
	for i := 0; i < len(data); i += 2 {
		if i+1 >= len(data) {
			break
		}
		hi := data[i]
		lo := data[i+1]
		char := uint16(hi) << 8 | uint16(lo)

		if char < 0x80 {
			result = append(result, byte(char))
		} else if char < 0x800 {
			result = append(result, 0xC0|byte(char>>6), 0x80|byte(char&0x3F))
		} else if char < 0xFFFF {
			result = append(result, 0xE0|byte(char>>12), 0x80|byte((char>>6)&0x3F), 0x80|byte(char&0x3F))
		}
	}

	return result, nil
}

func windows1252ToUTF8(data []byte) []byte {
	win1252 := []rune{}
	for _, b := range data {
		if b < 0x80 {
			win1252 = append(win1252, rune(b))
		} else {
			win1252 = append(win1252, rune(b))
		}
	}
	return []byte(string(win1252))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
