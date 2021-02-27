package rql

import (
	"database/sql"
	"testing"
	"time"
)

type Int int

type Date time.Time

type T struct {
	Age     int    `rql:"filter,sort"`
	Name    string `rql:"filter,sort"`
	Address struct {
		Name string `rql:"filter,sort"`
	}
	Admin     bool          `rql:"filter"`
	CreatedAt time.Time     `rql:"filter,sort"`
	Int       Int           `rql:"filter"`
	NullInt   sql.NullInt64 `rql:"filter"`
	Date      *Date         `rql:"filter"`
	Bool      bool          `rql:"filter"`
	PtrBool   *bool         `rql:"filter"`
	Work      *struct {
		Name    string `rql:"filter"`
		Address *struct {
			PtrString *string `rql:"filter"`
		}
	}
}

var p = MustNewParser(Config{
	Model:    T{},
	FieldSep: ".",
})

func BenchmarkLargeQuery(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := p.Parse([]byte(`{
		"filter": {
			"admin": true,
			"name": "foo",
			"address.name": "bar",
			"$or": [
				{ "age": { "$gte": 20 } },
				{ "age": { "$lte": 10 } },
				{ "name": { "$like": "foo" } },
				{ "work.name": { "$like": "bar" } },
				{ "work.address.ptr_string": { "$like": "baz" } }
			],
			"created_at": "2018-05-10T05:03:31.031Z",
			"int": 10,
			"null_int": 10,
			"date": "2018-05-10T05:03:31.031Z",
			"bool": true,
			"ptr_bool": false
		},
		"sort": [
			"address.name",
			"-age"
		],
		"offset": 100,
		"limit": 10
	}`))
		if err != nil {
			b.Error(err)
		}
	}
}

func BenchmarkMediumQuery(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := p.Parse([]byte(`{
		"filter": {
			"name": "foo",
			"address.name": "bar",
			"$or": [
				{ "age": { "$gt": 20 } },
				{ "age": { "$lt": 10 } }
			],
			"created_at": "2018-05-10T05:03:31.031Z"
		},
		"sort": [ "address.name" ],
		"offset": 100,
		"limit": 10
	}`))
		if err != nil {
			b.Error(err)
		}

	}
}

func BenchmarkSmallQuery(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := p.Parse([]byte(`{
		"filter": {
			"address.name": "TLV",
			"admin": true
		},
		"offset": 25,
		"limit": 10
	}`))
		if err != nil {
			b.Error(err)
		}
	}
}
