package formatting

import (
    "regexp"
    "sort"
    "strings"
)

// FormatReportWithCitations ensures that the final report contains a complete
// Sources section listing ALL available citations. It:
//  1) Parses inline citations used in the synthesis (e.g., [1], [2])
//  2) Removes any existing "## Sources" section from the synthesis
//  3) Appends a rebuilt Sources section from the provided citationsList
//     (one citation per line, already numbered), marking which were used inline
//
// citationsList is expected to be lines like: "[1] Title (URL) - Source, 2024-01-01"
func FormatReportWithCitations(synthesis string, citationsList string) string {
    s := strings.TrimSpace(synthesis)
    if s == "" {
        return synthesis
    }

    // 1) Collect used citation indices from inline markers like [1], [12]
    used := map[int]bool{}
    re := regexp.MustCompile(`\[(\d{1,3})\]`)
    for _, m := range re.FindAllStringSubmatch(s, -1) {
        if len(m) == 2 {
            // parse int safely
            var n int
            for i := 0; i < len(m[1]); i++ {
                n = n*10 + int(m[1][i]-'0')
            }
            if n > 0 {
                used[n] = true
            }
        }
    }

    // 2) Remove existing Sources section from synthesis
    //    Strategy: find the LAST occurrence of "## Sources" and truncate from there to end
    //    If not found, keep the synthesis as-is.
    //    Using last index avoids cutting content if the model references "## Sources" earlier in the body.
    cut := s
    lower := strings.ToLower(s)
    needle := strings.ToLower("## Sources")
    if idx := strings.LastIndex(lower, needle); idx != -1 {
        cut = strings.TrimSpace(s[:idx])
    }

    // 3) Build rebuilt Sources section from citationsList
    lines := strings.Split(strings.TrimSpace(citationsList), "\n")
    var rebuilt []string
    for _, ln := range lines {
        t := strings.TrimSpace(ln)
        if t == "" {
            continue
        }
        // Determine index from leading [n]
        idx := 0
        if m := re.FindStringSubmatch(t); len(m) == 2 {
            for i := 0; i < len(m[1]); i++ {
                idx = idx*10 + int(m[1][i]-'0')
            }
        }
        label := "Additional source"
        if used[idx] {
            label = "Used inline"
        }
        // Keep original line, append label
        rebuilt = append(rebuilt, t+" - "+label)
    }

    if len(rebuilt) == 0 {
        // Nothing to append
        return cut
    }

    // Keep lines ordered by their numeric index if possible
    sort.SliceStable(rebuilt, func(i, j int) bool {
        // Extract first number in each line
        first := func(s string) int {
            if m := re.FindStringSubmatch(s); len(m) == 2 {
                n := 0
                for i := 0; i < len(m[1]); i++ {
                    n = n*10 + int(m[1][i]-'0')
                }
                return n
            }
            return 0
        }
        return first(rebuilt[i]) < first(rebuilt[j])
    })

    var b strings.Builder
    if cut != "" {
        b.WriteString(strings.TrimRight(cut, "\n"))
        b.WriteString("\n\n")
    }
    b.WriteString("## Sources\n")
    b.WriteString(strings.Join(rebuilt, "\n"))
    return b.String()
}
