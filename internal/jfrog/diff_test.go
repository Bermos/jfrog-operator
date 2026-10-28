package jfrog

import (
	"reflect"
	"testing"
)

func TestDiff(t *testing.T) {
	tests := []struct {
		name     string
		desired  any
		observed any
		want     []string
	}{
		{
			name:     "equal",
			desired:  map[string]any{"a": "x", "n": 1},
			observed: map[string]any{"a": "x", "n": 1.0},
		},
		{
			name:     "extra observed fields are ignored",
			desired:  map[string]any{"a": "x"},
			observed: map[string]any{"a": "x", "b": "y"},
		},
		{
			name:     "changed scalar",
			desired:  map[string]any{"a": "x", "b": true},
			observed: map[string]any{"a": "y", "b": true},
			want:     []string{"a"},
		},
		{
			name:     "missing field",
			desired:  map[string]any{"a": "x"},
			observed: map[string]any{},
			want:     []string{"a"},
		},
		{
			name:     "empty equals missing",
			desired:  map[string]any{"a": "", "l": []string{}},
			observed: map[string]any{},
		},
		{
			name:     "nested",
			desired:  map[string]any{"o": map[string]any{"a": 1, "b": map[string]any{"c": "d"}}},
			observed: map[string]any{"o": map[string]any{"a": 1, "b": map[string]any{"c": "e"}, "z": 1}},
			want:     []string{"o.b.c"},
		},
		{
			name:     "nested object missing",
			desired:  map[string]any{"o": map[string]any{"a": 1}},
			observed: map[string]any{"o": "scalar"},
			want:     []string{"o"},
		},
		{
			name:     "list order matters",
			desired:  map[string]any{"l": []string{"a", "b"}},
			observed: map[string]any{"l": []any{"b", "a"}},
			want:     []string{"l[0]", "l[1]"},
		},
		{
			name:     "list length differs",
			desired:  map[string]any{"l": []string{"a"}},
			observed: map[string]any{"l": []any{"a", "b"}},
			want:     []string{"l"},
		},
		{
			name:     "list elements may carry extra fields",
			desired:  map[string]any{"l": []any{map[string]any{"name": "x"}}},
			observed: map[string]any{"l": []any{map[string]any{"name": "x", "added": true}}},
		},
		{
			name:     "list element differs",
			desired:  map[string]any{"l": []any{map[string]any{"name": "x"}, map[string]any{"name": "y"}}},
			observed: map[string]any{"l": []any{map[string]any{"name": "x"}, map[string]any{"name": "z"}}},
			want:     []string{"l[1].name"},
		},
		{
			name:     "list against scalar",
			desired:  map[string]any{"l": []string{"a"}},
			observed: map[string]any{"l": "a"},
			want:     []string{"l"},
		},
		{
			name:     "typed structs",
			desired:  struct{ A string }{"x"},
			observed: map[string]any{"A": "x"},
		},
		{
			name:     "multiple sorted",
			desired:  map[string]any{"z": 1, "a": 1},
			observed: map[string]any{"z": 2, "a": 2},
			want:     []string{"a", "z"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Diff(tt.desired, tt.observed)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Diff = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDiffUnmarshalable(t *testing.T) {
	if _, err := Diff(map[string]any{"f": func() {}}, nil); err == nil {
		t.Fatal("want error for values that cannot be encoded")
	}
}
