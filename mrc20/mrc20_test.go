package mrc20

import (
	"encoding/json"
	"testing"
)

func TestFlexibleString_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "string value",
			input:    `{"amount": "100"}`,
			expected: "100",
			wantErr:  false,
		},
		{
			name:     "integer value",
			input:    `{"amount": 100}`,
			expected: "100",
			wantErr:  false,
		},
		{
			name:     "float value",
			input:    `{"amount": 100.5}`,
			expected: "100.5",
			wantErr:  false,
		},
		{
			name:     "large integer",
			input:    `{"amount": 1000000000000}`,
			expected: "1000000000000",
			wantErr:  false,
		},
		{
			name:     "string with decimals",
			input:    `{"amount": "123.456789"}`,
			expected: "123.456789",
			wantErr:  false,
		},
		{
			name:     "empty string",
			input:    `{"amount": ""}`,
			expected: "",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var data struct {
				Amount FlexibleString `json:"amount"`
			}
			err := json.Unmarshal([]byte(tt.input), &data)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if string(data.Amount) != tt.expected {
				t.Errorf("UnmarshalJSON() = %v, want %v", string(data.Amount), tt.expected)
			}
		})
	}
}

func TestMrc20ArrivalData_UnmarshalJSON(t *testing.T) {
	// 测试实际的 Arrival JSON 数据
	tests := []struct {
		name    string
		input   string
		wantAmt string
		wantErr bool
	}{
		{
			name: "amount as number",
			input: `{
				"assetOutpoint": "ca5f09e8953dc484a6750ed4f24d2be3a5240e1026a3c000787a400b98ecbffc:1",
				"amount": 100,
				"tickId": "2a69cbf5283f771bf38d525f85a32eb454d308ebeb6279c5afb445dae1387f72i0",
				"locationIndex": 0,
				"metadata": ""
			}`,
			wantAmt: "100",
			wantErr: false,
		},
		{
			name: "amount as string",
			input: `{
				"assetOutpoint": "ca5f09e8953dc484a6750ed4f24d2be3a5240e1026a3c000787a400b98ecbffc:1",
				"amount": "100",
				"tickId": "2a69cbf5283f771bf38d525f85a32eb454d308ebeb6279c5afb445dae1387f72i0",
				"locationIndex": 0,
				"metadata": ""
			}`,
			wantAmt: "100",
			wantErr: false,
		},
		{
			name: "amount as decimal string",
			input: `{
				"assetOutpoint": "test:0",
				"amount": "1000.123456",
				"tickId": "test",
				"locationIndex": 0
			}`,
			wantAmt: "1000.123456",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var data Mrc20ArrivalData
			err := json.Unmarshal([]byte(tt.input), &data)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unmarshal Mrc20ArrivalData error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if string(data.Amount) != tt.wantAmt {
				t.Errorf("Unmarshal Mrc20ArrivalData Amount = %v, want %v", string(data.Amount), tt.wantAmt)
			}
		})
	}
}
