package patterns

import "testing"

func TestIsTaskCompleteCJK(t *testing.T) {
	cases := []struct {
		name      string
		reasoning string
		want      bool
	}{
		// Chinese
		{"zh task complete", "信息已经足够，任务完成。", true},
		{"zh answer found", "已找到答案：特斯拉2023年营收为967.7亿美元。", true},
		{"zh problem solved", "问题已解决,无需进一步搜索。", true},
		{"zh in progress", "还需要搜索更多资料来验证这个数字。", false},
		{"zh forward-looking", "完成搜索后，我们再汇总结果。", false},
		{"zh not yet complete", "任务尚未完成，需要继续。", false},
		// Japanese
		{"ja complete", "調査は完了しました。", true},
		{"ja in progress", "さらに調査が必要です。", false},
		// Korean
		{"ko complete", "작업 완료되었습니다.", true},
		{"ko in progress", "추가 검색이 필요합니다.", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTaskCompleteCJK(tc.reasoning); got != tc.want {
				t.Fatalf("isTaskCompleteCJK(%q) = %v, want %v", tc.reasoning, got, tc.want)
			}
		})
	}
}

// Documents the gap this change fixes: the reasoner answers in the user's
// language, but the English-only phrase list cannot match CJK responses.
func TestIsTaskCompleteMissesCJK(t *testing.T) {
	if isTaskComplete("任务完成。") {
		t.Fatalf("isTaskComplete unexpectedly matched a Chinese response; English-only assumption changed?")
	}
	if !isTaskComplete("Task complete.") {
		t.Fatalf("isTaskComplete should still match English phrases")
	}
}

func TestAreSimilarCJK(t *testing.T) {
	cases := []struct {
		name   string
		s1, s2 string
		want   bool
	}{
		{
			"zh near-duplicate observations",
			"Action result: 特斯拉2023年营收为967.7亿美元，同比增长19%，主要来自汽车业务。",
			"Action result: 根据年报，特斯拉2023年营收为967.7亿美元，同比增长19%，主要来自汽车业务。",
			true,
		},
		{
			"zh different topics",
			"Action result: 特斯拉2023年营收为967.7亿美元，同比增长19%。",
			"Action result: 比亚迪发布了全新的电池技术，续航里程突破一千公里。",
			false,
		},
		{
			"identical strings",
			"Action result: 任务执行成功。",
			"Action result: 任务执行成功。",
			true,
		},
		{
			"en near-duplicate observations",
			"Action result: Tesla reported total revenues of 96.77 billion dollars for fiscal year 2023.",
			"Action result: Tesla reported total revenues of 96.77 billion dollars in fiscal year 2023.",
			true,
		},
		{
			"en different topics",
			"Action result: Tesla reported total revenues of 96.77 billion dollars for 2023.",
			"Action result: BYD announced a new battery technology with much longer range.",
			false,
		},
		{
			"length prefilter",
			"Action result: 特斯拉营收。",
			"Action result: 特斯拉2023年营收为967.7亿美元，同比增长19%，主要来自汽车业务，能源业务也在增长。",
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := areSimilarCJK(tc.s1, tc.s2); got != tc.want {
				t.Fatalf("areSimilarCJK(%q, %q) = %v, want %v", tc.s1, tc.s2, got, tc.want)
			}
		})
	}
}

// Documents the gap this change fixes: strings.Fields cannot tokenize CJK text
// (no spaces), so near-duplicate CJK observations collapse into single unequal
// tokens and never register as similar.
func TestAreSimilarMissesCJK(t *testing.T) {
	s1 := "Action result: 特斯拉2023年营收为967.7亿美元，同比增长19%。"
	s2 := "Action result: 根据年报，特斯拉2023年营收为967.7亿美元，同比增长19%。"
	if areSimilar(s1, s2) {
		t.Fatalf("areSimilar unexpectedly handled CJK; whitespace-tokenization assumption changed?")
	}
}

func TestSignificantTokens(t *testing.T) {
	tokens := significantTokens("特斯拉营收 Tesla revenue up 19%")

	// CJK character bigrams
	for _, want := range []string{"特斯", "斯拉", "拉营", "营收"} {
		if !tokens[want] {
			t.Fatalf("expected CJK bigram %q in tokens %v", want, tokens)
		}
	}
	// Alphabetic words longer than 3 runes, lowercased
	for _, want := range []string{"tesla", "revenue"} {
		if !tokens[want] {
			t.Fatalf("expected word %q in tokens %v", want, tokens)
		}
	}
	// Short words are skipped
	if tokens["up"] {
		t.Fatalf("short word %q should be skipped", "up")
	}
	// CJK runs are not glued across the alphabetic boundary
	if tokens["收t"] || tokens["收te"] {
		t.Fatalf("CJK bigrams must not cross script boundaries: %v", tokens)
	}
}
