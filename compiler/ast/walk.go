package ast

func WalkDecl(decl Decl, visit func(node any)) {
	visit(decl)
	switch d := decl.(type) {
	case *DataDecl:
		for i := range d.Fields {
			visit(&d.Fields[i])
		}
	case *ClassDecl:
		for i := range d.Fields {
			visit(&d.Fields[i])
		}
		for i := range d.Methods {
			WalkStmtSlice(d.Methods[i].Body, visit)
		}
	case *DriverDecl:
		for i := range d.Fields {
			visit(&d.Fields[i])
		}
		for i := range d.Methods {
			WalkStmtSlice(d.Methods[i].Body, visit)
		}
	case *DriverPathDecl:
		for i := range d.Fields {
			visit(&d.Fields[i])
		}
	case *ExecutorDecl:
		for i := range d.Fields {
			visit(&d.Fields[i])
		}
		for i := range d.Methods {
			WalkStmtSlice(d.Methods[i].Body, visit)
		}
	case *ImageDecl:
		for i := range d.Transitions {
			visit(&d.Transitions[i])
		}
		for i := range d.Phases {
			WalkStmtSlice(d.Phases[i].Body, visit)
		}
	case *PhaseDecl:
		for i := range d.Params {
			visit(&d.Params[i])
		}
		WalkStmtSlice(d.Body, visit)
	}
}

func WalkExpr(expr Expr, visit func(node any)) {
	visit(expr)
	switch e := expr.(type) {
	case *ConstructorExpr:
		for i := range e.Args {
			visit(&e.Args[i])
			WalkExpr(e.Args[i].Value, visit)
		}
	case *CallExpr:
		WalkExpr(e.Receiver, visit)
		for i := range e.Args {
			visit(&e.Args[i])
			WalkExpr(e.Args[i].Value, visit)
		}
	case *FieldExpr:
		WalkExpr(e.Base, visit)
	case *BinaryExpr:
		WalkExpr(e.Left, visit)
		WalkExpr(e.Right, visit)
	}
}

func WalkStmt(stmt Stmt, visit func(node any)) {
	visit(stmt)
	switch s := stmt.(type) {
	case *LetStmt:
		WalkExpr(s.Expr, visit)
	case *ReturnStmt:
		if s.Value != nil {
			WalkExpr(s.Value, visit)
		}
	case *IfStmt:
		WalkExpr(s.Cond, visit)
		WalkStmtSlice(s.Then, visit)
		WalkStmtSlice(s.Else, visit)
	case *WhileStmt:
		WalkExpr(s.Cond, visit)
		WalkStmtSlice(s.Body, visit)
	case *ForStmt:
		WalkExpr(s.InExpr, visit)
		WalkStmtSlice(s.Body, visit)
	case *AssignStmt:
		WalkExpr(s.Target, visit)
		WalkExpr(s.Value, visit)
	case *ExprStmt:
		WalkExpr(s.Expr, visit)
	}
}

func WalkStmtSlice(stmts []Stmt, visit func(node any)) {
	for i := range stmts {
		WalkStmt(stmts[i], visit)
	}
}
