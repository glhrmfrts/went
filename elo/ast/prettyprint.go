// Pretty-print the AST to a buffer

package ast

import (
  "fmt"
  "bytes"
  //"github.com/glhrmfrts/elo-lang/elo/token"
)

type Prettyprinter struct {
  indent int
  indentSize int
  buf bytes.Buffer
}

func (p *Prettyprinter) doIndent() {
  for i := 0; i < p.indent * p.indentSize; i++ {
    p.buf.WriteString(" ")
  }
}

func (p *Prettyprinter) VisitNil(node *Nil) {
  p.buf.WriteString("(nil)")
}

func (p *Prettyprinter) VisitBool(node *Bool) {
  var val string
  if node.Value {
    val = "true"
  } else {
    val = "false"
  }
  p.buf.WriteString("(" + val + ")")
}

func (p *Prettyprinter) VisitNumber(node *Number) {
  p.buf.WriteString(fmt.Sprintf("(%s %s)", node.Type, node.Value))
}

func (p *Prettyprinter) VisitId(node *Id) {
  p.buf.WriteString("(id " + node.Value + ")")
}

func (p *Prettyprinter) VisitString(node *String) {
  p.buf.WriteString("(string \""+ node.Value + "\")")
}

func (p *Prettyprinter) VisitArray(node *Array) {
  p.buf.WriteString("(array")
  p.indent++

  for _, n := range node.Values {
    p.buf.WriteString("\n")
    p.doIndent()
    n.Accept(p)
  }

  p.indent--
  p.buf.WriteString(")")
}

func (p *Prettyprinter) VisitObjectField(node *ObjectField) {
  p.buf.WriteString("(field\n")
  p.indent++
  p.doIndent()

  if node.Key != nil {
    node.Key.Accept(p)
  }

  p.buf.WriteString("\n")
  p.doIndent()

  if node.Value != nil {
    node.Value.Accept(p)
  }

  p.indent--
  p.buf.WriteString(")")
}

func (p *Prettyprinter) VisitObject(node *Object) {
  p.buf.WriteString("(object")
  p.indent++

  for _, f := range node.Fields {
    p.buf.WriteString("\n")
    p.doIndent()
    f.Accept(p)
  }

  p.indent--
  p.buf.WriteString(")")
}

func (p *Prettyprinter) VisitFunction(node *Function) {
  p.buf.WriteString("(func ")
  if node.Name != nil {
    node.Name.Accept(p)
  }
  p.buf.WriteString("\n")
  p.indent++

  for _, a := range node.Args {
    p.doIndent()
    a.Accept(p)
    p.buf.WriteString("\n")
  }

  p.doIndent()
  p.buf.WriteString("->\n")

  p.doIndent()
  node.Body.Accept(p)

  p.indent--
  p.buf.WriteString(")")
}

func (p *Prettyprinter) VisitSelector(node *Selector) {
  p.buf.WriteString("(selector\n")

  p.indent++
  p.doIndent()

  node.Left.Accept(p)

  p.buf.WriteString("\n")
  p.doIndent()

  p.indent--
  p.buf.WriteString("'" + node.Value + "')")
}

func (p *Prettyprinter) VisitSubscript(node *Subscript) {
  p.buf.WriteString("(subscript\n")

  p.indent++
  p.doIndent()

  node.Left.Accept(p)

  p.buf.WriteString("\n")
  p.doIndent()

  node.Right.Accept(p)

  p.indent--
  p.buf.WriteString(")")
}

func (p *Prettyprinter) VisitSlice(node *Slice) {
  p.buf.WriteString("(slice\n")

  p.indent++
  p.doIndent()

  node.Start.Accept(p)

  p.buf.WriteString("\n")
  p.doIndent()

  node.End.Accept(p)

  p.indent--
  p.buf.WriteString(")")
}

func (p *Prettyprinter) VisitKwArg(node *KwArg) {
  p.buf.WriteString("(kwarg\n")

  p.indent++
  p.doIndent()

  p.buf.WriteString("'" + node.Key + "'\n")

  p.doIndent()
  node.Value.Accept(p)

  p.indent--
  p.buf.WriteString(")")
}

func (p *Prettyprinter) VisitVarArg(node *VarArg) {
  p.buf.WriteString("(vararg ")
  node.Arg.Accept(p)
  p.buf.WriteString(")")
}

func (p *Prettyprinter) VisitCallExpr(node *CallExpr) {
  p.buf.WriteString("(call\n")

  p.indent++
  p.doIndent()

  node.Left.Accept(p)

  for _, arg := range node.Args {
    p.buf.WriteString("\n")
    p.doIndent()
    arg.Accept(p)
  }

  p.indent--
  p.buf.WriteString(")")
}

func (p *Prettyprinter) VisitUnaryExpr(node *UnaryExpr) {
  p.buf.WriteString(fmt.Sprintf("(unary %s\n", node.Op))
  
  p.indent++
  p.doIndent()

  node.Right.Accept(p)

  p.indent--
  p.buf.WriteString(")")
}

func (p *Prettyprinter) VisitBinaryExpr(node *BinaryExpr) {
  p.buf.WriteString(fmt.Sprintf("(binary %s\n", node.Op))

  p.indent++
  p.doIndent()

  node.Left.Accept(p)

  p.buf.WriteString("\n")
  p.doIndent()

  node.Right.Accept(p)

  p.indent--
  p.buf.WriteString(")")
}

func (p *Prettyprinter) VisitDeclaration(node *Declaration) {
  keyword := "var"
  if node.IsConst {
    keyword = "const"
  }

  p.buf.WriteString(fmt.Sprintf("(%s", keyword))
  p.indent++

  for _, id := range node.Left {
    p.buf.WriteString("\n")
    p.doIndent()
    id.Accept(p)
  }

  for _, node := range node.Right {
    p.buf.WriteString("\n")
    p.doIndent()
    node.Accept(p)
  }

  p.indent--
  p.buf.WriteString(")") 
}

func (p *Prettyprinter) VisitAssignment(node *Assignment) {
  p.buf.WriteString("(assignment")
  p.indent++

  for _, node := range node.Left {
    p.buf.WriteString("\n")
    p.doIndent()
    node.Accept(p)
  }

  p.buf.WriteString("\n")
  p.doIndent()
  p.buf.WriteString(node.Op.String())

  for _, node := range node.Right {
    p.buf.WriteString("\n")
    p.doIndent()
    node.Accept(p)
  }

  p.indent--
  p.buf.WriteString(")")
}

func (p *Prettyprinter) VisitReturnStmt(node *ReturnStmt) {
  p.buf.WriteString("(return")
  p.indent++

  for _, v := range node.Values {
    p.buf.WriteString("\n")
    p.doIndent()
    v.Accept(p)
  }

  p.indent--
  p.buf.WriteString(")")
}

func (p *Prettyprinter) VisitBlock(node *Block) {
  p.buf.WriteString("(block")
  p.indent++

  for _, n := range node.Nodes {
    p.buf.WriteString("\n")
    p.doIndent()
    n.Accept(p)
  }

  p.indent--
  p.buf.WriteString(")")
}

func Prettyprint(root Node, indentSize int) string {
  v := Prettyprinter{indentSize: indentSize}
  root.Accept(&v)
  return v.buf.String()
}