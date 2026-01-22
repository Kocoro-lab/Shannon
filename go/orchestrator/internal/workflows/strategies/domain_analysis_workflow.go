package strategies

import (
	"fmt"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/agents"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metadata"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/opts"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type DomainAnalysisInput struct {
	ParentWorkflowID     string
	CallerWorkflowID     string
	Query                string
	CanonicalName        string
	DisambiguationTerms  []string
	ResearchAreas        []string
	ResearchDimensions   []activities.ResearchDimension
	OfficialDomains      []string
	ExactQueries         []string
	TargetLanguages      []string
	LocalizationNeeded   bool
	PrefetchSubpageLimit int
	RequestedRegions     []string
	PlanHints            []string
	Context              map[string]interface{}
	UserID               string
	SessionID            string
	History              []Message
	DomainAnalysisMode   string
}

type DomainAnalysisCoverage struct {
	Domain string
	Role   string
	Region string
	Status string
}

type DomainAnalysisStats struct {
	PrefetchAttempted int
	PrefetchSucceeded int
	PrefetchFailed    int
	FailureReasons    map[string]int
	DigestTokensUsed  int
}

type DomainAnalysisResult struct {
	DigestMarkdown          string
	Citations               []metadata.Citation
	OfficialDomainsSelected []DomainAnalysisCoverage
	PrefetchURLs            []string
	Stats                   DomainAnalysisStats
}

// DomainAnalysisWorkflow runs official-domain discovery + prefetch and returns a compact digest.
func DomainAnalysisWorkflow(ctx workflow.Context, input DomainAnalysisInput) (DomainAnalysisResult, error) {
	logger := workflow.GetLogger(ctx)

	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 8 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	if strings.EqualFold(strings.TrimSpace(input.DomainAnalysisMode), "off") {
		logger.Info("Domain analysis disabled by mode")
		return DomainAnalysisResult{}, nil
	}

	if strings.TrimSpace(input.CanonicalName) == "" {
		logger.Info("Domain analysis skipped: canonical name is empty")
		return DomainAnalysisResult{}, nil
	}

	workflowID := input.ParentWorkflowID
	if workflowID == "" {
		workflowID = workflow.GetInfo(ctx).WorkflowExecution.ID
	}
	childWorkflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
	rootWorkflowID := input.ParentWorkflowID
	if rootWorkflowID == "" {
		rootWorkflowID = workflowID
	}

	baseContext := cloneContextMap(input.Context)
	if input.ParentWorkflowID != "" {
		baseContext["parent_workflow_id"] = input.ParentWorkflowID
	}
	if input.CallerWorkflowID != "" {
		baseContext["caller_workflow_id"] = input.CallerWorkflowID
	}
	baseContext["domain_analysis_workflow_id"] = childWorkflowID
	baseContext["query_type"] = "company"
	if input.CanonicalName != "" {
		baseContext["canonical_name"] = input.CanonicalName
	}
	if len(input.ResearchAreas) > 0 {
		baseContext["research_areas"] = input.ResearchAreas
	}
	if len(input.ResearchDimensions) > 0 {
		baseContext["research_dimensions"] = input.ResearchDimensions
	}
	if len(input.TargetLanguages) > 0 {
		baseContext["target_languages"] = input.TargetLanguages
	}
	if input.LocalizationNeeded {
		baseContext["localization_needed"] = input.LocalizationNeeded
	}
	if len(input.OfficialDomains) > 0 {
		baseContext["official_domains"] = input.OfficialDomains
	}
	if len(input.ExactQueries) > 0 {
		baseContext["exact_queries"] = input.ExactQueries
	}
	if len(input.DisambiguationTerms) > 0 {
		baseContext["disambiguation_terms"] = input.DisambiguationTerms
	}

	metaBase := map[string]interface{}{
		"root_workflow_id":            rootWorkflowID,
		"caller_workflow_id":          input.CallerWorkflowID,
		"domain_analysis_workflow_id": childWorkflowID,
	}

	requestedRegions := input.RequestedRegions
	if len(requestedRegions) == 0 {
		requestedRegions = prefetchRegionsFromContext(baseContext)
		if len(requestedRegions) == 0 {
			requestedRegions = prefetchRegionsFromQuery(input.Query)
		}
	}

	intent := BuildDomainAnalysisIntent(
		input.Query,
		input.ResearchAreas,
		requestedRegions,
		input.TargetLanguages,
		input.LocalizationNeeded,
	)

	prefetchDiscoverOnlyVersion := workflow.GetVersion(ctx, "domain_prefetch_discover_only_v1", workflow.DefaultVersion, 3)
	domainDiscoveryVersion := workflow.GetVersion(ctx, "domain_discovery_search_first_v1", workflow.DefaultVersion, 1)
	domainAnalysisVersion := workflow.GetVersion(ctx, "domain_analysis_v1", workflow.DefaultVersion, 1)

	originRegion := originPrefetchRegionFromTargetLanguages(input.TargetLanguages)

	maxPrefetch := 8
	if len(requestedRegions) > 0 || !intent.MultinationalDefault {
		maxPrefetch = 5
	}
	if v, ok := baseContext["domain_prefetch_max_urls"]; ok {
		switch t := v.(type) {
		case int:
			maxPrefetch = t
		case float64:
			maxPrefetch = int(t)
		}
	}
	if maxPrefetch < 1 {
		maxPrefetch = 1
	}
	if maxPrefetch > 15 {
		maxPrefetch = 15
	}

	var discoveryResult activities.AgentExecutionResult
	var allDiscovered []string
	var prefetchDomains []string
	var prefetchURLs []string
	discoveredBySearch := make(map[string][]string)

	if prefetchDiscoverOnlyVersion >= 1 && domainDiscoveryVersion >= 1 {
		// Discover-only mode: reset any refinement-provided domains.
		delete(baseContext, "official_domains")
		delete(baseContext, "official_domains_source")

		searches := buildDomainDiscoverySearches(
			input.CanonicalName,
			input.DisambiguationTerms,
			originRegion,
			requestedRegions,
			input.ResearchAreas,
			input.OfficialDomains,
		)

		globalQuery := buildCompanyDomainDiscoverySearchQuery(input.CanonicalName, input.DisambiguationTerms, "")
		if prefetchDiscoverOnlyVersion >= 3 && len(requestedRegions) == 0 {
			originLang := originRegionToDiscoveryLanguageCode(originRegion)
			primaryQuery := buildCompanyDomainDiscoverySearchQuery(input.CanonicalName, input.DisambiguationTerms, originLang)
			if strings.TrimSpace(primaryQuery) == "" {
				primaryQuery = globalQuery
			}
			var topicSearches []domainDiscoverySearch
			for _, s := range searches {
				if s.Key == "ir" || s.Key == "docs" || s.Key == "careers" || strings.HasPrefix(s.Key, "product_") {
					topicSearches = append(topicSearches, s)
				}
			}
			if strings.TrimSpace(primaryQuery) != "" {
				searches = []domainDiscoverySearch{{Key: "primary", Query: primaryQuery}}
				searches = append(searches, topicSearches...)
			}
		}

		if domainAnalysisVersion >= 1 && len(requestedRegions) == 0 && !intent.MultinationalDefault {
			var filteredSearches []domainDiscoverySearch
			for _, s := range searches {
				if s.Key == "ir" || s.Key == "docs" || s.Key == "careers" ||
					strings.HasPrefix(s.Key, "product_") ||
					s.Key == "primary" || s.Key == "global" ||
					s.Key == originRegion {
					filteredSearches = append(filteredSearches, s)
				}
			}
			if len(filteredSearches) > 0 {
				searches = filteredSearches
				logger.Info("Domain analysis: non-multinational filtering applied",
					"original_count", len(searches),
					"filtered_count", len(filteredSearches),
					"multinational", intent.MultinationalDefault,
				)
			}
		}

		searchDomainsFromResults := domainsFromWebSearchToolExecutionsAll
		if prefetchDiscoverOnlyVersion >= 2 {
			searchDomainsFromResults = func(toolExecs []activities.ToolExecution) []string {
				return domainsFromWebSearchToolExecutionsAllV2(toolExecs, input.CanonicalName)
			}
		}

		discoveryContext := map[string]interface{}{
			"user_id":    input.UserID,
			"session_id": input.SessionID,
			"model_tier": "small",
			"role":       "domain_discovery",
			"response_format": map[string]interface{}{
				"type": "json_object",
			},
		}
		if input.ParentWorkflowID != "" {
			discoveryContext["parent_workflow_id"] = input.ParentWorkflowID
		}
		if input.CallerWorkflowID != "" {
			discoveryContext["caller_workflow_id"] = input.CallerWorkflowID
		}

		var allQueries []string
		for _, s := range searches {
			allQueries = append(allQueries, s.Query)
		}

		discoveryQuery := fmt.Sprintf(
			"Find official domains for %q.\n\n"+
				"STEP 1: Execute web_search for each query:\n",
			input.CanonicalName,
		)
		for _, q := range allQueries {
			discoveryQuery += fmt.Sprintf("- %s\n", q)
		}
		discoveryQuery += "\n" +
			"STEP 2: After ALL searches complete, respond with ONLY this JSON:\n" +
			"{\"domains\":[\"domain1.com\",\"domain2.com\"]}\n\n" +
			"RULES:\n" +
			"- Include: corporate sites, IR sites (abc.xyz), parent company sites\n" +
			"- Exclude: login/accounts, store, support, third-party (wikipedia, linkedin)\n" +
			"- Strip www prefix, no paths\n" +
			"- Max 10 domains\n\n" +
			"CRITICAL: Your response must be ONLY the JSON object, nothing else.\n"

		discoveryQuery += fmt.Sprintf("\n=== RESEARCH FOCUS ===\n"+
			"Original query: %s\n"+
			"Find official domains most relevant to answering this query.\n",
			input.Query)

		if len(input.ResearchAreas) > 0 {
			focusCategories := classifyFocusCategories(input.ResearchAreas)
			if len(focusCategories) > 0 {
				discoveryQuery += fmt.Sprintf("Domain type hints: %s\n",
					strings.Join(focusCategories, ", "))
			}
		}

		discoveryErr := workflow.ExecuteActivity(ctx,
			"ExecuteAgent",
			activities.AgentExecutionInput{
				Query:     discoveryQuery,
				AgentID:   "domain_discovery",
				Context:   discoveryContext,
				Mode:      "standard",
				SessionID: input.SessionID,
				History:   convertHistoryForAgent(input.History),
				SuggestedTools: []string{
					"web_search",
				},
				ToolParameters: map[string]interface{}{
					"tool":        "web_search",
					"query":       allQueries[0],
					"max_results": 20,
				},
				ParentWorkflowID: input.ParentWorkflowID,
			},
		).Get(ctx, &discoveryResult)

		if discoveryErr != nil || !discoveryResult.Success {
			logger.Warn("Domain discovery batch search failed",
				"canonical_name", input.CanonicalName,
				"queries", allQueries,
				"error", discoveryErr,
				"agent_error", discoveryResult.Error,
			)
			if discoveryErr != nil && discoveryResult.Error == "" {
				discoveryResult.Error = discoveryErr.Error()
			}
			discoveryResult.Success = false
			persistAgentExecutionLocalWithMeta(
				ctx,
				workflowID,
				"domain_discovery",
				fmt.Sprintf("Domain discovery: %s (batch, %d queries)", input.CanonicalName, len(allQueries)),
				discoveryResult,
				mergeDomainAnalysisMeta(metaBase, map[string]interface{}{
					"phase":       "domain_discovery",
					"batch_mode":  true,
					"query_count": len(allQueries),
					"queries":     allQueries,
					"status":      "failed",
				}),
			)
		} else {
			persistAgentExecutionLocalWithMeta(
				ctx,
				workflowID,
				"domain_discovery",
				fmt.Sprintf("Domain discovery: %s (batch, %d queries)", input.CanonicalName, len(allQueries)),
				discoveryResult,
				mergeDomainAnalysisMeta(metaBase, map[string]interface{}{
					"phase":             "domain_discovery",
					"batch_mode":        true,
					"query_count":       len(allQueries),
					"queries":           allQueries,
					"child_workflow_id": childWorkflowID,
				}),
			)

			searchDomainsAll := searchDomainsFromResults(discoveryResult.ToolExecutions)
			llmDomains := domainsFromDiscoveryResponseV2(discoveryResult.Response)

			if len(llmDomains) == 0 && prefetchDiscoverOnlyVersion >= 3 {
				logger.Warn("Domain discovery returned no parseable JSON domains",
					"canonical_name", input.CanonicalName,
					"queries", allQueries,
				)
			} else {
				searchSet := make(map[string]bool)
				for _, d := range searchDomainsAll {
					searchSet[d] = true
				}

				isGrounded := func(llmDomain string) bool {
					if searchSet[llmDomain] {
						return true
					}
					suffix := "." + llmDomain
					for sd := range searchSet {
						if strings.HasSuffix(sd, suffix) {
							return true
						}
					}
					return false
				}

				seenAll := make(map[string]bool)
				for _, d := range llmDomains {
					if isGrounded(d) && !seenAll[d] {
						seenAll[d] = true
						allDiscovered = append(allDiscovered, d)
					}
				}
			}

			if len(allDiscovered) > 0 {
				discoveredBySearch["batch"] = allDiscovered
			}

			if discoveryResult.TokensUsed > 0 || discoveryResult.InputTokens > 0 || discoveryResult.OutputTokens > 0 {
				inTok := discoveryResult.InputTokens
				outTok := discoveryResult.OutputTokens
				recCtx := opts.WithTokenRecordOptions(ctx)
				_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
					UserID:       input.UserID,
					SessionID:    input.SessionID,
					TaskID:       workflowID,
					AgentID:      "domain_discovery",
					Model:        discoveryResult.ModelUsed,
					Provider:     discoveryResult.Provider,
					InputTokens:  inTok,
					OutputTokens: outTok,
					Metadata:     map[string]interface{}{"phase": "domain_discovery", "batch_mode": true, "query_count": len(allQueries)},
				}).Get(recCtx, nil)
			}
		}
	}

	if len(allDiscovered) > 0 {
		candidateDomains := allDiscovered
		officialDomainsSource := "search_first_discovered_only_v3"
		if len(requestedRegions) > 0 {
			scoped := selectDomainsForPrefetch(candidateDomains, requestedRegions, originRegion, 100)
			if len(scoped) > 0 {
				baseContext["official_domains"] = scoped
				baseContext["official_domains_source"] = officialDomainsSource
			}
		} else {
			baseContext["official_domains"] = candidateDomains
			baseContext["official_domains_source"] = officialDomainsSource
		}

		if domainAnalysisVersion >= 1 {
			prefetchDomains = selectDomainsForPrefetchWithFocus(candidateDomains, requestedRegions, originRegion, maxPrefetch, intent.FocusCategories)
		} else {
			prefetchDomains = selectDomainsForPrefetch(candidateDomains, requestedRegions, originRegion, maxPrefetch)
		}
		prefetchURLs = buildPrefetchURLsFromDomains(prefetchDomains)

		logger.Info("Domain discovery (prefetch) completed",
			"canonical_name", input.CanonicalName,
			"origin_region", originRegion,
			"requested_regions", requestedRegions,
			"searches", discoveredBySearch,
			"prefetch_domains", prefetchDomains,
			"prefetch_urls", prefetchURLs,
		)
	} else {
		logger.Info("Domain discovery returned no domains; skipping prefetch",
			"canonical_name", input.CanonicalName,
			"origin_region", originRegion,
			"requested_regions", requestedRegions,
		)
	}

	stats := DomainAnalysisStats{
		PrefetchAttempted: len(prefetchURLs),
		FailureReasons:    map[string]int{},
	}
	var prefetchResults []activities.AgentExecutionResult
	var coverage []DomainAnalysisCoverage

	if len(prefetchURLs) > 0 {
		baseSubpageLimit := 15
		if input.PrefetchSubpageLimit > 0 {
			baseSubpageLimit = input.PrefetchSubpageLimit
			if baseSubpageLimit < 10 {
				baseSubpageLimit = 10
			}
			if baseSubpageLimit > 20 {
				baseSubpageLimit = 20
			}
		}

		isPrimaryDomain := func(urlStr string) bool {
			host := strings.ToLower(urlStr)
			if idx := strings.Index(host, "://"); idx != -1 {
				host = host[idx+3:]
			}
			if idx := strings.Index(host, "/"); idx != -1 {
				host = host[:idx]
			}
			host = strings.TrimPrefix(host, "www.")

			canonicalLower := strings.ToLower(input.CanonicalName)
			if canonicalLower != "" && strings.Contains(host, canonicalLower) {
				return true
			}
			for _, od := range input.OfficialDomains {
				odLower := strings.ToLower(od)
				if strings.Contains(host, odLower) || strings.Contains(odLower, host) {
					return true
				}
			}
			return false
		}

		type prefetchPayload struct {
			Result activities.AgentExecutionResult
			URL    string
			Index  int
			Err    error
		}

		prefetchChan := workflow.NewChannel(ctx)

		focusCategories := intent.FocusCategories
		targetKeywords := buildPrefetchTargetKeywords(focusCategories)
		targetPaths := buildPrefetchTargetPaths(focusCategories)

		for i, u := range prefetchURLs {
			url := u
			idx := i + 1

			domainLimit := baseSubpageLimit
			if !isPrimaryDomain(url) {
				domainLimit = baseSubpageLimit - 5
				if domainLimit < 8 {
					domainLimit = 8
				}
			}

			workflow.Go(ctx, func(gctx workflow.Context) {
				prefetchContext := make(map[string]interface{})
				for k, v := range baseContext {
					prefetchContext[k] = v
				}
				prefetchContext["research_mode"] = "prefetch"
				prefetchContext["prefetch_url"] = url
				prefetchContext["role"] = "domain_prefetch"
				prefetchContext["model_tier"] = "small"

				prefetchAgentName := agents.GetAgentName(workflowID, agents.IdxDomainPrefetchBase+idx)

				prefetchQuery := fmt.Sprintf("Use web_subpage_fetch on %s to extract company information.", url)
				prefetchQuery += fmt.Sprintf("\n\nResearch focus: %s", input.Query)
				if len(input.PlanHints) > 0 {
					prefetchQuery += "\n\nFocus hints:\n- " + strings.Join(input.PlanHints, "\n- ")
				}

				var prefetchResult activities.AgentExecutionResult
				err := workflow.ExecuteActivity(gctx,
					"ExecuteAgent",
					activities.AgentExecutionInput{
						Query:          prefetchQuery,
						AgentID:        prefetchAgentName,
						Context:        prefetchContext,
						Mode:           "standard",
						SessionID:      input.SessionID,
						History:        convertHistoryForAgent(input.History),
						SuggestedTools: []string{"web_subpage_fetch"},
						ToolParameters: map[string]interface{}{
							"tool":            "web_subpage_fetch",
							"url":             url,
							"limit":           domainLimit,
							"target_keywords": targetKeywords,
							"target_paths":    targetPaths,
						},
						ParentWorkflowID: input.ParentWorkflowID,
					}).Get(gctx, &prefetchResult)

				prefetchChan.Send(gctx, prefetchPayload{
					Result: prefetchResult,
					URL:    url,
					Index:  idx,
					Err:    err,
				})
			})
		}

		for range prefetchURLs {
			var payload prefetchPayload
			prefetchChan.Receive(ctx, &payload)

			prefetchAgentName := agents.GetAgentName(workflowID, agents.IdxDomainPrefetchBase+payload.Index)
			prefetchURLRole := classifyDomainRole(payload.URL)
			coverage = append(coverage, DomainAnalysisCoverage{
				Domain: normalizeDomainCandidateHost(payload.URL),
				Role:   prefetchURLRole,
				Region: inferRegionFromDomain(payload.URL),
				Status: "failed",
			})

			if payload.Err != nil {
				stats.PrefetchFailed++
				stats.FailureReasons["activity_error"]++
				payload.Result.Success = false
				if payload.Result.Error == "" {
					payload.Result.Error = payload.Err.Error()
				}
				persistAgentExecutionLocalWithMeta(
					ctx,
					workflowID,
					prefetchAgentName,
					fmt.Sprintf("Domain prefetch: %s", payload.URL),
					payload.Result,
					mergeDomainAnalysisMeta(metaBase, map[string]interface{}{
						"phase":    "domain_prefetch",
						"url":      payload.URL,
						"url_role": prefetchURLRole,
						"index":    payload.Index,
						"status":   "failed",
					}),
				)
				continue
			}

			if !payload.Result.Success {
				stats.PrefetchFailed++
				stats.FailureReasons["agent_error"]++
				persistAgentExecutionLocalWithMeta(
					ctx,
					workflowID,
					prefetchAgentName,
					fmt.Sprintf("Domain prefetch: %s", payload.URL),
					payload.Result,
					mergeDomainAnalysisMeta(metaBase, map[string]interface{}{
						"phase":    "domain_prefetch",
						"url":      payload.URL,
						"url_role": prefetchURLRole,
						"index":    payload.Index,
						"status":   "failed",
					}),
				)
				continue
			}

			stats.PrefetchSucceeded++
			coverage[len(coverage)-1].Status = "ok"

			prefetchResults = append(prefetchResults, payload.Result)

			persistAgentExecutionLocalWithMeta(
				ctx,
				workflowID,
				prefetchAgentName,
				fmt.Sprintf("Domain prefetch: %s", payload.URL),
				payload.Result,
				mergeDomainAnalysisMeta(metaBase, map[string]interface{}{
					"phase":    "domain_prefetch",
					"url":      payload.URL,
					"url_role": prefetchURLRole,
					"index":    payload.Index,
					"status":   "ok",
				}),
			)

			if payload.Result.TokensUsed > 0 || payload.Result.InputTokens > 0 || payload.Result.OutputTokens > 0 {
				inTok := payload.Result.InputTokens
				outTok := payload.Result.OutputTokens
				recCtx := opts.WithTokenRecordOptions(ctx)
				_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
					UserID:       input.UserID,
					SessionID:    input.SessionID,
					TaskID:       workflowID,
					AgentID:      prefetchAgentName,
					Model:        payload.Result.ModelUsed,
					Provider:     payload.Result.Provider,
					InputTokens:  inTok,
					OutputTokens: outTok,
					Metadata:     map[string]interface{}{"phase": "domain_prefetch"},
				}).Get(recCtx, nil)
			}
		}
		stats.PrefetchFailed = stats.PrefetchAttempted - stats.PrefetchSucceeded
	}

	coverageSummary := buildCoverageSummary(coverage)
	digestAgentResults := []activities.AgentExecutionResult{}
	if strings.TrimSpace(coverageSummary) != "" {
		digestAgentResults = append(digestAgentResults, activities.AgentExecutionResult{
			AgentID:  "domain_analysis_coverage",
			Response: coverageSummary,
			Success:  true,
		})
	}
	if discoveryResult.Response != "" || discoveryResult.Success {
		digestAgentResults = append(digestAgentResults, discoveryResult)
	}
	digestAgentResults = append(digestAgentResults, prefetchResults...)

	digestQuery := buildDomainAnalysisDigestQuery(input.Query, input.CanonicalName, input.PlanHints)
	digestContext := buildDomainAnalysisSynthesisContext(baseContext)
	digestContext["synthesis_template"] = "domain_analysis_digest"

	var digest activities.SynthesisResult
	if len(digestAgentResults) > 0 {
		err := workflow.ExecuteActivity(ctx,
			activities.SynthesizeResultsLLM,
			activities.SynthesisInput{
				Query:            digestQuery,
				AgentResults:     digestAgentResults,
				Context:          digestContext,
				ParentWorkflowID: input.ParentWorkflowID,
			}).Get(ctx, &digest)
		if err != nil {
			logger.Warn("Domain analysis synthesis failed", "error", err)
		} else {
			stats.DigestTokensUsed = digest.TokensUsed
			if digest.TokensUsed > 0 {
				inTok := digest.InputTokens
				outTok := digest.CompletionTokens
				if inTok == 0 && outTok > 0 {
					est := digest.TokensUsed - outTok
					if est > 0 {
						inTok = est
					}
				}
				recCtx := opts.WithTokenRecordOptions(ctx)
				_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
					UserID:       input.UserID,
					SessionID:    input.SessionID,
					TaskID:       workflowID,
					AgentID:      "domain_analysis_digest",
					Model:        digest.ModelUsed,
					Provider:     digest.Provider,
					InputTokens:  inTok,
					OutputTokens: outTok,
					Metadata:     map[string]interface{}{"phase": "domain_analysis_digest"},
				}).Get(recCtx, nil)
			}

			persistAgentExecutionSyncWithMeta(
				ctx,
				workflowID,
				"domain_analysis_digest",
				fmt.Sprintf("Domain analysis digest: %s", input.CanonicalName),
				activities.AgentExecutionResult{
					AgentID:    "domain_analysis_digest",
					Response:   digest.FinalResult,
					TokensUsed: digest.TokensUsed,
					ModelUsed:  digest.ModelUsed,
					Provider:   digest.Provider,
					Success:    true,
				},
				mergeDomainAnalysisMeta(metaBase, map[string]interface{}{
					"phase":  "domain_analysis_digest",
					"status": "ok",
				}),
			)
		}
	}

	citations := collectDomainAnalysisCitations(prefetchResults, input.CanonicalName, input.ExactQueries, input.OfficialDomains, workflow.Now(ctx))

	return DomainAnalysisResult{
		DigestMarkdown:          digest.FinalResult,
		Citations:               citations,
		OfficialDomainsSelected: coverage,
		PrefetchURLs:            prefetchURLs,
		Stats:                   stats,
	}, nil
}

func cloneContextMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return map[string]interface{}{}
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func buildDomainAnalysisDigestQuery(query, canonicalName string, planHints []string) string {
	entity := strings.TrimSpace(canonicalName)
	if entity == "" {
		entity = strings.TrimSpace(query)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Domain Analysis digest for %s.\n", entity)
	fmt.Fprintf(&b, "User query: %s\n\n", query)
	b.WriteString("Requirements:\n")
	b.WriteString("- Start with \"Official Domains Coverage\" table (domain, role, region, status)\n")
	b.WriteString("- Then use section headings: Company Overview, Products, Business Model, Leadership, Recent News\n")
	b.WriteString("- Write 1-2 paragraphs per section with source attribution\n")
	b.WriteString("- Do NOT add Key Findings, Summary, or bullet lists - go directly to sections\n")
	b.WriteString("- Do NOT repeat facts across sections\n")
	if len(planHints) > 0 {
		b.WriteString("\nPlan hints:\n- ")
		b.WriteString(strings.Join(planHints, "\n- "))
		b.WriteString("\n")
	}
	return b.String()
}

func buildDomainAnalysisSynthesisContext(parent map[string]interface{}) map[string]interface{} {
	ctx := cloneContextMap(parent)
	if _, ok := ctx["synthesis_style"]; !ok {
		ctx["synthesis_style"] = "comprehensive"
	}
	return ctx
}

func buildCoverageSummary(coverage []DomainAnalysisCoverage) string {
	if len(coverage) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Official Domains Coverage:\n")
	b.WriteString("| Domain | Role | Region | Status |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, c := range coverage {
		region := c.Region
		if strings.TrimSpace(region) == "" {
			region = "-"
		}
		status := c.Status
		if strings.TrimSpace(status) == "" {
			status = "-"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", c.Domain, c.Role, region, status)
	}
	return b.String()
}

func buildPrefetchTargetKeywords(focusCategories []string) string {
	keywords := map[string]bool{
		"about":      true,
		"company":    true,
		"overview":   true,
		"mission":    true,
		"history":    true,
		"leadership": true,
		"team":       true,
		"products":   true,
		"services":   true,
	}
	for _, cat := range focusCategories {
		switch cat {
		case "ir":
			keywords["investor"] = true
			keywords["financials"] = true
			keywords["earnings"] = true
			keywords["revenue"] = true
			keywords["annual"] = true
		case "docs":
			keywords["docs"] = true
			keywords["documentation"] = true
			keywords["api"] = true
			keywords["developer"] = true
			keywords["reference"] = true
		case "careers":
			keywords["careers"] = true
			keywords["jobs"] = true
			keywords["hiring"] = true
			keywords["culture"] = true
		case "store":
			keywords["pricing"] = true
			keywords["plans"] = true
			keywords["purchase"] = true
		}
	}
	out := make([]string, 0, len(keywords))
	for k := range keywords {
		out = append(out, k)
	}
	return strings.Join(out, " ")
}

func buildPrefetchTargetPaths(focusCategories []string) []string {
	paths := map[string]bool{
		"/about":      true,
		"/about-us":   true,
		"/company":    true,
		"/overview":   true,
		"/mission":    true,
		"/team":       true,
		"/leadership": true,
		"/management": true,
		"/products":   true,
		"/services":   true,
	}
	for _, cat := range focusCategories {
		switch cat {
		case "ir":
			paths["/ir"] = true
			paths["/investor-relations"] = true
			paths["/investors"] = true
			paths["/financials"] = true
			paths["/governance"] = true
		case "docs":
			paths["/docs"] = true
			paths["/documentation"] = true
			paths["/developer"] = true
			paths["/api"] = true
			paths["/guides"] = true
			paths["/reference"] = true
		case "careers":
			paths["/careers"] = true
			paths["/jobs"] = true
			paths["/hiring"] = true
		case "store":
			paths["/pricing"] = true
			paths["/plans"] = true
			paths["/store"] = true
			paths["/shop"] = true
			paths["/buy"] = true
		}
	}
	out := make([]string, 0, len(paths))
	for p := range paths {
		out = append(out, p)
	}
	return out
}

func inferRegionFromDomain(raw string) string {
	host := normalizeDomainCandidateHost(raw)
	if host == "" {
		return ""
	}
	host = strings.ToLower(host)
	switch {
	case strings.HasPrefix(host, "jp.") || strings.HasSuffix(host, ".jp") || strings.Contains(host, ".jp."):
		return "jp"
	case strings.HasPrefix(host, "cn.") || strings.HasSuffix(host, ".cn") || strings.Contains(host, ".cn."):
		return "cn"
	case strings.HasPrefix(host, "kr.") || strings.HasSuffix(host, ".kr") || strings.Contains(host, ".kr."):
		return "kr"
	case strings.HasPrefix(host, "us.") || strings.HasSuffix(host, ".us") || strings.Contains(host, ".us."):
		return "us"
	case strings.HasSuffix(host, ".eu") || strings.Contains(host, ".eu."):
		return "eu"
	case strings.HasSuffix(host, ".co.uk") || strings.HasSuffix(host, ".uk"):
		return "eu"
	default:
		return "global"
	}
}

func collectDomainAnalysisCitations(results []activities.AgentExecutionResult, canonicalName string, aliases []string, domains []string, now time.Time) []metadata.Citation {
	var resultsForCitations []interface{}
	for _, ar := range results {
		var toolExecs []interface{}
		if len(ar.ToolExecutions) > 0 {
			for _, te := range ar.ToolExecutions {
				toolExecs = append(toolExecs, map[string]interface{}{
					"tool":    te.Tool,
					"success": te.Success,
					"output":  te.Output,
					"error":   te.Error,
				})
			}
		}
		resultsForCitations = append(resultsForCitations, map[string]interface{}{
			"agent_id":        ar.AgentID,
			"tool_executions": toolExecs,
			"response":        ar.Response,
		})
	}
	citations, _ := metadata.CollectCitations(resultsForCitations, now, 0)
	if len(citations) == 0 {
		return nil
	}
	if canonicalName != "" {
		filterResult := ApplyCitationFilterWithFallback(citations, canonicalName, aliases, domains)
		citations = filterResult.Citations
	}
	for i := range citations {
		citations[i].Content = ""
	}
	return citations
}

func buildDomainAnalysisPlanHints(subtasks []activities.Subtask) []string {
	if len(subtasks) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, st := range subtasks {
		desc := strings.TrimSpace(st.Description)
		if desc == "" || seen[desc] {
			continue
		}
		seen[desc] = true
		out = append(out, desc)
	}
	return out
}

func domainAnalysisDigestResult(result *DomainAnalysisResult) activities.AgentExecutionResult {
	if result == nil {
		return activities.AgentExecutionResult{}
	}
	return activities.AgentExecutionResult{
		AgentID:  "domain_analysis_evidence", // NOT "synthesizer" to avoid (Synthesis) tag in upstream prompt
		Role:     "domain_analysis",
		Response: result.DigestMarkdown,
		Success:  true,
	}
}

func extractDomainsFromCoverage(coverage []DomainAnalysisCoverage) []string {
	seen := make(map[string]bool)
	var out []string
	for _, c := range coverage {
		d := strings.TrimSpace(c.Domain)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

func mergeDomainAnalysisMeta(base, extra map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(base)+len(extra))
	for k, v := range base {
		if v != nil && v != "" {
			out[k] = v
		}
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func persistAgentExecutionSyncWithMeta(ctx workflow.Context, workflowID, agentID, input string, result activities.AgentExecutionResult, extraMeta map[string]interface{}) {
	logger := workflow.GetLogger(ctx)
	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	actCtx := workflow.WithActivityOptions(ctx, activityOpts)

	state := "COMPLETED"
	if !result.Success {
		state = "FAILED"
	}

	metadata := map[string]interface{}{
		"workflow": "research",
		"strategy": "react",
	}
	for k, v := range extraMeta {
		metadata[k] = v
	}

	err := workflow.ExecuteActivity(actCtx,
		activities.PersistAgentExecutionStandalone,
		activities.PersistAgentExecutionInput{
			WorkflowID: workflowID,
			AgentID:    agentID,
			Input:      input,
			Output:     result.Response,
			State:      state,
			TokensUsed: result.TokensUsed,
			ModelUsed:  result.ModelUsed,
			DurationMs: result.DurationMs,
			Error:      result.Error,
			Metadata:   metadata,
		},
	).Get(actCtx, nil)
	if err != nil {
		logger.Warn("Failed to persist agent execution", "agent_id", agentID, "error", err)
	}
}
