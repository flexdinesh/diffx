package reviewstore

import "testing"

func TestRebindNumbersPlaceholdersOutsideQuotesAndComments(t *testing.T) {
	for _, test := range []struct{ query, want string }{
		{`SELECT 1`, `SELECT 1`},
		{`SELECT a FROM t WHERE b=? AND c IN (?,?)`, `SELECT a FROM t WHERE b=$1 AND c IN ($2,$3)`},
		{`SELECT '?', "a?b", 'it''s ?' FROM t WHERE x=?`, `SELECT '?', "a?b", 'it''s ?' FROM t WHERE x=$1`},
		{"SELECT ? -- trailing ?\nFROM t WHERE y=?", "SELECT $1 -- trailing ?\nFROM t WHERE y=$2"},
	} {
		if got := rebind(test.query); got != test.want {
			t.Errorf("rebind(%q) = %q, want %q", test.query, got, test.want)
		}
	}
}
