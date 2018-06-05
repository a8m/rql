package fuzz

import (
	"time"

	"github.com/a8m/rql"
)

var QueryParser = rql.MustNewParser(rql.Config{
	Model:         User{},
	FieldSep:      ".",
	LimitMaxValue: 25,
})

type User struct {
	ID        uint      `gorm:"primary_key" rql:"filter,sort"`
	Admin     bool      `rql:"filter"`
	Name      string    `rql:"filter"`
	Address   string    `rql:"filter"`
	CreatedAt time.Time `rql:"filter,sort"`
}

func Fuzz(b []byte) int {
	params, err := QueryParser.Parse(b)
	if err != nil {
		return -1
	}
	if len(params.FilterArgs) > 0 || params.Sort != "" {
		return 1
	}
	return 0
}
