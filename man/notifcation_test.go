package man

import (
	"testing"
)

func TestGetStringFromMap(t *testing.T) {
	tests := []struct {
		name     string
		dataMap  map[string]interface{}
		key      string
		expected string
	}{
		{
			name:     "string value",
			dataMap:  map[string]interface{}{"key": "value"},
			key:      "key",
			expected: "value",
		},
		{
			name:     "float64 value",
			dataMap:  map[string]interface{}{"isLike": float64(1)},
			key:      "isLike",
			expected: "1",
		},
		{
			name:     "int value",
			dataMap:  map[string]interface{}{"count": 42},
			key:      "count",
			expected: "42",
		},
		{
			name:     "bool true",
			dataMap:  map[string]interface{}{"flag": true},
			key:      "flag",
			expected: "1",
		},
		{
			name:     "bool false",
			dataMap:  map[string]interface{}{"flag": false},
			key:      "flag",
			expected: "0",
		},
		{
			name:     "missing key",
			dataMap:  map[string]interface{}{"other": "value"},
			key:      "key",
			expected: "",
		},
		{
			name:     "nil value",
			dataMap:  map[string]interface{}{"key": nil},
			key:      "key",
			expected: "",
		},
		{
			name:     "empty string",
			dataMap:  map[string]interface{}{"key": ""},
			key:      "key",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getStringFromMap(tt.dataMap, tt.key)
			if result != tt.expected {
				t.Errorf("getStringFromMap() = %v, want %v", result, tt.expected)
			}
		})
	}
}
