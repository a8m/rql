package rql

import (
	"bytes"
	"container/list"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode"
)

//go:generate easyjson -omit_empty -disallow_unknown_fields -snake_case rql.go

// Query is the decoded result of the user input.
//
//easyjson:json
type Query struct {
	// Limit must be > 0 and <= to `LimitMaxValue`.
	Limit int `json:"limit,omitempty"`
	// Offset must be >= 0.
	Offset int `json:"offset,omitempty"`
	// Select contains the list of expressions define the value for the `SELECT` clause.
	// For example:
	//
	//	params, err := p.Parse([]byte(`{
	//		"select": ["name", "age"]
	//	}`))
	//
	Select []string `json:"select,omitempty"`
	// Sort contains list of expressions define the value for the `ORDER BY` clause.
	// In order to return the rows in descending order you can prefix your field with `-`.
	// For example:
	//
	//	params, err := p.Parse([]byte(`{
	//		"sort": ["name", "-age", "+redundant"]
	//	}`))
	//
	Sort []string `json:"sort,omitempty"`
	// Filter is the query object for building the value for the `WHERE` clause.
	// The full documentation of the supported operators is writtern in the README.
	// An example for filter object:
	//
	//	params, err := p.Parse([]byte(`{
	//		"filter": {
	//			"account": { "$like": "%github%" },
	//			"$or": [
	//				{ "city": "TLV" },
	//				{ "city": "NYC" }
	//			]
	//		}
	//	}`))
	//
	Filter map[string]interface{} `json:"filter,omitempty"`
}

// Params is the parser output after calling to `Parse`. You should pass its
// field values to your query tool. For example, Suppose you use `gorm`:
//
//	params, err := p.Parse(b)
//	if err != nil {
//		return nil, err
//	}
//	var users []User
//	err := db.Where(params.FilterExp, params.FilterArgs...).
//		Order(params.Sort).
//		Find(&users).
//		Error
//	if err != nil {
//		return nil, err
//	}
//	return users, nil
type Params struct {
	// Limit represents the number of rows returned by the SELECT statement.
	Limit int
	// Offset specifies the offset of the first row to return. Useful for pagination.
	Offset int
	// Select contains the expression for the `SELECT` clause defined in the Query.
	Select string
	// Sort used as a parameter for the `ORDER BY` clause. For example, "age desc, name".
	Sort string
	// FilterExp and FilterArgs come together and used as a parameters for the `WHERE` clause.
	//
	// examples:
	// 	1. Exp: "name = ?"
	//	   Args: "a8m"
	//
	//	2. Exp: "name = ? AND age >= ?"
	// 	   Args: "a8m", 22
	FilterExp  string
	FilterArgs []interface{}
}

// ParseError is type of error returned when there is a parsing problem.
type ParseError struct {
	msg string
}

func (p ParseError) Error() string {
	return p.msg
}

type Validator func(Op, FieldMeta, interface{}) error
type Converter func(Op, FieldMeta, interface{}) interface{}

// field is a configuration of a struct field.
type Field struct {
	*FieldMeta
	// Validation for the type. for example, unit8 greater than or equal to 0.
	ValidateFn Validator
	// ConvertFn converts the given value to the type value.
	CovertFn Converter
}

type FieldMeta struct {
	// Name of the column.
	Name string
	// Has a "sort" option in the tag.
	Sortable bool
	// Has a "filter" option in the tag.
	Filterable bool
	// All supported operators for this field.
	FilterOps map[string]bool
	// Type of the field
	Type reflect.Type
	// Time Layout
	Layout string
}

// A Parser parses various types. The result from the Parse method is a Param object.
// It is safe for concurrent use by multiple goroutines except for configuration changes.
type Parser struct {
	Config
	fields map[string]*Field
}

// NewParser creates a new Parser. it fails if the configuration is invalid.
func NewParser(c Config) (*Parser, error) {
	if err := c.defaults(); err != nil {
		return nil, err
	}
	p := &Parser{
		Config: c,
		fields: make(map[string]*Field),
	}
	if err := p.init(); err != nil {
		return nil, err
	}
	return p, nil
}

// MustNewParser is like NewParser but panics if the configuration is invalid.
// It simplifies safe initialization of global variables holding a resource parser.
func MustNewParser(c Config) *Parser {
	p, err := NewParser(c)
	if err != nil {
		panic(err)
	}
	return p
}

// Parse parses the given buffer into a Param object. It returns an error
// if the JSON is invalid, or its values don't follow the schema of rql.
func (p *Parser) Parse(b []byte) (pr *Params, err error) {
	q := &Query{}
	if err := q.UnmarshalJSON(b); err != nil {
		return nil, &ParseError{"decoding buffer to *Query: " + err.Error()}
	}
	return p.ParseQuery(q)
}

// ParseQuery parses the given struct into a Param object. It returns an error
// if one of the query values don't follow the schema of rql.
func (p *Parser) ParseQuery(q *Query) (pr *Params, err error) {
	defer func() {
		if e := recover(); e != nil {
			perr, ok := e.(*ParseError)
			if !ok {
				panic(e)
			}
			err = perr
			pr = nil
		}
	}()
	pr = &Params{
		Limit: p.DefaultLimit,
	}
	expect(q.Offset >= 0, "offset must be greater than or equal to 0")
	pr.Offset = q.Offset
	if q.Limit != 0 {
		expect(q.Limit > 0 && q.Limit <= p.LimitMaxValue, "limit must be greater than 0 and less than or equal to %d", p.LimitMaxValue)
		pr.Limit = q.Limit
	}
	ps := p.newParseState()
	ps.and(q.Filter)
	pr.FilterExp = ps.String()
	pr.FilterArgs = ps.values
	pr.Sort = p.sort(q.Sort)
	if len(pr.Sort) == 0 && len(p.DefaultSort) > 0 {
		pr.Sort = p.sort(p.DefaultSort)
	}
	pr.Select = strings.Join(q.Select, ", ")
	parseStatePool.Put(ps)
	return
}

// Column is the default function that converts field name into a database column.
// It used to convert the struct fields into their database names. For example:
//
//	Username => username
//	FullName => full_name
//	HTTPCode => http_code
func Column(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		r := rune(s[i])
		// put '.' if it is not a start or end of a word, current letter is an uppercase letter,
		// and previous letter is a lowercase letter (cases like: "UserName"), or next letter is
		// also a lowercase letter and previous letter is not "_".
		if i > 0 && i < len(s)-1 && unicode.IsUpper(r) &&
			(unicode.IsLower(rune(s[i-1])) ||
				unicode.IsLower(rune(s[i+1])) && unicode.IsLetter(rune(s[i-1]))) {
			b.WriteString("_")
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func GetSupportedOps(f *FieldMeta) []Op {
	t := f.Type
	switch t.Kind() {
	case reflect.Bool:
		return []Op{EQ, NEQ}
	case reflect.String:
		return []Op{EQ, NEQ, LT, LTE, GT, GTE, LIKE}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return []Op{EQ, NEQ, LT, LTE, GT, GTE}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return []Op{EQ, NEQ, LT, LTE, GT, GTE}
	case reflect.Float32, reflect.Float64:
		return []Op{EQ, NEQ, LT, LTE, GT, GTE}
	case reflect.Struct:
		switch v := reflect.Zero(t); v.Interface().(type) {
		case sql.NullBool:
			return []Op{EQ, NEQ}
		case sql.NullString:
			return []Op{EQ, NEQ}
		case sql.NullInt64:
			return []Op{EQ, NEQ, LT, LTE, GT, GTE}
		case sql.NullFloat64:
			return []Op{EQ, NEQ, LT, LTE, GT, GTE}
		case time.Time:
			return []Op{EQ, NEQ, LT, LTE, GT, GTE}
		default:
			if v.Type().ConvertibleTo(reflect.TypeOf(time.Time{})) {
				return []Op{EQ, NEQ, LT, LTE, GT, GTE}
			}
			return []Op{}
		}
	default:
		return []Op{}
	}
}

func GetConverterFn(f *FieldMeta) Converter {
	layout := f.Layout
	t := f.Type
	switch t.Kind() {
	case reflect.Bool:
		return valueFn
	case reflect.String:
		return valueFn
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return convertInt
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return convertInt
	case reflect.Float32, reflect.Float64:
		return valueFn
	case reflect.Struct:
		switch v := reflect.Zero(t); v.Interface().(type) {
		case sql.NullBool:
			return valueFn
		case sql.NullString:
			return valueFn
		case sql.NullInt64:
			return convertInt
		case sql.NullFloat64:
			return valueFn
		case time.Time:
			return convertTime(layout)
		default:
			if v.Type().ConvertibleTo(reflect.TypeOf(time.Time{})) {
				return convertTime(layout)
			}
		}
	}
	return valueFn
}

func GetValidateFn(f *FieldMeta) Validator {
	t := f.Type
	layout := f.Layout
	switch t.Kind() {
	case reflect.Bool:
		return validateBool
	case reflect.String:
		return validateString
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return validateInt
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return validateUInt
	case reflect.Float32, reflect.Float64:
		return validateFloat
	case reflect.Struct:
		switch v := reflect.Zero(t); v.Interface().(type) {
		case sql.NullBool:
			return validateBool
		case sql.NullString:
			return validateString
		case sql.NullInt64:
			return validateInt
		case sql.NullFloat64:
			return validateFloat
		case time.Time:
			return validateTime(layout)
		default:
			if !v.Type().ConvertibleTo(reflect.TypeOf(time.Time{})) {
				return nil
			}
			return validateTime(layout)
		}
	default:
		return nil
	}
}

// init initializes the parser parsing state. it scans the fields
// in a breath-first-search order and for each one of the field calls parseField.
func (p *Parser) init() error {
	t := reflect.TypeOf(p.Model)
	t = indirect(t)
	l := list.New()
	for i := 0; i < t.NumField(); i++ {
		l.PushFront(t.Field(i))
	}
	for l.Len() > 0 {
		f := l.Remove(l.Front()).(reflect.StructField)
		_, ok := f.Tag.Lookup(p.TagName)
		switch t := indirect(f.Type); {
		// no matter what the type of this field. if it has a tag,
		// it is probably a filterable or sortable.
		case ok:
			if err := p.parseField(f); err != nil {
				return err
			}
		case t.Kind() == reflect.Struct:
			for i := 0; i < t.NumField(); i++ {
				f1 := t.Field(i)
				if !f.Anonymous {
					f1.Name = f.Name + p.FieldSep + f1.Name
				}
				l.PushFront(f1)
			}
		case f.Anonymous:
			p.Log("ignore embedded field %q that is not struct type", f.Name)
		}
	}
	return nil
}

// parseField parses the given struct field tag, and add a rule
// in the parser according to its type and the options that were set on the tag.
func (p *Parser) parseField(sf reflect.StructField) error {
	f := &Field{
		FieldMeta: &FieldMeta{
			Name:      p.ColumnFn(sf.Name),
			FilterOps: make(map[string]bool),
		},
		CovertFn: valueFn,
	}
	layout := time.RFC3339
	opts := strings.Split(sf.Tag.Get(p.TagName), ",")
	for _, opt := range opts {
		switch s := strings.TrimSpace(opt); {
		case s == "sort":
			f.Sortable = true
		case s == "filter":
			f.Filterable = true
		case strings.HasPrefix(opt, "column"):
			f.Name = strings.TrimPrefix(opt, "column=")
		case strings.HasPrefix(opt, "layout"):
			layout = strings.TrimPrefix(opt, "layout=")
			// if it's one of the standard layouts, like: RFC822 or Kitchen.
			if ly, ok := layouts[layout]; ok {
				layout = ly
			}
			// test the layout on a value (on itself). however, some layouts are invalid
			// time values for time.Parse, due to formats such as _ for space padding and
			// Z for zone information.
			v := strings.NewReplacer("_", " ", "Z", "+").Replace(layout)
			if _, err := time.Parse(layout, v); err != nil {
				return fmt.Errorf("rql: layout %q is not parsable: %v", layout, err)
			}
		default:
			p.Log("Ignoring unknown option %q in struct tag", opt)
		}
	}
	f.Layout = layout

	f.Type = indirect(sf.Type)
	filterOps := p.Config.GetSupportedOps(f.FieldMeta)
	if len(filterOps) == 0 {
		return fmt.Errorf("rql: field type for %q is not supported", sf.Name)
	}
	f.CovertFn = p.Config.GetConverter(f.FieldMeta)
	f.ValidateFn = p.Config.GetValidator(f.FieldMeta)

	for _, op := range filterOps {
		f.FilterOps[p.op(op)] = true
	}
	p.fields[f.Name] = f
	return nil
}

type parseState struct {
	*Parser                     // reference of the parser config
	*bytes.Buffer               // query builder
	values        []interface{} // query values
}

var parseStatePool sync.Pool

func (p *Parser) newParseState() (ps *parseState) {
	if v := parseStatePool.Get(); v != nil {
		ps = v.(*parseState)
		ps.Reset()
		ps.values = nil
	} else {
		ps = new(parseState)
		// currently we're using an arbitrary size as the capacity of initial buffer.
		// What we can do in the future is to track the size of parse results, and use
		// the average value. Same thing applies to the `values` field below.
		ps.Buffer = bytes.NewBuffer(make([]byte, 0, 64))
	}
	ps.values = make([]interface{}, 0, 8)
	ps.Parser = p
	return
}

// sort build the sort clause.
func (p *Parser) sort(fields []string) string {
	sortParams := make([]string, len(fields))
	for i, field := range fields {
		expect(field != "", "sort field can not be empty")

		var orderBy string
		f0 := field[0]
		if f0 == byte(ASC) || f0 == byte(DESC) {
			orderBy = p.GetDBDir(Direction(f0))
			field = field[1:]
		}

		expect(p.fields[field] != nil, "unrecognized key %q for sorting", field)
		expect(p.fields[field].Sortable, "field %q is not sortable", field)
		colName := p.colName(field)
		if orderBy != "" {
			colName += " " + orderBy
		}
		sortParams[i] = colName
	}
	return strings.Join(sortParams, ", ")
}

func (p *parseState) and(f map[string]interface{}) {
	var i int
	for k, v := range f {
		if i > 0 {
			p.WriteString(" AND ")
		}
		switch {
		case k == p.op(OR):
			terms, ok := v.([]interface{})
			expect(ok, "$or must be type array")
			p.relOp(OR, terms)
		case k == p.op(AND):
			terms, ok := v.([]interface{})
			expect(ok, "$and must be type array")
			p.relOp(AND, terms)
		case p.fields[k] != nil:
			f := p.fields[k]
			expect(f.Filterable, "field %q is not filterable", k)
			p.field(f, v)
		default:
			expect(false, "unrecognized key %q for filtering", k)
		}
		i++
	}
}

func (p *parseState) relOp(op Op, terms []interface{}) {
	var i int
	if len(terms) > 1 {
		p.WriteByte('(')
	}
	for _, t := range terms {
		if i > 0 {
			p.WriteByte(' ')
			p.WriteString(p.GetDBOp(op, nil))
			p.WriteByte(' ')
		}
		mt, ok := t.(map[string]interface{})
		expect(ok, "expressions for $%s operator must be type object", op)
		p.and(mt)
		i++
	}
	if len(terms) > 1 {
		p.WriteByte(')')
	}
}

func (p *parseState) field(f *Field, v interface{}) {
	terms, ok := v.(map[string]interface{})
	// default equality check.
	if !ok {
		op := EQ
		err := f.ValidateFn(op, *f.FieldMeta, v)
		must(err, "invalid datatype for field %q", f.Name)
		p.WriteString(p.fmtOp(f, op))
		arg := f.CovertFn(op, *f.FieldMeta, v)
		p.values = append(p.values, arg)
	}
	var i int
	if len(terms) > 1 {
		p.WriteByte('(')
	}
	for opName, opVal := range terms {
		if i > 0 {
			p.WriteString(" AND ")
		}
		op := Op(opName[1:])
		expect(f.FilterOps[opName], "can not apply op %q on field %q", opName, f.Name)
		must(f.ValidateFn(op, *f.FieldMeta, opVal), "invalid datatype or format for field %q", f.Name)
		p.WriteString(p.fmtOp(f, op))
		arg := f.CovertFn(op, *f.FieldMeta, opVal)
		p.values = append(p.values, arg)
		i++
	}
	if len(terms) > 1 {
		p.WriteByte(')')
	}
}

// fmtOp create a string for the operation with a placeholder.
// for example: "name = ?", or "age >= ?".
func (p *Parser) fmtOp(f *Field, op Op) string {
	return p.colName(f.Name) + " " + p.GetDBOp(op, f.FieldMeta) + " ?"
}

// colName formats the query field to database column name in cases the user configured a custom
// field separator. for example: if the user configured the field separator to be ".", the fields
// like "address.name" will be changed to "address_name".
func (p *Parser) colName(field string) string {
	if p.FieldSep != DefaultFieldSep {
		return strings.Replace(field, p.FieldSep, DefaultFieldSep, -1)
	}
	return field
}

func (p *Parser) op(op Op) string {
	return p.OpPrefix + string(op)
}

// expect panic if the condition is false.
func expect(cond bool, msg string, args ...interface{}) {
	if !cond {
		panic(&ParseError{fmt.Sprintf(msg, args...)})
	}
}

// must panics if the error is not nil.
func must(err error, msg string, args ...interface{}) {
	if err != nil {
		args = append(args, err)
		panic(&ParseError{fmt.Sprintf(msg+": %s", args...)})
	}
}

// indirect returns the item at the end of indirection.
func indirect(t reflect.Type) reflect.Type {
	for ; t.Kind() == reflect.Ptr; t = t.Elem() {
	}
	return t
}

// --------------------------------------------------------
// Validators and Converters

func errorType(v interface{}, expected string) error {
	actual := "nil"
	if v != nil {
		actual = reflect.TypeOf(v).Kind().String()
	}
	return fmt.Errorf("expect <%s>, got <%s>", expected, actual)
}

// validate that the underlined element of given interface is a boolean.
func validateBool(op Op, f FieldMeta, v interface{}) error {
	if _, ok := v.(bool); !ok {
		return errorType(v, "bool")
	}
	return nil
}

// validate that the underlined element of given interface is a string.
func validateString(op Op, f FieldMeta, v interface{}) error {
	if _, ok := v.(string); !ok {
		return errorType(v, "string")
	}
	return nil
}

// validate that the underlined element of given interface is a float.
func validateFloat(op Op, f FieldMeta, v interface{}) error {
	if _, ok := v.(float64); !ok {
		return errorType(v, "float64")
	}
	return nil
}

// validate that the underlined element of given interface is an int.
func validateInt(op Op, f FieldMeta, v interface{}) error {
	n, ok := v.(float64)
	if !ok {
		return errorType(v, "int")
	}
	if math.Trunc(n) != n {
		return errors.New("not an integer")
	}
	return nil
}

// validate that the underlined element of given interface is an int and greater than 0.
func validateUInt(op Op, f FieldMeta, v interface{}) error {
	if err := validateInt(op, f, v); err != nil {
		return err
	}
	if v.(float64) < 0 {
		return errors.New("not an unsigned integer")
	}
	return nil
}

// validate that the underlined element of this interface is a "datetime" string.
func validateTime(layout string) Validator {
	return func(_ Op, _ FieldMeta, v interface{}) error {
		s, ok := v.(string)
		if !ok {
			return errorType(v, "string")
		}
		_, err := time.Parse(layout, s)
		return err
	}
}

// convert float to int.
func convertInt(op Op, f FieldMeta, v interface{}) interface{} {
	return int(v.(float64))
}

// convert string to time object.
func convertTime(layout string) func(Op, FieldMeta, interface{}) interface{} {
	return func(_ Op, _ FieldMeta, v interface{}) interface{} {
		t, _ := time.Parse(layout, v.(string))
		return t
	}
}

// nop converter.
func valueFn(op Op, f FieldMeta, v interface{}) interface{} {
	return v
}

// layouts holds all standard time.Time layouts.
var layouts = map[string]string{
	"ANSIC":       time.ANSIC,
	"UnixDate":    time.UnixDate,
	"RubyDate":    time.RubyDate,
	"RFC822":      time.RFC822,
	"RFC822Z":     time.RFC822Z,
	"RFC850":      time.RFC850,
	"RFC1123":     time.RFC1123,
	"RFC1123Z":    time.RFC1123Z,
	"RFC3339":     time.RFC3339,
	"RFC3339Nano": time.RFC3339Nano,
	"Kitchen":     time.Kitchen,
	"Stamp":       time.Stamp,
	"StampMilli":  time.StampMilli,
	"StampMicro":  time.StampMicro,
	"StampNano":   time.StampNano,
}
