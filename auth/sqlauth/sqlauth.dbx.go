//lint:file-ignore * generated file
// AUTOGENERATED BY storj.io/dbx
// DO NOT EDIT.

package sqlauth

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/jackc/pgconn"
	_ "github.com/jackc/pgx/v4/stdlib"
	"github.com/mattn/go-sqlite3"
	"math/rand"
)

// Prevent conditional imports from causing build failures
var _ = strconv.Itoa
var _ = strings.LastIndex
var _ = fmt.Sprint
var _ sync.Mutex

var (
	WrapErr = func(err *Error) error { return err }
	Logger  func(format string, args ...interface{})

	errTooManyRows       = errors.New("too many rows")
	errUnsupportedDriver = errors.New("unsupported driver")
	errEmptyUpdate       = errors.New("empty update")
)

func logError(format string, args ...interface{}) {
	if Logger != nil {
		Logger(format, args...)
	}
}

type ErrorCode int

const (
	ErrorCode_Unknown ErrorCode = iota
	ErrorCode_UnsupportedDriver
	ErrorCode_NoRows
	ErrorCode_TxDone
	ErrorCode_TooManyRows
	ErrorCode_ConstraintViolation
	ErrorCode_EmptyUpdate
)

type Error struct {
	Err         error
	Code        ErrorCode
	Driver      string
	Constraint  string
	QuerySuffix string
}

func (e *Error) Error() string {
	return e.Err.Error()
}

func wrapErr(e *Error) error {
	if WrapErr == nil {
		return e
	}
	return WrapErr(e)
}

func makeErr(err error) error {
	if err == nil {
		return nil
	}
	e := &Error{Err: err}
	switch err {
	case sql.ErrNoRows:
		e.Code = ErrorCode_NoRows
	case sql.ErrTxDone:
		e.Code = ErrorCode_TxDone
	}
	return wrapErr(e)
}

func unsupportedDriver(driver string) error {
	return wrapErr(&Error{
		Err:    errUnsupportedDriver,
		Code:   ErrorCode_UnsupportedDriver,
		Driver: driver,
	})
}

func emptyUpdate() error {
	return wrapErr(&Error{
		Err:  errEmptyUpdate,
		Code: ErrorCode_EmptyUpdate,
	})
}

func tooManyRows(query_suffix string) error {
	return wrapErr(&Error{
		Err:         errTooManyRows,
		Code:        ErrorCode_TooManyRows,
		QuerySuffix: query_suffix,
	})
}

func constraintViolation(err error, constraint string) error {
	return wrapErr(&Error{
		Err:        err,
		Code:       ErrorCode_ConstraintViolation,
		Constraint: constraint,
	})
}

type driver interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

var (
	notAPointer     = errors.New("destination not a pointer")
	lossyConversion = errors.New("lossy conversion")
)

type DB struct {
	*sql.DB
	dbMethods

	Hooks struct {
		Now func() time.Time
	}
}

func Open(driver, source string) (db *DB, err error) {
	var sql_db *sql.DB
	switch driver {
	case "pgxcockroach":
		sql_db, err = openpgxcockroach(source)
	case "sqlite3":
		sql_db, err = opensqlite3(source)
	default:
		return nil, unsupportedDriver(driver)
	}
	if err != nil {
		return nil, makeErr(err)
	}
	defer func(sql_db *sql.DB) {
		if err != nil {
			sql_db.Close()
		}
	}(sql_db)

	if err := sql_db.Ping(); err != nil {
		return nil, makeErr(err)
	}

	db = &DB{
		DB: sql_db,
	}
	db.Hooks.Now = time.Now

	switch driver {
	case "pgxcockroach":
		db.dbMethods = newpgxcockroach(db)
	case "sqlite3":
		db.dbMethods = newsqlite3(db)
	default:
		return nil, unsupportedDriver(driver)
	}

	return db, nil
}

func (obj *DB) Close() (err error) {
	return obj.makeErr(obj.DB.Close())
}

func (obj *DB) Open(ctx context.Context) (*Tx, error) {
	tx, err := obj.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, obj.makeErr(err)
	}

	return &Tx{
		Tx:        tx,
		txMethods: obj.wrapTx(tx),
	}, nil
}

func (obj *DB) NewRx() *Rx {
	return &Rx{db: obj}
}

func DeleteAll(ctx context.Context, db *DB) (int64, error) {
	tx, err := db.Open(ctx)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err == nil {
			err = db.makeErr(tx.Commit())
			return
		}

		if err_rollback := tx.Rollback(); err_rollback != nil {
			logError("delete-all: rollback failed: %v", db.makeErr(err_rollback))
		}
	}()
	return tx.deleteAll(ctx)
}

type Tx struct {
	Tx *sql.Tx
	txMethods
}

type dialectTx struct {
	tx *sql.Tx
}

func (tx *dialectTx) Commit() (err error) {
	return makeErr(tx.tx.Commit())
}

func (tx *dialectTx) Rollback() (err error) {
	return makeErr(tx.tx.Rollback())
}

type pgxcockroachImpl struct {
	db      *DB
	dialect __sqlbundle_pgxcockroach
	driver  driver
}

func (obj *pgxcockroachImpl) Rebind(s string) string {
	return obj.dialect.Rebind(s)
}

func (obj *pgxcockroachImpl) logStmt(stmt string, args ...interface{}) {
	pgxcockroachLogStmt(stmt, args...)
}

func (obj *pgxcockroachImpl) makeErr(err error) error {
	constraint, ok := obj.isConstraintError(err)
	if ok {
		return constraintViolation(err, constraint)
	}
	return makeErr(err)
}

type pgxcockroachDB struct {
	db *DB
	*pgxcockroachImpl
}

func newpgxcockroach(db *DB) *pgxcockroachDB {
	return &pgxcockroachDB{
		db: db,
		pgxcockroachImpl: &pgxcockroachImpl{
			db:     db,
			driver: db.DB,
		},
	}
}

func (obj *pgxcockroachDB) Schema() string {
	return `CREATE TABLE records (
	encryption_key_hash bytea NOT NULL,
	created_at timestamp with time zone NOT NULL,
	public boolean NOT NULL,
	satellite_address text NOT NULL,
	macaroon_head bytea NOT NULL,
	expires_at timestamp with time zone,
	encrypted_secret_key bytea NOT NULL,
	encrypted_access_grant bytea NOT NULL,
	invalid_reason text,
	invalid_at timestamp with time zone,
	PRIMARY KEY ( encryption_key_hash )
);`
}

func (obj *pgxcockroachDB) wrapTx(tx *sql.Tx) txMethods {
	return &pgxcockroachTx{
		dialectTx: dialectTx{tx: tx},
		pgxcockroachImpl: &pgxcockroachImpl{
			db:     obj.db,
			driver: tx,
		},
	}
}

type pgxcockroachTx struct {
	dialectTx
	*pgxcockroachImpl
}

func pgxcockroachLogStmt(stmt string, args ...interface{}) {
	// TODO: render placeholders
	if Logger != nil {
		out := fmt.Sprintf("stmt: %s\nargs: %v\n", stmt, pretty(args))
		Logger(out)
	}
}

type sqlite3Impl struct {
	db      *DB
	dialect __sqlbundle_sqlite3
	driver  driver
}

func (obj *sqlite3Impl) Rebind(s string) string {
	return obj.dialect.Rebind(s)
}

func (obj *sqlite3Impl) logStmt(stmt string, args ...interface{}) {
	sqlite3LogStmt(stmt, args...)
}

func (obj *sqlite3Impl) makeErr(err error) error {
	constraint, ok := obj.isConstraintError(err)
	if ok {
		return constraintViolation(err, constraint)
	}
	return makeErr(err)
}

type sqlite3DB struct {
	db *DB
	*sqlite3Impl
}

func newsqlite3(db *DB) *sqlite3DB {
	return &sqlite3DB{
		db: db,
		sqlite3Impl: &sqlite3Impl{
			db:     db,
			driver: db.DB,
		},
	}
}

func (obj *sqlite3DB) Schema() string {
	return `CREATE TABLE records (
	encryption_key_hash BLOB NOT NULL,
	created_at TIMESTAMP NOT NULL,
	public INTEGER NOT NULL,
	satellite_address TEXT NOT NULL,
	macaroon_head BLOB NOT NULL,
	expires_at TIMESTAMP,
	encrypted_secret_key BLOB NOT NULL,
	encrypted_access_grant BLOB NOT NULL,
	invalid_reason TEXT,
	invalid_at TIMESTAMP,
	PRIMARY KEY ( encryption_key_hash )
);`
}

func (obj *sqlite3DB) wrapTx(tx *sql.Tx) txMethods {
	return &sqlite3Tx{
		dialectTx: dialectTx{tx: tx},
		sqlite3Impl: &sqlite3Impl{
			db:     obj.db,
			driver: tx,
		},
	}
}

type sqlite3Tx struct {
	dialectTx
	*sqlite3Impl
}

func sqlite3LogStmt(stmt string, args ...interface{}) {
	// TODO: render placeholders
	if Logger != nil {
		out := fmt.Sprintf("stmt: %s\nargs: %v\n", stmt, pretty(args))
		Logger(out)
	}
}

type pretty []interface{}

func (p pretty) Format(f fmt.State, c rune) {
	fmt.Fprint(f, "[")
nextval:
	for i, val := range p {
		if i > 0 {
			fmt.Fprint(f, ", ")
		}
		rv := reflect.ValueOf(val)
		if rv.Kind() == reflect.Ptr {
			if rv.IsNil() {
				fmt.Fprint(f, "NULL")
				continue
			}
			val = rv.Elem().Interface()
		}
		switch v := val.(type) {
		case string:
			fmt.Fprintf(f, "%q", v)
		case time.Time:
			fmt.Fprintf(f, "%s", v.Format(time.RFC3339Nano))
		case []byte:
			for _, b := range v {
				if !unicode.IsPrint(rune(b)) {
					fmt.Fprintf(f, "%#x", v)
					continue nextval
				}
			}
			fmt.Fprintf(f, "%q", v)
		default:
			fmt.Fprintf(f, "%v", v)
		}
	}
	fmt.Fprint(f, "]")
}

type Record struct {
	EncryptionKeyHash    []byte
	CreatedAt            time.Time
	Public               bool
	SatelliteAddress     string
	MacaroonHead         []byte
	ExpiresAt            *time.Time
	EncryptedSecretKey   []byte
	EncryptedAccessGrant []byte
	InvalidReason        *string
	InvalidAt            *time.Time
}

func (Record) _Table() string { return "records" }

type Record_Create_Fields struct {
	ExpiresAt     Record_ExpiresAt_Field
	InvalidReason Record_InvalidReason_Field
	InvalidAt     Record_InvalidAt_Field
}

type Record_Update_Fields struct {
	InvalidReason Record_InvalidReason_Field
	InvalidAt     Record_InvalidAt_Field
}

type Record_EncryptionKeyHash_Field struct {
	_set   bool
	_null  bool
	_value []byte
}

func Record_EncryptionKeyHash(v []byte) Record_EncryptionKeyHash_Field {
	return Record_EncryptionKeyHash_Field{_set: true, _value: v}
}

func (f Record_EncryptionKeyHash_Field) value() interface{} {
	if !f._set || f._null {
		return nil
	}
	return f._value
}

func (Record_EncryptionKeyHash_Field) _Column() string { return "encryption_key_hash" }

type Record_CreatedAt_Field struct {
	_set   bool
	_null  bool
	_value time.Time
}

func Record_CreatedAt(v time.Time) Record_CreatedAt_Field {
	return Record_CreatedAt_Field{_set: true, _value: v}
}

func (f Record_CreatedAt_Field) value() interface{} {
	if !f._set || f._null {
		return nil
	}
	return f._value
}

func (Record_CreatedAt_Field) _Column() string { return "created_at" }

type Record_Public_Field struct {
	_set   bool
	_null  bool
	_value bool
}

func Record_Public(v bool) Record_Public_Field {
	return Record_Public_Field{_set: true, _value: v}
}

func (f Record_Public_Field) value() interface{} {
	if !f._set || f._null {
		return nil
	}
	return f._value
}

func (Record_Public_Field) _Column() string { return "public" }

type Record_SatelliteAddress_Field struct {
	_set   bool
	_null  bool
	_value string
}

func Record_SatelliteAddress(v string) Record_SatelliteAddress_Field {
	return Record_SatelliteAddress_Field{_set: true, _value: v}
}

func (f Record_SatelliteAddress_Field) value() interface{} {
	if !f._set || f._null {
		return nil
	}
	return f._value
}

func (Record_SatelliteAddress_Field) _Column() string { return "satellite_address" }

type Record_MacaroonHead_Field struct {
	_set   bool
	_null  bool
	_value []byte
}

func Record_MacaroonHead(v []byte) Record_MacaroonHead_Field {
	return Record_MacaroonHead_Field{_set: true, _value: v}
}

func (f Record_MacaroonHead_Field) value() interface{} {
	if !f._set || f._null {
		return nil
	}
	return f._value
}

func (Record_MacaroonHead_Field) _Column() string { return "macaroon_head" }

type Record_ExpiresAt_Field struct {
	_set   bool
	_null  bool
	_value *time.Time
}

func Record_ExpiresAt(v time.Time) Record_ExpiresAt_Field {
	return Record_ExpiresAt_Field{_set: true, _value: &v}
}

func Record_ExpiresAt_Raw(v *time.Time) Record_ExpiresAt_Field {
	if v == nil {
		return Record_ExpiresAt_Null()
	}
	return Record_ExpiresAt(*v)
}

func Record_ExpiresAt_Null() Record_ExpiresAt_Field {
	return Record_ExpiresAt_Field{_set: true, _null: true}
}

func (f Record_ExpiresAt_Field) isnull() bool { return !f._set || f._null || f._value == nil }

func (f Record_ExpiresAt_Field) value() interface{} {
	if !f._set || f._null {
		return nil
	}
	return f._value
}

func (Record_ExpiresAt_Field) _Column() string { return "expires_at" }

type Record_EncryptedSecretKey_Field struct {
	_set   bool
	_null  bool
	_value []byte
}

func Record_EncryptedSecretKey(v []byte) Record_EncryptedSecretKey_Field {
	return Record_EncryptedSecretKey_Field{_set: true, _value: v}
}

func (f Record_EncryptedSecretKey_Field) value() interface{} {
	if !f._set || f._null {
		return nil
	}
	return f._value
}

func (Record_EncryptedSecretKey_Field) _Column() string { return "encrypted_secret_key" }

type Record_EncryptedAccessGrant_Field struct {
	_set   bool
	_null  bool
	_value []byte
}

func Record_EncryptedAccessGrant(v []byte) Record_EncryptedAccessGrant_Field {
	return Record_EncryptedAccessGrant_Field{_set: true, _value: v}
}

func (f Record_EncryptedAccessGrant_Field) value() interface{} {
	if !f._set || f._null {
		return nil
	}
	return f._value
}

func (Record_EncryptedAccessGrant_Field) _Column() string { return "encrypted_access_grant" }

type Record_InvalidReason_Field struct {
	_set   bool
	_null  bool
	_value *string
}

func Record_InvalidReason(v string) Record_InvalidReason_Field {
	return Record_InvalidReason_Field{_set: true, _value: &v}
}

func Record_InvalidReason_Raw(v *string) Record_InvalidReason_Field {
	if v == nil {
		return Record_InvalidReason_Null()
	}
	return Record_InvalidReason(*v)
}

func Record_InvalidReason_Null() Record_InvalidReason_Field {
	return Record_InvalidReason_Field{_set: true, _null: true}
}

func (f Record_InvalidReason_Field) isnull() bool { return !f._set || f._null || f._value == nil }

func (f Record_InvalidReason_Field) value() interface{} {
	if !f._set || f._null {
		return nil
	}
	return f._value
}

func (Record_InvalidReason_Field) _Column() string { return "invalid_reason" }

type Record_InvalidAt_Field struct {
	_set   bool
	_null  bool
	_value *time.Time
}

func Record_InvalidAt(v time.Time) Record_InvalidAt_Field {
	return Record_InvalidAt_Field{_set: true, _value: &v}
}

func Record_InvalidAt_Raw(v *time.Time) Record_InvalidAt_Field {
	if v == nil {
		return Record_InvalidAt_Null()
	}
	return Record_InvalidAt(*v)
}

func Record_InvalidAt_Null() Record_InvalidAt_Field {
	return Record_InvalidAt_Field{_set: true, _null: true}
}

func (f Record_InvalidAt_Field) isnull() bool { return !f._set || f._null || f._value == nil }

func (f Record_InvalidAt_Field) value() interface{} {
	if !f._set || f._null {
		return nil
	}
	return f._value
}

func (Record_InvalidAt_Field) _Column() string { return "invalid_at" }

func toUTC(t time.Time) time.Time {
	return t.UTC()
}

func toDate(t time.Time) time.Time {
	// keep up the minute portion so that translations between timezones will
	// continue to reflect properly.
	return t.Truncate(time.Minute)
}

//
// runtime support for building sql statements
//

type __sqlbundle_SQL interface {
	Render() string

	private()
}

type __sqlbundle_Dialect interface {
	Rebind(sql string) string
}

type __sqlbundle_RenderOp int

const (
	__sqlbundle_NoFlatten __sqlbundle_RenderOp = iota
	__sqlbundle_NoTerminate
)

func __sqlbundle_Render(dialect __sqlbundle_Dialect, sql __sqlbundle_SQL, ops ...__sqlbundle_RenderOp) string {
	out := sql.Render()

	flatten := true
	terminate := true
	for _, op := range ops {
		switch op {
		case __sqlbundle_NoFlatten:
			flatten = false
		case __sqlbundle_NoTerminate:
			terminate = false
		}
	}

	if flatten {
		out = __sqlbundle_flattenSQL(out)
	}
	if terminate {
		out += ";"
	}

	return dialect.Rebind(out)
}

func __sqlbundle_flattenSQL(x string) string {
	// trim whitespace from beginning and end
	s, e := 0, len(x)-1
	for s < len(x) && (x[s] == ' ' || x[s] == '\t' || x[s] == '\n') {
		s++
	}
	for s <= e && (x[e] == ' ' || x[e] == '\t' || x[e] == '\n') {
		e--
	}
	if s > e {
		return ""
	}
	x = x[s : e+1]

	// check for whitespace that needs fixing
	wasSpace := false
	for i := 0; i < len(x); i++ {
		r := x[i]
		justSpace := r == ' '
		if (wasSpace && justSpace) || r == '\t' || r == '\n' {
			// whitespace detected, start writing a new string
			var result strings.Builder
			result.Grow(len(x))
			if wasSpace {
				result.WriteString(x[:i-1])
			} else {
				result.WriteString(x[:i])
			}
			for p := i; p < len(x); p++ {
				for p < len(x) && (x[p] == ' ' || x[p] == '\t' || x[p] == '\n') {
					p++
				}
				result.WriteByte(' ')

				start := p
				for p < len(x) && !(x[p] == ' ' || x[p] == '\t' || x[p] == '\n') {
					p++
				}
				result.WriteString(x[start:p])
			}

			return result.String()
		}
		wasSpace = justSpace
	}

	// no problematic whitespace found
	return x
}

// this type is specially named to match up with the name returned by the
// dialect impl in the sql package.
type __sqlbundle_postgres struct{}

func (p __sqlbundle_postgres) Rebind(sql string) string {
	type sqlParseState int
	const (
		sqlParseStart sqlParseState = iota
		sqlParseInStringLiteral
		sqlParseInQuotedIdentifier
		sqlParseInComment
	)

	out := make([]byte, 0, len(sql)+10)

	j := 1
	state := sqlParseStart
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch state {
		case sqlParseStart:
			switch ch {
			case '?':
				out = append(out, '$')
				out = append(out, strconv.Itoa(j)...)
				state = sqlParseStart
				j++
				continue
			case '-':
				if i+1 < len(sql) && sql[i+1] == '-' {
					state = sqlParseInComment
				}
			case '"':
				state = sqlParseInQuotedIdentifier
			case '\'':
				state = sqlParseInStringLiteral
			}
		case sqlParseInStringLiteral:
			if ch == '\'' {
				state = sqlParseStart
			}
		case sqlParseInQuotedIdentifier:
			if ch == '"' {
				state = sqlParseStart
			}
		case sqlParseInComment:
			if ch == '\n' {
				state = sqlParseStart
			}
		}
		out = append(out, ch)
	}

	return string(out)
}

// this type is specially named to match up with the name returned by the
// dialect impl in the sql package.
type __sqlbundle_sqlite3 struct{}

func (s __sqlbundle_sqlite3) Rebind(sql string) string {
	return sql
}

// this type is specially named to match up with the name returned by the
// dialect impl in the sql package.
type __sqlbundle_cockroach struct{}

func (p __sqlbundle_cockroach) Rebind(sql string) string {
	type sqlParseState int
	const (
		sqlParseStart sqlParseState = iota
		sqlParseInStringLiteral
		sqlParseInQuotedIdentifier
		sqlParseInComment
	)

	out := make([]byte, 0, len(sql)+10)

	j := 1
	state := sqlParseStart
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch state {
		case sqlParseStart:
			switch ch {
			case '?':
				out = append(out, '$')
				out = append(out, strconv.Itoa(j)...)
				state = sqlParseStart
				j++
				continue
			case '-':
				if i+1 < len(sql) && sql[i+1] == '-' {
					state = sqlParseInComment
				}
			case '"':
				state = sqlParseInQuotedIdentifier
			case '\'':
				state = sqlParseInStringLiteral
			}
		case sqlParseInStringLiteral:
			if ch == '\'' {
				state = sqlParseStart
			}
		case sqlParseInQuotedIdentifier:
			if ch == '"' {
				state = sqlParseStart
			}
		case sqlParseInComment:
			if ch == '\n' {
				state = sqlParseStart
			}
		}
		out = append(out, ch)
	}

	return string(out)
}

// this type is specially named to match up with the name returned by the
// dialect impl in the sql package.
type __sqlbundle_pgx struct{}

func (p __sqlbundle_pgx) Rebind(sql string) string {
	type sqlParseState int
	const (
		sqlParseStart sqlParseState = iota
		sqlParseInStringLiteral
		sqlParseInQuotedIdentifier
		sqlParseInComment
	)

	out := make([]byte, 0, len(sql)+10)

	j := 1
	state := sqlParseStart
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch state {
		case sqlParseStart:
			switch ch {
			case '?':
				out = append(out, '$')
				out = append(out, strconv.Itoa(j)...)
				state = sqlParseStart
				j++
				continue
			case '-':
				if i+1 < len(sql) && sql[i+1] == '-' {
					state = sqlParseInComment
				}
			case '"':
				state = sqlParseInQuotedIdentifier
			case '\'':
				state = sqlParseInStringLiteral
			}
		case sqlParseInStringLiteral:
			if ch == '\'' {
				state = sqlParseStart
			}
		case sqlParseInQuotedIdentifier:
			if ch == '"' {
				state = sqlParseStart
			}
		case sqlParseInComment:
			if ch == '\n' {
				state = sqlParseStart
			}
		}
		out = append(out, ch)
	}

	return string(out)
}

// this type is specially named to match up with the name returned by the
// dialect impl in the sql package.
type __sqlbundle_pgxcockroach struct{}

func (p __sqlbundle_pgxcockroach) Rebind(sql string) string {
	type sqlParseState int
	const (
		sqlParseStart sqlParseState = iota
		sqlParseInStringLiteral
		sqlParseInQuotedIdentifier
		sqlParseInComment
	)

	out := make([]byte, 0, len(sql)+10)

	j := 1
	state := sqlParseStart
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch state {
		case sqlParseStart:
			switch ch {
			case '?':
				out = append(out, '$')
				out = append(out, strconv.Itoa(j)...)
				state = sqlParseStart
				j++
				continue
			case '-':
				if i+1 < len(sql) && sql[i+1] == '-' {
					state = sqlParseInComment
				}
			case '"':
				state = sqlParseInQuotedIdentifier
			case '\'':
				state = sqlParseInStringLiteral
			}
		case sqlParseInStringLiteral:
			if ch == '\'' {
				state = sqlParseStart
			}
		case sqlParseInQuotedIdentifier:
			if ch == '"' {
				state = sqlParseStart
			}
		case sqlParseInComment:
			if ch == '\n' {
				state = sqlParseStart
			}
		}
		out = append(out, ch)
	}

	return string(out)
}

type __sqlbundle_Literal string

func (__sqlbundle_Literal) private() {}

func (l __sqlbundle_Literal) Render() string { return string(l) }

type __sqlbundle_Literals struct {
	Join string
	SQLs []__sqlbundle_SQL
}

func (__sqlbundle_Literals) private() {}

func (l __sqlbundle_Literals) Render() string {
	var out bytes.Buffer

	first := true
	for _, sql := range l.SQLs {
		if sql == nil {
			continue
		}
		if !first {
			out.WriteString(l.Join)
		}
		first = false
		out.WriteString(sql.Render())
	}

	return out.String()
}

type __sqlbundle_Condition struct {
	// set at compile/embed time
	Name  string
	Left  string
	Equal bool
	Right string

	// set at runtime
	Null bool
}

func (*__sqlbundle_Condition) private() {}

func (c *__sqlbundle_Condition) Render() string {
	// TODO(jeff): maybe check if we can use placeholders instead of the
	// literal null: this would make the templates easier.

	switch {
	case c.Equal && c.Null:
		return c.Left + " is null"
	case c.Equal && !c.Null:
		return c.Left + " = " + c.Right
	case !c.Equal && c.Null:
		return c.Left + " is not null"
	case !c.Equal && !c.Null:
		return c.Left + " != " + c.Right
	default:
		panic("unhandled case")
	}
}

type __sqlbundle_Hole struct {
	// set at compiile/embed time
	Name string

	// set at runtime or possibly embed time
	SQL __sqlbundle_SQL
}

func (*__sqlbundle_Hole) private() {}

func (h *__sqlbundle_Hole) Render() string {
	if h.SQL == nil {
		return ""
	}
	return h.SQL.Render()
}

//
// end runtime support for building sql statements
//

func (obj *pgxcockroachImpl) CreateNoReturn_Record(ctx context.Context,
	record_encryption_key_hash Record_EncryptionKeyHash_Field,
	record_public Record_Public_Field,
	record_satellite_address Record_SatelliteAddress_Field,
	record_macaroon_head Record_MacaroonHead_Field,
	record_encrypted_secret_key Record_EncryptedSecretKey_Field,
	record_encrypted_access_grant Record_EncryptedAccessGrant_Field,
	optional Record_Create_Fields) (
	err error) {

	__now := obj.db.Hooks.Now().UTC()
	__encryption_key_hash_val := record_encryption_key_hash.value()
	__created_at_val := __now
	__public_val := record_public.value()
	__satellite_address_val := record_satellite_address.value()
	__macaroon_head_val := record_macaroon_head.value()
	__expires_at_val := optional.ExpiresAt.value()
	__encrypted_secret_key_val := record_encrypted_secret_key.value()
	__encrypted_access_grant_val := record_encrypted_access_grant.value()
	__invalid_reason_val := optional.InvalidReason.value()
	__invalid_at_val := optional.InvalidAt.value()

	var __embed_stmt = __sqlbundle_Literal("INSERT INTO records ( encryption_key_hash, created_at, public, satellite_address, macaroon_head, expires_at, encrypted_secret_key, encrypted_access_grant, invalid_reason, invalid_at ) VALUES ( ?, ?, ?, ?, ?, ?, ?, ?, ?, ? )")

	var __values []interface{}
	__values = append(__values, __encryption_key_hash_val, __created_at_val, __public_val, __satellite_address_val, __macaroon_head_val, __expires_at_val, __encrypted_secret_key_val, __encrypted_access_grant_val, __invalid_reason_val, __invalid_at_val)

	var __stmt = __sqlbundle_Render(obj.dialect, __embed_stmt)
	obj.logStmt(__stmt, __values...)

	_, err = obj.driver.ExecContext(ctx, __stmt, __values...)
	if err != nil {
		return obj.makeErr(err)
	}
	return nil

}

func (obj *pgxcockroachImpl) Find_Record_By_EncryptionKeyHash(ctx context.Context,
	record_encryption_key_hash Record_EncryptionKeyHash_Field) (
	record *Record, err error) {

	var __embed_stmt = __sqlbundle_Literal("SELECT records.encryption_key_hash, records.created_at, records.public, records.satellite_address, records.macaroon_head, records.expires_at, records.encrypted_secret_key, records.encrypted_access_grant, records.invalid_reason, records.invalid_at FROM records WHERE records.encryption_key_hash = ?")

	var __values []interface{}
	__values = append(__values, record_encryption_key_hash.value())

	var __stmt = __sqlbundle_Render(obj.dialect, __embed_stmt)
	obj.logStmt(__stmt, __values...)

	record = &Record{}
	err = obj.driver.QueryRowContext(ctx, __stmt, __values...).Scan(&record.EncryptionKeyHash, &record.CreatedAt, &record.Public, &record.SatelliteAddress, &record.MacaroonHead, &record.ExpiresAt, &record.EncryptedSecretKey, &record.EncryptedAccessGrant, &record.InvalidReason, &record.InvalidAt)
	if err == sql.ErrNoRows {
		return (*Record)(nil), nil
	}
	if err != nil {
		return (*Record)(nil), obj.makeErr(err)
	}
	return record, nil

}

func (obj *pgxcockroachImpl) UpdateNoReturn_Record_By_EncryptionKeyHash_And_InvalidReason_Is_Null(ctx context.Context,
	record_encryption_key_hash Record_EncryptionKeyHash_Field,
	update Record_Update_Fields) (
	err error) {
	var __sets = &__sqlbundle_Hole{}

	var __embed_stmt = __sqlbundle_Literals{Join: "", SQLs: []__sqlbundle_SQL{__sqlbundle_Literal("UPDATE records SET "), __sets, __sqlbundle_Literal(" WHERE records.encryption_key_hash = ? AND records.invalid_reason is NULL")}}

	__sets_sql := __sqlbundle_Literals{Join: ", "}
	var __values []interface{}
	var __args []interface{}

	if update.InvalidReason._set {
		__values = append(__values, update.InvalidReason.value())
		__sets_sql.SQLs = append(__sets_sql.SQLs, __sqlbundle_Literal("invalid_reason = ?"))
	}

	if update.InvalidAt._set {
		__values = append(__values, update.InvalidAt.value())
		__sets_sql.SQLs = append(__sets_sql.SQLs, __sqlbundle_Literal("invalid_at = ?"))
	}

	if len(__sets_sql.SQLs) == 0 {
		return emptyUpdate()
	}

	__args = append(__args, record_encryption_key_hash.value())

	__values = append(__values, __args...)
	__sets.SQL = __sets_sql

	var __stmt = __sqlbundle_Render(obj.dialect, __embed_stmt)
	obj.logStmt(__stmt, __values...)

	_, err = obj.driver.ExecContext(ctx, __stmt, __values...)
	if err != nil {
		return obj.makeErr(err)
	}
	return nil
}

func (obj *pgxcockroachImpl) Delete_Record_By_EncryptionKeyHash(ctx context.Context,
	record_encryption_key_hash Record_EncryptionKeyHash_Field) (
	deleted bool, err error) {

	var __embed_stmt = __sqlbundle_Literal("DELETE FROM records WHERE records.encryption_key_hash = ?")

	var __values []interface{}
	__values = append(__values, record_encryption_key_hash.value())

	var __stmt = __sqlbundle_Render(obj.dialect, __embed_stmt)
	obj.logStmt(__stmt, __values...)

	__res, err := obj.driver.ExecContext(ctx, __stmt, __values...)
	if err != nil {
		return false, obj.makeErr(err)
	}

	__count, err := __res.RowsAffected()
	if err != nil {
		return false, obj.makeErr(err)
	}

	return __count > 0, nil

}

func (impl pgxcockroachImpl) isConstraintError(err error) (
	constraint string, ok bool) {
	if e, ok := err.(*pgconn.PgError); ok {
		if e.Code[:2] == "23" {
			return e.ConstraintName, true
		}
	}
	return "", false
}

func (obj *pgxcockroachImpl) deleteAll(ctx context.Context) (count int64, err error) {
	var __res sql.Result
	var __count int64
	__res, err = obj.driver.ExecContext(ctx, "DELETE FROM records;")
	if err != nil {
		return 0, obj.makeErr(err)
	}

	__count, err = __res.RowsAffected()
	if err != nil {
		return 0, obj.makeErr(err)
	}
	count += __count

	return count, nil

}

func (obj *sqlite3Impl) CreateNoReturn_Record(ctx context.Context,
	record_encryption_key_hash Record_EncryptionKeyHash_Field,
	record_public Record_Public_Field,
	record_satellite_address Record_SatelliteAddress_Field,
	record_macaroon_head Record_MacaroonHead_Field,
	record_encrypted_secret_key Record_EncryptedSecretKey_Field,
	record_encrypted_access_grant Record_EncryptedAccessGrant_Field,
	optional Record_Create_Fields) (
	err error) {

	__now := obj.db.Hooks.Now().UTC()
	__encryption_key_hash_val := record_encryption_key_hash.value()
	__created_at_val := __now
	__public_val := record_public.value()
	__satellite_address_val := record_satellite_address.value()
	__macaroon_head_val := record_macaroon_head.value()
	__expires_at_val := optional.ExpiresAt.value()
	__encrypted_secret_key_val := record_encrypted_secret_key.value()
	__encrypted_access_grant_val := record_encrypted_access_grant.value()
	__invalid_reason_val := optional.InvalidReason.value()
	__invalid_at_val := optional.InvalidAt.value()

	var __embed_stmt = __sqlbundle_Literal("INSERT INTO records ( encryption_key_hash, created_at, public, satellite_address, macaroon_head, expires_at, encrypted_secret_key, encrypted_access_grant, invalid_reason, invalid_at ) VALUES ( ?, ?, ?, ?, ?, ?, ?, ?, ?, ? )")

	var __values []interface{}
	__values = append(__values, __encryption_key_hash_val, __created_at_val, __public_val, __satellite_address_val, __macaroon_head_val, __expires_at_val, __encrypted_secret_key_val, __encrypted_access_grant_val, __invalid_reason_val, __invalid_at_val)

	var __stmt = __sqlbundle_Render(obj.dialect, __embed_stmt)
	obj.logStmt(__stmt, __values...)

	_, err = obj.driver.ExecContext(ctx, __stmt, __values...)
	if err != nil {
		return obj.makeErr(err)
	}
	return nil

}

func (obj *sqlite3Impl) Find_Record_By_EncryptionKeyHash(ctx context.Context,
	record_encryption_key_hash Record_EncryptionKeyHash_Field) (
	record *Record, err error) {

	var __embed_stmt = __sqlbundle_Literal("SELECT records.encryption_key_hash, records.created_at, records.public, records.satellite_address, records.macaroon_head, records.expires_at, records.encrypted_secret_key, records.encrypted_access_grant, records.invalid_reason, records.invalid_at FROM records WHERE records.encryption_key_hash = ?")

	var __values []interface{}
	__values = append(__values, record_encryption_key_hash.value())

	var __stmt = __sqlbundle_Render(obj.dialect, __embed_stmt)
	obj.logStmt(__stmt, __values...)

	record = &Record{}
	err = obj.driver.QueryRowContext(ctx, __stmt, __values...).Scan(&record.EncryptionKeyHash, &record.CreatedAt, &record.Public, &record.SatelliteAddress, &record.MacaroonHead, &record.ExpiresAt, &record.EncryptedSecretKey, &record.EncryptedAccessGrant, &record.InvalidReason, &record.InvalidAt)
	if err == sql.ErrNoRows {
		return (*Record)(nil), nil
	}
	if err != nil {
		return (*Record)(nil), obj.makeErr(err)
	}
	return record, nil

}

func (obj *sqlite3Impl) UpdateNoReturn_Record_By_EncryptionKeyHash_And_InvalidReason_Is_Null(ctx context.Context,
	record_encryption_key_hash Record_EncryptionKeyHash_Field,
	update Record_Update_Fields) (
	err error) {
	var __sets = &__sqlbundle_Hole{}

	var __embed_stmt = __sqlbundle_Literals{Join: "", SQLs: []__sqlbundle_SQL{__sqlbundle_Literal("UPDATE records SET "), __sets, __sqlbundle_Literal(" WHERE records.encryption_key_hash = ? AND records.invalid_reason is NULL")}}

	__sets_sql := __sqlbundle_Literals{Join: ", "}
	var __values []interface{}
	var __args []interface{}

	if update.InvalidReason._set {
		__values = append(__values, update.InvalidReason.value())
		__sets_sql.SQLs = append(__sets_sql.SQLs, __sqlbundle_Literal("invalid_reason = ?"))
	}

	if update.InvalidAt._set {
		__values = append(__values, update.InvalidAt.value())
		__sets_sql.SQLs = append(__sets_sql.SQLs, __sqlbundle_Literal("invalid_at = ?"))
	}

	if len(__sets_sql.SQLs) == 0 {
		return emptyUpdate()
	}

	__args = append(__args, record_encryption_key_hash.value())

	__values = append(__values, __args...)
	__sets.SQL = __sets_sql

	var __stmt = __sqlbundle_Render(obj.dialect, __embed_stmt)
	obj.logStmt(__stmt, __values...)

	_, err = obj.driver.ExecContext(ctx, __stmt, __values...)
	if err != nil {
		return obj.makeErr(err)
	}
	return nil
}

func (obj *sqlite3Impl) Delete_Record_By_EncryptionKeyHash(ctx context.Context,
	record_encryption_key_hash Record_EncryptionKeyHash_Field) (
	deleted bool, err error) {

	var __embed_stmt = __sqlbundle_Literal("DELETE FROM records WHERE records.encryption_key_hash = ?")

	var __values []interface{}
	__values = append(__values, record_encryption_key_hash.value())

	var __stmt = __sqlbundle_Render(obj.dialect, __embed_stmt)
	obj.logStmt(__stmt, __values...)

	__res, err := obj.driver.ExecContext(ctx, __stmt, __values...)
	if err != nil {
		return false, obj.makeErr(err)
	}

	__count, err := __res.RowsAffected()
	if err != nil {
		return false, obj.makeErr(err)
	}

	return __count > 0, nil

}

func (obj *sqlite3Impl) getLastRecord(ctx context.Context,
	pk int64) (
	record *Record, err error) {

	var __embed_stmt = __sqlbundle_Literal("SELECT records.encryption_key_hash, records.created_at, records.public, records.satellite_address, records.macaroon_head, records.expires_at, records.encrypted_secret_key, records.encrypted_access_grant, records.invalid_reason, records.invalid_at FROM records WHERE _rowid_ = ?")

	var __stmt = __sqlbundle_Render(obj.dialect, __embed_stmt)
	obj.logStmt(__stmt, pk)

	record = &Record{}
	err = obj.driver.QueryRowContext(ctx, __stmt, pk).Scan(&record.EncryptionKeyHash, &record.CreatedAt, &record.Public, &record.SatelliteAddress, &record.MacaroonHead, &record.ExpiresAt, &record.EncryptedSecretKey, &record.EncryptedAccessGrant, &record.InvalidReason, &record.InvalidAt)
	if err != nil {
		return (*Record)(nil), obj.makeErr(err)
	}
	return record, nil

}

func (impl sqlite3Impl) isConstraintError(err error) (
	constraint string, ok bool) {
	if e, ok := err.(sqlite3.Error); ok {
		if e.Code == sqlite3.ErrConstraint {
			msg := err.Error()
			colon := strings.LastIndex(msg, ":")
			if colon != -1 {
				return strings.TrimSpace(msg[colon:]), true
			}
			return "", true
		}
	}
	return "", false
}

func (obj *sqlite3Impl) deleteAll(ctx context.Context) (count int64, err error) {
	var __res sql.Result
	var __count int64
	__res, err = obj.driver.ExecContext(ctx, "DELETE FROM records;")
	if err != nil {
		return 0, obj.makeErr(err)
	}

	__count, err = __res.RowsAffected()
	if err != nil {
		return 0, obj.makeErr(err)
	}
	count += __count

	return count, nil

}

type Rx struct {
	db *DB
	tx *Tx
}

func (rx *Rx) UnsafeTx(ctx context.Context) (unsafe_tx *sql.Tx, err error) {
	tx, err := rx.getTx(ctx)
	if err != nil {
		return nil, err
	}
	return tx.Tx, nil
}

func (rx *Rx) getTx(ctx context.Context) (tx *Tx, err error) {
	if rx.tx == nil {
		if rx.tx, err = rx.db.Open(ctx); err != nil {
			return nil, err
		}
	}
	return rx.tx, nil
}

func (rx *Rx) Rebind(s string) string {
	return rx.db.Rebind(s)
}

func (rx *Rx) Commit() (err error) {
	if rx.tx != nil {
		err = rx.tx.Commit()
		rx.tx = nil
	}
	return err
}

func (rx *Rx) Rollback() (err error) {
	if rx.tx != nil {
		err = rx.tx.Rollback()
		rx.tx = nil
	}
	return err
}

func (rx *Rx) CreateNoReturn_Record(ctx context.Context,
	record_encryption_key_hash Record_EncryptionKeyHash_Field,
	record_public Record_Public_Field,
	record_satellite_address Record_SatelliteAddress_Field,
	record_macaroon_head Record_MacaroonHead_Field,
	record_encrypted_secret_key Record_EncryptedSecretKey_Field,
	record_encrypted_access_grant Record_EncryptedAccessGrant_Field,
	optional Record_Create_Fields) (
	err error) {
	var tx *Tx
	if tx, err = rx.getTx(ctx); err != nil {
		return
	}
	return tx.CreateNoReturn_Record(ctx, record_encryption_key_hash, record_public, record_satellite_address, record_macaroon_head, record_encrypted_secret_key, record_encrypted_access_grant, optional)

}

func (rx *Rx) Delete_Record_By_EncryptionKeyHash(ctx context.Context,
	record_encryption_key_hash Record_EncryptionKeyHash_Field) (
	deleted bool, err error) {
	var tx *Tx
	if tx, err = rx.getTx(ctx); err != nil {
		return
	}
	return tx.Delete_Record_By_EncryptionKeyHash(ctx, record_encryption_key_hash)
}

func (rx *Rx) Find_Record_By_EncryptionKeyHash(ctx context.Context,
	record_encryption_key_hash Record_EncryptionKeyHash_Field) (
	record *Record, err error) {
	var tx *Tx
	if tx, err = rx.getTx(ctx); err != nil {
		return
	}
	return tx.Find_Record_By_EncryptionKeyHash(ctx, record_encryption_key_hash)
}

func (rx *Rx) UpdateNoReturn_Record_By_EncryptionKeyHash_And_InvalidReason_Is_Null(ctx context.Context,
	record_encryption_key_hash Record_EncryptionKeyHash_Field,
	update Record_Update_Fields) (
	err error) {
	var tx *Tx
	if tx, err = rx.getTx(ctx); err != nil {
		return
	}
	return tx.UpdateNoReturn_Record_By_EncryptionKeyHash_And_InvalidReason_Is_Null(ctx, record_encryption_key_hash, update)
}

type Methods interface {
	CreateNoReturn_Record(ctx context.Context,
		record_encryption_key_hash Record_EncryptionKeyHash_Field,
		record_public Record_Public_Field,
		record_satellite_address Record_SatelliteAddress_Field,
		record_macaroon_head Record_MacaroonHead_Field,
		record_encrypted_secret_key Record_EncryptedSecretKey_Field,
		record_encrypted_access_grant Record_EncryptedAccessGrant_Field,
		optional Record_Create_Fields) (
		err error)

	Delete_Record_By_EncryptionKeyHash(ctx context.Context,
		record_encryption_key_hash Record_EncryptionKeyHash_Field) (
		deleted bool, err error)

	Find_Record_By_EncryptionKeyHash(ctx context.Context,
		record_encryption_key_hash Record_EncryptionKeyHash_Field) (
		record *Record, err error)

	UpdateNoReturn_Record_By_EncryptionKeyHash_And_InvalidReason_Is_Null(ctx context.Context,
		record_encryption_key_hash Record_EncryptionKeyHash_Field,
		update Record_Update_Fields) (
		err error)
}

type TxMethods interface {
	Methods

	Rebind(s string) string
	Commit() error
	Rollback() error
}

type txMethods interface {
	TxMethods

	deleteAll(ctx context.Context) (int64, error)
	makeErr(err error) error
}

type DBMethods interface {
	Methods

	Schema() string
	Rebind(sql string) string
}

type dbMethods interface {
	DBMethods

	wrapTx(tx *sql.Tx) txMethods
	makeErr(err error) error
}

func openpgxcockroach(source string) (*sql.DB, error) {
	// try first with "cockroach" as a driver in case someone has registered
	// some special stuff. if that fails, then try again with "pgx" as
	// the driver.
	db, err := sql.Open("cockroach", source)
	if err != nil {
		db, err = sql.Open("pgx", source)
	}
	return db, err
}

var sqlite3DriverName = func() string {
	var id [16]byte
	rand.Read(id[:])
	return fmt.Sprintf("sqlite3_%x", string(id[:]))
}()

func init() {
	sql.Register(sqlite3DriverName, &sqlite3.SQLiteDriver{
		ConnectHook: sqlite3SetupConn,
	})
}

// SQLite3JournalMode controls the journal_mode pragma for all new connections.
// Since it is read without a mutex, it must be changed to the value you want
// before any Open calls.
var SQLite3JournalMode = "WAL"

func sqlite3SetupConn(conn *sqlite3.SQLiteConn) (err error) {
	_, err = conn.Exec("PRAGMA foreign_keys = ON", nil)
	if err != nil {
		return makeErr(err)
	}
	_, err = conn.Exec("PRAGMA journal_mode = "+SQLite3JournalMode, nil)
	if err != nil {
		return makeErr(err)
	}
	return nil
}

func opensqlite3(source string) (*sql.DB, error) {
	return sql.Open(sqlite3DriverName, source)
}
