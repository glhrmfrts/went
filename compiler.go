package elo

import (
  "fmt"
  "math"
  "github.com/glhrmfrts/elo/ast"
)

type (
  CompileError struct {
    Line    int
    File    string
    Message string
  }

  // holds registers for a expression
  exprdata struct {
    propagate bool
    rega      int // rega is default for write
    regb      int // regb is default for read
  }

  // lexical scope of a name
  scope int

  // lexical context of a block, (function, loop, branch...)
  blockcontext int

  // information of a name in the program
  nameinfo struct {
    isConst bool
    value   Value // only set if isConst == true
    reg     int
    scope   scope
    block   *compilerblock
  }

  // lexical block structure for compiler
  compilerblock struct {
    context       blockcontext
    start         uint32
    register      int
    pendingBreaks []uint32
    names         map[string]*nameinfo
    proto         *FuncProto
    parent        *compilerblock
  }

  compiler struct {
    lastLine int
    filename string
    mainFunc *FuncProto
    block    *compilerblock
  }
)

// names lexical scopes
const (
  kScopeLocal scope = iota
  kScopeRef
  kScopeGlobal
)

// blocks context
const (
  kBlockContextFunc blockcontext = iota
  kBlockContextLoop
  kBlockContextBranch
)

// How much registers an array can append at one time
// when it's created in literal form (see VisitArray)
const kArrayMaxRegisters = 10


func (err *CompileError) Error() string {
  return fmt.Sprintf("%s:%d: %s", err.File, err.Line, err.Message)
}

//
// compilerblock
//

func newCompilerBlock(proto *FuncProto, context blockcontext, parent *compilerblock) *compilerblock {
  return &compilerblock{
    proto: proto,
    context: context,
    parent: parent,
    names: make(map[string]*nameinfo, 128),
  }
}

func (b *compilerblock) nameInfo(name string) (*nameinfo, bool) {
  var closures int
  block := b
  for block != nil {
    info, ok := block.names[name]
    if ok {
      if closures > 0 && info.scope == kScopeLocal {
        info.scope = kScopeRef
      }
      return info, true
    }
    if block.context == kBlockContextFunc {
      closures++
    }
    block = block.parent
  }

  return nil, false
}

func (b *compilerblock) addNameInfo(name string, info *nameinfo) {
  info.block = b
  b.names[name] = info
}


//
// helper functions
//

func (c *compiler) error(line int, msg string) {
  panic(&CompileError{Line: line, File: c.filename, Message: msg})
}

func (c *compiler) emitInstruction(instr uint32, line int) int {
  f := c.block.proto
  f.Code = append(f.Code, instr)
  f.NumCode++

  if line != c.lastLine || f.NumLines == 0 {
    f.Lines = append(f.Lines, LineInfo{f.NumCode - 1, uint16(line)})
    f.NumLines++
    c.lastLine = line
  }
  return int(f.NumCode - 1)
}

func (c *compiler) modifyInstruction(index int, instr uint32) bool {
  f := c.block.proto
  if uint32(index) < f.NumCode {
    f.Code[index] = instr
    return true
  }
  return false
}

func (c *compiler) emitAB(op Opcode, a, b, line int) int {
  return c.emitInstruction(OpNewAB(op, a, b), line)
}

func (cc *compiler) emitABC(op Opcode, a, b, c, line int) int {
  return cc.emitInstruction(OpNewABC(op, a, b, c), line)
}

func (c *compiler) emitABx(op Opcode, a, b, line int) int {
  return c.emitInstruction(OpNewABx(op, a, b), line)
}

func (c *compiler) emitAsBx(op Opcode, a, b, line int) int {
  return c.emitInstruction(OpNewAsBx(op, a, b), line)
}

func (c *compiler) modifyABx(index int, op Opcode, a, b int) bool {
  return c.modifyInstruction(index, OpNewABx(op, a, b))
}

func (c *compiler) modifyAsBx(index int, op Opcode, a, b int) bool {
  return c.modifyInstruction(index, OpNewAsBx(op, a, b))
}

func (c *compiler) newLabel() uint32 {
  return c.block.proto.NumCode
}

func (c *compiler) labelOffset(label uint32) int {
  return int(c.block.proto.NumCode - label)
}

func (c *compiler) genRegister() int {
  id := c.block.register
  c.block.register++
  return id
}

func (c *compiler) enterBlock(context blockcontext) {
  assert(c.block != nil, "c.block enterBlock")
  block := newCompilerBlock(c.block.proto, context, c.block)
  block.start = block.proto.NumCode
  block.register = c.block.register
  c.block = block
}

func (c *compiler) leaveBlock() {
  block := c.block
  if block.context == kBlockContextLoop {
    end := block.proto.NumCode - 1
    for _, index := range block.pendingBreaks {
      c.modifyAsBx(int(index), OP_JMP, 0, int(end - index))
    }
  }
  c.block = block.parent
}

// Add a constant to the current prototype's constant pool
// and return it's index
func (c *compiler) addConst(value Value) int {
  f := c.block.proto
  valueType := value.Type()
  for i, c := range f.Consts {
    if c.Type() == valueType && c == value {
      return i
    }
  }
  if f.NumConsts > funcMaxConsts - 1 {
    c.error(0, "too many constants") // should never happen
  }
  f.Consts = append(f.Consts, value)
  f.NumConsts++
  return int(f.NumConsts - 1)
}

// Try to "constant fold" an expression
func (c *compiler) constFold(node ast.Node) (Value, bool) {
  switch t := node.(type) {
  case *ast.Number:
    return Number(t.Value), true
  case *ast.Bool:
    return Bool(t.Value), true
  case *ast.String:
    return String(t.Value), true
  case *ast.Id:
    info, ok := c.block.nameInfo(t.Value)
    if ok && info.isConst {
      return info.value, true
    }
  case *ast.UnaryExpr:
    if t.Op == ast.T_MINUS {
      val, ok := c.constFold(t.Right)
      if ok && val.Type() == VALUE_NUMBER {
        f64, _ := val.assertFloat64()
        return Number(-f64), true
      }
      return nil, false
    } else {
      // 'not' operator
      val, ok := c.constFold(t.Right)
      if ok && val.Type() == VALUE_BOOL {
        bool_, _ := val.assertBool()
        return Bool(!bool_), true
      }
      return nil, false
    }
  case *ast.BinaryExpr:
    left, leftOk := c.constFold(t.Left)
    right, rightOk := c.constFold(t.Right)
    if leftOk && rightOk {
      var ret Value
      if left.Type() != right.Type() {
        return nil, false
      }
      lf64, ok := left.assertFloat64()
      rf64, _ := right.assertFloat64()
      if !ok {
        goto boolOps
      }

      // first check all arithmetic/relational operations
      switch t.Op {
      case ast.T_PLUS:
        ret = Number(lf64 + rf64)
      case ast.T_MINUS:
        ret = Number(lf64 - rf64)
      case ast.T_TIMES:
        ret = Number(lf64 * rf64)
      case ast.T_DIV:
        ret = Number(lf64 / rf64)
      case ast.T_TIMESTIMES:
        ret = Number(math.Pow(lf64, rf64))
      case ast.T_LT:
        ret = Bool(lf64 < rf64)
      case ast.T_LTEQ:
        ret = Bool(lf64 <= rf64)
      case ast.T_GT:
        ret = Bool(lf64 > rf64)
      case ast.T_GTEQ:
        ret = Bool(lf64 >= rf64)
      case ast.T_EQEQ:
        ret = Bool(lf64 == rf64)
      }
      if ret != nil {
        return ret, true
      }

    boolOps:
      // not arithmetic/relational, maybe logic?
      lb, ok := left.assertBool()
      rb, _ := right.assertBool()
      if !ok {
        goto stringOps
      }

      switch t.Op {
      case ast.T_AMPAMP:
        return Bool(lb && rb), true
      case ast.T_PIPEPIPE:
        return Bool(lb || rb), true
      }

    stringOps:
      ls, ok := left.assertString()
      rs, _ := right.assertString()
      if !ok {
        return nil, false
      }

      switch t.Op {
      case ast.T_PLUS:
        return String(ls + rs), true
      case ast.T_EQEQ:
        return Bool(ls == rs), true
      case ast.T_BANGEQ:
        return Bool(ls != rs), true
      }
    }
  }
  return nil, false
}

// declare local variables
// assignments are done in sequence, since the registers are created as needed
func (c *compiler) declare(names []*ast.Id, values []ast.Node) {
  var isCall, isUnpack bool
  nameCount, valueCount := len(names), len(values)
  if valueCount > 0 {
    _, isCall = values[valueCount - 1].(*ast.CallExpr)
    _, isUnpack = values[valueCount - 1].(*ast.VarArg)
  }
  start := c.block.register
  end := start + nameCount - 1
  for i, id := range names {
    _, ok := c.block.names[id.Value]
    if ok {
      c.error(id.NodeInfo.Line, fmt.Sprintf("cannot redeclare '%s'", id.Value))
    }
    reg := c.genRegister()
    c.block.addNameInfo(id.Value, &nameinfo{false, nil, reg, kScopeLocal, c.block})

    exprdata := exprdata{false, reg, reg}
    if i == valueCount - 1 && (isCall || isUnpack) {
      // last expression receives all the remaining registers
      // in case it's a function call with multiple return values
      rem := i + 1
      for rem < nameCount {
        // reserve the registers
        id := names[rem]
        _, ok := c.block.names[id.Value]
        if ok {
          c.error(id.NodeInfo.Line, fmt.Sprintf("cannot redeclare '%s'", id.Value))
        }
        end = c.genRegister()
        c.block.addNameInfo(id.Value, &nameinfo{false, nil, end, kScopeLocal, c.block})
        rem++
      }
      exprdata.regb, start = end, end + 1
      values[i].Accept(c, &exprdata)
      break
    }
    if i < valueCount {
      values[i].Accept(c, &exprdata)
      start = reg + 1
    }
  }
  if end >= start {
    // variables without initializer are set to nil
    c.emitAB(OP_LOADNIL, start, end, names[0].NodeInfo.Line)
  }
}

func (c *compiler) assignmentHelper(left ast.Node, assignReg int, valueReg int) {
  switch v := left.(type) {
  case *ast.Id:
    var scope scope
    info, ok := c.block.nameInfo(v.Value)
    if !ok {
      scope = kScopeGlobal
    } else {
      scope = info.scope
    }
    switch scope {
    case kScopeLocal:
      c.emitAB(OP_MOVE, info.reg, valueReg, v.NodeInfo.Line)
    case kScopeRef, kScopeGlobal:
      op := OP_SETGLOBAL
      if scope == kScopeRef {
        op = OP_SETREF
      }
      c.emitABx(op, valueReg, c.addConst(String(v.Value)), v.NodeInfo.Line)
    }
  case *ast.Subscript:
    arrData := exprdata{true, assignReg, assignReg}
    v.Left.Accept(c, &arrData)
    arrReg := arrData.regb

    subData := exprdata{true, assignReg, assignReg}
    v.Right.Accept(c, &subData)
    subReg := subData.regb
    c.emitABC(OP_SET, arrReg, subReg, valueReg, v.NodeInfo.Line)
  case *ast.Selector:
    objData := exprdata{true, assignReg, assignReg}
    v.Left.Accept(c, &objData)
    objReg := objData.regb
    key := OpConstOffset + c.addConst(String(v.Value))

    c.emitABC(OP_SET, objReg, key, valueReg, v.NodeInfo.Line)
  }
}

func (c *compiler) branchConditionHelper(cond, then, else_ ast.Node, reg int) {
  ternaryData := exprdata{true, reg + 1, reg + 1}
  cond.Accept(c, &ternaryData)
  condr := ternaryData.regb
  jmpInstr := c.emitAsBx(OP_JMPFALSE, condr, 0, c.lastLine)
  thenLabel := c.newLabel()

  ternaryData = exprdata{false, reg, reg}
  then.Accept(c, &ternaryData)
  successInstr := c.emitAsBx(OP_JMP, 0, 0, c.lastLine)

  c.modifyAsBx(jmpInstr, OP_JMPFALSE, condr, c.labelOffset(thenLabel))
  elseLabel := c.newLabel()

  ternaryData = exprdata{false, reg, reg}
  else_.Accept(c, &ternaryData)

  c.modifyAsBx(successInstr, OP_JMP, 0, c.labelOffset(elseLabel))
}

func (c *compiler) functionReturnGuard() {
  last := c.block.proto.Code[c.block.proto.NumCode-1]
  if OpGetOpcode(last) != OP_RETURN {
    c.emitAB(OP_RETURN, 0, 0, c.lastLine)
  }
}

//
// visitor interface
//

func (c *compiler) VisitNil(node *ast.Nil, data interface{}) {
  var rega, regb int
  expr, ok := data.(*exprdata)
  if ok {
    rega, regb = expr.rega, expr.regb
    if rega > regb {
      regb = rega
    }
  } else {
    rega = c.genRegister()
    regb = rega
  }
  c.emitAB(OP_LOADNIL, rega, regb, node.NodeInfo.Line)
}

func (c *compiler) VisitBool(node *ast.Bool, data interface{}) {
  var reg int
  value := Bool(node.Value)
  expr, ok := data.(*exprdata)
  if ok && expr.propagate {
    expr.regb = OpConstOffset + c.addConst(value)
    return
  } else if ok {
    reg = expr.rega
  } else {
    reg = c.genRegister()
  }
  c.emitABx(OP_LOADCONST, reg, c.addConst(value), node.NodeInfo.Line)
}

func (c *compiler) VisitNumber(node *ast.Number, data interface{}) {
  var reg int
  value := Number(node.Value)
  expr, ok := data.(*exprdata)
  if ok && expr.propagate {
    expr.regb = OpConstOffset + c.addConst(value)
    return
  } else if ok {
    reg = expr.rega
  } else {
    reg = c.genRegister()
  }
  c.emitABx(OP_LOADCONST, reg, c.addConst(value), node.NodeInfo.Line)
}

func (c *compiler) VisitString(node *ast.String, data interface{}) {
  var reg int
  value := String(node.Value)
  expr, ok := data.(*exprdata)
  if ok && expr.propagate {
    expr.regb = OpConstOffset + c.addConst(value)
    return
  } else if ok {
    reg = expr.rega
  } else {
    reg = c.genRegister()
  }
  c.emitABx(OP_LOADCONST, reg, c.addConst(value), node.NodeInfo.Line)
}

func (c *compiler) VisitId(node *ast.Id, data interface{}) {
  var reg int
  var scope scope = -1
  expr, exprok := data.(*exprdata)
  if !exprok {
    reg = c.genRegister()
  } else {
    reg = expr.rega
  }
  info, ok := c.block.nameInfo(node.Value)
  if ok && info.isConst {
    if exprok && expr.propagate {
      expr.regb = OpConstOffset + c.addConst(info.value)
      return
    }
    c.emitABx(OP_LOADCONST, reg, c.addConst(info.value), node.NodeInfo.Line)
  } else if ok {
    scope = info.scope
  } else {
    // assume global if it can't be found in the lexical scope
    scope = kScopeGlobal
  }
  switch scope {
  case kScopeLocal:
    if exprok && expr.propagate {
      expr.regb = info.reg
      return
    }
    c.emitAB(OP_MOVE, reg, info.reg, node.NodeInfo.Line)
  case kScopeRef, kScopeGlobal:
    op := OP_LOADGLOBAL
    if scope == kScopeRef {
      op = OP_LOADREF
    }
    c.emitABx(op, reg, c.addConst(String(node.Value)), node.NodeInfo.Line)
    if exprok && expr.propagate {
      expr.regb = reg
    }
  }
}

func (c *compiler) VisitArray(node *ast.Array, data interface{}) {
  var reg int
  expr, exprok := data.(*exprdata)
  if exprok {
    reg = expr.rega
  } else {
    reg = c.genRegister()
  }
  length := len(node.Elements)
  c.emitAB(OP_ARRAY, reg, 0, node.NodeInfo.Line)

  times := length / kArrayMaxRegisters + 1
  for t := 0; t < times; t++ {
    start, end := t * kArrayMaxRegisters, (t+1) * kArrayMaxRegisters
    end = int(math.Min(float64(end - start), float64(length - start)))
    if end == 0 {
      break
    }
    for i := 0; i < end; i++ {
      el := node.Elements[start + i]
      exprdata := exprdata{false, reg + i + 1, reg + i + 1}
      el.Accept(c, &exprdata)
    }
    c.emitAB(OP_APPEND, reg, end, node.NodeInfo.Line)
  }
  if exprok && expr.propagate {
    expr.regb = reg
  }
}

func (c *compiler) VisitObjectField(node *ast.ObjectField, data interface{}) {
  expr, exprok := data.(*exprdata)
  assert(exprok, "ObjectField exprok")
  objreg := expr.rega
  key := OpConstOffset + c.addConst(String(node.Key))

  valueData := exprdata{true, objreg + 1, objreg + 1}
  node.Value.Accept(c, &valueData)
  value := valueData.regb

  c.emitABC(OP_SET, objreg, key, value, node.NodeInfo.Line)
}

func (c *compiler) VisitObject(node *ast.Object, data interface{}) {
  var reg int
  expr, exprok := data.(*exprdata)
  if exprok {
    reg = expr.rega
  } else {
    reg = c.genRegister()
  }
  c.emitAB(OP_OBJECT, reg, 0, node.NodeInfo.Line)
  for _, field := range node.Fields {
    fieldData := exprdata{false, reg, reg}
    field.Accept(c, &fieldData)
  }
  if exprok && expr.propagate {
    expr.regb = reg
  }
}

func (c *compiler) VisitFunction(node *ast.Function, data interface{}) {
  var reg int
  expr, exprok := data.(*exprdata)
  if exprok {
    reg = expr.rega
  } else {
    reg = c.genRegister()
  }
  parent := c.block.proto
  proto := newFuncProto(parent.Source)

  block := newCompilerBlock(proto, kBlockContextFunc, c.block)
  c.block = block

  index := int(parent.NumFuncs)
  parent.Funcs = append(parent.Funcs, proto)
  parent.NumFuncs++

  // insert arguments into scope
  for _, n := range node.Args {
    switch arg := n.(type) {
    case *ast.Id:
      reg := c.genRegister()
      c.block.addNameInfo(arg.Value, &nameinfo{false, nil, reg, kScopeLocal, c.block})
    }
  }

  node.Body.Accept(c, nil)
  c.functionReturnGuard()

  c.block = c.block.parent
  c.emitABx(OP_FUNC, reg, index, node.NodeInfo.Line)

  if node.Name != nil {
    c.assignmentHelper(node.Name, reg + 1, reg)
  }
  if exprok && expr.propagate {
    expr.regb = reg
  }
}

func (c *compiler) VisitSelector(node *ast.Selector, data interface{}) {
  var reg int
  expr, exprok := data.(*exprdata)
  if exprok {
    reg = expr.rega
  } else {
    reg = c.genRegister()
  }
  objData := exprdata{true, reg + 1, reg + 1}
  node.Left.Accept(c, &objData)
  objReg := objData.regb

  key := OpConstOffset + c.addConst(String(node.Value))
  c.emitABC(OP_GET, reg, objReg, key, node.NodeInfo.Line)
  if exprok && expr.propagate {
    expr.regb = reg
  }
}

func (c *compiler) VisitSubscript(node *ast.Subscript, data interface{}) {
  var reg int
  expr, exprok := data.(*exprdata)
  if exprok {
    reg = expr.rega
  } else {
    reg = c.genRegister()
  }
  arrData := exprdata{true, reg + 1, reg + 1}
  node.Left.Accept(c, &arrData)
  arrReg := arrData.regb

  _, ok := node.Right.(*ast.Slice)
  if ok {
    // TODO: generate code for slice
    return
  }

  indexData := exprdata{true, reg + 1, reg + 1}
  node.Right.Accept(c, &indexData)
  indexReg := indexData.regb
  c.emitABC(OP_GET, reg, arrReg, indexReg, node.NodeInfo.Line)

  if exprok && expr.propagate {
    expr.regb = reg
  }
}

func (c *compiler) VisitSlice(node *ast.Slice, data interface{}) {

}

func (c *compiler) VisitKwArg(node *ast.KwArg, data interface{}) {
  
}

func (c *compiler) VisitVarArg(node *ast.VarArg, data interface{}) {

}

func (c *compiler) VisitCallExpr(node *ast.CallExpr, data interface{}) {
  var startReg, endReg, resultCount int
  expr, exprok := data.(*exprdata)
  if exprok {
    startReg, endReg = expr.rega, expr.regb
    resultCount = endReg - startReg + 1
  } else {
    startReg = c.genRegister()
    endReg = startReg
    resultCount = 1
  }
  callerData := exprdata{false, startReg, startReg}
  node.Left.Accept(c, &callerData)
  callerReg := callerData.regb
  assert(startReg == callerReg, "startReg == callerReg")

  for i, arg := range node.Args {
    reg := endReg + i + 1
    argData := exprdata{false, reg, reg}
    arg.Accept(c, &argData)
  }

  c.emitABC(OP_CALL, callerReg, resultCount, len(node.Args), node.NodeInfo.Line)
}

func (c *compiler) VisitPostfixExpr(node *ast.PostfixExpr, data interface{}) {
  var reg int
  expr, exprok := data.(*exprdata)
  if exprok {
    reg = expr.rega
  } else {
    reg = c.genRegister()
  }
  var op Opcode
  switch node.Op {
  case ast.T_PLUSPLUS:
    op = OP_ADD
  case ast.T_MINUSMINUS:
    op = OP_SUB
  }
  leftdata := exprdata{true, reg, reg}
  node.Left.Accept(c, &leftdata)
  left := leftdata.regb
  one := OpConstOffset + c.addConst(Number(1))

  // it wouldn't make sense to move it if we're not in an expression
  if exprok {
    c.emitAB(OP_MOVE, reg, left, node.NodeInfo.Line)
  }
  c.emitABC(op, left, left, one, node.NodeInfo.Line)
}

func (c *compiler) VisitUnaryExpr(node *ast.UnaryExpr, data interface{}) {
  var reg int
  expr, exprok := data.(*exprdata)
  if exprok {
    reg = expr.rega
  } else {
    reg = c.genRegister()
  }
  value, ok := c.constFold(node)
  if ok {
    if exprok && expr.propagate {
      expr.regb = OpConstOffset + c.addConst(value)
      return
    }
    c.emitABx(OP_LOADCONST, reg, c.addConst(value), node.NodeInfo.Line)
  } else if ast.IsPostfixOp(node.Op) {
    op := OP_ADD
    if node.Op == ast.T_MINUSMINUS {
      op = OP_SUB
    }
    exprdata := exprdata{true, reg, reg}
    node.Right.Accept(c, &exprdata)
    one := OpConstOffset + c.addConst(Number(1))
    c.emitABC(op, exprdata.regb, exprdata.regb, one, node.NodeInfo.Line)

    // it wouldn't make sense to move it if we're not in an expression
    if exprok {
      c.emitAB(OP_MOVE, reg, exprdata.regb, node.NodeInfo.Line)
    }
  } else {
    var op Opcode
    switch node.Op {
    case ast.T_MINUS:
      op = OP_NEG
    case ast.T_NOT, ast.T_BANG:
      op = OP_NOT
    case ast.T_TILDE:
      op = OP_CMPL
    }
    exprdata := exprdata{true, reg, reg}
    node.Right.Accept(c, &exprdata)
    c.emitABx(op, reg, exprdata.regb, node.NodeInfo.Line)
    if exprok && expr.propagate {
      expr.regb = reg
    }
  }
}

func (c *compiler) VisitBinaryExpr(node *ast.BinaryExpr, data interface{}) {
  var reg int
  expr, exprok := data.(*exprdata)
  if exprok {
    reg = expr.rega
  } else {
    reg = c.genRegister()
  }
  value, ok := c.constFold(node)
  if ok {
    if exprok && expr.propagate {
      expr.regb = OpConstOffset + c.addConst(value)
      return
    }
    c.emitABx(OP_LOADCONST, reg, c.addConst(value), node.NodeInfo.Line)
  } else {
    if isAnd, isOr := node.Op == ast.T_AMPAMP, node.Op == ast.T_PIPEPIPE; isAnd || isOr {
      var op Opcode
      if isAnd {
        op = OP_JMPFALSE
      } else {
        op = OP_JMPTRUE
      }
      exprdata := exprdata{true, reg, reg}
      node.Left.Accept(c, &exprdata)
      left := exprdata.regb

      jmpInstr := c.emitAsBx(op, left, 0, node.NodeInfo.Line)
      size := c.block.proto.NumCode

      exprdata.propagate = false
      node.Right.Accept(c, &exprdata)
      c.modifyAsBx(jmpInstr, op, left, int(c.block.proto.NumCode - size))
      return
    }
    
    var op Opcode
    switch node.Op {
    case ast.T_PLUS:
      op = OP_ADD
    case ast.T_MINUS:
      op = OP_SUB
    case ast.T_TIMES:
      op = OP_MUL
    case ast.T_DIV:
      op = OP_DIV
    case ast.T_TIMESTIMES:
      op = OP_POW
    case ast.T_LTLT:
      op = OP_SHL
    case ast.T_GTGT:
      op = OP_SHR
    case ast.T_AMP:
      op = OP_AND
    case ast.T_PIPE:
      op = OP_OR
    case ast.T_TILDE:
      op = OP_XOR
    case ast.T_LT, ast.T_GTEQ:
      op = OP_LT
    case ast.T_LTEQ, ast.T_GT:
      op = OP_LE
    case ast.T_EQ:
      op = OP_EQ
    case ast.T_BANGEQ:
      op = OP_NE
    }

    exprdata := exprdata{true, reg, 0}
    node.Left.Accept(c, &exprdata)
    left := exprdata.regb

    // temp register for right expression
    exprdata.rega += 1
    node.Right.Accept(c, &exprdata)
    right := exprdata.regb

    if node.Op == ast.T_GT || node.Op == ast.T_GTEQ {
      // invert operands
      c.emitABC(op, reg, right, left, node.NodeInfo.Line)  
    } else {
      c.emitABC(op, reg, left, right, node.NodeInfo.Line)
    }
    if exprok && expr.propagate {
      expr.regb = reg
    }
  }
}

func (c *compiler) VisitTernaryExpr(node *ast.TernaryExpr, data interface{}) {
  var reg int
  expr, exprok := data.(*exprdata)
  if exprok {
    reg = expr.rega
  } else {
    reg = c.genRegister()
  }
  c.branchConditionHelper(node.Cond, node.Then, node.Else, reg)
}

func (c *compiler) VisitDeclaration(node *ast.Declaration, data interface{}) {
  valueCount := len(node.Right)
  if node.IsConst {
    for i, id := range node.Left {
      _, ok := c.block.names[id.Value]
      if ok {
        c.error(node.NodeInfo.Line, fmt.Sprintf("cannot redeclare '%s'", id.Value))
      }
      if i >= valueCount {
        c.error(node.NodeInfo.Line, fmt.Sprintf("const '%s' without initializer", id.Value))
      }
      value, ok := c.constFold(node.Right[i])
      if !ok {
        c.error(node.NodeInfo.Line, fmt.Sprintf("const '%s' initializer is not a constant", id.Value))
      }
      c.block.addNameInfo(id.Value, &nameinfo{true, value, 0, kScopeLocal, c.block})
    }
    return
  }
  c.declare(node.Left, node.Right)
}

func (c *compiler) VisitAssignment(node *ast.Assignment, data interface{}) {
  if node.Op == ast.T_COLONEQ {
    // short variable declaration
    var names []*ast.Id
    for _, id := range node.Left {
      names = append(names, id.(*ast.Id))
    }
    c.declare(names, node.Right)
    return
  }
  // regular assignment, if the left-side is an identifier
  // then it has to be declared already
  varCount, valueCount := len(node.Left), len(node.Right)
  _, isCall := node.Right[valueCount - 1].(*ast.CallExpr)
  _, isUnpack := node.Right[valueCount - 1].(*ast.VarArg)
  start := c.block.register
  current := start
  end := start + varCount - 1

  // evaluate all expressions first with temp registers
  for i, _ := range node.Left {
    reg := start + i
    exprdata := exprdata{false, reg, reg}
    if i == valueCount - 1 && (isCall || isUnpack) {
      exprdata.regb, current = end, end
      node.Right[i].Accept(c, &exprdata)
      break
    }
    if i < valueCount {
      node.Right[i].Accept(c, &exprdata)
      current = reg + 1
    }
  }
  // assign the results to the variables
  for i, variable := range node.Left {
    valueReg := start + i

    // don't touch variables without a corresponding value
    if valueReg >= current {
      break
    }
    c.assignmentHelper(variable, current + 1, valueReg)
  }
}

func (c *compiler) VisitBranchStmt(node *ast.BranchStmt, data interface{}) {
  if c.block.context != kBlockContextLoop {
    c.error(node.NodeInfo.Line, fmt.Sprintf("%s outside loop", node.Type))
  }
  switch node.Type {
  case ast.T_CONTINUE:
    index := c.block.proto.NumCode
    c.emitAsBx(OP_JMP, 0, -int(index - c.block.start), node.NodeInfo.Line)
  case ast.T_BREAK:
    instr := c.emitAsBx(OP_JMP, 0, 0, node.NodeInfo.Line)
    c.block.pendingBreaks = append(c.block.pendingBreaks, uint32(instr))
  }
}

func (c *compiler) VisitReturnStmt(node *ast.ReturnStmt, data interface{}) {
  start := c.block.register
  for _, v := range node.Values {
    reg := c.genRegister()
    data := exprdata{false, reg, reg}
    v.Accept(c, &data)
  }
  c.emitAB(OP_RETURN, start, len(node.Values), node.NodeInfo.Line)
}

func (c *compiler) VisitIfStmt(node *ast.IfStmt, data interface{}) {
  _, ok := data.(*exprdata)
  if !ok {
    c.enterBlock(kBlockContextBranch)
    defer c.leaveBlock()
  }
  if node.Init != nil {
    node.Init.Accept(c, nil)
  }
  c.branchConditionHelper(node.Cond, node.Body, node.Else, c.block.register)
}

func (c *compiler) VisitForIteratorStmt(node *ast.ForIteratorStmt, data interface{}) {

}

func (c *compiler) VisitForStmt(node *ast.ForStmt, data interface{}) {
  c.enterBlock(kBlockContextLoop)
  defer c.leaveBlock()

  if node.Init != nil {
    node.Init.Accept(c, nil)
  }
  reg := c.block.register
  condLabel := c.newLabel()

  condData := exprdata{true, reg, reg}
  node.Cond.Accept(c, &condData)
  cond := condData.regb

  jmpInstr := c.emitAsBx(OP_JMPFALSE, cond, 0, c.lastLine)
  bodyLabel := c.newLabel()
  node.Body.Accept(c, nil)

  node.Step.Accept(c, nil)
  c.block.register -= 1 // discard register consumed by Step

  c.emitAsBx(OP_JMP, 0, -c.labelOffset(condLabel) - 1, c.lastLine)
  c.modifyAsBx(jmpInstr, OP_JMPFALSE, cond, c.labelOffset(bodyLabel))
}

func (c *compiler) VisitBlock(node *ast.Block, data interface{}) {
  for _, stmt := range node.Nodes {
    stmt.Accept(c, nil)

    if !ast.IsStmt(stmt) {
      c.block.register -= 1
    }
  }
}

// Compile receives the root node of the AST and generates code
// for the "main" function from it.
// Any type of Node is accepted, either a block representing the program
// or a single expression.
//
func Compile(root ast.Node, filename string) (res *FuncProto, err error) {
  defer func() {
    if r := recover(); r != nil {
      if cerr, ok := r.(*CompileError); ok {
        err = cerr
      } else {
        panic(r)
      }
    }
  }()

  var c compiler
  c.filename = filename
  c.mainFunc = newFuncProto(filename)
  c.block = newCompilerBlock(c.mainFunc, kBlockContextFunc, nil)
  
  root.Accept(&c, nil)
  c.functionReturnGuard()

  res = c.mainFunc
  return
}