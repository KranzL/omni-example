package judge

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const RubricsFile = "rubrics.yaml"

type Rubric struct {
	ID              string `yaml:"id"`
	CorrectAnswer   string `yaml:"correct_answer"`
	FailureCriteria string `yaml:"failure_criteria"`
	CloseButWrong   string `yaml:"close_but_wrong"`
}

func LoadRubrics(path string) (map[string]Rubric, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var list []Rubric
	if err := yaml.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	out := make(map[string]Rubric, len(list))
	for i, r := range list {
		if err := r.Validate(); err != nil {
			return nil, fmt.Errorf("rubric %d (%s): %w", i, r.ID, err)
		}
		if _, dup := out[r.ID]; dup {
			return nil, fmt.Errorf("duplicate rubric id %q", r.ID)
		}
		out[r.ID] = r
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no rubrics in %s", path)
	}
	return out, nil
}

func (r Rubric) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("empty id")
	}
	if strings.TrimSpace(r.CorrectAnswer) == "" {
		return fmt.Errorf("empty correct_answer")
	}
	if strings.TrimSpace(r.FailureCriteria) == "" {
		return fmt.Errorf("empty failure_criteria")
	}
	if strings.TrimSpace(r.CloseButWrong) == "" {
		return fmt.Errorf("empty close_but_wrong")
	}
	return nil
}

func FormatExpected(expected any) string {
	switch v := expected.(type) {
	case nil:
		return "(no pinned answer)"
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return fmt.Sprintf("%v", v)
	case []string:
		return strings.Join(v, "\n")
	case []any:
		lines := make([]string, len(v))
		for i, item := range v {
			lines[i] = FormatExpected(item)
		}
		return strings.Join(lines, "\n")
	case map[string]any:
		cols, _ := v["columns"].([]any)
		rows, _ := v["rows"].([]any)
		var sb strings.Builder
		names := make([]string, len(cols))
		for i, c := range cols {
			names[i] = FormatExpected(c)
		}
		sb.WriteString(strings.Join(names, " | "))
		for _, r := range rows {
			cells, _ := r.([]any)
			line := make([]string, len(cells))
			for i, c := range cells {
				line[i] = FormatExpected(c)
			}
			sb.WriteString("\n" + strings.Join(line, " | "))
		}
		return sb.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}
