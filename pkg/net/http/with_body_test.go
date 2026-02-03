// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package http

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatErrorFieldName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "extracts field name from namespace",
			input:    "CreateRequest.field_name",
			expected: "field_name",
		},
		{
			name:     "extracts after first dot for nested",
			input:    "Request.Parent.child_field",
			expected: "Parent.child_field",
		},
		{
			name:     "returns original if no dot",
			input:    "simple_field",
			expected: "simple_field",
		},
		{
			name:     "handles empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatErrorFieldName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindUnknownFields(t *testing.T) {
	tests := []struct {
		name      string
		original  map[string]any
		marshaled map[string]any
		expected  map[string]any
	}{
		{
			name:      "no differences",
			original:  map[string]any{"field1": "value1", "field2": "value2"},
			marshaled: map[string]any{"field1": "value1", "field2": "value2"},
			expected:  map[string]any{},
		},
		{
			name:      "unknown field in original",
			original:  map[string]any{"field1": "value1", "unknown": "value"},
			marshaled: map[string]any{"field1": "value1"},
			expected:  map[string]any{"unknown": "value"},
		},
		{
			name:      "empty maps",
			original:  map[string]any{},
			marshaled: map[string]any{},
			expected:  map[string]any{},
		},
		{
			name:      "ignores zero float values",
			original:  map[string]any{"field1": "value1", "zero_field": 0.0},
			marshaled: map[string]any{"field1": "value1"},
			expected:  map[string]any{},
		},
		{
			name:      "detects different values",
			original:  map[string]any{"field1": "original"},
			marshaled: map[string]any{"field1": "changed"},
			expected:  map[string]any{"field1": "original"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findUnknownFields(tt.original, tt.marshaled)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindUnknownFields_NestedMaps(t *testing.T) {
	original := map[string]any{
		"parent": map[string]any{
			"known":   "value",
			"unknown": "extra",
		},
	}
	marshaled := map[string]any{
		"parent": map[string]any{
			"known": "value",
		},
	}

	result := findUnknownFields(original, marshaled)

	assert.Contains(t, result, "parent")
	nestedDiff, ok := result["parent"].(map[string]any)
	require.True(t, ok, "expected parent to be map[string]any")
	assert.Contains(t, nestedDiff, "unknown")
}

func TestFindUnknownFields_Arrays(t *testing.T) {
	original := map[string]any{
		"items": []any{"item1", "item2", "item3"},
	}
	marshaled := map[string]any{
		"items": []any{"item1", "item2"},
	}

	result := findUnknownFields(original, marshaled)

	assert.Contains(t, result, "items")
}

func TestCompareSlices(t *testing.T) {
	tests := []struct {
		name      string
		original  []any
		marshaled []any
		hasDiff   bool
	}{
		{
			name:      "identical slices",
			original:  []any{"a", "b", "c"},
			marshaled: []any{"a", "b", "c"},
			hasDiff:   false,
		},
		{
			name:      "original longer",
			original:  []any{"a", "b", "c"},
			marshaled: []any{"a", "b"},
			hasDiff:   true,
		},
		{
			name:      "marshaled longer",
			original:  []any{"a"},
			marshaled: []any{"a", "b", "c"},
			hasDiff:   true,
		},
		{
			name:      "different values",
			original:  []any{"a", "x"},
			marshaled: []any{"a", "b"},
			hasDiff:   true,
		},
		{
			name:      "empty slices",
			original:  []any{},
			marshaled: []any{},
			hasDiff:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compareSlices(tt.original, tt.marshaled)
			if tt.hasDiff {
				assert.NotEmpty(t, result)
			} else {
				assert.Empty(t, result)
			}
		})
	}
}

func TestGetValidator(t *testing.T) {
	t.Run("returns validator instance", func(t *testing.T) {
		v, trans, err := getValidator()

		assert.NoError(t, err)
		assert.NotNil(t, v)
		assert.NotNil(t, trans)
	})

	t.Run("returns same instance on multiple calls", func(t *testing.T) {
		v1, _, _ := getValidator()
		v2, _, _ := getValidator()

		assert.Same(t, v1, v2)
	})
}

func TestValidateStruct(t *testing.T) {
	type TestStruct struct {
		Name  string `json:"name" validate:"required"`
		Email string `json:"email" validate:"required,email"`
	}

	tests := []struct {
		name        string
		input       any
		expectError bool
	}{
		{
			name: "valid struct",
			input: &TestStruct{
				Name:  "John",
				Email: "john@example.com",
			},
			expectError: false,
		},
		{
			name: "missing required field",
			input: &TestStruct{
				Name:  "",
				Email: "john@example.com",
			},
			expectError: true,
		},
		{
			name: "invalid email",
			input: &TestStruct{
				Name:  "John",
				Email: "invalid-email",
			},
			expectError: true,
		},
		{
			name:        "non-struct input",
			input:       "string value",
			expectError: false,
		},
		{
			name:        "nil pointer",
			input:       (*TestStruct)(nil),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStruct(tt.input)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestWrapJSONError(t *testing.T) {
	t.Run("wraps generic error", func(t *testing.T) {
		err := wrapJSONError(assert.AnError)
		assert.Contains(t, err.Error(), "invalid JSON")
	})
}

func TestNewOfType(t *testing.T) {
	type TestStruct struct {
		Field string
	}

	t.Run("creates new instance from pointer", func(t *testing.T) {
		source := &TestStruct{Field: "original"}
		result, err := newOfType(source)

		assert.NoError(t, err)
		assert.NotNil(t, result)

		resultStruct, ok := result.(*TestStruct)
		assert.True(t, ok)
		assert.Equal(t, "", resultStruct.Field) // New instance should have zero values
	})

	t.Run("returns error for non-pointer", func(t *testing.T) {
		source := TestStruct{Field: "value"}
		_, err := newOfType(source)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected pointer")
	})
}
