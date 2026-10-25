package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
)

type PlanNode struct {
	Depth       int
	Prefix      string
	Node        string
	Relation    string
	Index       string
	Join        string
	StartupCost float64
	TotalCost   float64
	PlanRows    float64
	HasActual   bool
	ActualRows  float64
	ActualTime  float64
	Loops       float64
}

type StatementPlan struct {
	Number        int
	SQL           string
	NoPlan        bool
	Error         string
	Nodes         []PlanNode
	HasTiming     bool
	PlanningTime  float64
	ExecutionTime float64
}

type Explanation struct {
	Analyze    bool
	Statements []StatementPlan
	Stopped    bool
}

type rawNode struct {
	NodeType        string    `json:"Node Type"`
	Operation       string    `json:"Operation"`
	RelationName    string    `json:"Relation Name"`
	IndexName       string    `json:"Index Name"`
	JoinType        string    `json:"Join Type"`
	StartupCost     float64   `json:"Startup Cost"`
	TotalCost       float64   `json:"Total Cost"`
	PlanRows        float64   `json:"Plan Rows"`
	ActualRows      *float64  `json:"Actual Rows"`
	ActualTotalTime *float64  `json:"Actual Total Time"`
	ActualLoops     *float64  `json:"Actual Loops"`
	Plans           []rawNode `json:"Plans"`
}

type rawExplain struct {
	Plan          rawNode  `json:"Plan"`
	PlanningTime  *float64 `json:"Planning Time"`
	ExecutionTime *float64 `json:"Execution Time"`
}

var explainKeywords = map[string]bool{"select": true, "insert": true, "update": true, "delete": true, "with": true, "values": true, "table": true, "merge": true}

func SplitStatements(sql string) []string {
	var out []string
	start, i := 0, 0
	for i < len(sql) {
		switch {
		case strings.HasPrefix(sql[i:], "--"):
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case strings.HasPrefix(sql[i:], "/*"):
			if end := strings.Index(sql[i+2:], "*/"); end < 0 {
				i = len(sql)
			} else {
				i += end + 4
			}
		case sql[i] == '\'' || sql[i] == '"' || sql[i] == '`':
			q := sql[i]
			i++
			for i < len(sql) {
				if sql[i] == q {
					if i+1 < len(sql) && sql[i+1] == q {
						i += 2
						continue
					}
					break
				}
				i++
			}
			i++
		case sql[i] == '$' && dollarTag.MatchString(sql[i:]):
			tag := dollarTag.FindString(sql[i:])
			if end := strings.Index(sql[i+len(tag):], tag); end < 0 {
				i = len(sql)
			} else {
				i += len(tag) + end + len(tag)
			}
		case sql[i] == ';':
			out = appendStatement(out, sql[start:i])
			i++
			start = i
		default:
			i++
		}
	}
	if start < len(sql) {
		out = appendStatement(out, sql[start:])
	}
	return out
}

func appendStatement(out []string, s string) []string {
	if strings.TrimSpace(stripNoise(s)) == "" {
		return out
	}
	return append(out, strings.TrimSpace(s))
}

func Explainable(stmt string) bool {
	s := strings.TrimLeft(stripNoise(stmt), " \t\r\n(")
	end := strings.IndexFunc(s, func(r rune) bool { return !unicode.IsLetter(r) })
	if end >= 0 {
		s = s[:end]
	}
	return explainKeywords[strings.ToLower(s)]
}

func ParsePlan(raw []byte) ([]PlanNode, *float64, *float64, error) {
	var list []rawExplain
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, nil, nil, err
	}
	if len(list) == 0 {
		return nil, nil, nil, fmt.Errorf("hasil EXPLAIN kosong")
	}
	var nodes []PlanNode
	flatten(&nodes, list[0].Plan, 0, "", "")
	return nodes, list[0].PlanningTime, list[0].ExecutionTime, nil
}

func flatten(out *[]PlanNode, n rawNode, depth int, prefix, guide string) {
	node := n.NodeType
	if n.Operation != "" {
		node += " (" + n.Operation + ")"
	}
	p := PlanNode{Depth: depth, Prefix: prefix, Node: node, Relation: n.RelationName, Index: n.IndexName, Join: n.JoinType, StartupCost: n.StartupCost, TotalCost: n.TotalCost, PlanRows: n.PlanRows}
	if n.ActualRows != nil {
		p.HasActual = true
		p.ActualRows = *n.ActualRows
		if n.ActualTotalTime != nil {
			p.ActualTime = *n.ActualTotalTime
		}
		if n.ActualLoops != nil {
			p.Loops = *n.ActualLoops
		}
	}
	*out = append(*out, p)
	for i, child := range n.Plans {
		branch, next := "├─ ", "│  "
		if i == len(n.Plans)-1 {
			branch, next = "└─ ", "   "
		}
		flatten(out, child, depth+1, guide+branch, guide+next)
	}
}

func (e *Executor) Explain(ctx context.Context, dbID int64, sql string, analyze bool) (*Explanation, error) {
	if et, ok := e.targets.(SQLTargets); ok {
		if eng, err := et.Engine(ctx, dbID); err == nil && dbconn.IsMySQLFamily(eng) {
			return nil, ErrExplainUnsupported
		}
	}
	pool, err := e.targets.Pool(ctx, dbID)
	if err != nil {
		return nil, err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTargetUnavailable, dbconn.Wrap(err))
	}
	defer conn.Release()
	ctx, cancel := context.WithTimeout(ctx, e.Timeout+5*time.Second)
	defer cancel()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTargetUnavailable, dbconn.Wrap(err))
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", e.Timeout.Milliseconds())); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '10s'"); err != nil {
		return nil, err
	}
	ex := &Explanation{Analyze: analyze}
	for i, stmt := range SplitStatements(sql) {
		sp := explainOne(ctx, tx, stmt, analyze)
		sp.Number = i + 1
		ex.Statements = append(ex.Statements, sp)
		if sp.Error != "" {
			ex.Stopped = true
			break
		}
	}
	return ex, nil
}

func explainOne(ctx context.Context, tx pgx.Tx, stmt string, analyze bool) StatementPlan {
	sp := StatementPlan{SQL: stmt}
	if !Explainable(stmt) {
		if _, err := tx.Exec(ctx, stmt, pgx.QueryExecModeSimpleProtocol); err != nil {
			sp.Error = truncate(err.Error(), 500)
			return sp
		}
		sp.NoPlan = true
		return sp
	}
	opts := "FORMAT JSON"
	if analyze {
		opts = "ANALYZE, FORMAT JSON"
	}
	var raw string
	if err := tx.QueryRow(ctx, "EXPLAIN ("+opts+") "+stmt, pgx.QueryExecModeSimpleProtocol).Scan(&raw); err != nil {
		sp.Error = truncate(err.Error(), 500)
		return sp
	}
	nodes, planning, execution, err := ParsePlan([]byte(raw))
	if err != nil {
		sp.Error = "Hasil EXPLAIN tidak dapat dibaca."
		return sp
	}
	sp.Nodes = nodes
	if planning != nil {
		sp.HasTiming = true
		sp.PlanningTime = *planning
	}
	if execution != nil {
		sp.HasTiming = true
		sp.ExecutionTime = *execution
	}
	return sp
}
