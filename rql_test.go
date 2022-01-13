package rql

import (
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestInit(t *testing.T) {
	tests := []struct {
		name    string
		model   interface{}
		wantErr bool
	}{
		{
			name: "simple struct without tags",
			model: new(struct {
				Age  int
				Name string
			}),
		},
		{
			name: "simple filtering",
			model: new(struct {
				Age  int    `rql:"filter"`
				Name string `rql:"filter"`
			}),
		},
		{
			name: "ignore unrecognized options",
			model: new(struct {
				Age int `rql:"filter,foo"`
			}),
		},
		{
			name: "return an error for unsupported types",
			model: new(struct {
				Age interface{} `rql:"filter"`
			}),
			wantErr: true,
		},
		{
			name:    "model is mandatory",
			wantErr: true,
		},
		{
			name:    "model must be a struct type 1",
			model:   1,
			wantErr: true,
		},
		{
			name:    "model must be a struct type 2",
			model:   new(interface{}),
			wantErr: true,
		},
		{
			name:    "model must be a struct type 2",
			model:   new(interface{}),
			wantErr: true,
		},
		{
			name: "nested objects",
			model: new(struct {
				Name    string `rql:"filter"`
				Address struct {
					City    string `rql:"filter"`
					ZIPCode string `rql:"sort"`
				}
			}),
		},
		{
			name: "embedded objects",
			model: (func() interface{} {
				type Person struct {
					Age  int    `rql:"sort"`
					Name string `rql:"filter"`
				}
				return struct {
					Person
					Job struct {
						Type   int `rql:"filter"`
						Salary int `rql:"filter,sort"`
					}
				}{}
			})(),
		},
		{
			name: "type aliases",
			model: (func() interface{} {
				type JobType int
				return struct {
					Name    string  `rql:"filter,sort"`
					JobType JobType `rql:"filter,sort"`
				}{}
			})(),
		},
		{
			name: "time format",
			model: new(struct {
				CreatedAt time.Time `rql:"filter,layout=2006-01-02 15:04"`
				UpdatedAt time.Time `rql:"filter,layout=Kitchen"`
			}),
		},
		{
			name: "uuid format",
			model: new(struct {
				ID uuid.UUID `rql:"filter"`
			}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewParser(Config{
				Model: tt.model,
				Log:   t.Logf,
			})
			if tt.wantErr != (err != nil) {
				t.Fatalf("want: %v\ngot:%v\nerr: %v", tt.wantErr, err != nil, err)
			}
		})
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		conf    Config
		input   []byte
		wantErr bool
		wantOut *Params
	}{
		{
			name: "simple test",
			conf: Config{
				Model: new(struct {
					Age     int    `rql:"filter"`
					Name    string `rql:"filter"`
					Address string `rql:"filter"`
				}),
				DefaultLimit: 25,
			},
			input: []byte(`{
				"filter": {
					"name": "foo",
					"age": 12,
					"$or": [
						{ "address": "DC" },
						{ "address": "Marvel" }
					],
					"$and": [
						{ "age": { "$neq": 10} },
						{ "age": { "$neq": 20} },
						{ "$or": [{ "age": 11 }, {"age": 10}] }
					]
				}
			}`),
			wantOut: &Params{
				Limit:      25,
				FilterExp:  "name = ? AND age = ? AND (address = ? OR address = ?) AND (age <> ? AND age <> ? AND (age = ? OR age = ?))",
				FilterArgs: []interface{}{"foo", 12, "DC", "Marvel", 10, 20, 11, 10},
			},
		},
		{
			name: "nested model",
			conf: Config{
				Model: new(struct {
					Age     int    `rql:"filter"`
					Name    string `rql:"filter"`
					Address struct {
						Name string `rql:"filter"`
					}
				}),
				DefaultLimit: 25,
			},
			input: []byte(`{
				"filter": {
					"name": "foo",
					"age": 12,
					"$or": [
						{ "address_name": "DC" },
						{ "address_name": "Marvel" }
					]
				}
			}`),
			wantOut: &Params{
				Limit:      25,
				FilterExp:  "name = ? AND age = ? AND (address_name = ? OR address_name = ?)",
				FilterArgs: []interface{}{"foo", 12, "DC", "Marvel"},
			},
		},
		{
			name: "nested model with custom sep",
			conf: Config{
				Model: new(struct {
					Age     int    `rql:"filter"`
					Name    string `rql:"filter"`
					Address struct {
						Name string `rql:"filter"`
					}
				}),
				FieldSep:     ".",
				DefaultLimit: 25,
			},
			input: []byte(`{
				"filter": {
					"name": "foo",
					"age": 12,
					"$or": [
						{ "address.name": "DC" },
						{ "address.name": "Marvel" }
					]
				}
			}`),
			wantOut: &Params{
				Limit:      25,
				FilterExp:  "name = ? AND age = ? AND (address_name = ? OR address_name = ?)",
				FilterArgs: []interface{}{"foo", 12, "DC", "Marvel"},
			},
		},
		{
			name: "embed models",
			conf: Config{
				Model: (func() interface{} {
					type Person struct {
						Age  int    `rql:"filter"`
						Name string `rql:"filter"`
					}
					return struct {
						Person
						Address string `rql:"filter"`
					}{}
				})(),
				FieldSep:     ".",
				DefaultLimit: 25,
			},
			input: []byte(`{
				"filter": {
					"name": "foo",
					"age": 12,
					"$or": [
						{ "address": "DC" },
						{ "address": "Marvel" }
					]
				}
			}`),
			wantOut: &Params{
				Limit:      25,
				FilterExp:  "name = ? AND age = ? AND (address = ? OR address = ?)",
				FilterArgs: []interface{}{"foo", 12, "DC", "Marvel"},
			},
		},
		{
			name: "ignore non-struct embedding",
			conf: Config{
				Model: struct {
					int
				}{},
				DefaultLimit: 25,
			},
			input: []byte(`{}`),
			wantOut: &Params{
				Limit:      25,
				FilterExp:  "",
				FilterArgs: []interface{}{},
			},
		},
		{
			name: "type alias",
			conf: Config{
				Model: (func() interface{} {
					type Number float64
					return struct {
						Age     Number `rql:"filter"`
						Address string `rql:"filter"`
					}{}
				})(),
				DefaultLimit: 25,
			},
			input: []byte(`{
				"filter": {
					"address": "foo",
					"age": 12.5
				}
			}`),
			wantOut: &Params{
				Limit:      25,
				FilterExp:  "address = ? AND age = ?",
				FilterArgs: []interface{}{"foo", 12.5},
			},
		},
		{
			name: "sql types 1",
			conf: Config{
				Model: struct {
					Bool        bool          `rql:"filter"`
					Int8        int8          `rql:"filter"`
					Uint8       uint8         `rql:"filter"`
					NullBool    sql.NullBool  `rql:"filter"`
					PtrNullBool *sql.NullBool `rql:"filter"`
				}{},
				DefaultLimit: 25,
			},
			input: []byte(`{
				"filter": {
					"bool": true,
					"int8": 1,
					"uint8": 1,
					"null_bool": true,
					"ptr_null_bool": true
				}
			}`),
			wantOut: &Params{
				Limit:      25,
				FilterExp:  "bool = ? AND int8 = ? AND uint8 = ? AND null_bool = ? AND ptr_null_bool = ?",
				FilterArgs: []interface{}{true, 1, 1, true, true},
			},
		},
		{
			name: "sql types 2",
			conf: Config{
				Model: struct {
					NullInt64      sql.NullInt64    `rql:"filter"`
					PtrNullInt64   *sql.NullInt64   `rql:"filter"`
					NullFloat64    sql.NullFloat64  `rql:"filter"`
					PtrNullFloat64 *sql.NullFloat64 `rql:"filter"`
					NullString     sql.NullString   `rql:"filter"`
					PtrNullString  *sql.NullString  `rql:"filter"`
				}{},
				DefaultLimit: 25,
			},
			input: []byte(`{
				"filter": {
					"null_int64": 1,
					"ptr_null_int64": 1,
					"null_float64": 1,
					"ptr_null_float64": 1,
					"null_string": "",
					"ptr_null_string": ""
				}
			}`),
			wantOut: &Params{
				Limit:      25,
				FilterExp:  "null_int64 = ? AND ptr_null_int64 = ? AND null_float64 = ? AND ptr_null_float64 = ? AND null_string = ? AND ptr_null_string = ?",
				FilterArgs: []interface{}{1, 1, 1.0, 1.0, "", ""},
			},
		},
		{
			name: "time",
			conf: Config{
				Model: func() interface{} {
					type Date time.Time
					return struct {
						CreatedAt      time.Time  `rql:"filter"`
						UpdatedAt      *time.Time `rql:"filter"`
						SwaggerDate    Date       `rql:"filter"`
						PtrSwaggerDate *Date      `rql:"filter"`
					}{}
				}(),
				DefaultLimit: 25,
			},
			input: []byte(`{
				"filter": {
					"created_at": "2018-01-14T06:05:48.839Z",
					"updated_at": "2018-01-14T06:05:48.839Z",
					"swagger_date": "2018-01-14T06:05:48.839Z",
					"ptr_swagger_date": "2018-01-14T06:05:48.839Z"
				}
			}`),
			wantOut: &Params{
				Limit:     25,
				FilterExp: "created_at = ? AND updated_at = ? AND swagger_date = ? AND ptr_swagger_date = ?",
				FilterArgs: []interface{}{
					mustParseTime(time.RFC3339, "2018-01-14T06:05:48.839Z"),
					mustParseTime(time.RFC3339, "2018-01-14T06:05:48.839Z"),
					mustParseTime(time.RFC3339, "2018-01-14T06:05:48.839Z"),
					mustParseTime(time.RFC3339, "2018-01-14T06:05:48.839Z"),
				},
			},
		},
		{
			name: "uuid",
			conf: Config{
				Model: func() interface{} {
					return struct {
						ID         uuid.UUID  `rql:"filter"`
						ExternalID *uuid.UUID `rql:"filter"`
					}{}
				}(),
				DefaultLimit: 25,
			},
			input: []byte(`{
				"filter": {
					"id": "e701d193-420f-47db-9daf-2234bfcbb986",
					"external_id": "91aa0f70-7f2e-4e7b-b418-3a46dc48cdd9"
				}
			}`),
			wantOut: &Params{
				Limit:     25,
				FilterExp: "id = ? AND external_id = ?",
				FilterArgs: []interface{}{
					mustParseUUID("e701d193-420f-47db-9daf-2234bfcbb986"),
					mustParseUUID("91aa0f70-7f2e-4e7b-b418-3a46dc48cdd9"),
				},
			},
		},
		{
			name: "valid operations",
			conf: Config{
				Model: new(struct {
					Age     int    `rql:"filter"`
					Name    string `rql:"filter"`
					Address string `rql:"filter"`
				}),
				DefaultLimit: 25,
			},
			input: []byte(`{
				"filter": {
					"age": { "$gt": 10 },
					"name": { "$like": "%foo%" },
					"$or": [
						{ "name": { "$in": ["foo", "bar"] } },
						{ "age": { "$nin": [25, 35] } },
						{ "address": { "$eq": "DC" } },
						{ "address": { "$neq": "Marvel" } }
					]
				}
			}`),
			wantOut: &Params{
				Limit:      25,
				FilterExp:  "age > ? AND name LIKE ? AND (name IN (?,?) OR age NOT IN (?,?) OR address = ? OR address <> ?)",
				FilterArgs: []interface{}{10, "%foo%", "foo", "bar", 25, 35, "DC", "Marvel"},
			},
		},
		{
			name: "custom operation prefix",
			conf: Config{
				Model: new(struct {
					CreatedAt time.Time `rql:"filter"`
					Work      struct {
						Address string `rql:"filter"`
						Salary  int    `rql:"filter"`
					}
				}),
				OpPrefix:     "@",
				FieldSep:     "#",
				DefaultLimit: 25,
			},
			input: []byte(`{
				"filter": {
					"created_at": { "@gt": "2018-01-14T06:05:48.839Z" },
					"work#address": { "@like": "%DC%" },
					"@or": [
						{
							"work#salary": 100
						},
						{
							"work#salary": { "@gte": 200, "@lte": 300 }
						}
					]
				}
			}`),
			wantOut: &Params{
				Limit:      25,
				FilterExp:  "created_at > ? AND work_address LIKE ? AND (work_salary = ? OR (work_salary >= ? AND work_salary <= ?))",
				FilterArgs: []interface{}{mustParseTime(time.RFC3339, "2018-01-14T06:05:48.839Z"), "%DC%", 100, 200, 300},
			},
		},
		{
			name: "sort",
			conf: Config{
				Model: struct {
					Age     int    `rql:"filter,sort"`
					Name    string `rql:"filter,sort"`
					Address struct {
						Name string `rql:"filter,sort"`
						ZIP  *struct {
							Code int `rql:"filter,sort"`
						}
					}
				}{},
				FieldSep:     ".",
				DefaultLimit: 25,
			},
			input: []byte(`{
				"filter": {
					"address.zip.code": 100
				},
				"sort": ["address.name", "-address.zip.code", "+age"]
			}`),
			wantOut: &Params{
				Limit:      25,
				FilterExp:  "address_zip_code = ?",
				FilterArgs: []interface{}{100},
				Sort:       "address_name, address_zip_code desc, age asc",
			},
		},
		{
			name: "sort with default field separator",
			conf: Config{
				Model: struct {
					Age     int    `rql:"filter,sort"`
					Name    string `rql:"filter,sort"`
					Address struct {
						Name string `rql:"filter,sort"`
						ZIP  *struct {
							Code int `rql:"filter,sort"`
						}
					}
				}{},
				DefaultLimit: 25,
			},
			input: []byte(`{
				"filter": {
					"address_zip_code": 100
				},
				"sort": ["address_name", "-address_zip_code", "+age"]
			}`),
			wantOut: &Params{
				Limit:      25,
				FilterExp:  "address_zip_code = ?",
				FilterArgs: []interface{}{100},
				Sort:       "address_name, address_zip_code desc, age asc",
			},
		},
		{
			name: "sort with default sort field configured, and no sort in query",
			conf: Config{
				Model: struct {
					Age     int    `rql:"filter,sort"`
					Name    string `rql:"filter,sort"`
					Address struct {
						Name string `rql:"filter,sort"`
						ZIP  *struct {
							Code int `rql:"filter,sort"`
						}
					}
				}{},
				DefaultLimit: 25,
				DefaultSort:  []string{"-name"},
			},
			input: []byte(`{
				"filter": {
					"address_zip_code": 100
				},
				"sort": []
			}`),
			wantOut: &Params{
				Limit:      25,
				FilterExp:  "address_zip_code = ?",
				FilterArgs: []interface{}{100},
				Sort:       "name desc",
			},
		},
		{
			name: "sort with default sort field configured, and sort specified in query",
			conf: Config{
				Model: struct {
					Age     int    `rql:"filter,sort"`
					Name    string `rql:"filter,sort"`
					Address struct {
						Name string `rql:"filter,sort"`
						ZIP  *struct {
							Code int `rql:"filter,sort"`
						}
					}
				}{},
				DefaultLimit: 25,
				DefaultSort:  []string{"-name"},
			},
			input: []byte(`{
				"filter": {
					"address_zip_code": 100
				},
				"sort": ["-age"]
			}`),
			wantOut: &Params{
				Limit:      25,
				FilterExp:  "address_zip_code = ?",
				FilterArgs: []interface{}{100},
				Sort:       "age desc",
			},
		},
		{
			name: "select one",
			conf: Config{
				Model: struct {
					Age  int    `rql:"filter,sort"`
					Name string `rql:"filter,sort"`
				}{},
				DefaultLimit: 25,
			},
			input: []byte(`{
				"select": ["name"]
			}`),
			wantOut: &Params{
				Limit:  25,
				Select: "name",
			},
		},
		{
			name: "select many",
			conf: Config{
				Model: struct {
					Age  int    `rql:"filter,sort"`
					Name string `rql:"filter,sort"`
				}{},
				DefaultLimit: 25,
			},
			input: []byte(`{
				"select": ["name", "age"]
			}`),
			wantOut: &Params{
				Limit:  25,
				Select: "name, age",
			},
		},
		{
			name: "custom column name",
			conf: Config{
				Model: struct {
					Name string `rql:"filter,column=full_name,sort"`
				}{},
				DefaultLimit: 25,
			},
			input: []byte(`{
				"filter": {
					"full_name": "a8m"
				},
				"sort": ["full_name"]
			}`),
			wantOut: &Params{
				Limit:      25,
				FilterExp:  "full_name = ?",
				FilterArgs: []interface{}{"a8m"},
				Sort:       "full_name",
			},
		},
		{
			name: "naming columns",
			conf: Config{
				Model: struct {
					ID           string `rql:"filter"`
					FullName     string `rql:"filter"`
					HTTPUrl      string `rql:"filter"`
					NestedStruct struct {
						UUID string `rql:"filter"`
					}
				}{},
				FieldSep: ".",
			},
			input: []byte(`{
				"filter": {
					"id": "id",
					"full_name": "full_name",
					"http_url": "http_url",
					"nested_struct.uuid": "uuid"
				}
			}`),
			wantOut: &Params{
				Limit:      25,
				FilterExp:  "id = ? AND full_name = ? AND http_url = ? AND nested_struct_uuid = ?",
				FilterArgs: []interface{}{"id", "full_name", "http_url", "uuid"},
			},
		},
		{
			name: "time unix layout",
			conf: Config{
				Model: new(struct {
					CreatedAt time.Time `rql:"filter,layout=UnixDate"`
				}),
			},
			input: []byte(`{
				"filter": {
					"created_at": { "$gt": "Thu May 23 09:30:06 IDT 2000" }
				}
			}`),
			wantOut: &Params{
				Limit:      25,
				FilterExp:  "created_at > ?",
				FilterArgs: []interface{}{mustParseTime(time.UnixDate, "Thu May 23 09:30:06 IDT 2000")},
			},
		},
		{
			name: "time custom layout",
			conf: Config{
				Model: new(struct {
					CreatedAt time.Time `rql:"filter,layout=2006-01-02 15:04"`
				}),
			},
			input: []byte(`{
				"filter": {
					"created_at": { "$gt": "2006-01-02 15:04" }
				}
			}`),
			wantOut: &Params{
				Limit:      25,
				FilterExp:  "created_at > ?",
				FilterArgs: []interface{}{mustParseTime("2006-01-02 15:04", "2006-01-02 15:04")},
			},
		},
		{
			name: "mismatch time unix layout",
			conf: Config{
				Model: new(struct {
					CreatedAt time.Time `rql:"filter,layout=UnixDate"`
				}),
			},
			input: []byte(`{
				"filter": {
					"created_at": { "$gt": "2006-01-02 15:04" }
				}
			}`),
			wantErr: true,
		},
		{
			name: "mismatch int type 1",
			conf: Config{
				Model: struct {
					Age  int    `rql:"filter"`
					Name string `rql:"filter"`
				}{},
			},
			input: []byte(`{
				"filter": {
					"age": "a8m",
					"name": 10
				}
			}`),
			wantErr: true,
		},
		{
			name: "mismatch int type 2",
			conf: Config{
				Model: struct {
					Age int `rql:"filter"`
				}{},
			},
			input: []byte(`{
				"filter": {
					"age": 1.1
				}
			}`),
			wantErr: true,
		},
		{
			name: "mismatch string type",
			conf: Config{
				Model: struct {
					Name string `rql:"filter"`
				}{},
			},
			input: []byte(`{
				"filter": {
					"name": 10
				}
			}`),
			wantErr: true,
		},
		{
			name: "mismatch uint type 1",
			conf: Config{
				Model: struct {
					Age uint `rql:"filter"`
				}{},
			},
			input: []byte(`{
				"filter": {
					"age": "a8m"
				}
			}`),
			wantErr: true,
		},
		{
			name: "mismatch uint type 2",
			conf: Config{
				Model: struct {
					Age uint `rql:"filter"`
				}{},
			},
			input: []byte(`{
				"filter": {
					"age": -1
				}
			}`),
			wantErr: true,
		},
		{
			name: "mismatch time type 1",
			conf: Config{
				Model: struct {
					CreatedAt time.Time `rql:"filter"`
				}{},
			},
			input: []byte(`{
				"filter": {
					"created_at": "Sunday?"
				}
			}`),
			wantErr: true,
		},
		{
			name: "mismatch time type 2",
			conf: Config{
				Model: struct {
					CreatedAt time.Time `rql:"filter"`
				}{},
			},
			input: []byte(`{
				"filter": {
					"created_at": 12736186894
				}
			}`),
			wantErr: true,
		},
		{
			name: "mismatch bool type",
			conf: Config{
				Model: struct {
					Admin bool `rql:"filter"`
				}{},
			},
			input: []byte(`{
				"filter": {
					"admin": "false"
				}
			}`),
			wantErr: true,
		},
		{
			name: "mismatch float type",
			conf: Config{
				Model: struct {
					Age float32 `rql:"filter"`
				}{},
			},
			input: []byte(`{
				"filter": {
					"age": "13"
				}
			}`),
			wantErr: true,
		},
		{
			name: "unrecognized fields",
			conf: Config{
				Model: struct {
					Name string `rql:"filter"`
				}{},
			},
			input: []byte(`{
				"filter": {
					"age": "a8m"
				}
			}`),
			wantErr: true,
		},
		{
			name: "field is not sortable",
			conf: Config{
				Model: struct {
					Name string `rql:"filter"`
				}{},
			},
			input: []byte(`{
				"sort": ["name"]
			}`),
			wantErr: true,
		},
		{
			name: "invalid operation",
			conf: Config{
				Model: struct {
					Name string `rql:"filter"`
				}{},
			},
			input: []byte(`{
				"filter": {
					"name": {
						"$gt": 10
					}
				}
			}`),
			wantErr: true,
		},
		{
			name: "unrecognized operation",
			conf: Config{
				Model: struct {
					Name string `rql:"filter"`
				}{},
			},
			input: []byte(`{
				"filter": {
					"name": {
						"$regex": ".*"
					}
				}
			}`),
			wantErr: true,
		},
		{
			name: "limit and offset",
			conf: Config{
				Model: struct{}{},
			},
			input: []byte(`{
				"limit": 10,
				"offset": 4
			}`),
			wantOut: &Params{
				Limit:  10,
				Offset: 4,
			},
		},
		{
			name: "invalid offset",
			conf: Config{
				Model: struct{}{},
			},
			input: []byte(`{
				"limit": 10,
				"offset": -14
			}`),
			wantErr: true,
		},
		{
			name: "invalid limit 1",
			conf: Config{
				Model: struct{}{},
			},
			input: []byte(`{
				"limit": -10
			}`),
			wantErr: true,
		},
		{
			name: "invalid limit 2",
			conf: Config{
				Model:         struct{}{},
				LimitMaxValue: 100,
			},
			input: []byte(`{
				"limit": 200
			}`),
			wantErr: true,
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

// AssertQueryEqual tests if two query input are equal.
// TODO: improve this in the future.
func assertParams(t *testing.T, got *Params, want *Params) {
	if got == nil && want == nil {
		return
	}
	if got.Limit != want.Limit {
		t.Fatalf("limit: got: %v want %v", got.Limit, want.Limit)
	}
	if got.Offset != want.Offset {
		t.Fatalf("offset: got: %v want %v", got.Limit, want.Limit)
	}
	if got.Sort != want.Sort {
		t.Fatalf("sort: got: %q want %q", got.Sort, want.Sort)
	}
	if got.Search != want.Search {
		t.Fatalf("search: got: %q want %q", got.Search, want.Search)
	}
	if got.Select != want.Select {
		t.Fatalf("select: got: %q want %q", got.Select, want.Select)
	}
	if !equalExp(got.FilterExp, want.FilterExp) || !equalExp(want.FilterExp, got.FilterExp) {
		t.Fatalf("filter expr:\n\tgot: %q\n\twant %q", got.FilterExp, want.FilterExp)
	}
	if !equalArgs(got.FilterArgs, got.FilterArgs) || !equalArgs(want.FilterArgs, got.FilterArgs) {
		t.Fatalf("filter args:\n\tgot: %v\n\twant %v", got.FilterArgs, want.FilterArgs)
	}
}

func equalArgs(a, b []interface{}) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make([]bool, len(b))
	for _, arg1 := range a {
		var found bool
		for i, arg2 := range b {
			// skip values that matched before.
			if !seen[i] && reflect.DeepEqual(arg1, arg2) {
				seen[i] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func equalExp(e1, e2 string) bool {
	s1, s2 := split(e1), split(e2)
	for i := range s1 {
		var found bool
		for j := range s2 {
			// if it is a start of conjunction.
			if s1[i][0] == '(' && s2[j][0] == '(' {
				found = equalExp(s1[i][1:len(s1[i])-1], s2[j][1:len(s2[j])-1])
			} else {
				found = s1[i] == s2[j]
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func split(e string) []string {
	var s []string
	for len(e) > 0 {
		if e[0] == '(' {
			end := strings.LastIndexByte(e, ')') + 1
			s = append(s, e[:end])
			e = e[end:]
		} else {
			end := strings.IndexByte(e, '?') + 1
			s = append(s, e[:end])
			e = e[end:]
		}
		e = strings.TrimPrefix(e, " AND ")
		e = strings.TrimPrefix(e, " OR ")
	}
	return s
}

func mustParseTime(layout, s string) time.Time {
	t, _ := time.Parse(layout, s)
	return t
}

func mustParseUUID(s string) uuid.UUID {
	id, _ := uuid.Parse(s)
	return id
}
