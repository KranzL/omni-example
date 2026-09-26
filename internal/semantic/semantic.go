package semantic

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const FileName = "ecomm.yaml"

type Column struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
}

type ForeignKey struct {
	Column     string `yaml:"column"`
	References string `yaml:"references"`
}

type Table struct {
	Name        string       `yaml:"name"`
	Purpose     string       `yaml:"purpose"`
	Grain       string       `yaml:"grain"`
	ApproxRows  int          `yaml:"approx_rows"`
	PrimaryKey  string       `yaml:"primary_key"`
	ForeignKeys []ForeignKey `yaml:"foreign_keys"`
	Columns     []Column     `yaml:"columns"`
}

type Enum struct {
	Column string   `yaml:"column"`
	Values []string `yaml:"values"`
}

type Metric struct {
	Name         string `yaml:"name"`
	Description  string `yaml:"description"`
	StatusFilter string `yaml:"status_filter"`
	SQL          string `yaml:"sql"`
}

type Example struct {
	Question string `yaml:"question"`
	SQL      string `yaml:"sql"`
}

type PathStep struct {
	From string `yaml:"from"`
	Join string `yaml:"join"`
	On   string `yaml:"on"`
}

type Path struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Grain       string     `yaml:"grain"`
	Steps       []PathStep `yaml:"steps"`
}

type Synonym struct {
	Term     string `yaml:"term"`
	RefersTo string `yaml:"refers_to"`
}

type Semantic struct {
	Schema      string    `yaml:"schema"`
	Dataset     string    `yaml:"dataset"`
	Title       string    `yaml:"title,omitempty"`
	Tables      []Table   `yaml:"tables"`
	Enums       []Enum    `yaml:"enums"`
	Metrics     []Metric  `yaml:"metrics"`
	Paths       []Path    `yaml:"paths"`
	Synonyms    []Synonym `yaml:"synonyms"`
	Conventions []string  `yaml:"conventions"`
	Gotchas     []string  `yaml:"gotchas"`
	Examples    []Example `yaml:"examples"`

	enums map[string][]string
}

func Load(path string) (*Semantic, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func Parse(data []byte) (*Semantic, error) {
	var s Semantic
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	s.indexEnums()
	return &s, nil
}

func DefaultPath() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, "semantic", FileName)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("semantic/%s not found above %s", FileName, dir)
		}
		dir = parent
	}
}

func (s *Semantic) Validate() error {
	if strings.TrimSpace(s.Schema) == "" {
		return fmt.Errorf("schema is empty")
	}
	if len(s.Tables) == 0 {
		return fmt.Errorf("no tables defined")
	}
	byName := make(map[string]Table, len(s.Tables))
	for _, t := range s.Tables {
		if strings.TrimSpace(t.Name) == "" {
			return fmt.Errorf("table with empty name")
		}
		if _, dup := byName[t.Name]; dup {
			return fmt.Errorf("duplicate table %q", t.Name)
		}
		byName[t.Name] = t
	}
	for _, t := range s.Tables {
		if strings.TrimSpace(t.Purpose) == "" {
			return fmt.Errorf("table %q has empty purpose", t.Name)
		}
		if strings.TrimSpace(t.Grain) == "" {
			return fmt.Errorf("table %q has empty grain", t.Name)
		}
		if len(t.Columns) == 0 {
			return fmt.Errorf("table %q has no columns", t.Name)
		}
		cols := make(map[string]bool, len(t.Columns))
		for _, c := range t.Columns {
			if strings.TrimSpace(c.Name) == "" {
				return fmt.Errorf("table %q has a column with empty name", t.Name)
			}
			if cols[c.Name] {
				return fmt.Errorf("table %q has duplicate column %q", t.Name, c.Name)
			}
			cols[c.Name] = true
			if strings.TrimSpace(c.Type) == "" {
				return fmt.Errorf("column %s.%s has empty type", t.Name, c.Name)
			}
			if strings.TrimSpace(c.Description) == "" {
				return fmt.Errorf("column %s.%s has empty description", t.Name, c.Name)
			}
		}
		if !cols[t.PrimaryKey] {
			return fmt.Errorf("table %q primary key %q is not a column", t.Name, t.PrimaryKey)
		}
		seenFK := make(map[string]bool, len(t.ForeignKeys))
		for _, fk := range t.ForeignKeys {
			if !cols[fk.Column] {
				return fmt.Errorf("table %q foreign key column %q is not a column", t.Name, fk.Column)
			}
			if seenFK[fk.Column] {
				return fmt.Errorf("table %q has duplicate foreign key on %q", t.Name, fk.Column)
			}
			seenFK[fk.Column] = true
			refTable, refCol, ok := strings.Cut(fk.References, ".")
			if !ok || refTable == "" || refCol == "" {
				return fmt.Errorf("table %q foreign key %q has malformed references %q", t.Name, fk.Column, fk.References)
			}
			rt, ok := byName[refTable]
			if !ok {
				return fmt.Errorf("table %q foreign key %q references unknown table %q", t.Name, fk.Column, refTable)
			}
			found := false
			for _, c := range rt.Columns {
				if c.Name == refCol {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("table %q foreign key %q references unknown column %q", t.Name, fk.Column, fk.References)
			}
		}
	}
	seenEnum := make(map[string]bool, len(s.Enums))
	for _, e := range s.Enums {
		table, col, ok := strings.Cut(e.Column, ".")
		if !ok || table == "" || col == "" {
			return fmt.Errorf("enum has malformed column %q", e.Column)
		}
		t, ok := byName[table]
		if !ok {
			return fmt.Errorf("enum column %q references unknown table %q", e.Column, table)
		}
		found := false
		for _, c := range t.Columns {
			if c.Name == col {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("enum column %q does not exist", e.Column)
		}
		if seenEnum[e.Column] {
			return fmt.Errorf("duplicate enum for column %q", e.Column)
		}
		seenEnum[e.Column] = true
		if len(e.Values) == 0 {
			return fmt.Errorf("enum for column %q has no values", e.Column)
		}
		seenVal := make(map[string]bool, len(e.Values))
		for _, v := range e.Values {
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("enum for column %q has an empty value", e.Column)
			}
			if seenVal[v] {
				return fmt.Errorf("enum for column %q has duplicate value %q", e.Column, v)
			}
			seenVal[v] = true
		}
	}
	seenMetric := make(map[string]bool, len(s.Metrics))
	for _, m := range s.Metrics {
		if strings.TrimSpace(m.Name) == "" {
			return fmt.Errorf("metric with empty name")
		}
		if seenMetric[m.Name] {
			return fmt.Errorf("duplicate metric %q", m.Name)
		}
		seenMetric[m.Name] = true
		if strings.TrimSpace(m.Description) == "" {
			return fmt.Errorf("metric %q has empty description", m.Name)
		}
		if strings.TrimSpace(m.StatusFilter) == "" {
			return fmt.Errorf("metric %q has empty status filter", m.Name)
		}
		if strings.TrimSpace(m.SQL) == "" {
			return fmt.Errorf("metric %q has empty SQL", m.Name)
		}
	}
	if len(s.Paths) == 0 {
		return fmt.Errorf("no paths defined")
	}
	seenPath := make(map[string]bool, len(s.Paths))
	for _, p := range s.Paths {
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("path with empty name")
		}
		if seenPath[p.Name] {
			return fmt.Errorf("duplicate path %q", p.Name)
		}
		seenPath[p.Name] = true
		if strings.TrimSpace(p.Description) == "" {
			return fmt.Errorf("path %q has empty description", p.Name)
		}
		if strings.TrimSpace(p.Grain) == "" {
			return fmt.Errorf("path %q has empty grain", p.Name)
		}
		if len(p.Steps) == 0 {
			return fmt.Errorf("path %q has no steps", p.Name)
		}
		for i, st := range p.Steps {
			if _, ok := byName[st.From]; !ok {
				return fmt.Errorf("path %q step %d references unknown table %q", p.Name, i, st.From)
			}
			if _, ok := byName[st.Join]; !ok {
				return fmt.Errorf("path %q step %d references unknown table %q", p.Name, i, st.Join)
			}
			left, right, ok := strings.Cut(st.On, "=")
			if !ok {
				return fmt.Errorf("path %q step %d has malformed on %q", p.Name, i, st.On)
			}
			for _, side := range []string{strings.TrimSpace(left), strings.TrimSpace(right)} {
				table, col, ok := strings.Cut(side, ".")
				if !ok || table == "" || col == "" || strings.Contains(col, ".") {
					return fmt.Errorf("path %q step %d has malformed on %q", p.Name, i, st.On)
				}
				t, ok := byName[table]
				if !ok {
					return fmt.Errorf("path %q step %d references unknown table %q", p.Name, i, table)
				}
				if !hasColumn(t, col) {
					return fmt.Errorf("path %q step %d references unknown column %q", p.Name, i, side)
				}
			}
		}
	}
	if len(s.Synonyms) == 0 {
		return fmt.Errorf("no synonyms defined")
	}
	seenTerm := make(map[string]bool, len(s.Synonyms))
	for _, syn := range s.Synonyms {
		if strings.TrimSpace(syn.Term) == "" {
			return fmt.Errorf("synonym with empty term")
		}
		if seenTerm[syn.Term] {
			return fmt.Errorf("duplicate synonym %q", syn.Term)
		}
		seenTerm[syn.Term] = true
		if strings.TrimSpace(syn.RefersTo) == "" {
			return fmt.Errorf("synonym %q has empty refers_to", syn.Term)
		}
		if table, col, ok := strings.Cut(syn.RefersTo, "."); ok {
			t, ok := byName[table]
			if !ok {
				return fmt.Errorf("synonym %q references unknown table %q", syn.Term, table)
			}
			if !hasColumn(t, col) {
				return fmt.Errorf("synonym %q references unknown column %q", syn.Term, syn.RefersTo)
			}
		} else if _, ok := byName[syn.RefersTo]; !ok {
			return fmt.Errorf("synonym %q references unknown table %q", syn.Term, syn.RefersTo)
		}
	}
	if len(s.Conventions) == 0 {
		return fmt.Errorf("no conventions defined")
	}
	for i, c := range s.Conventions {
		if strings.TrimSpace(c) == "" {
			return fmt.Errorf("convention %d is empty", i)
		}
	}
	if len(s.Gotchas) == 0 {
		return fmt.Errorf("no gotchas defined")
	}
	for i, g := range s.Gotchas {
		if strings.TrimSpace(g) == "" {
			return fmt.Errorf("gotcha %d is empty", i)
		}
	}
	if len(s.Examples) == 0 {
		return fmt.Errorf("no examples defined")
	}
	for i, e := range s.Examples {
		if strings.TrimSpace(e.Question) == "" {
			return fmt.Errorf("example %d has empty question", i)
		}
		if strings.TrimSpace(e.SQL) == "" {
			return fmt.Errorf("example %d has empty SQL", i)
		}
	}
	return nil
}

func hasColumn(t Table, col string) bool {
	for _, c := range t.Columns {
		if c.Name == col {
			return true
		}
	}
	return false
}

func (s *Semantic) indexEnums() {
	s.enums = make(map[string][]string, len(s.Enums))
	for _, e := range s.Enums {
		vals := append([]string(nil), e.Values...)
		sort.Strings(vals)
		s.enums[e.Column] = vals
	}
}

const (
	LevelModel = "model"
	LevelTopic = "topic"
	LevelField = "field"
	LevelsAll  = "all"
)

type Levels struct {
	Model bool
	Topic bool
	Field bool
}

func AllLevels() Levels {
	return Levels{Model: true, Topic: true, Field: true}
}

func ParseLevels(spec string) (Levels, error) {
	trimmed := strings.TrimSpace(spec)
	if trimmed == "" || strings.ToLower(trimmed) == LevelsAll {
		return AllLevels(), nil
	}
	var out Levels
	for _, tok := range strings.Split(trimmed, ",") {
		switch strings.ToLower(strings.TrimSpace(tok)) {
		case LevelModel:
			out.Model = true
		case LevelTopic:
			out.Topic = true
		case LevelField:
			out.Field = true
		default:
			return Levels{}, fmt.Errorf("unknown context level %q, want a comma list of model, topic, field or all", strings.TrimSpace(tok))
		}
	}
	return out, nil
}

func (l Levels) IsAll() bool {
	return l.Model && l.Topic && l.Field
}

func (l Levels) Slug() string {
	var parts []string
	if l.Model {
		parts = append(parts, LevelModel)
	}
	if l.Topic {
		parts = append(parts, LevelTopic)
	}
	if l.Field {
		parts = append(parts, LevelField)
	}
	return strings.Join(parts, "-")
}

func (l Levels) String() string {
	if l.IsAll() {
		return LevelsAll
	}
	return strings.ReplaceAll(l.Slug(), "-", ",")
}

func (s *Semantic) sortedTables() []Table {
	tables := append([]Table(nil), s.Tables...)
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	return tables
}

func (s *Semantic) sortedMetrics() []Metric {
	metrics := append([]Metric(nil), s.Metrics...)
	sort.Slice(metrics, func(i, j int) bool { return metrics[i].Name < metrics[j].Name })
	return metrics
}

func (s *Semantic) sortedPaths() []Path {
	paths := append([]Path(nil), s.Paths...)
	sort.Slice(paths, func(i, j int) bool { return paths[i].Name < paths[j].Name })
	return paths
}

func (s *Semantic) sortedSynonyms() []Synonym {
	synonyms := append([]Synonym(nil), s.Synonyms...)
	sort.Slice(synonyms, func(i, j int) bool { return synonyms[i].Term < synonyms[j].Term })
	return synonyms
}

func (s *Semantic) sortedExamples() []Example {
	examples := append([]Example(nil), s.Examples...)
	sort.Slice(examples, func(i, j int) bool { return examples[i].Question < examples[j].Question })
	return examples
}

func (s *Semantic) renderTables(withField bool) string {
	tables := s.sortedTables()
	var b strings.Builder
	if strings.TrimSpace(s.Title) != "" {
		fmt.Fprintf(&b, "You answer questions about %s by writing SQL.\n\n", strings.TrimSpace(s.Title))
	} else {
		b.WriteString("You answer questions about an ecommerce database by writing SQL.\n\n")
	}
	fmt.Fprintf(&b, "Dataset: %s.\n", s.Dataset)
	fmt.Fprintf(&b, "Schema: %s. Query this schema only.\n", s.Schema)
	fmt.Fprintf(&b, "\nTables (%d)\n", len(tables))
	for _, t := range tables {
		fmt.Fprintf(&b, "\n## %s\n", t.Name)
		fmt.Fprintf(&b, "Purpose: %s\n", t.Purpose)
		fmt.Fprintf(&b, "Grain: %s\n", t.Grain)
		if t.ApproxRows > 0 {
			fmt.Fprintf(&b, "Rows: about %d.\n", t.ApproxRows)
		}
		fmt.Fprintf(&b, "Primary key: %s.\n", t.PrimaryKey)
		cols := append([]Column(nil), t.Columns...)
		sort.Slice(cols, func(i, j int) bool { return cols[i].Name < cols[j].Name })
		b.WriteString("Columns:\n")
		for _, c := range cols {
			if !withField {
				fmt.Fprintf(&b, "- %s (%s)\n", c.Name, c.Type)
				continue
			}
			fmt.Fprintf(&b, "- %s (%s): %s", c.Name, c.Type, c.Description)
			if vals, ok := s.enums[t.Name+"."+c.Name]; ok {
				fmt.Fprintf(&b, " Allowed values: %s.", strings.Join(vals, ", "))
			}
			b.WriteString("\n")
		}
		if len(t.ForeignKeys) > 0 {
			fks := append([]ForeignKey(nil), t.ForeignKeys...)
			sort.Slice(fks, func(i, j int) bool { return fks[i].Column < fks[j].Column })
			b.WriteString("Foreign keys:\n")
			for _, fk := range fks {
				fmt.Fprintf(&b, "- %s references %s.\n", fk.Column, fk.References)
			}
		}
	}
	return b.String()
}

func (s *Semantic) renderPaths() string {
	paths := s.sortedPaths()
	var b strings.Builder
	fmt.Fprintf(&b, "\nJoin paths (%d)\n", len(paths))
	b.WriteString("Copy the ON clauses verbatim. Each path states the grain of its result.\n")
	for _, p := range paths {
		fmt.Fprintf(&b, "\n## %s\n", p.Name)
		fmt.Fprintf(&b, "%s\n", p.Description)
		fmt.Fprintf(&b, "Grain: %s\n", p.Grain)
		for _, st := range p.Steps {
			fmt.Fprintf(&b, "- %s JOIN %s ON %s\n", st.From, st.Join, strings.TrimSpace(st.On))
		}
	}
	return b.String()
}

func (s *Semantic) renderSynonyms() string {
	synonyms := s.sortedSynonyms()
	var b strings.Builder
	fmt.Fprintf(&b, "\nSynonyms (%d)\n", len(synonyms))
	b.WriteString("Question words and the tables or columns they mean.\n")
	for _, syn := range synonyms {
		fmt.Fprintf(&b, "- %s means %s.\n", syn.Term, syn.RefersTo)
	}
	return b.String()
}

func (s *Semantic) renderMetrics() string {
	metrics := s.sortedMetrics()
	var b strings.Builder
	fmt.Fprintf(&b, "\nMetrics (%d)\n", len(metrics))
	b.WriteString("Use these definitions so every answer matches. Each entry states its status filter.\n")
	for _, m := range metrics {
		fmt.Fprintf(&b, "\n## %s\n", m.Name)
		fmt.Fprintf(&b, "%s\n", m.Description)
		fmt.Fprintf(&b, "Status filter: %s\n", m.StatusFilter)
		fmt.Fprintf(&b, "SQL: %s\n", strings.TrimSpace(m.SQL))
	}
	return b.String()
}

func (s *Semantic) renderConventions() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\nConventions (%d)\n", len(s.Conventions))
	for i, c := range s.Conventions {
		fmt.Fprintf(&b, "%d. %s\n", i+1, c)
	}
	return b.String()
}

func (s *Semantic) renderGotchas() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\nGotchas (%d)\n", len(s.Gotchas))
	for i, g := range s.Gotchas {
		fmt.Fprintf(&b, "%d. %s\n", i+1, g)
	}
	return b.String()
}

func (s *Semantic) renderExamples() string {
	examples := s.sortedExamples()
	var b strings.Builder
	fmt.Fprintf(&b, "\nWorked examples (%d)\n", len(examples))
	for _, e := range examples {
		fmt.Fprintf(&b, "\nQ: %s\n", e.Question)
		fmt.Fprintf(&b, "SQL: %s\n", strings.TrimSpace(e.SQL))
	}
	return b.String()
}

func (s *Semantic) Render() string {
	return s.RenderLevels(AllLevels())
}

func (s *Semantic) RenderLevels(l Levels) string {
	var b strings.Builder
	b.WriteString(s.renderTables(l.Field))
	if l.Topic {
		b.WriteString(s.renderPaths())
	}
	if l.Field {
		b.WriteString(s.renderSynonyms())
	}
	if l.Topic {
		b.WriteString(s.renderMetrics())
	}
	if l.Model {
		b.WriteString(s.renderConventions())
	}
	if l.Model {
		b.WriteString(s.renderGotchas())
	}
	if l.Topic {
		b.WriteString(s.renderExamples())
	}
	return b.String()
}
