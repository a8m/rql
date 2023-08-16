package rql

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"
)

var customOpFormat = map[Op]string{
	EQ:       "=",
	NEQ:      "<>",
	LT:       "<",
	GT:       ">",
	LTE:      "<=",
	GTE:      ">=",
	LIKE:     "ILIKE",
	OR:       "OR",
	AND:      "AND",
	IN:       "IN",
	NIN:      "NOT IN",
	ALL:      "@>",
	OVERLAP:  "&&",
	CONTAINS: "@>",
	EXISTS:   "?|",
}

type StructAlias map[string]interface{}

func TestParse2(t *testing.T) {
	tests := []struct {
		name    string
		conf    Config
		input   []byte
		wantErr bool
		wantOut *Params
	}{
		{

			name: "custom conv/val func",
			conf: Config{
				Model: struct {
					IDs      []string               `rql:"filter,column=ids"`
					StrSl    []string               `rql:"filter,column=str_sl"`
					Inty     int                    `rql:"filter,column=inty"`
					IntSl    []int                  `rql:"filter,column=int_sl"`
					Floats   []float64              `rql:"filter,column=floats"`
					Map      map[string]interface{} `rql:"filter,column=map"`
					AliasMap StructAlias            `rql:"filter,column=alias_map"`
				}{},
				FieldSep: ".",
				GetDBStatement: func(o Op, f *FieldMeta) (string, string) {
					if o == Op("any") {
						return customOpFormat[o], "%v %v (%v)"
					}
					return customOpFormat[o], "%v %v %v"
				},
				GetSupportedOps: CustomGetSupportedOps,
				GetValidator:    CustomGetValidateFn,
				GetConverter:    CustomGetConverterFn,
			},
			input: []byte(`{
				"filter": {
					"floats" :{"$overlap":[1.2,3.2,1]},
					"map" : {"$contains": {"key":{"someobject":"fdf"}}},
					"alias_map" : {"$exists": "str"},
					"ids": ["1"],
					"inty": {"$in":[2]},
					"int_sl": {"$all":[1,2]}
				}
				}`),
			wantOut: &Params{
				Limit:     25,
				FilterExp: "map @> ? AND alias_map ?| ? AND ids = ? AND inty IN ? AND int_sl @> ? AND floats && ?",
				FilterArgs: []interface{}{
					[]interface{}{1.2, 3.2, float64(1)},
					map[string]interface{}{"key": map[string]interface{}{"someobject": "fdf"}},
					"str",
					[]interface{}{"1"},
					[]interface{}{2},
					[]interface{}{1, 2}},
				Sort: "",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.conf.Log = t.Logf
			p, err := NewParser(tt.conf)
			if err != nil {
				t.Fatalf("failed to build parser: %v", err)
			}
			out, err := p.Parse(tt.input)
			if tt.wantErr != (err != nil) {
				t.Fatalf("want: %v\ngot:%v\nerr: %v", tt.wantErr, err != nil, err)
			}
			assertParams(t, out, tt.wantOut)
		})
	}
}

var (
	IN       = Op("in")
	NIN      = Op("nin")
	OVERLAP  = Op("overlap")
	ALL      = Op("all")
	CONTAINS = Op("contains")
	EXISTS   = Op("exists")
)

func CustomGetSupportedOps(f *FieldMeta) []Op {
	t := f.Type
	switch t.Kind() {
	case reflect.Bool:
		return []Op{EQ, NEQ}
	case reflect.String:
		return []Op{EQ, NEQ, LT, LTE, GT, GTE, LIKE, IN, NIN}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return []Op{EQ, NEQ, LT, LTE, GT, GTE, IN, NIN}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return []Op{EQ, NEQ, LT, LTE, GT, GTE, IN, NIN}
	case reflect.Float32, reflect.Float64:
		return []Op{EQ, NEQ, LT, LTE, GT, GTE, IN, NIN}
	case reflect.Slice:
		return []Op{EQ, NEQ, OVERLAP, ALL}
	case reflect.Map:
		return []Op{CONTAINS, EXISTS}
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

func CustomGetConverterFn(f *FieldMeta) Converter {
	layout := f.Layout
	t := f.Type
	switch t.Kind() {
	case reflect.Bool:
		return valueFn
	case reflect.String:
		return valueFn
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return customConvertInt
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return customConvertInt
	case reflect.Float32, reflect.Float64:
		return valueFn
	case reflect.Slice:
		return convertSlice
	case reflect.Map:
		return valueFn
	case reflect.Struct:
		switch v := reflect.Zero(t); v.Interface().(type) {
		case sql.NullBool:
			return valueFn
		case sql.NullString:
			return valueFn
		case sql.NullInt64:
			return customConvertInt
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

func CustomGetValidateFn(f *FieldMeta) Validator {
	t := f.Type
	layout := f.Layout
	switch t.Kind() {
	case reflect.Bool:
		return validateBool
	case reflect.String:
		return customValidateString
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return customValidateInt
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return customValidateUInt
	case reflect.Float32, reflect.Float64:
		return customValidateFloat
	case reflect.Slice:
		return validateSliceOp
	case reflect.Map:
		return validateMapOp
	case reflect.Struct:
		switch v := reflect.Zero(t); v.Interface().(type) {
		case sql.NullBool:
			return validateBool
		case sql.NullString:
			return customValidateString
		case sql.NullInt64:
			return customValidateInt
		case sql.NullFloat64:
			return customValidateFloat
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

func validateSliceElem(v interface{}, expectedElemType reflect.Type) error {
	slice, ok := v.([]interface{})
	if !ok {
		return errorType(v, "array")
	}
	for _, item := range slice {
		it := reflect.TypeOf(item)
		c := isComparable(expectedElemType, it)

		if !c {
			t := expectedElemType.Kind().String()
			return errorType(item, t)
		}
	}
	return nil
}

// validate that the underlined element of given interface is a string.
func customValidateString(op Op, f FieldMeta, v interface{}) error {
	if op == IN || op == NIN {
		validateSliceElem(v, reflect.TypeOf(""))
	}
	if _, ok := v.(string); !ok {
		return errorType(v, "string")
	}
	return nil
}

// validate that the underlined element of given interface is a float.
func customValidateFloat(op Op, f FieldMeta, v interface{}) error {
	if op == IN || op == NIN {
		return validateSliceElem(v, reflect.TypeOf(1.1))
	}
	if _, ok := v.(float64); !ok {
		return errorType(v, "float64")
	}
	return nil
}

// validate that the underlined element of given interface is an int.
func customValidateInt(op Op, f FieldMeta, v interface{}) error {
	if op == IN || op == NIN {
		return validateSliceElem(v, reflect.TypeOf(1.1))
	}
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
func customValidateUInt(op Op, f FieldMeta, v interface{}) error {
	if op == IN || op == NIN {
		return validateSliceElem(v, reflect.TypeOf(1.1))
	}
	if err := validateInt(op, f, v); err != nil {
		return err
	}
	if v.(float64) < 0 {
		return errors.New("not an unsigned integer")
	}
	return nil
}

// convert float to int.
func customConvertInt(op Op, f FieldMeta, v interface{}) interface{} {
	if op == IN || op == NIN {
		sl, ok := v.([]interface{})
		if !ok {
			return v
		}
		for i, f := range sl {
			newInt := int(f.(float64))
			sl[i] = newInt
		}
		return sl
	}
	return int(v.(float64))
}

func convertSlice(op Op, f FieldMeta, v interface{}) interface{} {
	t := f.Type
	if isNumeric(t.Elem()) {
		switch t.Elem().Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:

			sl, ok := v.([]interface{})
			if !ok {
				return v
			}
			for i, f := range sl {
				newInt := int(f.(float64))
				sl[i] = newInt
			}
			return sl
		}
	}
	return v
}

func isNumeric(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128:
		return true
	default:
		return false
	}
}

// does not handle int IN []int
func isComparable(t reflect.Type, t2 reflect.Type) bool {
	return t == t2 || (isNumeric(t) && isNumeric(t2))
}

func validateSliceOp(op Op, f FieldMeta, v interface{}) error {
	t := f.Type
	if t.Kind() != reflect.Slice {
		return fmt.Errorf("t is not a slice, wrong validate func")
	}

	vType := reflect.TypeOf(v)
	if vType.Kind() != reflect.Slice {
		return fmt.Errorf("not a slice")
	}
	return validateSliceElem(v, t.Elem())
}

func validateMapOp(op Op, f FieldMeta, v interface{}) error {
	if op == EXISTS {
		_, ok := v.(string)
		if !ok {
			return fmt.Errorf("exists expects a string arg")
		}
	}
	if op == ALL {
		_, ok := v.(map[string]interface{})
		if !ok {
			return fmt.Errorf("exists expects a string arg")
		}
	}
	return nil
}
