package activities

import "testing"

func TestExtractFirstJSONResponse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain JSON object",
			input: `{"type":"clarification","message":"hello"}`,
			want:  `{"type":"clarification","message":"hello"}`,
		},
		{
			name:  "JSON with leading whitespace",
			input: `  {"type":"workflow_config","strategy":"ads_research"}`,
			want:  `{"type":"workflow_config","strategy":"ads_research"}`,
		},
		{
			name:  "markdown fenced JSON",
			input: "```json\n{\"type\":\"clarification\",\"message\":\"test\"}\n```",
			want:  `{"type":"clarification","message":"test"}`,
		},
		{
			name:  "markdown fence without json tag",
			input: "```\n{\"key\":\"value\"}\n```",
			want:  `{"key":"value"}`,
		},
		{
			name:  "prose before JSON",
			input: "Here is the extracted JSON:\n{\"type\":\"clarification\",\"message\":\"which keywords?\"}",
			want:  `{"type":"clarification","message":"which keywords?"}`,
		},
		{
			name:  "prose + markdown fenced JSON",
			input: "I found the JSON object:\n```json\n{\"type\":\"workflow_config\",\"cron\":\"0 6 * * 1-5\"}\n```",
			want:  `{"type":"workflow_config","cron":"0 6 * * 1-5"}`,
		},
		{
			name:  "nested objects",
			input: `{"outer":{"inner":{"deep":"value"}},"list":[1,2,3]}`,
			want:  `{"outer":{"inner":{"deep":"value"}},"list":[1,2,3]}`,
		},
		{
			name:  "braces inside string values",
			input: `{"msg":"hello}world{test","ok":true}`,
			want:  `{"msg":"hello}world{test","ok":true}`,
		},
		{
			name:  "escaped quotes inside strings",
			input: `{"msg":"she said \"hello\"","ok":true}`,
			want:  `{"msg":"she said \"hello\"","ok":true}`,
		},
		{
			name:  "JSON array",
			input: `[{"a":1},{"b":2}]`,
			want:  `[{"a":1},{"b":2}]`,
		},
		{
			name:  "no JSON found",
			input: "This is just plain text with no JSON",
			want:  "",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace only",
			input: "   \n\n  ",
			want:  "",
		},
		{
			name:  "unclosed JSON",
			input: `{"type":"clarification","message":"incomplete`,
			want:  "",
		},
		{
			name:  "Japanese content in JSON",
			input: `{"type":"clarification","message":"キーワードを教えてください"}`,
			want:  `{"type":"clarification","message":"キーワードを教えてください"}`,
		},
		{
			name:  "prose with Japanese + fenced JSON",
			input: "ご質問ありがとうございます。\n```json\n{\"type\":\"clarification\",\"message\":\"URLを教えてください\"}\n```",
			want:  `{"type":"clarification","message":"URLを教えてください"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFirstJSONResponse(tt.input)
			if got != tt.want {
				t.Errorf("extractFirstJSONResponse()\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}
