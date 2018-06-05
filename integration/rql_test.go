package integration

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/a8m/rql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jinzhu/gorm"
)

var (
	CreateTime, _ = time.Parse(time.RFC3339, "2000-05-16T16:00:00.000Z")
	MySQLConn     = os.Getenv("CONN_STRING")
	QueryParser   = rql.MustNewParser(rql.Config{
		Model:    User{},
		FieldSep: ".",
	})
)

type User struct {
	ID          int       `rql:"filter,sort"`
	Admin       bool      `rql:"filter"`
	Name        string    `rql:"filter"`
	AddressName string    `rql:"filter"`
	CreatedAt   time.Time `rql:"filter"`
}

func TestMySQL(t *testing.T) {
	db := Connect(t)
	SetUp(t, db)
	defer Teardown(t, db)
	AssertCount(t, db, 1, `{ "filter": { "id": 1 } }`)
	AssertCount(t, db, 1, `{ "filter": { "id": 100 } }`)
	AssertCount(t, db, 50, `{ "filter": { "id": { "$gt": 50 } } }`)
	AssertCount(t, db, 50, `{ "filter": { "id": { "$lte": 50 } } }`)
	AssertCount(t, db, 99, `{ "filter": { "$or": [{ "id":{ "$gt": 50 } }, { "id":{ "$lt": 50 } }] } }`)
	AssertCount(t, db, 1, `{ "filter": {"name": "user_1" } }`)
	AssertCount(t, db, 100, `{ "filter": {"name": {"$like": "user%" } } }`) // all
	AssertCount(t, db, 2, `{ "filter": {"name": {"$like": "%10%" } } }`)    // 10 or 100
	AssertCount(t, db, 50, `{ "filter": {"admin": true } }`)                // 50 users
	AssertCount(t, db, 0, `{ "filter": {"address_name": "??" } }`)          // nothing
	AssertCount(t, db, 1, `{ "filter": {"address_name": "address_1" } }`)   // 1st user
	AssertCount(t, db, 100, fmt.Sprintf(`{"filter": {"created_at": { "$gt": %q } } }`, CreateTime.Add(-time.Hour).Format(time.RFC3339)))
	AssertCount(t, db, 100, fmt.Sprintf(`{"filter": {"created_at": { "$lte": %q } } }`, CreateTime.Add(time.Hour).Format(time.RFC3339)))
	AssertMatchIDs(t, db, []int{1}, `{ "filter": { "id": 1 } }`)
	AssertMatchIDs(t, db, []int{2, 3}, `{ "filter": { "$or": [ { "id": 2 }, { "id": 3 } ] } }`)
	AssertMatchIDs(t, db, []int{3, 2}, `{ "filter": { "$or": [ { "id": 2 }, { "id": 3 } ] }, "sort": ["-id"] }`)
	AssertMatchIDs(t, db, []int{5, 4, 3, 2, 1}, `{ "filter": { "id": { "$lte": 5 } }, "sort": ["-id"] }`)
}

func AssertCount(t *testing.T, db *gorm.DB, expected int, query string) {
	params, err := QueryParser.Parse([]byte(query))
	must(t, err, "parse query: %s", query)
	count := 0
	err = db.Model(User{}).Where(params.FilterExp, params.FilterArgs...).Count(&count).Error
	must(t, err, "count users")
	if count != expected {
		t.Errorf("AssertCount:\n\twant: %d\n\tgot: %d", expected, count)
	}
}

func AssertMatchIDs(t *testing.T, db *gorm.DB, expected []int, query string) {
	params, err := QueryParser.Parse([]byte(query))
	must(t, err, "parse query: %s", query)
	var ids []int
	err = db.Model(User{}).Where(params.FilterExp, params.FilterArgs...).Order(params.Sort).Pluck("id", &ids).Error
	must(t, err, "select ids")
	if len(ids) != len(expected) {
		t.Errorf("AssertMatchIDs:\n\twant: %d\n\tgot: %d", expected, ids)
		return
	}
	for i := range expected {
		if ids[i] != expected[i] {
			t.Errorf("AssertMatchIDs:\n\twant: %d\n\tgot: %d", expected, ids)
			return
		}
	}
}

func Connect(t *testing.T) *gorm.DB {
	if MySQLConn == "" {
		t.Fatal("missing database connection string")
	}
	for i := 1; i <= 5; i++ {
		db, err := gorm.Open("mysql", MySQLConn)
		if err == nil {
			return db
		}
		time.Sleep(time.Second * time.Duration(i))
	}
	t.Log("failed connect to the database")
	return nil
}

func SetUp(t *testing.T, db *gorm.DB) {
	must(t, db.AutoMigrate(User{}).Error, "migrate db")
	var wg sync.WaitGroup
	wg.Add(100)
	for i := 1; i <= 100; i++ {
		go func(i int) {
			defer wg.Done()
			err := db.Create(&User{
				ID:          i,
				Admin:       i%2 == 0,
				Name:        fmt.Sprintf("user_%d", i),
				AddressName: fmt.Sprintf("address_%d", i),
				CreatedAt:   CreateTime.Add(time.Minute * 1),
			}).Error
			must(t, err, "create user")
		}(i)
	}
	wg.Wait()
}

func Teardown(t *testing.T, db *gorm.DB) {
	must(t, db.DropTable(User{}).Error, "drop table")
	must(t, db.Close(), "close conn to mysql")
}

func must(t *testing.T, err error, msg string, args ...interface{}) {
	if err != nil {
		args = append(args, err)
		t.Fatalf(msg+": %s", args...)
	}
}
