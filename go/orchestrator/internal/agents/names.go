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
// 220-239: Reserved for vendor extensions
// 240:     React synthesizer
// 250:     DAG synthesis
// 251:     Supervisor synthesis
// 252:     Streaming synthesis
// 300+:    Swarm dynamically spawned agents (300+i)
const (
	IdxSynthesis             = 200
	IdxIntermediateSynthesis = 201
	IdxResearchRefiner       = 210
	IdxCoverageEvaluator     = 211
	IdxSubqueryGenerator     = 212
	IdxFactExtraction        = 213
	IdxEntityLocalization    = 214
	IdxFallbackSearch        = 215
	IdxReactSynthesizer = 240
	IdxDAGSynthesis          = 250
	IdxSupervisorSynthesis   = 251
	IdxStreamingSynthesis    = 252
	IdxDomainPrefetchBase    = 100 // Use 100+i for domain prefetch
	IdxSwarmDynamicBase      = 300 // Use 300+i for dynamically spawned swarm agents
)

// stationNames is the pool of Japanese station-inspired agent names.
// The list is fixed to maintain determinism for workflow replays.
var stationNames = []string{
	// Classics with proper romanization
	"Ōme", "Gora", "Maji", "Ebisu", "Ōsaki",
	"Otaru", "Namba", "Tenma", "Mejiro", "Kōenji",
	"Gotanda", "Ryōgoku", "Yūtenji", "Nippori", "Asagaya",
	"Mojikō", "Kottoi", "Taishō", "Yumoto", "Odawara",
	"Enoshima", "Ogikubo", "Ichigaya", "Komazawa", "Todoroki",
	// Quirky names
	"Obama", "Usa", "Gero", "Ōboke", "Koboke",
	"Naruto", "Zushi", "Fussa", "Oppama", "Pippu",
	"Mashike", "Zōshiki",
	// Remote & scenic gems
	"Nikkō", "Hakone", "Beppu", "Atami", "Wakkanai",
	"Koboro", "Shimonada", "Tadami", "Tsuwano", "Okutama",
	"Nagatoro", "Kazamatsuri", "Chōshi", "Kururi", "Biei",
	"Minobu", "Shimonita",
	// Saitama & West Tokyo deep cuts
	"Tama", "Musashi", "Urawa", "Kawagoe", "Hannō",
	"Chichibu", "Takao", "Mitaka", "Kichijōji",
	// Bonus obscure finds
	"Karasuyama", "Ashikaga", "Sasago", "Shimokita", "Kuragano",
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
