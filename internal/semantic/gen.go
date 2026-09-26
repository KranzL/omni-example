package semantic

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const MaxGenEnumValues = 20

type GenOptions struct {
	Schema  string
	Dataset string
	Title   string
}

type genColumn struct {
	name    string
	ctype   string
	notnull bool
	pkOrder int
}

func GenerateSQLite(sqldb *sql.DB, opts GenOptions) (*Semantic, error) {
	names, err := genTableNames(sqldb)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no tables found")
	}
	s := &Semantic{
		Schema:  opts.Schema,
		Dataset: opts.Dataset,
		Title:   opts.Title,
	}
	if s.Schema == "" {
		s.Schema = "main"
	}
	known := map[string]bool{}
	for _, n := range names {
		known[n] = true
	}
	for _, name := range names {
		t, err := genTable(sqldb, name, known)
		if err != nil {
			return nil, err
		}
		s.Tables = append(s.Tables, t)
	}
	pruneForeignKeys(s.Tables)
	enums, err := genEnums(sqldb, s.Tables)
	if err != nil {
		return nil, err
	}
	s.Enums = enums
	paths := genPaths(s.Tables)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no foreign keys to build join paths from")
	}
	s.Paths = paths
	s.Synonyms = genSynonyms(s.Tables)
	s.Conventions = genConventions()
	s.Gotchas = genGotchas()
	s.Examples = genExamples(s.Tables)
	if err := s.Validate(); err != nil {
		return nil, err
	}
	s.indexEnums()
	return s, nil
}

func (s *Semantic) ToYAML() ([]byte, error) {
	return yaml.Marshal(s)
}

func genTableNames(sqldb *sql.DB) ([]string, error) {
	rows, err := sqldb.Query("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func genTable(sqldb *sql.DB, name string, known map[string]bool) (Table, error) {
	pragmaRows, err := sqldb.Query(fmt.Sprintf("PRAGMA table_info(%s)", quoteIdent(name)))
	if err != nil {
		return Table{}, err
	}
	var cols []genColumn
	for pragmaRows.Next() {
		var cid int
		var cname, ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := pragmaRows.Scan(&cid, &cname, &ctype, &notnull, &dflt, &pk); err != nil {
			pragmaRows.Close()
			return Table{}, err
		}
		cols = append(cols, genColumn{name: cname, ctype: ctype, notnull: notnull == 1, pkOrder: pk})
	}
	pragmaRows.Close()
	if err := pragmaRows.Err(); err != nil {
		return Table{}, err
	}
	if len(cols) == 0 {
		return Table{}, fmt.Errorf("table %q has no columns", name)
	}
	t := Table{
		Name:       name,
		Purpose:    fmt.Sprintf("The %s table of the database.", name),
		Grain:      fmt.Sprintf("One row per %s record.", name),
		PrimaryKey: genPrimaryKey(cols),
	}
	for _, c := range cols {
		typ := strings.TrimSpace(c.ctype)
		if typ == "" {
			typ = "ANY"
		}
		desc := fmt.Sprintf("The %s column (%s).", c.name, typ)
		if c.pkOrder > 0 {
			desc += " Part of the primary key."
		} else if c.notnull {
			desc += " Never null."
		}
		t.Columns = append(t.Columns, Column{Name: c.name, Type: typ, Description: desc})
	}
	var approx int
	if err := sqldb.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteIdent(name))).Scan(&approx); err != nil {
		return Table{}, err
	}
	t.ApproxRows = approx
	fks, err := genForeignKeys(sqldb, name, known)
	if err != nil {
		return Table{}, err
	}
	t.ForeignKeys = fks
	return t, nil
}

func pruneForeignKeys(tables []Table) {
	byName := map[string]Table{}
	for _, t := range tables {
		byName[t.Name] = t
	}
	for i, t := range tables {
		seen := map[string]bool{}
		var kept []ForeignKey
		for _, fk := range t.ForeignKeys {
			refTable, _, _ := strings.Cut(fk.References, ".")
			if seen[fk.Column] || !byNameHasColumn(byName, refTable, fk.References) {
				continue
			}
			seen[fk.Column] = true
			kept = append(kept, fk)
		}
		tables[i].ForeignKeys = kept
	}
}

func genPrimaryKey(cols []genColumn) string {
	best := ""
	bestOrder := 0
	for _, c := range cols {
		if c.pkOrder > 0 && (best == "" || c.pkOrder < bestOrder) {
			best, bestOrder = c.name, c.pkOrder
		}
	}
	if best != "" {
		return best
	}
	for _, c := range cols {
		if strings.EqualFold(c.name, "id") {
			return c.name
		}
	}
	for _, c := range cols {
		if strings.Contains(strings.ToUpper(c.ctype), "INT") {
			return c.name
		}
	}
	return cols[0].name
}

func genForeignKeys(sqldb *sql.DB, name string, known map[string]bool) ([]ForeignKey, error) {
	rows, err := sqldb.Query(fmt.Sprintf("PRAGMA foreign_key_list(%s)", quoteIdent(name)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ForeignKey
	seen := map[string]bool{}
	for rows.Next() {
		var id, seq int
		var refTable, from, to string
		var onUpdate, onDelete, match string
		if err := rows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			return nil, err
		}
		if !known[refTable] || from == "" || to == "" || seen[from] {
			continue
		}
		seen[from] = true
		out = append(out, ForeignKey{Column: from, References: refTable + "." + to})
	}
	return out, rows.Err()
}

func genEnums(sqldb *sql.DB, tables []Table) ([]Enum, error) {
	var out []Enum
	for _, t := range tables {
		for _, c := range t.Columns {
			vals, ok, err := genColumnValues(sqldb, t.Name, c.Name)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			out = append(out, Enum{Column: t.Name + "." + c.Name, Values: vals})
		}
	}
	return out, nil
}

func genColumnValues(sqldb *sql.DB, table, column string) ([]string, bool, error) {
	var distinct int
	q := fmt.Sprintf("SELECT COUNT(DISTINCT %s) FROM %s", quoteIdent(column), quoteIdent(table))
	if err := sqldb.QueryRow(q).Scan(&distinct); err != nil {
		return nil, false, err
	}
	if distinct == 0 || distinct > MaxGenEnumValues {
		return nil, false, nil
	}
	rows, err := sqldb.Query(fmt.Sprintf("SELECT DISTINCT %s FROM %s WHERE %s IS NOT NULL", quoteIdent(column), quoteIdent(table), quoteIdent(column)))
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var v any
		if err := rows.Scan(&v); err != nil {
			return nil, false, err
		}
		text := genValueText(v)
		if strings.TrimSpace(text) == "" || seen[text] {
			continue
		}
		seen[text] = true
		out = append(out, text)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(out) == 0 {
		return nil, false, nil
	}
	sort.Strings(out)
	return out, true, nil
}

func genValueText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	case time.Time:
		return t.Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprintf("%v", v)
	}
}

func genPaths(tables []Table) []Path {
	byName := map[string]Table{}
	for _, t := range tables {
		byName[t.Name] = t
	}
	seen := map[string]bool{}
	var out []Path
	for _, t := range tables {
		for _, fk := range t.ForeignKeys {
			refTable, _, _ := strings.Cut(fk.References, ".")
			if !byNameHasColumn(byName, refTable, fk.References) {
				continue
			}
			name := t.Name + "-to-" + refTable
			if seen[name] {
				name = t.Name + "-to-" + refTable + "-on-" + fk.Column
			}
			seen[name] = true
			out = append(out, Path{
				Name:        name,
				Description: fmt.Sprintf("Join %s to %s.", t.Name, refTable),
				Grain:       fmt.Sprintf("One row per %s row.", t.Name),
				Steps:       []PathStep{{From: t.Name, Join: refTable, On: t.Name + "." + fk.Column + " = " + fk.References}},
			})
		}
	}
	return out
}

func byNameHasColumn(byName map[string]Table, refTable, references string) bool {
	_, refCol, ok := strings.Cut(references, ".")
	if !ok {
		return false
	}
	t, ok := byName[refTable]
	if !ok {
		return false
	}
	return hasColumn(t, refCol)
}

func genSynonyms(tables []Table) []Synonym {
	seen := map[string]bool{}
	var out []Synonym
	for _, t := range tables {
		term := humanizeName(t.Name)
		if seen[term] {
			term = term + " table"
		}
		seen[term] = true
		out = append(out, Synonym{Term: term, RefersTo: t.Name})
	}
	return out
}

func humanizeName(name string) string {
	withSpaces := strings.ReplaceAll(name, "_", " ")
	var b strings.Builder
	for i, r := range withSpaces {
		if i > 0 && r >= 'A' && r <= 'Z' && withSpaces[i-1] != ' ' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	out := strings.Join(strings.Fields(strings.ToLower(b.String())), " ")
	if out == "" {
		return name
	}
	return out
}

func genConventions() []string {
	return []string{
		"This database is SQLite. Use SQLite syntax and functions: STRFTIME for dates, || for string concatenation, CAST(x AS FLOAT) for ratios.",
		"Query read-only with one SELECT or WITH statement and no trailing semicolon.",
		"Quote identifiers that contain spaces or special characters with double quotes.",
		"Copy ON clauses from the join paths verbatim.",
	}
}

func genGotchas() []string {
	return []string{
		"Integer division truncates: CAST one side to FLOAT before dividing.",
		"Some columns store dates as TEXT; compare them as strings in YYYY-MM-DD form.",
		"NULL values never equal anything; use IS NULL or IS NOT NULL to test them.",
	}
}

func genExamples(tables []Table) []Example {
	biggest := tables[0].Name
	most := -1
	for _, t := range tables {
		if t.ApproxRows > most {
			biggest, most = t.Name, t.ApproxRows
		}
	}
	return []Example{{
		Question: fmt.Sprintf("How many rows are in %s?", biggest),
		SQL:      fmt.Sprintf("SELECT COUNT(*) FROM %s", biggest),
	}}
}
