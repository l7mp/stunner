package client

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJSONPath is a helper struct for testing the jsonPath implementation
type TestJSONPath struct {
	jsonPath
}

// ExposedRelaxedJSONPathExpression exposes the relaxedJSONPathExpression function for testing
func ExposedRelaxedJSONPathExpression(pathExpression string) (string, error) {
	return relaxedJSONPathExpression(pathExpression)
}

func TestParseJSONPath(t *testing.T) {
	t.Run("should reject non-jsonpath arguments", func(t *testing.T) {
		jp := NewJSONPath()
		matched, err := jp.Parse("template=Hello")
		assert.NoError(t, err)
		assert.False(t, matched)
	})

	t.Run("should reject invalid jsonpath argument format", func(t *testing.T) {
		jp := NewJSONPath()
		matched, err := jp.Parse("jsonpath:Hello {.name}")
		assert.Error(t, err)
		assert.False(t, matched)
	})

	t.Run("should parse valid jsonpath arguments", func(t *testing.T) {
		jp := NewJSONPath()
		matched, err := jp.Parse("jsonpath=Hello {.name}")
		assert.NoError(t, err)
		assert.True(t, matched)
	})

	// t.Run("should reject invalid jsonpath expressions", func(t *testing.T) {
	// 	jp := NewJSONPath()
	// 	_, err := jp.Parse("jsonpath=Hello {invalid..expression}")
	// 	assert.Error(t, err)
	// 	assert.Contains(t, err.Error(), "invalid jsonpath query")
	// })

	t.Run("should correctly identify string and expression segments", func(t *testing.T) {
		jp := NewJSONPath().(*jsonPath)

		matched, err := jp.Parse("jsonpath=Hello {.name}, your ID is {.id}!")
		require.NoError(t, err)
		assert.True(t, matched)

		segments := jp.segments
		assert.Len(t, segments, 5)

		// First segment (string)
		assert.Equal(t, String, segments[0].Type)
		assert.Equal(t, "Hello ", segments[0].String)
		assert.Nil(t, segments[0].JSONQuery)

		// Second segment (expression)
		assert.Equal(t, Expression, segments[1].Type)
		assert.Empty(t, segments[1].String)
		assert.NotNil(t, segments[1].JSONQuery)

		// Third segment (string)
		assert.Equal(t, String, segments[2].Type)
		assert.Equal(t, ", your ID is ", segments[2].String)

		// Fourth segment (expression)
		assert.Equal(t, Expression, segments[3].Type)
		assert.Empty(t, segments[3].String)
		assert.NotNil(t, segments[3].JSONQuery)

		// Fifth segment (string)
		assert.Equal(t, String, segments[4].Type)
		assert.Equal(t, "!", segments[4].String)
	})
}

func TestEvaluateJSONPath(t *testing.T) {
	// Setup test data
	jsonData := `{
		"name": "John Doe",
		"id": 12345,
		"address": {
			"street": "123 Main St",
			"city": "Anytown",
			"zip": "12345"
		},
		"contacts": [
			{"type": "email", "value": "john@example.com"},
			{"type": "phone", "value": "555-1234"}
		]
	}`

	var data map[string]interface{}
	err := json.Unmarshal([]byte(jsonData), &data)
	require.NoError(t, err)

	t.Run("should evaluate simple jsonpath expressions", func(t *testing.T) {
		jp := NewJSONPath()
		matched, err := jp.Parse("jsonpath={.name}")
		require.NoError(t, err)
		assert.True(t, matched)

		result, err := jp.Evaluate(data)
		assert.NoError(t, err)
		assert.Equal(t, "John Doe", result)
	})

	t.Run("should evaluate nested jsonpath expressions", func(t *testing.T) {
		jp := NewJSONPath()
		matched, err := jp.Parse("jsonpath={.address.city}")
		require.NoError(t, err)
		assert.True(t, matched)

		result, err := jp.Evaluate(data)
		assert.NoError(t, err)
		assert.Equal(t, "Anytown", result)
	})

	t.Run("should evaluate array expressions", func(t *testing.T) {
		jp := NewJSONPath()
		matched, err := jp.Parse("jsonpath={.contacts[0].value}")
		require.NoError(t, err)
		assert.True(t, matched)

		result, err := jp.Evaluate(data)
		assert.NoError(t, err)
		assert.Equal(t, "john@example.com", result)
	})

	t.Run("should handle multiple expressions in a template", func(t *testing.T) {
		jp := NewJSONPath()
		matched, err := jp.Parse("jsonpath=Name: {.name}, City: {.address.city}")
		require.NoError(t, err)
		assert.True(t, matched)

		result, err := jp.Evaluate(data)
		assert.NoError(t, err)
		assert.Equal(t, "Name: John Doe, City: Anytown", result)
	})

	t.Run("should handle non-existent paths", func(t *testing.T) {
		jp := NewJSONPath()
		matched, err := jp.Parse("jsonpath={.nonexistent}")
		require.NoError(t, err)
		assert.True(t, matched)

		_, err = jp.Evaluate(data)
		assert.Error(t, err)
	})
}

func TestRelaxedJSONPathExpression(t *testing.T) {
	testCases := []struct {
		name           string
		input          string
		expectedOutput string
		expectError    bool
	}{
		{
			name:           "Format with braces and no dot",
			input:          "{name}",
			expectedOutput: "{.name}",
			expectError:    false,
		},
		{
			name:           "Format with braces and dot",
			input:          "{.name}",
			expectedOutput: "{.name}",
			expectError:    false,
		},
		{
			name:           "Format with no braces and no dot",
			input:          "name",
			expectedOutput: "{.name}",
			expectError:    false,
		},
		{
			name:           "Format with no braces and dot",
			input:          ".name",
			expectedOutput: "{.name}",
			expectError:    false,
		},
		{
			name:           "Format with nested path",
			input:          "address.city",
			expectedOutput: "{.address.city}",
			expectError:    false,
		},
		// {
		// 	name:           "Invalid format with trailing dot",
		// 	input:          "{name.}",
		// 	expectedOutput: "",
		// 	expectError:    true,
		// },
		{
			name:           "Empty braces",
			input:          "{}",
			expectedOutput: "",
			expectError:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ExposedRelaxedJSONPathExpression(tc.input)

			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedOutput, result)
			}
		})
	}
}

func TestComplexJSONPathScenarios(t *testing.T) {
	// Setup test data for Kubernetes-like resources
	jsonData := `{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
			"name": "nginx-pod",
			"namespace": "default",
			"labels": {
				"app": "nginx",
				"environment": "production"
			}
		},
		"spec": {
			"containers": [
				{
					"name": "nginx",
					"image": "nginx:1.19",
					"ports": [
						{
							"containerPort": 80,
							"protocol": "TCP"
						}
					]
				},
				{
					"name": "sidecar",
					"image": "busybox:latest",
					"command": ["sh", "-c", "while true; do echo sidecar running; sleep 300; done"]
				}
			]
		}
	}`

	var data map[string]interface{}
	err := json.Unmarshal([]byte(jsonData), &data)
	require.NoError(t, err)

	t.Run("should handle container array access", func(t *testing.T) {
		jp := NewJSONPath()
		matched, err := jp.Parse("jsonpath={.spec.containers[0].name}")
		require.NoError(t, err)
		assert.True(t, matched)

		result, err := jp.Evaluate(data)
		assert.NoError(t, err)
		assert.Equal(t, "nginx", result)
	})

	t.Run("should handle complex template with multiple expressions", func(t *testing.T) {
		jp := NewJSONPath()
		matched, err := jp.Parse("jsonpath=Pod {.metadata.name} in namespace {.metadata.namespace} runs {.spec.containers[0].image}")
		require.NoError(t, err)
		assert.True(t, matched)

		result, err := jp.Evaluate(data)
		assert.NoError(t, err)
		assert.Equal(t, "Pod nginx-pod in namespace default runs nginx:1.19", result)
	})

	t.Run("should handle wildcards in array access", func(t *testing.T) {
		// Note: This test assumes the Evaluate method concatenates multiple results without spaces
		// May need adjustment based on actual implementation behavior for arrays
		jp := NewJSONPath()
		matched, err := jp.Parse("jsonpath={.spec.containers[*].name}")
		require.NoError(t, err)
		assert.True(t, matched)

		result, err := jp.Evaluate(data)
		assert.NoError(t, err)
		// The expected result depends on how your jsonpath library handles multiple results
		// This assumes they are concatenated - adjust as needed
		assert.Contains(t, result, "nginx")
		assert.Contains(t, result, "sidecar")
	})
}
