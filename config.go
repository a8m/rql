package rql

import (
	"errors"
	"log"
	"reflect"
)

// Op is a filter operator used by rql.
type Op string

// SQL returns the SQL representation of the operator.
func (o Op) SQL() string {
	return opFormat[o]
}

// Operators that support by rql.
const (
	EQ   = Op("eq")   // =
	NEQ  = Op("neq")  // <>
	LT   = Op("lt")   // <
	GT   = Op("gt")   // >
	LTE  = Op("lte")  // <=
	GTE  = Op("gte")  // >=
	LIKE = Op("like") // LIKE "PATTERN"
	OR   = Op("or")   // disjunction
	AND  = Op("and")  // conjunction
)

// Default values for configuration.
const (
	DefaultTagName  = "rql"
	DefaultOpPrefix = "$"
	DefaultFieldSep = "_"
	DefaultLimit    = 25
	DefaultMaxLimit = 100
	Offset          = "offset"
	Limit           = "limit"
)

var (

	// A sorting expression can be optionally prefixed with + or - to control the
	// sorting direction, ascending or descending. For example, '+field' or '-field'.
	// If the predicate is missing or empty then it defaults to '+'
	sortDirection = map[byte]string{
		'+': "asc",
		'-': "desc",
	}
	opFormat = map[Op]string{
		EQ:   "=",
		NEQ:  "<>",
		LT:   "<",
		GT:   ">",
		LTE:  "<=",
		GTE:  ">=",
		LIKE: "LIKE",
		OR:   "OR",
		AND:  "AND",
	}
)

// Config is the configuration for the parser.
type Config struct {
	// TagName is an optional tag name for configuration. t defaults to "rql".
	TagName string
	// Model is the resource definition. The parser is configured based on the its definition.
	// For example, given the following struct definition:
	//
	//	type User struct {
	//		Age	 int	`rql:"filter,sort"`
	// 		Name string	`rql:"filter"`
	// 	}
	//
	// In order to create a parser for the given resource, you will do it like so:
	//
	//	var QueryParser = rql.MustNewParser(
	// 		Model: User{},
	// 	})
	//
	Model interface{}
	// OpPrefix is the prefix for operators. it defaults to "$". for example, in order
	// to use the "gt" (greater-than) operator, you need to prefix it with "$".
	// It similar to the MongoDB query language.
	OpPrefix string
	// FieldSep is the separator for nested fields in a struct. For example, given the following struct:
	//
	//	type User struct {
	// 		Name 	string	`rql:"filter"`
	//		Address	struct {
	//			City string `rql:"filter"``
	//		}
	// 	}
	//
	// We assume the schema for this struct contains a column named "address_city". Therefore, the default
	// separator is underscore ("_"). But, you can change it to "." for convenience or readability reasons.
	// Then you will be able to query your resource like this:
	//
	//	{
	//		"filter": {
	//			"address.city": "DC"
	// 		}
	//	}
	//
	// The parser will automatically convert it to underscore ("_"). If you want to control the name of
	// the column, use the "column" option in the struct definition. For example:
	//
	//	type User struct {
	// 		Name 	string	`rql:"filter,column=full_name"`
	// 	}
	//
	FieldSep string
	// ColumnFn is the function that translate the struct field string into a table column.
	// For example, given the following fields and their column names:
	//
	//	FullName => "full_name"
	// 	HTTPPort => "http_port"
	//
	// It is preferred that you will follow the same convention that your ORM or other DB helper use.
	// For example, If you are using `gorm` you want to se this option like this:
	//
	//	var QueryParser = rql.MustNewParser(
	// 		ColumnFn: gorm.ToDBName,
	// 	})
	//
	ColumnFn func(string) string
	// Log the the logging function used to log debug information in the initialization of the parser.
	// It defaults `to log.Printf`.
	Log func(string, ...interface{})
	// DefaultLimit is the default value for the `Limit` field that returns when no limit supplied by the caller.
	// It defaults to 25.
	DefaultLimit int
	// LimitMaxValue is the upper boundary for the limit field. User will get an error if the given value is greater
	// than this value. It defaults to 100.
	LimitMaxValue int
	// DefaultSort is the default value for the 'Sort' field that returns when no sort expression is supplied by the caller.
	// It defaults to an empty string slice.
	DefaultSort []string
}

// defaults sets the default configuration of Config.
func (c *Config) defaults() error {
	if c.Model == nil {
		return errors.New("rql: 'Model' is a required field")
	}
	if indirect(reflect.TypeOf(c.Model)).Kind() != reflect.Struct {
		return errors.New("rql: 'Model' must be a struct type")
	}
	if c.Log == nil {
		c.Log = log.Printf
	}
	if c.ColumnFn == nil {
		c.ColumnFn = Column
	}
	defaultString(&c.TagName, DefaultTagName)
	defaultString(&c.OpPrefix, DefaultOpPrefix)
	defaultString(&c.FieldSep, DefaultFieldSep)
	defaultInt(&c.DefaultLimit, DefaultLimit)
	defaultInt(&c.LimitMaxValue, DefaultMaxLimit)
	return nil
}

func defaultString(s *string, v string) {
	if *s == "" {
		*s = v
	}
}

func defaultInt(i *int, v int) {
	if *i == 0 {
		*i = v
	}
}
