package jfrog

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

// Diff compares desired against observed and returns the paths of all fields
// set in desired that differ in observed. Fields only present in observed are
// ignored, because JFrog fills in defaults for everything left unset. Both
// values are normalised through JSON first so that e.g. int and float64
// compare equal. Nested objects are compared recursively. Lists must have the
// same length and order; their elements are compared recursively.
func Diff(desired, observed any) ([]string, error) {
	d, err := normalise(desired)
	if err != nil {
		return nil, err
	}
	o, err := normalise(observed)
	if err != nil {
		return nil, err
	}
	var paths []string
	diff("", d, o, &paths)
	sort.Strings(paths)
	return paths, nil
}

func normalise(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("normalising: %w", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("normalising: %w", err)
	}
	return out, nil
}

func diff(path string, desired, observed any, paths *[]string) {
	if dl, ok := desired.([]any); ok {
		ol, ok := observed.([]any)
		if !ok || len(dl) != len(ol) {
			if !isEmpty(desired) || !isEmpty(observed) {
				*paths = append(*paths, path)
			}
			return
		}
		// Lists are compared element by element, so that objects in a list
		// may also carry fields the platform added.
		for i := range dl {
			diff(fmt.Sprintf("%s[%d]", path, i), dl[i], ol[i], paths)
		}
		return
	}
	dm, ok := desired.(map[string]any)
	if !ok {
		if !reflect.DeepEqual(desired, observed) && (!isEmpty(desired) || !isEmpty(observed)) {
			*paths = append(*paths, path)
		}
		return
	}
	om, ok := observed.(map[string]any)
	if !ok {
		*paths = append(*paths, path)
		return
	}
	for k, dv := range dm {
		p := k
		if path != "" {
			p = path + "." + k
		}
		diff(p, dv, om[k], paths)
	}
}

// isEmpty treats nil, "" and empty lists and objects as the same value, as
// JFrog APIs tend to omit empty fields.
func isEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}
