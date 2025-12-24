package agents

import "hash/fnv"

// Reserved agent index ranges to avoid collision:
// 0-49:    Main subtask agents (parallel/sequential/hybrid execution)
// 100-149: Domain prefetch agents
// 200:     Final synthesis agent
// 201:     Intermediate synthesis agent
// 210:     Research refiner
// 211:     Coverage evaluator
// 212:     Subquery generator
// 213:     Fact extraction
// 214:     Entity localization
// 215:     Fallback search
// 220:     Ads keyword extraction
// 221-229: Ads LP analysis (221+i)
// 230:     Ads synthesis
// 240:     React synthesizer
// 250:     DAG synthesis
// 251:     Supervisor synthesis
// 252:     Streaming synthesis
const (
	IdxSynthesis             = 200
	IdxIntermediateSynthesis = 201
	IdxResearchRefiner       = 210
	IdxCoverageEvaluator     = 211
	IdxSubqueryGenerator     = 212
	IdxFactExtraction        = 213
	IdxEntityLocalization    = 214
	IdxFallbackSearch        = 215
	IdxAdsKeywordExtraction  = 220
	IdxAdsLPAnalysisBase     = 221 // Use 221+i for LP analysis
	IdxAdsSynthesis          = 230
	IdxReactSynthesizer      = 240
	IdxDAGSynthesis          = 250
	IdxSupervisorSynthesis   = 251
	IdxStreamingSynthesis    = 252
	IdxDomainPrefetchBase    = 100 // Use 100+i for domain prefetch
)

// stationNames is the pool of Japanese station-inspired agent names.
// The list is fixed to maintain determinism for workflow replays.
var stationNames = []string{
	"Ome", "Gora", "Maji", "Ueno", "Ebisu",
	"Osaki", "Otaru", "Namba", "Tenma", "Mejiro",
	"Koenji", "Gotanda", "Ryogoku", "Yutenji", "Nippori",
	"Asagaya", "Mojiko", "Kottoi", "Taisho", "Yumoto",
	"Harajuku", "Shibuya", "Odawara", "Enoshima", "Ogikubo",
	"Ichigaya", "Komazawa", "Shinjuku", "Wakkanai", "Todoroki",
	"Obama", "Usa", "Gero", "Oboke", "Koboke",
	"Naruto", "Zushi", "Fussa", "Oppama",
	"Nikko", "Hakone", "Beppu", "Atami", "Ginza",
	"Akiba", "Kamakura", "Yokohama", "Nagasaki", "Sapporo",
	"Tama", "Musashi", "Omiya", "Urawa", "Kawagoe",
	"Hanno", "Chichibu", "Takao", "Mitaka", "Kichijoji",
}

// GetAgentName returns a deterministic agent name for a given workflow and index.
// This is safe for Temporal workflow replays: the same workflowID and index
// will always produce the same name.
func GetAgentName(workflowID string, index int) string {
	if len(stationNames) == 0 {
		return ""
	}

	hash := fnv32a(workflowID)
	nameIndex := (int(hash) + index) % len(stationNames)
	return stationNames[nameIndex]
}

func fnv32a(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}
