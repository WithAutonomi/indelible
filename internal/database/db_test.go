package database

import "testing"

func TestRebindPostgres(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no placeholders",
			in:   `SELECT * FROM users`,
			want: `SELECT * FROM users`,
		},
		{
			name: "single placeholder",
			in:   `SELECT * FROM users WHERE id = ?`,
			want: `SELECT * FROM users WHERE id = $1`,
		},
		{
			name: "multiple placeholders",
			in:   `INSERT INTO x (a, b, c) VALUES (?, ?, ?)`,
			want: `INSERT INTO x (a, b, c) VALUES ($1, $2, $3)`,
		},
		{
			name: "question mark inside single-quoted literal",
			in:   `SELECT 'is this ok?' WHERE id = ?`,
			want: `SELECT 'is this ok?' WHERE id = $1`,
		},
		{
			name: "escaped single quote inside literal",
			in:   `SELECT 'don''t ? me' WHERE id = ?`,
			want: `SELECT 'don''t ? me' WHERE id = $1`,
		},
		{
			name: "question mark inside double-quoted identifier",
			in:   `SELECT "weird?name" FROM x WHERE id = ?`,
			want: `SELECT "weird?name" FROM x WHERE id = $1`,
		},
		{
			name: "question mark inside line comment",
			in:   "SELECT 1 -- huh ? \nWHERE id = ?",
			want: "SELECT 1 -- huh ? \nWHERE id = $1",
		},
		{
			name: "question mark inside block comment",
			in:   `SELECT /* hmm ? */ 1 WHERE id = ?`,
			want: `SELECT /* hmm ? */ 1 WHERE id = $1`,
		},
		{
			name: "JSON ?| operator preserved",
			in:   `SELECT data ?| array['a'] FROM x WHERE id = ?`,
			want: `SELECT data ?| array['a'] FROM x WHERE id = $1`,
		},
		{
			name: "JSON ?& operator preserved",
			in:   `SELECT data ?& array['a'] FROM x WHERE id = ?`,
			want: `SELECT data ?& array['a'] FROM x WHERE id = $1`,
		},
		{
			name: "JSON ?? operator preserved",
			in:   `SELECT data ?? 'a' FROM x WHERE id = ?`,
			want: `SELECT data ?? 'a' FROM x WHERE id = $1`,
		},
		{
			name: "multi-line query",
			in: `INSERT INTO uploads (id, name)
			     VALUES (?, ?)
			     RETURNING id`,
			want: `INSERT INTO uploads (id, name)
			     VALUES ($1, $2)
			     RETURNING id`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rebindPostgres(tc.in)
			if got != tc.want {
				t.Errorf("rebindPostgres mismatch\n  in:   %q\n  got:  %q\n  want: %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRebindPostgres_SqliteNoOp(t *testing.T) {
	d := &DB{driver: "sqlite"}
	in := `SELECT * FROM x WHERE id = ?`
	if got := d.rebind(in); got != in {
		t.Errorf("sqlite path should not rewrite; got %q", got)
	}
}

func TestRebindPostgres_PostgresApplies(t *testing.T) {
	d := &DB{driver: "postgres"}
	in := `SELECT * FROM x WHERE id = ?`
	want := `SELECT * FROM x WHERE id = $1`
	if got := d.rebind(in); got != want {
		t.Errorf("postgres path should rewrite\n  got:  %q\n  want: %q", got, want)
	}
}
