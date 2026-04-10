package shell

import (
	"fmt"
	"strings"
)

// StatementKind identifies the type of a parsed statement.
type StatementKind string

const (
	StmtExport      StatementKind = "export"
	StmtSpawn       StatementKind = "spawn"
	StmtPipeline    StatementKind = "pipeline"
	StmtIf          StatementKind = "if"
	StmtFor         StatementKind = "for"
	StmtWhile       StatementKind = "while"
	StmtBuiltin     StatementKind = "builtin"
	StmtFnDef       StatementKind = "fn-def"
	StmtFnCall      StatementKind = "fn-call"
	StmtReturn      StatementKind = "return"
	StmtArrayLit    StatementKind = "array-lit"
	StmtMapLit      StatementKind = "map-lit"
	StmtAssignIndex StatementKind = "assign-index"
	StmtAssignProp  StatementKind = "assign-prop"
	StmtParallel    StatementKind = "parallel"
	StmtSource      StatementKind = "source"
)

// MaxLoopIterations is the safety limit for while loops to prevent infinite execution.
const MaxLoopIterations = 10000

// MaxCallDepth is the safety limit for nested function calls to prevent infinite recursion.
const MaxCallDepth = 100

// ExportStmt holds a parsed export KEY=VALUE.
type ExportStmt struct {
	Key   string
	Value string
}

// Condition represents a binary comparison: $VAR.PROP OP VALUE or $VAR OP VALUE.
type Condition struct {
	VarName  string
	Property string // "exitcode", "result", or "" for plain variable
	Operator string // "==" or "!="
	Value    string
}

// IfBlock holds a parsed if/else/end block with nested statement lists.
type IfBlock struct {
	Condition Condition
	Then      []Statement
	Else      []Statement
}

// ForBlock holds a parsed for/in/end loop with variable binding and iteration list.
type ForBlock struct {
	VarName string
	List    []string
	Body    []Statement
}

// WhileBlock holds a parsed while/end loop with a condition.
type WhileBlock struct {
	Condition Condition
	Body      []Statement
}

// ParallelBlock holds a parsed parallel/end block. Body is restricted to
// StmtSpawn and StmtPipeline statements only.
type ParallelBlock struct {
	Body []Statement
}

// SourceStmt holds a parsed source statement: source <path>
type SourceStmt struct {
	Path string // source target file path (raw value, may contain variables)
}

// BuiltinStmt holds a parsed builtin command (wait, sleep, exit).
type BuiltinStmt struct {
	Command string
	Args    []string
}

// FnDef holds a parsed function definition: fn NAME(PARAMS) ... end
type FnDef struct {
	Name   string
	Params []string
	Body   []Statement
}

// FnCallStmt holds a parsed function call: NAME(ARGS) or VAR = NAME(ARGS)
type FnCallStmt struct {
	Name string
	Args []string
}

// ReturnStmt holds a parsed return statement inside a function body.
type ReturnStmt struct {
	Value string
}

// ArrayLitStmt holds a parsed array literal: VAR = ["a", "b", "c"]
type ArrayLitStmt struct {
	Items []string
}

// MapLitStmt holds a parsed map literal: VAR = {key: "value", key2: "value2"}
type MapLitStmt struct {
	Entries []MapEntry
}

// MapEntry is a single key-value pair in a map literal.
type MapEntry struct {
	Key   string
	Value string
}

// IndexAssign holds a parsed index assignment: VAR[N] = VALUE
type IndexAssign struct {
	VarName string
	Index   string
	Value   string
}

// PropAssign holds a parsed property assignment: VAR.KEY = VALUE
type PropAssign struct {
	VarName  string
	Property string
	Value    string
}

// ErrScriptExit signals a controlled script termination via the exit builtin.
type ErrScriptExit struct {
	Code int
}

func (e *ErrScriptExit) Error() string {
	return fmt.Sprintf("script exit with code %d", e.Code)
}

// ErrFnReturn signals a function return for flow control (not a real error).
type ErrFnReturn struct {
	Value string
}

func (e *ErrFnReturn) Error() string {
	return fmt.Sprintf("function return: %s", e.Value)
}

// SpawnResult captures the outcome of an assignment spawn for condition evaluation.
type SpawnResult struct {
	ExitCode int
	Result   string
	Tokens   int
}

// Statement is a single parsed line of a script.
type Statement struct {
	Kind        StatementKind
	Export      *ExportStmt
	Spawn       *Command
	Pipeline    *Pipeline
	If          *IfBlock
	For         *ForBlock
	While       *WhileBlock
	Parallel    *ParallelBlock
	Source      *SourceStmt
	Builtin     *BuiltinStmt
	FnDef       *FnDef
	FnCall      *FnCallStmt
	Return      *ReturnStmt
	ArrayLit    *ArrayLitStmt
	MapLit      *MapLitStmt
	IndexAssign *IndexAssign
	PropAssign  *PropAssign
	Assign      string   // variable name for assignment spawn/fn-call/array-lit/map-lit
	OnError     *Command // on-error handler spawn command
	Raw         string
	Line        int // 1-based line number from source
}

// Script is a sequence of parsed statements.
type Script struct {
	Statements []Statement
	Functions  map[string]*FnDef
}

// StripShebang removes a leading shebang line (#!...) from script content.
// Exported for use by `rnix run` command.
func StripShebang(content string) string {
	return stripShebang(content)
}

// reservedKeywords contains keywords that cannot be used as variable names.
var reservedKeywords = map[string]bool{
	"for": true, "in": true, "while": true, "if": true, "else": true, "end": true,
	"fn": true, "return": true, "parallel": true, "source": true,
	"wait": true, "sleep": true, "exit": true,
	"export": true, "spawn": true,
}

var builtinFunctions = map[string]int{
	"len": 1, "append": 2, "keys": 1,
}

func isReservedKeyword(s string) bool {
	return reservedKeywords[strings.ToLower(s)]
}

func isValidIdentifier(s string) bool {
	if len(s) == 0 || !isVarStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isVarChar(s[i]) {
			return false
		}
	}
	return true
}

func isValidVarName(s string) bool {
	if len(s) == 0 || !isVarStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isVarChar(s[i]) {
			return false
		}
	}
	return !isReservedKeyword(s)
}

func isBuiltinKeyword(lower string) bool {
	fields := strings.Fields(lower)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "wait", "sleep", "exit":
		return true
	}
	return false
}

func stripShebang(content string) string {
	if strings.HasPrefix(content, "#!") {
		if _, after, ok := strings.Cut(content, "\n"); ok {
			return after
		}
		return ""
	}
	return content
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
