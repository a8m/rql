package rql

import "fmt"

// Postgres is the postgres dialect
type postgres struct{}

// Postgres returns a new postgres dialect
func Postgres() Dialect {
	return &postgres{}
}

// FormatOp implements the postgres argument generator
func (postgres) FormatOp(col string, op Op, argn int) string {
	return col + " " + op.SQL() + " $" + fmt.Sprintf("%d", argn+1)
}
