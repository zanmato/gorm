package gorm_test

// Run tests
// $ docker-compose up
// $ ./test_all.sh

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/erikstmartin/go-testdb"
	"github.com/jinzhu/now"
	"github.com/zanmato/gorm"
	_ "github.com/zanmato/gorm/dialects/mssql"
	_ "github.com/zanmato/gorm/dialects/mysql"
	"github.com/zanmato/gorm/dialects/postgres"
	_ "github.com/zanmato/gorm/dialects/sqlite"
)

var (
	DB                 *gorm.DB
	t1, t2, t3, t4, t5 time.Time
)

func init() {
	var err error

	if DB, err = OpenTestConnection(); err != nil {
		panic(fmt.Sprintf("No error should happen when connecting to test database, but got err=%+v", err))
	}

	runMigration()
}

func OpenTestConnection() (db *gorm.DB, err error) {
	dbDSN := os.Getenv("GORM_DSN")
	switch os.Getenv("GORM_DIALECT") {
	case "mysql":
		fmt.Println("testing mysql...")
		if dbDSN == "" {
			dbDSN = "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True"
		}
		db, err = gorm.Open("mysql", dbDSN)
	case "postgres":
		fmt.Println("testing postgres...")
		if dbDSN == "" {
			dbDSN = "user=gorm password=gorm dbname=gorm port=9920 sslmode=disable"
		}
		db, err = gorm.Open("postgres", dbDSN)
	case "mssql":
		// CREATE LOGIN gorm WITH PASSWORD = 'LoremIpsum86';
		// CREATE DATABASE gorm;
		// USE gorm;
		// CREATE USER gorm FROM LOGIN gorm;
		// sp_changedbowner 'gorm';
		fmt.Println("testing mssql...")
		if dbDSN == "" {
			dbDSN = "sqlserver://gorm:LoremIpsum86@localhost:9930?database=gorm"
		}
		db, err = gorm.Open("mssql", dbDSN)
	default:
		fmt.Println("testing sqlite3...")
		db, err = gorm.Open("sqlite3", filepath.Join(os.TempDir(), "gorm.db"))
	}

	// db.SetLogger(Logger{log.New(os.Stdout, "\r\n", 0)})
	// db.SetLogger(log.New(os.Stdout, "\r\n", 0))
	if debug := os.Getenv("DEBUG"); debug == "true" {
		db.LogMode(true)
	} else if debug == "false" {
		db.LogMode(false)
	}

	db.DB().SetMaxIdleConns(10)

	return
}

func TestOpen_ReturnsError_WithBadArgs(t *testing.T) {
	stringRef := "foo"
	testCases := []interface{}{42, time.Now(), &stringRef}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%v", tc), func(t *testing.T) {
			_, err := gorm.Open("postgresql", tc)
			if err == nil {
				t.Error("Should got error with invalid database source")
			}
			if !strings.HasPrefix(err.Error(), "invalid database source:") {
				t.Errorf("Should got error starting with \"invalid database source:\", but got %q", err.Error())
			}
		})
	}
}

func TestStringPrimaryKey(t *testing.T) {
	type UUIDStruct struct {
		ID   string `gorm:"primary_key"`
		Name string
	}
	DB.DropTable(&UUIDStruct{})
	DB.AutoMigrate(&UUIDStruct{})

	data := UUIDStruct{ID: "uuid", Name: "hello"}
	if err := DB.Save(&data).Error; err != nil || data.ID != "uuid" || data.Name != "hello" {
		t.Errorf("string primary key should not be populated")
	}

	data = UUIDStruct{ID: "uuid", Name: "hello world"}
	if err := DB.Save(&data).Error; err != nil || data.ID != "uuid" || data.Name != "hello world" {
		t.Errorf("string primary key should not be populated")
	}
}

func TestExceptionsWithInvalidSql(t *testing.T) {
	var columns []string
	if DB.Where("sdsd.zaaa = ?", "sd;;;aa").Pluck("aaa", &columns).Error == nil {
		t.Errorf("Should got error with invalid SQL")
	}

	if DB.Model(&User{}).Where("sdsd.zaaa = ?", "sd;;;aa").Pluck("aaa", &columns).Error == nil {
		t.Errorf("Should got error with invalid SQL")
	}

	if DB.Where("sdsd.zaaa = ?", "sd;;;aa").Find(&User{}).Error == nil {
		t.Errorf("Should got error with invalid SQL")
	}

	var count1, count2 int64
	DB.Model(&User{}).Count(&count1)
	if count1 <= 0 {
		t.Errorf("Should find some users")
	}

	if DB.Where("name = ?", "jinzhu; delete * from users").First(&User{}).Error == nil {
		t.Errorf("Should got error with invalid SQL")
	}

	DB.Model(&User{}).Count(&count2)
	if count1 != count2 {
		t.Errorf("No user should not be deleted by invalid SQL")
	}
}

func TestSetTable(t *testing.T) {
	DB.Create(getPreparedUser("pluck_user1", "pluck_user"))
	DB.Create(getPreparedUser("pluck_user2", "pluck_user"))
	DB.Create(getPreparedUser("pluck_user3", "pluck_user"))

	if err := DB.Table("users").Where("role = ?", "pluck_user").Pluck("age", &[]int{}).Error; err != nil {
		t.Error("No errors should happen if set table for pluck", err)
	}

	var users []User
	if DB.Table("users").Find(&[]User{}).Error != nil {
		t.Errorf("No errors should happen if set table for find")
	}

	if DB.Table("invalid_table").Find(&users).Error == nil {
		t.Errorf("Should got error when table is set to an invalid table")
	}

	DB.Exec("drop table deleted_users;")
	if DB.Table("deleted_users").CreateTable(&User{}).Error != nil {
		t.Errorf("Create table with specified table")
	}

	DB.Table("deleted_users").Save(&User{Name: "DeletedUser"})

	var deletedUsers []User
	DB.Table("deleted_users").Find(&deletedUsers)
	if len(deletedUsers) != 1 {
		t.Errorf("Query from specified table")
	}

	var user User
	DB.Table("deleted_users").First(&user, "name = ?", "DeletedUser")

	user.Age = 20
	DB.Table("deleted_users").Save(&user)
	if DB.Table("deleted_users").First(&user, "name = ? AND age = ?", "DeletedUser", 20).RecordNotFound() {
		t.Errorf("Failed to found updated user")
	}

	DB.Save(getPreparedUser("normal_user", "reset_table"))
	DB.Table("deleted_users").Save(getPreparedUser("deleted_user", "reset_table"))
	var user1, user2, user3 User
	DB.Where("role = ?", "reset_table").First(&user1).Table("deleted_users").First(&user2).Table("").First(&user3)
	if (user1.Name != "normal_user") || (user2.Name != "deleted_user") || (user3.Name != "normal_user") {
		t.Errorf("unset specified table with blank string")
	}
}

type Order struct {
}

type Cart struct {
}

func (c Cart) TableName() string {
	return "shopping_cart"
}

func TestHasTable(t *testing.T) {
	type Foo struct {
		Id    int
		Stuff string
	}
	DB.DropTable(&Foo{})

	// Table should not exist at this point, HasTable should return false
	if ok := DB.HasTable("foos"); ok {
		t.Errorf("Table should not exist, but does")
	}
	if ok := DB.HasTable(&Foo{}); ok {
		t.Errorf("Table should not exist, but does")
	}

	// We create the table
	if err := DB.CreateTable(&Foo{}).Error; err != nil {
		t.Errorf("Table should be created")
	}

	// And now it should exits, and HasTable should return true
	if ok := DB.HasTable("foos"); !ok {
		t.Errorf("Table should exist, but HasTable informs it does not")
	}
	if ok := DB.HasTable(&Foo{}); !ok {
		t.Errorf("Table should exist, but HasTable informs it does not")
	}
}

func TestTableName(t *testing.T) {
	DB := DB.Model("")
	if DB.NewScope(Order{}).TableName() != "orders" {
		t.Errorf("Order's table name should be orders")
	}

	if DB.NewScope(&Order{}).TableName() != "orders" {
		t.Errorf("&Order's table name should be orders")
	}

	if DB.NewScope([]Order{}).TableName() != "orders" {
		t.Errorf("[]Order's table name should be orders")
	}

	if DB.NewScope(&[]Order{}).TableName() != "orders" {
		t.Errorf("&[]Order's table name should be orders")
	}

	DB.SingularTable(true)
	if DB.NewScope(Order{}).TableName() != "order" {
		t.Errorf("Order's singular table name should be order")
	}

	if DB.NewScope(&Order{}).TableName() != "order" {
		t.Errorf("&Order's singular table name should be order")
	}

	if DB.NewScope([]Order{}).TableName() != "order" {
		t.Errorf("[]Order's singular table name should be order")
	}

	if DB.NewScope(&[]Order{}).TableName() != "order" {
		t.Errorf("&[]Order's singular table name should be order")
	}

	if DB.NewScope(&Cart{}).TableName() != "shopping_cart" {
		t.Errorf("&Cart's singular table name should be shopping_cart")
	}

	if DB.NewScope(Cart{}).TableName() != "shopping_cart" {
		t.Errorf("Cart's singular table name should be shopping_cart")
	}

	if DB.NewScope(&[]Cart{}).TableName() != "shopping_cart" {
		t.Errorf("&[]Cart's singular table name should be shopping_cart")
	}

	if DB.NewScope([]Cart{}).TableName() != "shopping_cart" {
		t.Errorf("[]Cart's singular table name should be shopping_cart")
	}
	DB.SingularTable(false)
}

func TestTableNameConcurrently(t *testing.T) {
	DB := DB.Model("")
	if DB.NewScope(Order{}).TableName() != "orders" {
		t.Errorf("Order's table name should be orders")
	}

	var wg sync.WaitGroup
	wg.Add(10)

	for i := 1; i <= 10; i++ {
		go func(db *gorm.DB) {
			DB.SingularTable(true)
			wg.Done()
		}(DB)
	}
	wg.Wait()

	if DB.NewScope(Order{}).TableName() != "order" {
		t.Errorf("Order's singular table name should be order")
	}

	DB.SingularTable(false)
}

func TestNullValues(t *testing.T) {
	DB.DropTable(&NullValue{})
	DB.AutoMigrate(&NullValue{})

	if err := DB.Save(&NullValue{
		Name:    sql.NullString{String: "hello", Valid: true},
		Gender:  &sql.NullString{String: "M", Valid: true},
		Age:     sql.NullInt64{Int64: 18, Valid: true},
		Male:    sql.NullBool{Bool: true, Valid: true},
		Height:  sql.NullFloat64{Float64: 100.11, Valid: true},
		AddedAt: NullTime{Time: time.Now(), Valid: true},
	}).Error; err != nil {
		t.Errorf("Not error should raise when test null value")
	}

	var nv NullValue
	DB.First(&nv, "name = ?", "hello")

	if nv.Name.String != "hello" || nv.Gender.String != "M" || nv.Age.Int64 != 18 || nv.Male.Bool != true || nv.Height.Float64 != 100.11 || nv.AddedAt.Valid != true {
		t.Errorf("Should be able to fetch null value")
	}

	if err := DB.Save(&NullValue{
		Name:    sql.NullString{String: "hello-2", Valid: true},
		Gender:  &sql.NullString{String: "F", Valid: true},
		Age:     sql.NullInt64{Int64: 18, Valid: false},
		Male:    sql.NullBool{Bool: true, Valid: true},
		Height:  sql.NullFloat64{Float64: 100.11, Valid: true},
		AddedAt: NullTime{Time: time.Now(), Valid: false},
	}).Error; err != nil {
		t.Errorf("Not error should raise when test null value")
	}

	var nv2 NullValue
	DB.First(&nv2, "name = ?", "hello-2")
	if nv2.Name.String != "hello-2" || nv2.Gender.String != "F" || nv2.Age.Int64 != 0 || nv2.Male.Bool != true || nv2.Height.Float64 != 100.11 || nv2.AddedAt.Valid != false {
		t.Errorf("Should be able to fetch null value")
	}

	if err := DB.Save(&NullValue{
		Name:    sql.NullString{String: "hello-3", Valid: false},
		Gender:  &sql.NullString{String: "M", Valid: true},
		Age:     sql.NullInt64{Int64: 18, Valid: false},
		Male:    sql.NullBool{Bool: true, Valid: true},
		Height:  sql.NullFloat64{Float64: 100.11, Valid: true},
		AddedAt: NullTime{Time: time.Now(), Valid: false},
	}).Error; err == nil {
		t.Errorf("Can't save because of name can't be null")
	}
}

func TestNullValuesWithFirstOrCreate(t *testing.T) {
	var nv1 = NullValue{
		Name:   sql.NullString{String: "first_or_create", Valid: true},
		Gender: &sql.NullString{String: "M", Valid: true},
	}

	var nv2 NullValue
	result := DB.Where(nv1).FirstOrCreate(&nv2)

	if result.RowsAffected != 1 {
		t.Errorf("RowsAffected should be 1 after create some record")
	}

	if result.Error != nil {
		t.Errorf("Should not raise any error, but got %v", result.Error)
	}

	if nv2.Name.String != "first_or_create" || nv2.Gender.String != "M" {
		t.Errorf("first or create with nullvalues")
	}

	if err := DB.Where(nv1).Assign(NullValue{Age: sql.NullInt64{Int64: 18, Valid: true}}).FirstOrCreate(&nv2).Error; err != nil {
		t.Errorf("Should not raise any error, but got %v", err)
	}

	if nv2.Age.Int64 != 18 {
		t.Errorf("should update age to 18")
	}
}

func TestTransaction(t *testing.T) {
	tx := DB.Begin()
	u := User{Name: "transcation"}
	if err := tx.Save(&u).Error; err != nil {
		t.Errorf("No error should raise")
	}

	if err := tx.First(&User{}, "name = ?", "transcation").Error; err != nil {
		t.Errorf("Should find saved record")
	}

	if sqlTx, ok := tx.CommonDB().(*sql.Tx); !ok || sqlTx == nil {
		t.Errorf("Should return the underlying sql.Tx")
	}

	tx.Rollback()

	if err := tx.First(&User{}, "name = ?", "transcation").Error; err == nil {
		t.Errorf("Should not find record after rollback")
	}

	tx2 := DB.Begin()
	u2 := User{Name: "transcation-2"}
	if err := tx2.Save(&u2).Error; err != nil {
		t.Errorf("No error should raise")
	}

	if err := tx2.First(&User{}, "name = ?", "transcation-2").Error; err != nil {
		t.Errorf("Should find saved record")
	}

	tx2.Commit()

	if err := DB.First(&User{}, "name = ?", "transcation-2").Error; err != nil {
		t.Errorf("Should be able to find committed record")
	}

	tx3 := DB.Begin()
	u3 := User{Name: "transcation-3"}
	if err := tx3.Save(&u3).Error; err != nil {
		t.Errorf("No error should raise")
	}

	if err := tx3.First(&User{}, "name = ?", "transcation-3").Error; err != nil {
		t.Errorf("Should find saved record")
	}

	tx3.RollbackUnlessCommitted()

	if err := tx.First(&User{}, "name = ?", "transcation").Error; err == nil {
		t.Errorf("Should not find record after rollback")
	}

	tx4 := DB.Begin()
	u4 := User{Name: "transcation-4"}
	if err := tx4.Save(&u4).Error; err != nil {
		t.Errorf("No error should raise")
	}

	if err := tx4.First(&User{}, "name = ?", "transcation-4").Error; err != nil {
		t.Errorf("Should find saved record")
	}

	tx4.Commit()

	tx4.RollbackUnlessCommitted()

	if err := DB.First(&User{}, "name = ?", "transcation-4").Error; err != nil {
		t.Errorf("Should be able to find committed record")
	}
}

func assertPanic(t *testing.T, f func()) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("The code did not panic")
		}
	}()
	f()
}

func TestTransactionWithBlock(t *testing.T) {
	// rollback
	err := DB.Transaction(func(tx *gorm.DB) error {
		u := User{Name: "transcation"}
		if err := tx.Save(&u).Error; err != nil {
			t.Errorf("No error should raise")
		}

		if err := tx.First(&User{}, "name = ?", "transcation").Error; err != nil {
			t.Errorf("Should find saved record")
		}

		return errors.New("the error message")
	})

	if err.Error() != "the error message" {
		t.Errorf("Transaction return error will equal the block returns error")
	}

	if err := DB.First(&User{}, "name = ?", "transcation").Error; err == nil {
		t.Errorf("Should not find record after rollback")
	}

	// commit
	DB.Transaction(func(tx *gorm.DB) error {
		u2 := User{Name: "transcation-2"}
		if err := tx.Save(&u2).Error; err != nil {
			t.Errorf("No error should raise")
		}

		if err := tx.First(&User{}, "name = ?", "transcation-2").Error; err != nil {
			t.Errorf("Should find saved record")
		}
		return nil
	})

	if err := DB.First(&User{}, "name = ?", "transcation-2").Error; err != nil {
		t.Errorf("Should be able to find committed record")
	}

	// panic will rollback
	assertPanic(t, func() {
		DB.Transaction(func(tx *gorm.DB) error {
			u3 := User{Name: "transcation-3"}
			if err := tx.Save(&u3).Error; err != nil {
				t.Errorf("No error should raise")
			}

			if err := tx.First(&User{}, "name = ?", "transcation-3").Error; err != nil {
				t.Errorf("Should find saved record")
			}

			panic("force panic")
		})
	})

	if err := DB.First(&User{}, "name = ?", "transcation-3").Error; err == nil {
		t.Errorf("Should not find record after panic rollback")
	}
}

func TestTransaction_NoErrorOnRollbackAfterCommit(t *testing.T) {
	tx := DB.Begin()
	u := User{Name: "transcation"}
	if err := tx.Save(&u).Error; err != nil {
		t.Errorf("No error should raise")
	}

	if err := tx.Commit().Error; err != nil {
		t.Errorf("Commit should not raise error")
	}

	if err := tx.Rollback().Error; err != nil {
		t.Errorf("Rollback should not raise error")
	}
}

func TestTransactionReadonly(t *testing.T) {
	dialect := os.Getenv("GORM_DIALECT")
	if dialect == "" {
		dialect = "sqlite"
	}
	switch dialect {
	case "mssql", "sqlite":
		t.Skipf("%s does not support readonly transactions\n", dialect)
	}

	tx := DB.Begin()
	u := User{Name: "transcation"}
	if err := tx.Save(&u).Error; err != nil {
		t.Errorf("No error should raise")
	}
	tx.Commit()

	tx = DB.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err := tx.First(&User{}, "name = ?", "transcation").Error; err != nil {
		t.Errorf("Should find saved record")
	}

	if sqlTx, ok := tx.CommonDB().(*sql.Tx); !ok || sqlTx == nil {
		t.Errorf("Should return the underlying sql.Tx")
	}

	u = User{Name: "transcation-2"}
	if err := tx.Save(&u).Error; err == nil {
		t.Errorf("Error should have been raised in a readonly transaction")
	}

	tx.Rollback()
}

func TestRow(t *testing.T) {
	user1 := User{Name: "RowUser1", Age: 1, Birthday: parseTime("2000-1-1")}
	user2 := User{Name: "RowUser2", Age: 10, Birthday: parseTime("2010-1-1")}
	user3 := User{Name: "RowUser3", Age: 20, Birthday: parseTime("2020-1-1")}
	DB.Save(&user1).Save(&user2).Save(&user3)

	row := DB.Table("users").Where("name = ?", user2.Name).Select("age").Row()
	var age int64
	row.Scan(&age)
	if age != 10 {
		t.Errorf("Scan with Row")
	}
}

func TestRows(t *testing.T) {
	user1 := User{Name: "RowsUser1", Age: 1, Birthday: parseTime("2000-1-1")}
	user2 := User{Name: "RowsUser2", Age: 10, Birthday: parseTime("2010-1-1")}
	user3 := User{Name: "RowsUser3", Age: 20, Birthday: parseTime("2020-1-1")}
	DB.Save(&user1).Save(&user2).Save(&user3)

	rows, err := DB.Table("users").Where("name = ? or name = ?", user2.Name, user3.Name).Select("name, age").Rows()
	if err != nil {
		t.Errorf("Not error should happen, got %v", err)
	}

	count := 0
	for rows.Next() {
		var name string
		var age int64
		rows.Scan(&name, &age)
		count++
	}

	if count != 2 {
		t.Errorf("Should found two records")
	}
}

func TestScanRows(t *testing.T) {
	user1 := User{Name: "ScanRowsUser1", Age: 1, Birthday: parseTime("2000-1-1")}
	user2 := User{Name: "ScanRowsUser2", Age: 10, Birthday: parseTime("2010-1-1")}
	user3 := User{Name: "ScanRowsUser3", Age: 20, Birthday: parseTime("2020-1-1")}
	DB.Save(&user1).Save(&user2).Save(&user3)

	rows, err := DB.Table("users").Where("name = ? or name = ?", user2.Name, user3.Name).Select("name, age").Rows()
	if err != nil {
		t.Errorf("Not error should happen, got %v", err)
	}

	type Result struct {
		Name string
		Age  int
	}

	var results []Result
	for rows.Next() {
		var result Result
		if err := DB.ScanRows(rows, &result); err != nil {
			t.Errorf("should get no error, but got %v", err)
		}
		results = append(results, result)
	}

	if !reflect.DeepEqual(results, []Result{{Name: "ScanRowsUser2", Age: 10}, {Name: "ScanRowsUser3", Age: 20}}) {
		t.Errorf("Should find expected results")
	}
}

func TestScan(t *testing.T) {
	user1 := User{Name: "ScanUser1", Age: 1, Birthday: parseTime("2000-1-1")}
	user2 := User{Name: "ScanUser2", Age: 10, Birthday: parseTime("2010-1-1")}
	user3 := User{Name: "ScanUser3", Age: 20, Birthday: parseTime("2020-1-1")}
	DB.Save(&user1).Save(&user2).Save(&user3)

	type result struct {
		Name string
		Age  int
	}

	var res result
	DB.Table("users").Select("name, age").Where("name = ?", user3.Name).Scan(&res)
	if res.Name != user3.Name {
		t.Errorf("Scan into struct should work")
	}

	var doubleAgeRes = &result{}
	if err := DB.Table("users").Select("age + age as age").Where("name = ?", user3.Name).Scan(&doubleAgeRes).Error; err != nil {
		t.Errorf("Scan to pointer of pointer")
	}
	if doubleAgeRes.Age != res.Age*2 {
		t.Errorf("Scan double age as age")
	}

	var ress []result
	DB.Table("users").Select("name, age").Where("name in (?)", []string{user2.Name, user3.Name}).Scan(&ress)
	if len(ress) != 2 || ress[0].Name != user2.Name || ress[1].Name != user3.Name {
		t.Errorf("Scan into struct map")
	}
}

func TestRaw(t *testing.T) {
	user1 := User{Name: "ExecRawSqlUser1", Age: 1, Birthday: parseTime("2000-1-1")}
	user2 := User{Name: "ExecRawSqlUser2", Age: 10, Birthday: parseTime("2010-1-1")}
	user3 := User{Name: "ExecRawSqlUser3", Age: 20, Birthday: parseTime("2020-1-1")}
	DB.Save(&user1).Save(&user2).Save(&user3)

	type result struct {
		Name  string
		Email string
	}

	var ress []result
	DB.Raw("SELECT name, age FROM users WHERE name = ? or name = ?", user2.Name, user3.Name).Scan(&ress)
	if len(ress) != 2 || ress[0].Name != user2.Name || ress[1].Name != user3.Name {
		t.Errorf("Raw with scan")
	}

	rows, _ := DB.Raw("select name, age from users where name = ?", user3.Name).Rows()
	count := 0
	for rows.Next() {
		count++
	}
	if count != 1 {
		t.Errorf("Raw with Rows should find one record with name 3")
	}

	DB.Exec("update users set name=? where name in (?)", "jinzhu", []string{user1.Name, user2.Name, user3.Name})
	if DB.Where("name in (?)", []string{user1.Name, user2.Name, user3.Name}).First(&User{}).Error != gorm.ErrRecordNotFound {
		t.Error("Raw sql to update records")
	}
}

func TestGroup(t *testing.T) {
	rows, err := DB.Select("name").Table("users").Group("name").Rows()

	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var name string
			rows.Scan(&name)
		}
	} else {
		t.Errorf("Should not raise any error")
	}
}

func TestJoins(t *testing.T) {
	var user = User{
		Name:       "joins",
		CreditCard: CreditCard{Number: "411111111111"},
		Emails:     []Email{{Email: "join1@example.com"}, {Email: "join2@example.com"}},
	}
	DB.Save(&user)

	var users1 []User
	DB.Joins("left join emails on emails.user_id = users.id").Where("name = ?", "joins").Find(&users1)
	if len(users1) != 2 {
		t.Errorf("should find two users using left join")
	}

	var users2 []User
	DB.Joins("left join emails on emails.user_id = users.id AND emails.email = ?", "join1@example.com").Where("name = ?", "joins").First(&users2)
	if len(users2) != 1 {
		t.Errorf("should find one users using left join with conditions")
	}

	var users3 []User
	DB.Joins("join emails on emails.user_id = users.id AND emails.email = ?", "join1@example.com").Joins("join credit_cards on credit_cards.user_id = users.id AND credit_cards.number = ?", "411111111111").Where("name = ?", "joins").First(&users3)
	if len(users3) != 1 {
		t.Errorf("should find one users using multiple left join conditions")
	}

	var users4 []User
	DB.Joins("join emails on emails.user_id = users.id AND emails.email = ?", "join1@example.com").Joins("join credit_cards on credit_cards.user_id = users.id AND credit_cards.number = ?", "422222222222").Where("name = ?", "joins").First(&users4)
	if len(users4) != 0 {
		t.Errorf("should find no user when searching with unexisting credit card")
	}

	var users5 []User
	db5 := DB.Joins("join emails on emails.user_id = users.id AND emails.email = ?", "join1@example.com").Joins("join credit_cards on credit_cards.user_id = users.id AND credit_cards.number = ?", "411111111111").Where(User{Id: 1}).Where(Email{Id: 1}).Not(Email{Id: 10}).First(&users5)
	if db5.Error != nil {
		t.Errorf("Should not raise error for join where identical fields in different tables. Error: %s", db5.Error.Error())
	}
}

type JoinedIds struct {
	UserID           int64 `gorm:"column:id"`
	BillingAddressID int64 `gorm:"column:id"`
	EmailID          int64 `gorm:"column:id"`
}

func TestScanIdenticalColumnNames(t *testing.T) {
	var user = User{
		Name:  "joinsIds",
		Email: "joinIds@example.com",
		BillingAddress: Address{
			Address1: "One Park Place",
		},
		Emails: []Email{{Email: "join1@example.com"}, {Email: "join2@example.com"}},
	}
	DB.Save(&user)

	var users []JoinedIds
	DB.Select("users.id, addresses.id, emails.id").Table("users").
		Joins("left join addresses on users.billing_address_id = addresses.id").
		Joins("left join emails on emails.user_id = users.id").
		Where("name = ?", "joinsIds").Scan(&users)

	if len(users) != 2 {
		t.Fatal("should find two rows using left join")
	}

	if user.Id != users[0].UserID {
		t.Errorf("Expected result row to contain UserID %d, but got %d", user.Id, users[0].UserID)
	}
	if user.Id != users[1].UserID {
		t.Errorf("Expected result row to contain UserID %d, but got %d", user.Id, users[1].UserID)
	}

	if user.BillingAddressID.Int64 != users[0].BillingAddressID {
		t.Errorf("Expected result row to contain BillingAddressID %d, but got %d", user.BillingAddressID.Int64, users[0].BillingAddressID)
	}
	if user.BillingAddressID.Int64 != users[1].BillingAddressID {
		t.Errorf("Expected result row to contain BillingAddressID %d, but got %d", user.BillingAddressID.Int64, users[0].BillingAddressID)
	}

	if users[0].EmailID == users[1].EmailID {
		t.Errorf("Email ids should be unique. Got %d and %d", users[0].EmailID, users[1].EmailID)
	}

	if int64(user.Emails[0].Id) != users[0].EmailID && int64(user.Emails[1].Id) != users[0].EmailID {
		t.Errorf("Expected result row ID to be either %d or %d, but was %d", user.Emails[0].Id, user.Emails[1].Id, users[0].EmailID)
	}

	if int64(user.Emails[0].Id) != users[1].EmailID && int64(user.Emails[1].Id) != users[1].EmailID {
		t.Errorf("Expected result row ID to be either %d or %d, but was %d", user.Emails[0].Id, user.Emails[1].Id, users[1].EmailID)
	}
}

func TestJoinsWithSelect(t *testing.T) {
	type result struct {
		Name  string
		Email string
	}

	user := User{
		Name:   "joins_with_select",
		Emails: []Email{{Email: "join1@example.com"}, {Email: "join2@example.com"}},
	}
	DB.Save(&user)

	var results []result
	DB.Table("users").Select("name, emails.email").Joins("left join emails on emails.user_id = users.id").Where("name = ?", "joins_with_select").Scan(&results)

	sort.Slice(results, func(i, j int) bool {
		return strings.Compare(results[i].Email, results[j].Email) < 0
	})

	if len(results) != 2 || results[0].Email != "join1@example.com" || results[1].Email != "join2@example.com" {
		t.Errorf("Should find all two emails with Join select")
	}
}

func TestHaving(t *testing.T) {
	rows, err := DB.Select("name, count(*) as total").Table("users").Group("name").Having("name IN (?)", []string{"2", "3"}).Rows()

	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var name string
			var total int64
			rows.Scan(&name, &total)

			if name == "2" && total != 1 {
				t.Errorf("Should have one user having name 2")
			}
			if name == "3" && total != 2 {
				t.Errorf("Should have two users having name 3")
			}
		}
	} else {
		t.Errorf("Should not raise any error")
	}
}

func TestQueryBuilderSubselectInWhere(t *testing.T) {
	user := User{Name: "query_expr_select_ruser1", Email: "root@user1.com", Age: 32}
	DB.Save(&user)
	user = User{Name: "query_expr_select_ruser2", Email: "nobody@user2.com", Age: 16}
	DB.Save(&user)
	user = User{Name: "query_expr_select_ruser3", Email: "root@user3.com", Age: 64}
	DB.Save(&user)
	user = User{Name: "query_expr_select_ruser4", Email: "somebody@user3.com", Age: 128}
	DB.Save(&user)

	var users []User
	DB.Select("*").Where("name IN (?)", DB.
		Select("name").Table("users").Where("name LIKE ?", "query_expr_select%").QueryExpr()).Find(&users)

	if len(users) != 4 {
		t.Errorf("Four users should be found, instead found %d", len(users))
	}

	DB.Select("*").Where("name LIKE ?", "query_expr_select%").Where("age >= (?)", DB.
		Select("AVG(age)").Table("users").Where("name LIKE ?", "query_expr_select%").QueryExpr()).Find(&users)

	if len(users) != 2 {
		t.Errorf("Two users should be found, instead found %d", len(users))
	}
}

func TestQueryBuilderRawQueryWithSubquery(t *testing.T) {
	user := User{Name: "subquery_test_user1", Age: 10}
	DB.Save(&user)
	user = User{Name: "subquery_test_user2", Age: 11}
	DB.Save(&user)
	user = User{Name: "subquery_test_user3", Age: 12}
	DB.Save(&user)

	var count int
	err := DB.Raw("select count(*) from (?) tmp",
		DB.Table("users").
			Select("name").
			Where("age >= ? and name in (?)", 10, []string{"subquery_test_user1", "subquery_test_user2"}).
			Group("name").
			QueryExpr(),
	).Count(&count).Error

	if err != nil {
		t.Errorf("Expected to get no errors, but got %v", err)
	}
	if count != 2 {
		t.Errorf("Row count must be 2, instead got %d", count)
	}

	err = DB.Raw("select count(*) from (?) tmp",
		DB.Table("users").
			Select("name").
			Where("name LIKE ?", "subquery_test%").
			Not("age <= ?", 10).Not("name in (?)", []string{"subquery_test_user1", "subquery_test_user2"}).
			Group("name").
			QueryExpr(),
	).Count(&count).Error

	if err != nil {
		t.Errorf("Expected to get no errors, but got %v", err)
	}
	if count != 1 {
		t.Errorf("Row count must be 1, instead got %d", count)
	}
}

func TestQueryBuilderSubselectInHaving(t *testing.T) {
	user := User{Name: "query_expr_having_ruser1", Email: "root@user1.com", Age: 64}
	DB.Save(&user)
	user = User{Name: "query_expr_having_ruser2", Email: "root@user2.com", Age: 128}
	DB.Save(&user)
	user = User{Name: "query_expr_having_ruser3", Email: "root@user1.com", Age: 64}
	DB.Save(&user)
	user = User{Name: "query_expr_having_ruser4", Email: "root@user2.com", Age: 128}
	DB.Save(&user)

	var users []User
	DB.Select("AVG(age) as avgage").Where("name LIKE ?", "query_expr_having_%").Group("email").Having("AVG(age) > (?)", DB.
		Select("AVG(age)").Where("name LIKE ?", "query_expr_having_%").Table("users").QueryExpr()).Find(&users)

	if len(users) != 1 {
		t.Errorf("Two user group should be found, instead found %d", len(users))
	}
}

func DialectHasTzSupport() bool {
	// NB: mssql and FoundationDB do not support time zones.
	if dialect := os.Getenv("GORM_DIALECT"); dialect == "foundation" {
		return false
	}
	return true
}

func TestTimeWithZone(t *testing.T) {
	var format = "2006-01-02 15:04:05 -0700"
	var times []time.Time
	GMT8, _ := time.LoadLocation("Asia/Shanghai")
	times = append(times, time.Date(2013, 02, 19, 1, 51, 49, 123456789, GMT8))
	times = append(times, time.Date(2013, 02, 18, 17, 51, 49, 123456789, time.UTC))

	for index, vtime := range times {
		name := "time_with_zone_" + strconv.Itoa(index)
		user := User{Name: name, Birthday: &vtime}

		if !DialectHasTzSupport() {
			// If our driver dialect doesn't support TZ's, just use UTC for everything here.
			utcBirthday := user.Birthday.UTC()
			user.Birthday = &utcBirthday
		}

		DB.Save(&user)
		expectedBirthday := "2013-02-18 17:51:49 +0000"
		foundBirthday := user.Birthday.UTC().Format(format)
		if foundBirthday != expectedBirthday {
			t.Errorf("User's birthday should not be changed after save for name=%s, expected bday=%+v but actual value=%+v", name, expectedBirthday, foundBirthday)
		}

		var findUser, findUser2, findUser3 User
		DB.First(&findUser, "name = ?", name)
		foundBirthday = findUser.Birthday.UTC().Format(format)
		if foundBirthday != expectedBirthday {
			t.Errorf("User's birthday should not be changed after find for name=%s, expected bday=%+v but actual value=%+v", name, expectedBirthday, foundBirthday)
		}

		if DB.Where("id = ? AND birthday >= ?", findUser.Id, user.Birthday.Add(-time.Minute)).First(&findUser2).RecordNotFound() {
			t.Errorf("User should be found")
		}

		if !DB.Where("id = ? AND birthday >= ?", findUser.Id, user.Birthday.Add(time.Minute)).First(&findUser3).RecordNotFound() {
			t.Errorf("User should not be found")
		}
	}
}

func TestHstore(t *testing.T) {
	type Details struct {
		Id   int64
		Bulk postgres.Hstore
	}

	if dialect := os.Getenv("GORM_DIALECT"); dialect != "postgres" {
		t.Skip()
	}

	if err := DB.Exec("CREATE EXTENSION IF NOT EXISTS hstore").Error; err != nil {
		fmt.Println("\033[31mHINT: Must be superuser to create hstore extension (ALTER USER gorm WITH SUPERUSER;)\033[0m")
		panic(fmt.Sprintf("No error should happen when create hstore extension, but got %+v", err))
	}

	DB.Exec("drop table details")

	if err := DB.CreateTable(&Details{}).Error; err != nil {
		panic(fmt.Sprintf("No error should happen when create table, but got %+v", err))
	}

	bankAccountId, phoneNumber, opinion := "123456", "14151321232", "sharkbait"
	bulk := map[string]*string{
		"bankAccountId": &bankAccountId,
		"phoneNumber":   &phoneNumber,
		"opinion":       &opinion,
	}
	d := Details{Bulk: bulk}
	DB.Save(&d)

	var d2 Details
	if err := DB.First(&d2).Error; err != nil {
		t.Errorf("Got error when tried to fetch details: %+v", err)
	}

	for k := range bulk {
		if r, ok := d2.Bulk[k]; ok {
			if res, _ := bulk[k]; *res != *r {
				t.Errorf("Details should be equal")
			}
		} else {
			t.Errorf("Details should be existed")
		}
	}
}

func TestSetAndGet(t *testing.T) {
	if value, ok := DB.Set("hello", "world").Get("hello"); !ok {
		t.Errorf("Should be able to get setting after set")
	} else {
		if value.(string) != "world" {
			t.Errorf("Setted value should not be changed")
		}
	}

	if _, ok := DB.Get("non_existing"); ok {
		t.Errorf("Get non existing key should return error")
	}
}

func TestCompatibilityMode(t *testing.T) {
	DB, _ := gorm.Open("testdb", "")
	testdb.SetQueryFunc(func(query string) (driver.Rows, error) {
		columns := []string{"id", "name", "age"}
		result := `
		1,Tim,20
		2,Joe,25
		3,Bob,30
		`
		return testdb.RowsFromCSVString(columns, result), nil
	})

	var users []User
	DB.Find(&users)
	if (users[0].Name != "Tim") || len(users) != 3 {
		t.Errorf("Unexcepted result returned")
	}
}

func TestOpenExistingDB(t *testing.T) {
	DB.Save(&User{Name: "jnfeinstein"})
	dialect := os.Getenv("GORM_DIALECT")

	db, err := gorm.Open(dialect, DB.DB())
	if err != nil {
		t.Errorf("Should have wrapped the existing DB connection")
	}

	var user User
	if db.Where("name = ?", "jnfeinstein").First(&user).Error == gorm.ErrRecordNotFound {
		t.Errorf("Should have found existing record")
	}
}

func TestDdlErrors(t *testing.T) {
	var err error

	if err = DB.Close(); err != nil {
		t.Errorf("Closing DDL test db connection err=%s", err)
	}
	defer func() {
		// Reopen DB connection.
		if DB, err = OpenTestConnection(); err != nil {
			t.Fatalf("Failed re-opening db connection: %s", err)
		}
	}()

	if err := DB.Find(&User{}).Error; err == nil {
		t.Errorf("Expected operation on closed db to produce an error, but err was nil")
	}
}

func TestOpenWithOneParameter(t *testing.T) {
	db, err := gorm.Open("dialect")
	if db != nil {
		t.Error("Open with one parameter returned non nil for db")
	}
	if err == nil {
		t.Error("Open with one parameter returned err as nil")
	}
}

func TestSaveAssociations(t *testing.T) {
	db := DB.New()
	deltaAddressCount := 0
	if err := db.Model(&Address{}).Count(&deltaAddressCount).Error; err != nil {
		t.Errorf("failed to fetch address count")
		t.FailNow()
	}

	placeAddress := &Address{
		Address1: "somewhere on earth",
	}
	ownerAddress1 := &Address{
		Address1: "near place address",
	}
	ownerAddress2 := &Address{
		Address1: "address2",
	}
	db.Create(placeAddress)

	addressCountShouldBe := func(t *testing.T, expectedCount int) {
		countFromDB := 0
		t.Helper()
		err := db.Model(&Address{}).Count(&countFromDB).Error
		if err != nil {
			t.Error("failed to fetch address count")
		}
		if countFromDB != expectedCount {
			t.Errorf("address count mismatch: %d", countFromDB)
		}
	}
	addressCountShouldBe(t, deltaAddressCount+1)

	// owner address should be created, place address should be reused
	place1 := &Place{
		PlaceAddressID: placeAddress.ID,
		PlaceAddress:   placeAddress,
		OwnerAddress:   ownerAddress1,
	}
	err := db.Create(place1).Error
	if err != nil {
		t.Errorf("failed to store place: %s", err.Error())
	}
	addressCountShouldBe(t, deltaAddressCount+2)

	// owner address should be created again, place address should be reused
	place2 := &Place{
		PlaceAddressID: placeAddress.ID,
		PlaceAddress: &Address{
			ID:       777,
			Address1: "address1",
		},
		OwnerAddress:   ownerAddress2,
		OwnerAddressID: 778,
	}
	err = db.Create(place2).Error
	if err != nil {
		t.Errorf("failed to store place: %s", err.Error())
	}
	addressCountShouldBe(t, deltaAddressCount+3)

	count := 0
	db.Model(&Place{}).Where(&Place{
		PlaceAddressID: placeAddress.ID,
		OwnerAddressID: ownerAddress1.ID,
	}).Count(&count)
	if count != 1 {
		t.Errorf("only one instance of (%d, %d) should be available, found: %d",
			placeAddress.ID, ownerAddress1.ID, count)
	}

	db.Model(&Place{}).Where(&Place{
		PlaceAddressID: placeAddress.ID,
		OwnerAddressID: ownerAddress2.ID,
	}).Count(&count)
	if count != 1 {
		t.Errorf("only one instance of (%d, %d) should be available, found: %d",
			placeAddress.ID, ownerAddress2.ID, count)
	}

	db.Model(&Place{}).Where(&Place{
		PlaceAddressID: placeAddress.ID,
	}).Count(&count)
	if count != 2 {
		t.Errorf("two instances of (%d) should be available, found: %d",
			placeAddress.ID, count)
	}
}

func TestBlockGlobalUpdate(t *testing.T) {
	db := DB.New()
	db.Create(&Toy{Name: "Stuffed Animal", OwnerType: "Nobody"})

	err := db.Model(&Toy{}).Update("OwnerType", "Human").Error
	if err != nil {
		t.Error("Unexpected error on global update")
	}

	err = db.Delete(&Toy{}).Error
	if err != nil {
		t.Error("Unexpected error on global delete")
	}

	db.BlockGlobalUpdate(true)

	db.Create(&Toy{Name: "Stuffed Animal", OwnerType: "Nobody"})

	err = db.Model(&Toy{}).Update("OwnerType", "Human").Error
	if err == nil {
		t.Error("Expected error on global update")
	}

	err = db.Model(&Toy{}).Where(&Toy{OwnerType: "Martian"}).Update("OwnerType", "Astronaut").Error
	if err != nil {
		t.Error("Unxpected error on conditional update")
	}

	err = db.Delete(&Toy{}).Error
	if err == nil {
		t.Error("Expected error on global delete")
	}
	err = db.Where(&Toy{OwnerType: "Martian"}).Delete(&Toy{}).Error
	if err != nil {
		t.Error("Unexpected error on conditional delete")
	}
}

func TestCountWithHaving(t *testing.T) {
	db := DB.New()
	db.Delete(User{})
	defer db.Delete(User{})

	DB.Create(getPreparedUser("user1", "pluck_user"))
	DB.Create(getPreparedUser("user2", "pluck_user"))
	user3 := getPreparedUser("user3", "pluck_user")
	user3.Languages = []Language{}
	DB.Create(user3)

	var count int
	err := db.Model(User{}).Select("users.id").
		Joins("LEFT JOIN user_languages ON user_languages.user_id = users.id").
		Joins("LEFT JOIN languages ON user_languages.language_id = languages.id").
		Group("users.id").Having("COUNT(languages.id) > 1").Count(&count).Error

	if err != nil {
		t.Error("Unexpected error on query count with having")
	}

	if count != 2 {
		t.Error("Unexpected result on query count with having")
	}
}

func TestPluck(t *testing.T) {
	db := DB.New()
	db.Delete(User{})
	defer db.Delete(User{})

	DB.Create(&User{Id: 1, Name: "user1"})
	DB.Create(&User{Id: 2, Name: "user2"})
	DB.Create(&User{Id: 3, Name: "user3"})

	var ids []int64
	err := db.Model(User{}).Order("id").Pluck("id", &ids).Error

	if err != nil {
		t.Error("Unexpected error on pluck")
	}

	if len(ids) != 3 || ids[0] != 1 || ids[1] != 2 || ids[2] != 3 {
		t.Error("Unexpected result on pluck")
	}

	err = db.Model(User{}).Order("id").Pluck("id", &ids).Error

	if err != nil {
		t.Error("Unexpected error on pluck again")
	}

	if len(ids) != 3 || ids[0] != 1 || ids[1] != 2 || ids[2] != 3 {
		t.Error("Unexpected result on pluck again")
	}
}

func TestCountWithQueryOption(t *testing.T) {
	db := DB.New()
	db.Delete(User{})
	defer db.Delete(User{})

	DB.Create(&User{Name: "user1"})
	DB.Create(&User{Name: "user2"})
	DB.Create(&User{Name: "user3"})

	var count int
	err := db.Model(User{}).Select("users.id").
		Set("gorm:query_option", "WHERE users.name='user2'").
		Count(&count).Error

	if err != nil {
		t.Error("Unexpected error on query count with query_option")
	}

	if count != 1 {
		t.Error("Unexpected result on query count with query_option")
	}
}

func TestSubQueryWithQueryOption(t *testing.T) {
	db := DB.New()

	subQuery := db.Model(User{}).Select("users.id").
		Set("gorm:query_option", "WHERE users.name='user2'").
		SubQuery()

	matched, _ := regexp.MatchString(
		`^&{.+\s+WHERE users\.name='user2'.*\s\[]}$`, fmt.Sprint(subQuery))
	if !matched {
		t.Error("Unexpected result of SubQuery with query_option")
	}
}

func TestQueryExprWithQueryOption(t *testing.T) {
	db := DB.New()

	queryExpr := db.Model(User{}).Select("users.id").
		Set("gorm:query_option", "WHERE users.name='user2'").
		QueryExpr()

	matched, _ := regexp.MatchString(
		`^&{.+\s+WHERE users\.name='user2'.*\s\[]}$`, fmt.Sprint(queryExpr))
	if !matched {
		t.Error("Unexpected result of QueryExpr with query_option")
	}
}

func TestQueryHint1(t *testing.T) {
	db := DB.New()

	_, err := db.Model(User{}).Raw("select 1").Rows()

	if err != nil {
		t.Error("Unexpected error on query count with query_option")
	}
}

func TestQueryHint2(t *testing.T) {
	type TestStruct struct {
		ID   string `gorm:"primary_key"`
		Name string
	}
	DB.DropTable(&TestStruct{})
	DB.AutoMigrate(&TestStruct{})

	data := TestStruct{ID: "uuid", Name: "hello"}
	if err := DB.Set("gorm:query_hint", "/*master*/").Save(&data).Error; err != nil {
		t.Error("Unexpected error on query count with query_option")
	}
}

func TestFloatColumnPrecision(t *testing.T) {
	if dialect := os.Getenv("GORM_DIALECT"); dialect != "mysql" && dialect != "sqlite" {
		t.Skip()
	}

	type FloatTest struct {
		ID         string  `gorm:"primary_key"`
		FloatValue float64 `gorm:"column:float_value" sql:"type:float(255,5);"`
	}
	DB.DropTable(&FloatTest{})
	DB.AutoMigrate(&FloatTest{})

	data := FloatTest{ID: "uuid", FloatValue: 112.57315}
	if err := DB.Save(&data).Error; err != nil || data.ID != "uuid" || data.FloatValue != 112.57315 {
		t.Errorf("Float value should not lose precision")
	}
}

func TestWhereUpdates(t *testing.T) {
	type OwnerEntity struct {
		gorm.Model
		OwnerID   uint
		OwnerType string
	}

	type SomeEntity struct {
		gorm.Model
		Name        string
		OwnerEntity OwnerEntity `gorm:"polymorphic:Owner"`
	}

	DB.DropTable(&SomeEntity{})
	DB.AutoMigrate(&SomeEntity{})

	a := SomeEntity{Name: "test"}
	DB.Model(&a).Where(a).Updates(SomeEntity{Name: "test2"})
}

func BenchmarkGorm(b *testing.B) {
	b.N = 2000
	for x := 0; x < b.N; x++ {
		e := strconv.Itoa(x) + "benchmark@example.org"
		now := time.Now()
		email := EmailWithIdx{Email: e, UserAgent: "pc", RegisteredAt: &now}
		// Insert
		DB.Save(&email)
		// Query
		DB.First(&EmailWithIdx{}, "email = ?", e)
		// Update
		DB.Model(&email).UpdateColumn("email", "new-"+e)
		// Delete
		DB.Delete(&email)
	}
}

func BenchmarkRawSql(b *testing.B) {
	DB, _ := sql.Open("postgres", "user=gorm DB.ame=gorm sslmode=disable")
	DB.SetMaxIdleConns(10)
	insertSql := "INSERT INTO emails (user_id,email,user_agent,registered_at,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id"
	querySql := "SELECT * FROM emails WHERE email = $1 ORDER BY id LIMIT 1"
	updateSql := "UPDATE emails SET email = $1, updated_at = $2 WHERE id = $3"
	deleteSql := "DELETE FROM orders WHERE id = $1"

	b.N = 2000
	for x := 0; x < b.N; x++ {
		var id int64
		e := strconv.Itoa(x) + "benchmark@example.org"
		now := time.Now()
		email := EmailWithIdx{Email: e, UserAgent: "pc", RegisteredAt: &now}
		// Insert
		DB.QueryRow(insertSql, email.UserId, email.Email, email.UserAgent, email.RegisteredAt, time.Now(), time.Now()).Scan(&id)
		// Query
		rows, _ := DB.Query(querySql, email.Email)
		rows.Close()
		// Update
		DB.Exec(updateSql, "new-"+e, time.Now(), id)
		// Delete
		DB.Exec(deleteSql, id)
	}
}

func parseTime(str string) *time.Time {
	t := now.New(time.Now().UTC()).MustParse(str)
	return &t
}
