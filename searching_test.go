package rql

import (
	"fmt"
	"testing"

	"github.com/jinzhu/gorm"
)

func TestSearching(t *testing.T) {
	tests := []struct {
		name    string
		conf    Config
		input   []byte
		wantErr bool
		wantOut *Params
	}{
		{
			name: "simple search of single column",
			conf: Config{
				Model: new(struct {
					Age  int    `rql:"filter"`
					Name string `rql:"filter, search"`
				}),
				DefaultLimit: 25,
			},
			input: []byte(`{
				"search": {
					"query": "foo"
				}
			}`),
			wantOut: &Params{
				Limit:  25,
				Search: "LOWER(name) LIKE LOWER('%' || ? || '%')",
			},
		},
		{
			name: "multi-column search",
			conf: Config{
				Model: new(struct {
					Age  int    `rql:"filter"`
					Name string `rql:"filter, search"`
					City string `rql:"filter, search"`
				}),
				DefaultLimit: 25,
			},
			input: []byte(`{
				"search": {
					"query": "foo"
				}
			}`),
			wantOut: &Params{
				Limit:  25,
				Search: "LOWER(name) LIKE LOWER('%' || ? || '%') OR LOWER(city) LIKE LOWER('%' || ? || '%')",
			},
		},
		{ // todo: the succes of this is undeterministic, since the order of elements in map is not guaranteed
			name: "multi-column search with mapping",
			conf: Config{
				Model: new(struct {
					Age  int    `rql:"filter"`
					Name string `rql:"filter, search"`
					City string `rql:"filter, search"`
				}),
				ColumnFn: func(columnName string) string {
					return fmt.Sprintf("person.%s", gorm.ToDBName(columnName))
				},
				DefaultLimit: 25,
			},
			input: []byte(`{
				"search": {
					"query": "foo"
				}
			}`),
			wantOut: &Params{
				Limit:  25,
				Search: "LOWER(person.city) LIKE LOWER('%' || ? || '%') OR LOWER(person.name) LIKE LOWER('%' || ? || '%')",
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
