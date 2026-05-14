// Package database also provides a thin wrapper over *sql.DB that rewrites
// `?` placeholders to `$N` when the underlying driver is Postgres. Service
// code keeps writing queries with `?` (the SQLite-friendly form); the rebind
// happens at call time and is a no-op on SQLite.
//
// The wrapper embeds *sql.DB, so any *sql.DB method not explicitly overridden
// here is still callable (Ping, Close, Stats, SetMaxOpenConns, ...). Only the
// query-execution methods are overridden to interpose the rebinder. The same
// pattern is mirrored on Tx for transactions.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// DB wraps *sql.DB with placeholder rewriting. Construct via Open.
type DB struct {
	*sql.DB
	driver string
}

// Driver returns the driver name ("sqlite" or "postgres") this DB was opened with.
func (d *DB) Driver() string { return d.driver }

// Exec rebinds `?` to `$N` on Postgres and delegates to the embedded *sql.DB.
func (d *DB) Exec(query string, args ...any) (sql.Result, error) {
	return d.DB.Exec(d.rebind(query), args...)
}

// ExecContext is the context-aware form of Exec.
func (d *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.DB.ExecContext(ctx, d.rebind(query), args...)
}

// Query rebinds `?` to `$N` on Postgres and delegates to the embedded *sql.DB.
func (d *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return d.DB.Query(d.rebind(query), args...)
}

// QueryContext is the context-aware form of Query.
func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.DB.QueryContext(ctx, d.rebind(query), args...)
}

// QueryRow rebinds `?` to `$N` on Postgres and delegates to the embedded *sql.DB.
func (d *DB) QueryRow(query string, args ...any) *sql.Row {
	return d.DB.QueryRow(d.rebind(query), args...)
}

// QueryRowContext is the context-aware form of QueryRow.
func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.DB.QueryRowContext(ctx, d.rebind(query), args...)
}

// Prepare rebinds `?` to `$N` on Postgres before preparing the statement.
func (d *DB) Prepare(query string) (*sql.Stmt, error) {
	return d.DB.Prepare(d.rebind(query))
}

// PrepareContext is the context-aware form of Prepare.
func (d *DB) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return d.DB.PrepareContext(ctx, d.rebind(query))
}

// Begin starts a transaction. The returned *Tx applies the same placeholder
// rewriting as DB.
func (d *DB) Begin() (*Tx, error) {
	tx, err := d.DB.Begin()
	if err != nil {
		return nil, err
	}
	return &Tx{Tx: tx, driver: d.driver}, nil
}

// BeginTx is the context-aware form of Begin.
func (d *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := d.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{Tx: tx, driver: d.driver}, nil
}

func (d *DB) rebind(q string) string {
	if d.driver != "postgres" {
		return q
	}
	return rebindPostgres(q)
}

// Tx wraps *sql.Tx with placeholder rewriting. Obtain from DB.Begin / BeginTx.
type Tx struct {
	*sql.Tx
	driver string
}

// Exec rebinds and delegates.
func (t *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return t.Tx.Exec(t.rebind(query), args...)
}

// ExecContext rebinds and delegates.
func (t *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.Tx.ExecContext(ctx, t.rebind(query), args...)
}

// Query rebinds and delegates.
func (t *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.Tx.Query(t.rebind(query), args...)
}

// QueryContext rebinds and delegates.
func (t *Tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.Tx.QueryContext(ctx, t.rebind(query), args...)
}

// QueryRow rebinds and delegates.
func (t *Tx) QueryRow(query string, args ...any) *sql.Row {
	return t.Tx.QueryRow(t.rebind(query), args...)
}

// QueryRowContext rebinds and delegates.
func (t *Tx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.Tx.QueryRowContext(ctx, t.rebind(query), args...)
}

// Prepare rebinds and delegates.
func (t *Tx) Prepare(query string) (*sql.Stmt, error) {
	return t.Tx.Prepare(t.rebind(query))
}

// PrepareContext rebinds and delegates.
func (t *Tx) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return t.Tx.PrepareContext(ctx, t.rebind(query))
}

func (t *Tx) rebind(q string) string {
	if t.driver != "postgres" {
		return q
	}
	return rebindPostgres(q)
}

// rebindPostgres walks the query and replaces each unquoted `?` with the
// next $N placeholder. It is aware of single-quoted SQL string literals
// (with '' doubled-quote escapes), double-quoted identifiers (with ""
// escapes), -- line comments, /* */ block comments, and the Postgres JSON
// operators ?|, ?&, and ?? — none of which should be rebound.
func rebindPostgres(q string) string {
	var b strings.Builder
	b.Grow(len(q) + 8)
	n := 1
	var (
		inSingle   bool
		inDouble   bool
		inLineCmt  bool
		inBlockCmt bool
	)
	for i := 0; i < len(q); i++ {
		c := q[i]

		// State-aware handling: while inside a literal or comment, copy
		// bytes verbatim and look only for the closing delimiter.
		switch {
		case inLineCmt:
			b.WriteByte(c)
			if c == '\n' {
				inLineCmt = false
			}
			continue
		case inBlockCmt:
			b.WriteByte(c)
			if c == '*' && i+1 < len(q) && q[i+1] == '/' {
				b.WriteByte('/')
				i++
				inBlockCmt = false
			}
			continue
		case inSingle:
			b.WriteByte(c)
			if c == '\'' {
				if i+1 < len(q) && q[i+1] == '\'' {
					b.WriteByte('\'')
					i++
				} else {
					inSingle = false
				}
			}
			continue
		case inDouble:
			b.WriteByte(c)
			if c == '"' {
				if i+1 < len(q) && q[i+1] == '"' {
					b.WriteByte('"')
					i++
				} else {
					inDouble = false
				}
			}
			continue
		}

		switch c {
		case '\'':
			inSingle = true
			b.WriteByte(c)
		case '"':
			inDouble = true
			b.WriteByte(c)
		case '-':
			if i+1 < len(q) && q[i+1] == '-' {
				inLineCmt = true
				b.WriteString("--")
				i++
			} else {
				b.WriteByte(c)
			}
		case '/':
			if i+1 < len(q) && q[i+1] == '*' {
				inBlockCmt = true
				b.WriteString("/*")
				i++
			} else {
				b.WriteByte(c)
			}
		case '?':
			// JSON operators (?| ?& ??) must not be rebound. Treat the
			// `?` and its companion byte as a single token and emit verbatim.
			if i+1 < len(q) {
				next := q[i+1]
				if next == '|' || next == '&' || next == '?' {
					b.WriteByte('?')
					b.WriteByte(next)
					i++
					continue
				}
			}
			fmt.Fprintf(&b, "$%d", n)
			n++
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
