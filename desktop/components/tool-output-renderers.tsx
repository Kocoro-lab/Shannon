"use client";

import { useState, useEffect } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import {
    Accordion,
    AccordionContent,
    AccordionItem,
    AccordionTrigger,
} from "@/components/ui/accordion";
import {
    ExternalLink,
    ChevronDown,
    ChevronUp,
    Globe,
    Search,
    Users,
    Eye,
    FileText,
    Key,
    Camera,
    Clock,
    DollarSign,
    Tag,
    Building,
    Layers,
    TrendingUp,
    TrendingDown,
    Minus,
    Newspaper,
    Loader2,
} from "lucide-react";
import { cn, safeToFixed, safeNumber } from "@/lib/utils";
import { BlobReference, resolveImageField } from "@/lib/shannon/api";

// =============================================================================
// Types
// =============================================================================

interface SerpAd {
    title: string;
    description: string;
    link: string;
    displayed_link: string;
    position: string;
    type: string;
    advertiser?: string | null;
    sitelinks?: Array<{
        title: string;
        link: string;
        snippets?: string[];
    }>;
    extensions?: string[];
}

interface SerpAdsResult {
    ads: SerpAd[];
    keywords: string;
    total_ads: number;
    cost_usd?: number;
    timestamp?: string;
    search_metadata?: {
        google_url?: string;
        total_time_taken?: number;
    };
}

interface Competitor {
    domain: string;
    name?: string;
    description?: string;
    similarity_score?: number;
    ad_count?: number;
    keywords?: string[];
}

interface CompetitorResult {
    competitors: Competitor[];
    query: string;
    total_found?: number;
    cost_usd?: number;
}

interface AdsTransparencyCreative {
    advertiser?: string;
    details_link?: string;
    first_shown?: number;
    format?: string;
    last_shown?: number;
    regions?: string[];
    text?: string;
    title?: string;
    description?: string;
    image_url?: string;
}

interface AdsTransparencyResult {
    ad_formats?: string[];
    advertiser?: {
        id?: string;
        name?: string;
    };
    advertiser_name?: string;
    cached?: boolean;
    cost_usd?: number;
    creatives?: AdsTransparencyCreative[];
    domain?: string;
    // Legacy format support
    ads?: AdsTransparencyCreative[];
    total_ads?: number;
}

interface KeywordResult {
    keywords: string[];
    query?: string;
    detected_country?: string;
    detected_language?: string;
    language?: string;
    api_cost_usd?: number;
    cost_usd?: number;
}

interface SECFiling {
    accession_number: string;
    description: string;
    filed_date: string;
    filing_url: string;
    form_type: string;
    is_material: boolean;
}

interface SECFilingsResult {
    cik: string;
    company_name: string;
    filings: SECFiling[];
    has_material_events: boolean;
    source: string;
    ticker: string;
    total_count: number;
}

interface TwitterSentimentResult {
    analysis: string;
    citations?: string[];
    cost_usd?: number;
    date_range?: {
        from: string;
        to: string;
    };
    model?: string;
    sentiment: string;
    source?: string;
    ticker: string;
}

interface AlpacaNewsArticle {
    author: string;
    headline: string;
    id: number;
    published_at: string;
    source: string;
    summary: string;
    symbols: string[];
    url: string;
}

interface AlpacaNewsResult {
    articles: AlpacaNewsArticle[];
    heuristic_sentiment_score?: number;
    sentiment_method?: string;
    sentiment_summary?: {
        positive: number;
        negative: number;
        neutral: number;
    };
    source?: string;
    symbols: string;
}

interface LPAnalysisSection {
    name: string;
    position: number;
    key_content: string;
}

interface LPCTA {
    text: string;
    type: string;
    color?: string;
    urgency_language?: string | null;
}

interface LPVisionStructured {
    above_the_fold?: {
        main_headline?: string;
        sub_headline?: string;
        value_proposition?: string;
        primary_cta?: {
            text?: string;
            color?: string;
            placement?: string;
        };
        hero_image_type?: string;
    };
    page_sections?: LPAnalysisSection[];
    ctas?: LPCTA[];
    visual_design?: {
        primary_colors?: string[];
        style?: string;
        image_style?: string;
        quality_rating?: string;
        mobile_optimized?: boolean;
    };
    target_audience?: {
        apparent_demographic?: string;
        industry_signals?: string[];
        pain_points_addressed?: string[];
    };
    conversion_tactics?: {
        scarcity?: string | null;
        social_proof_placement?: string;
        form_fields_visible?: number;
        chat_widget?: boolean;
        exit_intent_likely?: boolean;
    };
    pricing?: {
        visible?: boolean;
        currency?: string | null;
        plans?: unknown[];
    };
    trust_elements?: {
        testimonials?: { count: number; format?: string | null };
        customer_logos?: string[];
        statistics?: string[];
        badges?: string[];
        awards?: string[];
    };
}

interface LPVisionAnalysis {
    analysis?: string;
    structured?: LPVisionStructured;
    success?: boolean;
    model?: string;
    cost_usd?: {
        total?: number;
        vision?: number;
    };
}

interface LPAnalyzeResult {
    // Common fields
    url: string;
    success?: boolean;
    error?: string | null;
    timestamp?: string;

    // Full page mode fields
    partial?: boolean;
    capture_method?: "playwright" | "firecrawl";
    s3_screenshot_url?: string | null;         // S3 permanent URL (preferred)
    screenshot_url?: string | null;           // Firecrawl only
    screenshot_b64?: string | null;           // Playwright only (inline for small images)
    screenshot_b64_ref?: BlobReference;       // Playwright only (blob ref for large images)
    popup_detected?: boolean;
    popups_dismissed?: number;
    popup_artifacts?: Array<{                 // Playwright popups
        type?: string;
        slice_index?: number;
        total_slices?: number;
        media_type?: string;
        data_base64?: string;
        data_base64_ref?: BlobReference;      // Blob ref for large popup images
        size_kb?: number;
    }> | null;
    vision_analysis?: LPVisionAnalysis;
    markdown?: string | null;
    metadata?: {
        title?: string;
        description?: string | null;
        language?: string | null;
        og_image?: string | null;
    };

    // Legacy format support
    headline?: string;
    subheadline?: string;
    cta_text?: string;
    trust_elements?: string[];
    analysis?: { type: string; content?: string; score?: number; suggestions?: string[] }[];
    overall_score?: number;
    cost_usd?: number;
}

interface LPBatchResult {
    results: LPAnalyzeResult[];
    total_analyzed?: number;
    total_urls?: number;
    successful?: number;
    failed?: number;
    batch_id?: string;
    timestamp?: string;
    language?: string;
    ocr_enabled?: boolean;
    invalid_urls?: string[];
    cost_usd?: number | {
        total: number;
        firecrawl?: number;
        ocr?: number;
        vision?: number;
    };
    usage?: {
        input_tokens: number;
        output_tokens: number;
    };
}

// Section-mode response types for LP analysis
interface LPSection {
    index: number;                    // 0-based
    y: number;                        // Y position
    height: number;
    width: number;
    block_type: string;               // "FV"|"Pain Points"|"Benefits"|etc.
    block_type_ja: string;            // Japanese name
    confidence: number;               // 0.0-1.0
    key_elements: string[];           // Notable elements
    cta_present: boolean;
    text_summary: string;             // Brief summary
    s3_screenshot_url?: string | null; // S3 permanent URL (preferred)
    screenshot_b64?: string;          // Base64 PNG (inline for small images)
    screenshot_b64_ref?: BlobReference; // Blob ref for large images
}

interface LPSectionsResult {
    // Sections mode specific fields
    url: string;
    device: "desktop" | "mobile" | "tablet";
    viewport: { width: number; height: number };
    total_page_height: number;
    sections_found: number;
    sections_analyzed: number;
    sections: LPSection[];
    title?: string | null;
    cost_usd: number;
    timestamp: string;
}

interface CreativeAnalysis {
    headline: string;
    description: string;
    persuasion_techniques?: string[];
    emotional_triggers?: string[];
    strengths?: string[];
    weaknesses?: string[];
    suggestions?: string[];
}

interface CreativeAnalyzeResultLegacy {
    analyses: CreativeAnalysis[];
    industry?: string;
    cost_usd?: number;
}

// New comprehensive creative analysis format
interface CreativeCompetitiveGap {
    gap: string;
    opportunity: string;
}

interface CreativeCompetitorAnalyzed {
    ad_count: number;
    domain: string;
    messaging_theme: string;
    unique_selling_points: string[];
}

interface CreativeCtaPattern {
    action_type: string;
    text: string;
    urgency_level: string;
}

interface CreativeEmotionalTrigger {
    competitor: string;
    emotion: string;
    trigger_phrase: string;
}

interface CreativeHeadlinePattern {
    examples: string[];
    frequency: string;
    pattern: string;
}

interface CreativePersuasionTechnique {
    examples: string[];
    used: boolean;
}

interface CreativeRecommendation {
    area: string;
    based_on: string;
    suggestion: string;
}

interface CreativeValueProposition {
    competitors_using: string[];
    example_phrases: string[];
    theme: string;
}

interface CreativeAnalyzeResult {
    ads_analyzed: number;
    analysis: {
        competitive_gaps: CreativeCompetitiveGap[];
        competitors_analyzed: CreativeCompetitorAnalyzed[];
        cta_patterns: CreativeCtaPattern[];
        emotional_triggers: CreativeEmotionalTrigger[];
        headline_patterns: CreativeHeadlinePattern[];
        persuasion_techniques: Record<string, CreativePersuasionTechnique>;
        recommendations: CreativeRecommendation[];
        value_proposition_themes: CreativeValueProposition[];
    };
    cost_usd?: number;
    metadata?: {
        language?: string;
        model_used?: string;
    };
    timestamp?: string;
    unique_domains?: number;
    usage?: {
        input_tokens: number;
        output_tokens: number;
        total_tokens: number;
    };
}

interface ScreenshotResult {
    url: string;
    screenshot?: string; // base64 encoded
    screenshot_base64?: string; // alternative field name
    screenshot_url?: string;
    title?: string;
    elapsed_ms?: number;
    content_type?: string;
    cost_usd?: number;
}

// =============================================================================
// Helper Components
// =============================================================================

function MetadataFooter({ cost, timestamp }: { cost?: number; timestamp?: string }) {
    if (!cost && !timestamp) return null;
    return (
        <div className="flex items-center gap-4 pt-3 mt-3 border-t text-xs text-muted-foreground">
            {cost !== undefined && (
                <span className="flex items-center gap-1">
                    <DollarSign className="h-3 w-3" />
                    ${safeToFixed(cost, 4)}
                </span>
            )}
            {timestamp && (
                <span className="flex items-center gap-1">
                    <Clock className="h-3 w-3" />
                    {new Date(timestamp).toLocaleString()}
                </span>
            )}
        </div>
    );
}

function ExternalLinkButton({ href, children }: { href: string; children: React.ReactNode }) {
    return (
        <a
            href={href}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 text-xs text-blue-600 dark:text-blue-400 hover:underline"
        >
            {children}
            <ExternalLink className="h-3 w-3" />
        </a>
    );
}

// =============================================================================
// Blob Image Component (handles both inline and blob reference images)
// =============================================================================

interface BlobImageProps {
    inline?: string | null;       // Inline base64 data
    blobRef?: BlobReference;      // Blob reference to fetch
    alt: string;
    className?: string;
    onClick?: () => void;
    mediaType?: string;           // For popup artifacts (e.g., "image/png")
}

function BlobImage({ inline, blobRef, alt, className, onClick, mediaType = "image/png" }: BlobImageProps): React.ReactElement | null {
    const [imageData, setImageData] = useState<string | null>(inline || null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        // If we have inline data, use it directly
        if (inline) {
            setImageData(inline);
            setLoading(false);
            setError(null);
            return;
        }

        // If we have a blob reference, fetch it
        if (blobRef?.blob_id) {
            setLoading(true);
            setError(null);

            resolveImageField("screenshot_b64", { screenshot_b64_ref: blobRef })
                .then((data) => {
                    setImageData(data);
                })
                .catch((err) => {
                    console.error("[BlobImage] Failed to fetch blob:", err);
                    setError(err instanceof Error ? err.message : "Failed to load image");
                })
                .finally(() => {
                    setLoading(false);
                });
        }
    }, [inline, blobRef]);

    if (loading) {
        return (
            <div className={cn("flex items-center justify-center bg-muted", className)}>
                <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
        );
    }

    if (error) {
        return (
            <div className={cn("flex items-center justify-center bg-muted text-muted-foreground text-xs", className)}>
                Failed to load
            </div>
        );
    }

    if (!imageData) {
        return null;
    }

    return (
        <img
            src={`data:${mediaType};base64,${imageData}`}
            alt={alt}
            className={className}
            onClick={onClick}
        />
    );
}

// Hook to resolve an image from either inline data or blob reference
function useBlobImage(inline?: string | null, blobRef?: BlobReference): {
    imageData: string | null;
    loading: boolean;
    error: string | null;
} {
    const [imageData, setImageData] = useState<string | null>(inline || null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        // If we have inline data, use it directly
        if (inline) {
            setImageData(inline);
            setLoading(false);
            setError(null);
            return;
        }

        // If we have a blob reference, fetch it
        if (blobRef?.blob_id) {
            setLoading(true);
            setError(null);

            resolveImageField("data", { data_ref: blobRef })
                .then((data) => {
                    setImageData(data);
                })
                .catch((err) => {
                    console.error("[useBlobImage] Failed to fetch blob:", err);
                    setError(err instanceof Error ? err.message : "Failed to load image");
                })
                .finally(() => {
                    setLoading(false);
                });
        }
    }, [inline, blobRef]);

    return { imageData, loading, error };
}

// Component to display LP screenshots with blob reference support
function LPScreenshotDisplay({ result }: { result: LPAnalyzeResult }): React.ReactElement | null {
    // Determine if we have any screenshot source
    const hasS3Url = !!result.s3_screenshot_url;
    const hasScreenshotUrl = !!result.screenshot_url;
    const hasScreenshotInline = !!result.screenshot_b64;
    const hasScreenshotRef = !!result.screenshot_b64_ref;
    const hasPopupArtifact = result.popup_artifacts && result.popup_artifacts.length > 0;
    const firstPopup = result.popup_artifacts?.[0];

    // Prefer S3 permanent URL (best performance, no auth)
    if (hasS3Url) {
        return (
            <img
                src={result.s3_screenshot_url!}
                alt="Landing page screenshot"
                className="w-full rounded border"
            />
        );
    }

    // If we have screenshot_url (Firecrawl), use it directly
    if (hasScreenshotUrl) {
        return (
            <img
                src={result.screenshot_url!}
                alt="Landing page screenshot"
                className="w-full rounded border"
            />
        );
    }

    // If we have screenshot_b64 or screenshot_b64_ref (Playwright), use BlobImage
    if (hasScreenshotInline || hasScreenshotRef) {
        return (
            <BlobImage
                inline={result.screenshot_b64}
                blobRef={result.screenshot_b64_ref}
                alt="Landing page screenshot"
                className="w-full rounded border"
            />
        );
    }

    // If we have popup_artifacts, use the first one (may have inline or ref)
    if (hasPopupArtifact && firstPopup) {
        return (
            <BlobImage
                inline={firstPopup.data_base64}
                blobRef={firstPopup.data_base64_ref}
                alt="Landing page screenshot"
                className="w-full rounded border"
                mediaType={firstPopup.media_type || "image/png"}
            />
        );
    }

    return null;
}

// =============================================================================
// SERP Ads Renderer
// =============================================================================

function AdCard({ ad, index }: { ad: SerpAd; index: number }) {
    const [expanded, setExpanded] = useState(false);
    const hasSitelinks = ad.sitelinks && ad.sitelinks.length > 0;

    return (
        <Card className="overflow-hidden">
            <CardContent className="p-4">
                <div className="space-y-2">
                    {/* Position badge */}
                    <div className="flex items-center gap-2">
                        <Badge variant="outline" className="text-xs">
                            #{index + 1} {ad.position}
                        </Badge>
                        {ad.type && (
                            <Badge variant="secondary" className="text-xs">
                                {ad.type}
                            </Badge>
                        )}
                    </div>

                    {/* Title */}
                    <a
                        href={ad.link}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="block text-blue-600 dark:text-blue-400 font-medium hover:underline line-clamp-2"
                    >
                        {ad.title}
                    </a>

                    {/* Display URL */}
                    <p className="text-xs text-green-700 dark:text-green-500 truncate">
                        {ad.displayed_link}
                    </p>

                    {/* Description */}
                    <p className="text-sm text-muted-foreground line-clamp-2">
                        {ad.description}
                    </p>

                    {/* Sitelinks (collapsible) */}
                    {hasSitelinks && (
                        <Collapsible open={expanded} onOpenChange={setExpanded}>
                            <CollapsibleTrigger asChild>
                                <Button variant="ghost" size="sm" className="h-7 px-2 text-xs">
                                    {expanded ? <ChevronUp className="h-3 w-3 mr-1" /> : <ChevronDown className="h-3 w-3 mr-1" />}
                                    {ad.sitelinks!.length} sitelinks
                                </Button>
                            </CollapsibleTrigger>
                            <CollapsibleContent className="pt-2">
                                <div className="grid grid-cols-2 gap-2">
                                    {ad.sitelinks!.map((sitelink, i) => (
                                        <a
                                            key={i}
                                            href={sitelink.link}
                                            target="_blank"
                                            rel="noopener noreferrer"
                                            className="block p-2 rounded bg-muted/50 hover:bg-muted text-xs"
                                        >
                                            <span className="text-blue-600 dark:text-blue-400 font-medium">
                                                {sitelink.title}
                                            </span>
                                            {sitelink.snippets?.[0] && (
                                                <p className="text-muted-foreground mt-1 line-clamp-1">
                                                    {sitelink.snippets[0]}
                                                </p>
                                            )}
                                        </a>
                                    ))}
                                </div>
                            </CollapsibleContent>
                        </Collapsible>
                    )}
                </div>
            </CardContent>
        </Card>
    );
}

export function SerpAdsRenderer({ data }: { data: SerpAdsResult }) {
    const [showAll, setShowAll] = useState(false);
    const displayAds = showAll ? data.ads : data.ads.slice(0, 3);

    return (
        <div className="space-y-4">
            {/* Header */}
            <div className="flex items-center gap-3">
                <div className="p-2 rounded-lg bg-red-100 dark:bg-red-900/30">
                    <Search className="h-5 w-5 text-red-500" />
                </div>
                <div>
                    <h3 className="font-semibold">Google Ads Results</h3>
                    <p className="text-sm text-muted-foreground">
                        Found {data.total_ads} ads for "{data.keywords}"
                    </p>
                </div>
            </div>

            {/* Ad cards */}
            <div className="space-y-3">
                {displayAds.map((ad, index) => (
                    <AdCard key={index} ad={ad} index={index} />
                ))}
            </div>

            {/* Show more/less */}
            {data.ads.length > 3 && (
                <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setShowAll(!showAll)}
                    className="w-full"
                >
                    {showAll ? "Show less" : `Show all ${data.ads.length} ads`}
                </Button>
            )}

            <MetadataFooter cost={data.cost_usd} timestamp={data.timestamp} />
        </div>
    );
}

// =============================================================================
// Competitor Renderer
// =============================================================================

function CompetitorCard({ competitor }: { competitor: Competitor }) {
    return (
        <Card>
            <CardContent className="p-4">
                <div className="flex items-start gap-3">
                    <div className="p-2 rounded-lg bg-muted shrink-0">
                        <Globe className="h-4 w-4 text-muted-foreground" />
                    </div>
                    <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                            <a
                                href={`https://${competitor.domain}`}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="font-medium text-blue-600 dark:text-blue-400 hover:underline truncate"
                            >
                                {competitor.domain}
                            </a>
                            {competitor.similarity_score && (
                                <Badge variant="secondary" className="text-xs shrink-0">
                                    {Math.round(competitor.similarity_score * 100)}% match
                                </Badge>
                            )}
                        </div>
                        {competitor.name && (
                            <p className="text-sm font-medium mt-1">{competitor.name}</p>
                        )}
                        {competitor.description && (
                            <p className="text-sm text-muted-foreground mt-1 line-clamp-2">
                                {competitor.description}
                            </p>
                        )}
                        {competitor.ad_count !== undefined && (
                            <p className="text-xs text-muted-foreground mt-2">
                                {competitor.ad_count} active ads
                            </p>
                        )}
                    </div>
                </div>
            </CardContent>
        </Card>
    );
}

export function CompetitorRenderer({ data }: { data: CompetitorResult }) {
    return (
        <div className="space-y-4">
            {/* Header */}
            <div className="flex items-center gap-3">
                <div className="p-2 rounded-lg bg-red-100 dark:bg-red-900/30">
                    <Users className="h-5 w-5 text-red-500" />
                </div>
                <div>
                    <h3 className="font-semibold">Competitor Discovery</h3>
                    <p className="text-sm text-muted-foreground">
                        Found {data.competitors.length} competitors for "{data.query}"
                    </p>
                </div>
            </div>

            {/* Competitor cards */}
            <div className="grid gap-3 sm:grid-cols-2">
                {data.competitors.map((competitor, index) => (
                    <CompetitorCard key={index} competitor={competitor} />
                ))}
            </div>

            <MetadataFooter cost={data.cost_usd} />
        </div>
    );
}

// =============================================================================
// Ads Transparency Renderer
// =============================================================================

function formatTimestamp(unixSeconds: number): string {
    try {
        return new Date(unixSeconds * 1000).toLocaleDateString();
    } catch {
        return "N/A";
    }
}

export function AdsTransparencyRenderer({ data }: { data: AdsTransparencyResult }) {
    const [showAll, setShowAll] = useState(false);

    // Support both creatives array and legacy ads array
    const allAds = data.creatives || data.ads || [];
    const displayAds = showAll ? allAds : allAds.slice(0, 6);
    const totalAds = data.total_ads || allAds.length;

    // Get advertiser name from various possible formats
    const advertiserName = data.advertiser_name || data.advertiser?.name || "Unknown Advertiser";
    const advertiserId = data.advertiser?.id;

    return (
        <div className="space-y-4">
            {/* Header */}
            <div className="flex items-center gap-3">
                <div className="p-2 rounded-lg bg-red-100 dark:bg-red-900/30">
                    <Eye className="h-5 w-5 text-red-500" />
                </div>
                <div>
                    <h3 className="font-semibold">Ads Transparency</h3>
                    <p className="text-sm text-muted-foreground">
                        {totalAds} creatives found
                    </p>
                </div>
            </div>

            {/* Advertiser info */}
            <Card>
                <CardContent className="p-4">
                    <div className="flex items-center gap-3">
                        <Building className="h-5 w-5 text-muted-foreground" />
                        <div className="flex-1 min-w-0">
                            <p className="font-medium truncate">{advertiserName}</p>
                            {advertiserId && (
                                <p className="text-xs text-muted-foreground truncate">ID: {advertiserId}</p>
                            )}
                            {data.domain && (
                                <ExternalLinkButton href={`https://${data.domain}`}>
                                    {data.domain}
                                </ExternalLinkButton>
                            )}
                        </div>
                    </div>

                    {/* Ad formats */}
                    {data.ad_formats && data.ad_formats.length > 0 && (
                        <div className="mt-3 flex flex-wrap gap-1">
                            {data.ad_formats.map((format, i) => (
                                <Badge key={i} variant="secondary" className="text-xs capitalize">
                                    {format}
                                </Badge>
                            ))}
                        </div>
                    )}
                </CardContent>
            </Card>

            {/* Ad creatives list */}
            {displayAds.length > 0 && (
                <div className="space-y-2">
                    {displayAds.map((ad, index) => {
                        const hasRichContent = ad.text || ad.title || ad.description || ad.image_url;
                        const displayText = ad.title || ad.text;

                        // Compact single-line format when no rich content
                        if (!hasRichContent) {
                            return (
                                <div
                                    key={index}
                                    className="flex items-center gap-3 p-2 rounded-lg border bg-muted/20 text-sm"
                                >
                                    {ad.format && (
                                        <Badge variant="outline" className="text-xs capitalize shrink-0">
                                            {ad.format}
                                        </Badge>
                                    )}
                                    <span className="text-xs text-muted-foreground shrink-0">
                                        {ad.first_shown && ad.last_shown
                                            ? `${formatTimestamp(ad.first_shown)} - ${formatTimestamp(ad.last_shown)}`
                                            : ad.first_shown
                                                ? formatTimestamp(ad.first_shown)
                                                : ad.last_shown
                                                    ? formatTimestamp(ad.last_shown)
                                                    : ""}
                                    </span>
                                    {ad.regions && ad.regions.length > 0 && (
                                        <span className="text-xs text-muted-foreground truncate">
                                            {ad.regions.slice(0, 3).join(", ")}
                                            {ad.regions.length > 3 && ` +${ad.regions.length - 3}`}
                                        </span>
                                    )}
                                    <div className="flex-1" />
                                    {ad.details_link && (
                                        <ExternalLinkButton href={ad.details_link}>
                                            View
                                        </ExternalLinkButton>
                                    )}
                                </div>
                            );
                        }

                        // Full card format when there's rich content
                        return (
                            <Card key={index} className="overflow-hidden">
                                <CardContent className="p-3">
                                    <div className="space-y-2">
                                        {/* Format and dates row */}
                                        <div className="flex items-center gap-2 flex-wrap">
                                            {ad.format && (
                                                <Badge variant="outline" className="text-xs capitalize">
                                                    {ad.format}
                                                </Badge>
                                            )}
                                            {(ad.first_shown || ad.last_shown) && (
                                                <span className="text-xs text-muted-foreground">
                                                    {ad.first_shown && ad.last_shown
                                                        ? `${formatTimestamp(ad.first_shown)} - ${formatTimestamp(ad.last_shown)}`
                                                        : ad.first_shown
                                                            ? `From ${formatTimestamp(ad.first_shown)}`
                                                            : ad.last_shown
                                                                ? `Until ${formatTimestamp(ad.last_shown)}`
                                                                : ""}
                                                </span>
                                            )}
                                            <div className="flex-1" />
                                            {ad.details_link && (
                                                <ExternalLinkButton href={ad.details_link}>
                                                    View on Google
                                                </ExternalLinkButton>
                                            )}
                                        </div>

                                        {/* Image if available */}
                                        {ad.image_url && (
                                            <img
                                                src={ad.image_url}
                                                alt={displayText || `Ad creative ${index + 1}`}
                                                className="w-full max-h-48 object-contain rounded border bg-muted/30"
                                            />
                                        )}

                                        {/* Title */}
                                        {ad.title && (
                                            <p className="font-medium text-sm">{ad.title}</p>
                                        )}

                                        {/* Description or text */}
                                        {(ad.description || (!ad.title && ad.text)) && (
                                            <p className="text-sm text-muted-foreground">
                                                {ad.description || ad.text}
                                            </p>
                                        )}

                                        {/* Text when title is also present */}
                                        {ad.title && ad.text && (
                                            <p className="text-sm text-muted-foreground">{ad.text}</p>
                                        )}

                                        {/* Regions */}
                                        {ad.regions && ad.regions.length > 0 && (
                                            <div className="flex flex-wrap gap-1">
                                                {ad.regions.slice(0, 5).map((region, i) => (
                                                    <Badge key={i} variant="secondary" className="text-xs">
                                                        {region}
                                                    </Badge>
                                                ))}
                                                {ad.regions.length > 5 && (
                                                    <Badge variant="secondary" className="text-xs">
                                                        +{ad.regions.length - 5} more
                                                    </Badge>
                                                )}
                                            </div>
                                        )}
                                    </div>
                                </CardContent>
                            </Card>
                        );
                    })}
                </div>
            )}

            {allAds.length > 6 && (
                <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setShowAll(!showAll)}
                    className="w-full"
                >
                    {showAll ? "Show less" : `Show all ${allAds.length} creatives`}
                </Button>
            )}

            <MetadataFooter cost={data.cost_usd} />
        </div>
    );
}

// =============================================================================
// Keyword Renderer
// =============================================================================

export function KeywordRenderer({ data }: { data: KeywordResult }) {
    const subtitle = data.query
        ? `${data.keywords.length} keywords from "${data.query}"`
        : data.detected_country || data.detected_language
            ? `${data.keywords.length} keywords (${data.detected_country?.toUpperCase() || ""} ${data.detected_language || ""})`
            : `${data.keywords.length} keywords extracted`;

    return (
        <div className="space-y-4">
            {/* Header */}
            <div className="flex items-center gap-3">
                <div className="p-2 rounded-lg bg-orange-100 dark:bg-orange-900/30">
                    <Key className="h-5 w-5 text-orange-500" />
                </div>
                <div>
                    <h3 className="font-semibold">Extracted Keywords</h3>
                    <p className="text-sm text-muted-foreground">{subtitle}</p>
                </div>
            </div>

            {/* Keyword chips */}
            <div className="flex flex-wrap gap-2">
                {data.keywords.map((keyword, index) => (
                    <Badge
                        key={index}
                        variant="secondary"
                        className="px-3 py-1.5 text-sm font-normal"
                    >
                        <Tag className="h-3 w-3 mr-1.5" />
                        {keyword}
                    </Badge>
                ))}
            </div>

            <MetadataFooter cost={data.api_cost_usd || data.cost_usd} />
        </div>
    );
}

// =============================================================================
// LP Analyze Renderer
// =============================================================================

function LPCard({ result }: { result: LPAnalyzeResult }) {
    const [sectionsExpanded, setSectionsExpanded] = useState(false);
    const [detailsExpanded, setDetailsExpanded] = useState(false);

    const structured = result.vision_analysis?.structured;
    const aboveTheFold = structured?.above_the_fold;
    const hasVisionAnalysis = !!structured;

    // Handle failed results
    if (result.success === false || result.error) {
        return (
            <Card className="border-red-200 dark:border-red-800">
                <CardContent className="p-4">
                    <div className="flex items-start gap-3">
                        <div className="p-2 rounded-lg bg-red-100 dark:bg-red-900/30">
                            <Globe className="h-4 w-4 text-red-500" />
                        </div>
                        <div className="flex-1 min-w-0">
                            <p className="text-sm font-medium truncate">{result.url}</p>
                            <p className="text-sm text-red-600 dark:text-red-400">
                                {result.error || "Failed to analyze"}
                            </p>
                        </div>
                    </div>
                </CardContent>
            </Card>
        );
    }

    return (
        <Card>
            <CardContent className="p-4 space-y-4">
                {/* URL and metadata */}
                <div className="space-y-1">
                    <ExternalLinkButton href={result.url}>
                        {new URL(result.url).hostname}
                    </ExternalLinkButton>
                    {result.metadata?.title && (
                        <p className="text-sm font-medium line-clamp-2">{result.metadata.title}</p>
                    )}
                    {result.capture_method && (
                        <Badge variant="outline" className="text-xs">
                            {result.capture_method.replace(/_/g, " ")}
                        </Badge>
                    )}
                </div>

                {/* Screenshot - from screenshot_url (Firecrawl), screenshot_b64/screenshot_b64_ref (Playwright), or popup_artifacts */}
                <LPScreenshotDisplay result={result} />

                {/* Vision Analysis - Above the Fold */}
                {aboveTheFold && (
                    <div className="p-3 rounded-lg bg-purple-50 dark:bg-purple-900/20 border border-purple-200 dark:border-purple-800 space-y-2">
                        <p className="text-xs font-medium text-purple-600 dark:text-purple-400 uppercase tracking-wide">Above the Fold</p>
                        {aboveTheFold.main_headline && (
                            <p className="font-semibold text-lg">{aboveTheFold.main_headline}</p>
                        )}
                        {aboveTheFold.sub_headline && (
                            <p className="text-sm text-muted-foreground">{aboveTheFold.sub_headline}</p>
                        )}
                        {aboveTheFold.value_proposition && (
                            <p className="text-sm italic">"{aboveTheFold.value_proposition}"</p>
                        )}
                        {aboveTheFold.primary_cta && (
                            <div className="flex items-center gap-2 pt-1">
                                <Badge className={cn(
                                    "text-white",
                                    aboveTheFold.primary_cta.color?.toLowerCase().includes("green") ? "bg-green-600" :
                                    aboveTheFold.primary_cta.color?.toLowerCase().includes("blue") ? "bg-blue-600" :
                                    aboveTheFold.primary_cta.color?.toLowerCase().includes("red") ? "bg-red-600" :
                                    aboveTheFold.primary_cta.color?.toLowerCase().includes("orange") ? "bg-orange-600" :
                                    "bg-purple-600"
                                )}>
                                    {aboveTheFold.primary_cta.text}
                                </Badge>
                                {aboveTheFold.primary_cta.placement && (
                                    <span className="text-xs text-muted-foreground">({aboveTheFold.primary_cta.placement})</span>
                                )}
                            </div>
                        )}
                        {aboveTheFold.hero_image_type && (
                            <p className="text-xs text-muted-foreground">Hero: {aboveTheFold.hero_image_type}</p>
                        )}
                    </div>
                )}

                {/* CTAs */}
                {structured?.ctas && structured.ctas.length > 0 && (
                    <div className="space-y-2">
                        <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Call-to-Actions</p>
                        <div className="flex flex-wrap gap-2">
                            {structured.ctas.map((cta, i) => (
                                <div key={i} className="flex items-center gap-1">
                                    <Badge variant="secondary" className="text-sm">
                                        {cta.text}
                                    </Badge>
                                    <span className="text-xs text-muted-foreground">({cta.type})</span>
                                </div>
                            ))}
                        </div>
                    </div>
                )}

                {/* Page Sections (collapsible) */}
                {structured?.page_sections && structured.page_sections.length > 0 && (
                    <Collapsible open={sectionsExpanded} onOpenChange={setSectionsExpanded}>
                        <CollapsibleTrigger asChild>
                            <Button variant="ghost" size="sm" className="w-full justify-between">
                                <span className="text-xs font-medium">Page Sections ({structured.page_sections.length})</span>
                                {sectionsExpanded ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
                            </Button>
                        </CollapsibleTrigger>
                        <CollapsibleContent className="pt-2 space-y-2">
                            {structured.page_sections.map((section, i) => (
                                <div key={i} className="p-2 rounded bg-muted/50 text-sm">
                                    <div className="flex items-center gap-2">
                                        <Badge variant="outline" className="text-xs">#{section.position}</Badge>
                                        <span className="font-medium">{section.name}</span>
                                    </div>
                                    <p className="text-muted-foreground mt-1 text-xs">{section.key_content}</p>
                                </div>
                            ))}
                        </CollapsibleContent>
                    </Collapsible>
                )}

                {/* Visual Design & Target Audience */}
                {(structured?.visual_design || structured?.target_audience) && (
                    <div className="grid grid-cols-2 gap-3">
                        {structured.visual_design && (
                            <div className="p-2 rounded bg-muted/50 space-y-1">
                                <p className="text-xs font-medium">Visual Design</p>
                                {structured.visual_design.style && (
                                    <p className="text-xs text-muted-foreground">Style: {structured.visual_design.style}</p>
                                )}
                                {structured.visual_design.quality_rating && (
                                    <p className="text-xs text-muted-foreground">Quality: {structured.visual_design.quality_rating}</p>
                                )}
                                {structured.visual_design.primary_colors && structured.visual_design.primary_colors.length > 0 && (
                                    <div className="flex gap-1 mt-1">
                                        {structured.visual_design.primary_colors.slice(0, 4).map((color, i) => (
                                            <Badge key={i} variant="outline" className="text-xs capitalize">
                                                {color}
                                            </Badge>
                                        ))}
                                    </div>
                                )}
                            </div>
                        )}
                        {structured.target_audience && (
                            <div className="p-2 rounded bg-muted/50 space-y-1">
                                <p className="text-xs font-medium">Target Audience</p>
                                {structured.target_audience.apparent_demographic && (
                                    <p className="text-xs text-muted-foreground">{structured.target_audience.apparent_demographic}</p>
                                )}
                                {structured.target_audience.industry_signals && structured.target_audience.industry_signals.length > 0 && (
                                    <div className="flex flex-wrap gap-1 mt-1">
                                        {structured.target_audience.industry_signals.slice(0, 3).map((sig, i) => (
                                            <Badge key={i} variant="secondary" className="text-xs">
                                                {sig}
                                            </Badge>
                                        ))}
                                    </div>
                                )}
                            </div>
                        )}
                    </div>
                )}

                {/* Trust Elements */}
                {structured?.trust_elements && (
                    <div className="space-y-2">
                        <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Trust Elements</p>
                        <div className="flex flex-wrap gap-1">
                            {structured.trust_elements.testimonials && structured.trust_elements.testimonials.count > 0 && (
                                <Badge variant="outline" className="text-xs">
                                    {structured.trust_elements.testimonials.count} testimonials
                                </Badge>
                            )}
                            {structured.trust_elements.customer_logos?.map((logo, i) => (
                                <Badge key={i} variant="secondary" className="text-xs">{logo}</Badge>
                            ))}
                            {structured.trust_elements.badges?.map((badge, i) => (
                                <Badge key={i} variant="outline" className="text-xs">{badge}</Badge>
                            ))}
                            {structured.trust_elements.statistics?.map((stat, i) => (
                                <Badge key={i} variant="secondary" className="text-xs">{stat}</Badge>
                            ))}
                        </div>
                    </div>
                )}

                {/* Conversion Tactics */}
                {structured?.conversion_tactics && (
                    <div className="flex flex-wrap gap-2 text-xs">
                        {structured.conversion_tactics.scarcity && (
                            <Badge variant="destructive" className="text-xs">Scarcity: {structured.conversion_tactics.scarcity}</Badge>
                        )}
                        {structured.conversion_tactics.chat_widget && (
                            <Badge variant="outline" className="text-xs">Chat Widget</Badge>
                        )}
                        {structured.conversion_tactics.form_fields_visible !== undefined && structured.conversion_tactics.form_fields_visible > 0 && (
                            <Badge variant="outline" className="text-xs">{structured.conversion_tactics.form_fields_visible} form fields</Badge>
                        )}
                        {structured.conversion_tactics.social_proof_placement && (
                            <Badge variant="outline" className="text-xs">Social proof: {structured.conversion_tactics.social_proof_placement}</Badge>
                        )}
                    </div>
                )}

                {/* Legacy format support */}
                {!hasVisionAnalysis && (
                    <div className="space-y-2">
                        {result.headline && (
                            <div>
                                <p className="text-xs text-muted-foreground">Headline</p>
                                <p className="font-medium">{result.headline}</p>
                            </div>
                        )}
                        {result.cta_text && (
                            <div>
                                <p className="text-xs text-muted-foreground">CTA</p>
                                <Badge className="bg-green-600">{result.cta_text}</Badge>
                            </div>
                        )}
                        {result.trust_elements && result.trust_elements.length > 0 && (
                            <div>
                                <p className="text-xs text-muted-foreground mb-1">Trust Elements</p>
                                <div className="flex flex-wrap gap-1">
                                    {result.trust_elements.map((el, i) => (
                                        <Badge key={i} variant="outline" className="text-xs">
                                            {el}
                                        </Badge>
                                    ))}
                                </div>
                            </div>
                        )}
                    </div>
                )}

                {/* Score (legacy) */}
                {result.overall_score !== undefined && (
                    <div className="flex items-center gap-2">
                        <span className="text-sm text-muted-foreground">Score:</span>
                        <Badge
                            className={cn(
                                result.overall_score >= 80 ? "bg-green-600" :
                                result.overall_score >= 60 ? "bg-yellow-600" : "bg-red-600"
                            )}
                        >
                            {result.overall_score}/100
                        </Badge>
                    </div>
                )}

                {/* Legacy analysis details (collapsible) */}
                {!hasVisionAnalysis && result.analysis && result.analysis.length > 0 && (
                    <Collapsible open={detailsExpanded} onOpenChange={setDetailsExpanded}>
                        <CollapsibleTrigger asChild>
                            <Button variant="ghost" size="sm" className="w-full">
                                {detailsExpanded ? <ChevronUp className="h-4 w-4 mr-2" /> : <ChevronDown className="h-4 w-4 mr-2" />}
                                {detailsExpanded ? "Hide analysis" : "Show detailed analysis"}
                            </Button>
                        </CollapsibleTrigger>
                        <CollapsibleContent className="pt-2 space-y-2">
                            {result.analysis.map((section, i) => (
                                <div key={i} className="p-2 rounded bg-muted/50">
                                    <p className="text-xs font-medium">{section.type}</p>
                                    {section.content && (
                                        <p className="text-sm mt-1">{section.content}</p>
                                    )}
                                    {section.suggestions && section.suggestions.length > 0 && (
                                        <ul className="text-xs text-muted-foreground mt-1 list-disc list-inside">
                                            {section.suggestions.map((s, j) => (
                                                <li key={j}>{s}</li>
                                            ))}
                                        </ul>
                                    )}
                                </div>
                            ))}
                        </CollapsibleContent>
                    </Collapsible>
                )}

                {/* Vision Analysis cost */}
                {result.vision_analysis?.cost_usd?.total && (
                    <div className="text-xs text-muted-foreground flex items-center gap-1">
                        <DollarSign className="h-3 w-3" />
                        Analysis cost: ${safeToFixed(result.vision_analysis.cost_usd.total, 4)}
                    </div>
                )}
            </CardContent>
        </Card>
    );
}

// Block type colors for section cards
const BLOCK_TYPE_COLORS: Record<string, { bg: string; text: string; border: string }> = {
    FV: { bg: "bg-blue-100 dark:bg-blue-900/30", text: "text-blue-700 dark:text-blue-300", border: "border-blue-200 dark:border-blue-800" },
    "Pain Points": { bg: "bg-red-100 dark:bg-red-900/30", text: "text-red-700 dark:text-red-300", border: "border-red-200 dark:border-red-800" },
    Benefits: { bg: "bg-green-100 dark:bg-green-900/30", text: "text-green-700 dark:text-green-300", border: "border-green-200 dark:border-green-800" },
    Proof: { bg: "bg-purple-100 dark:bg-purple-900/30", text: "text-purple-700 dark:text-purple-300", border: "border-purple-200 dark:border-purple-800" },
    VOC: { bg: "bg-orange-100 dark:bg-orange-900/30", text: "text-orange-700 dark:text-orange-300", border: "border-orange-200 dark:border-orange-800" },
    Product: { bg: "bg-cyan-100 dark:bg-cyan-900/30", text: "text-cyan-700 dark:text-cyan-300", border: "border-cyan-200 dark:border-cyan-800" },
    QA: { bg: "bg-gray-100 dark:bg-gray-900/30", text: "text-gray-700 dark:text-gray-300", border: "border-gray-200 dark:border-gray-800" },
    Usage: { bg: "bg-teal-100 dark:bg-teal-900/30", text: "text-teal-700 dark:text-teal-300", border: "border-teal-200 dark:border-teal-800" },
    Offer: { bg: "bg-yellow-100 dark:bg-yellow-900/30", text: "text-yellow-700 dark:text-yellow-300", border: "border-yellow-200 dark:border-yellow-800" },
    Other: { bg: "bg-gray-100 dark:bg-gray-900/30", text: "text-gray-700 dark:text-gray-300", border: "border-gray-200 dark:border-gray-800" },
};

function getBlockTypeColors(blockType: string): { bg: string; text: string; border: string } {
    return BLOCK_TYPE_COLORS[blockType] || BLOCK_TYPE_COLORS.Other;
}

function LPSectionCard({ section, onClick }: { section: LPSection; onClick?: () => void }): React.ReactElement {
    const colors = getBlockTypeColors(section.block_type);
    const confidencePercent = Math.round(section.confidence * 100);
    const hasScreenshot = section.s3_screenshot_url || section.screenshot_b64 || section.screenshot_b64_ref;

    return (
        <Card
            className={cn("overflow-hidden cursor-pointer hover:shadow-md transition-shadow", colors.border)}
            onClick={onClick}
        >
            <CardContent className="p-3 space-y-2">
                {/* Thumbnail - prefer S3 URL, then inline, then blob reference */}
                {hasScreenshot && (
                    <div className="aspect-video bg-muted rounded overflow-hidden">
                        {section.s3_screenshot_url ? (
                            <img
                                src={section.s3_screenshot_url}
                                alt={section.block_type_ja || section.block_type}
                                className="w-full h-full object-cover object-top"
                            />
                        ) : (
                            <BlobImage
                                inline={section.screenshot_b64}
                                blobRef={section.screenshot_b64_ref}
                                alt={section.block_type_ja || section.block_type}
                                className="w-full h-full object-cover object-top"
                            />
                        )}
                    </div>
                )}

                {/* Block type badge */}
                <div className="flex items-center gap-2">
                    <Badge className={cn("text-xs", colors.bg, colors.text)}>
                        {section.block_type}
                    </Badge>
                    <span className="text-xs text-muted-foreground">
                        {section.block_type_ja}
                    </span>
                </div>

                {/* Confidence bar */}
                <div className="space-y-1">
                    <div className="flex items-center justify-between text-xs">
                        <span className="text-muted-foreground">Confidence</span>
                        <span className="font-medium">{confidencePercent}%</span>
                    </div>
                    <div className="h-1.5 bg-muted rounded-full overflow-hidden">
                        <div
                            className={cn(
                                "h-full rounded-full transition-all",
                                confidencePercent >= 80 ? "bg-green-500" :
                                confidencePercent >= 60 ? "bg-yellow-500" : "bg-red-500"
                            )}
                            style={{ width: `${confidencePercent}%` }}
                        />
                    </div>
                </div>

                {/* CTA indicator */}
                {section.cta_present && (
                    <div className="flex items-center gap-1 text-xs text-green-600 dark:text-green-400">
                        <div className="w-2 h-2 rounded-full bg-green-500" />
                        CTA Present
                    </div>
                )}
            </CardContent>
        </Card>
    );
}

function LPSectionsGrid({ sections, url }: { sections: LPSection[]; url?: string }): React.ReactElement {
    // Store the expanded section (for both inline and blob ref support)
    const [expandedSection, setExpandedSection] = useState<LPSection | null>(null);

    return (
        <div className="space-y-4">
            {/* Header */}
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                    <Badge variant="outline">{sections.length} sections</Badge>
                    {url && (
                        <ExternalLinkButton href={url}>
                            {(() => {
                                try {
                                    return new URL(url).hostname;
                                } catch {
                                    return url;
                                }
                            })()}
                        </ExternalLinkButton>
                    )}
                </div>
            </div>

            {/* Card grid */}
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
                {sections.map((section, index) => (
                    <LPSectionCard
                        key={index}
                        section={section}
                        onClick={() => {
                            if (section.s3_screenshot_url || section.screenshot_b64 || section.screenshot_b64_ref) {
                                setExpandedSection(section);
                            }
                        }}
                    />
                ))}
            </div>

            {/* Expanded image modal */}
            {expandedSection && (
                <div
                    className="fixed inset-0 z-50 bg-black/80 flex items-center justify-center p-4"
                    onClick={() => setExpandedSection(null)}
                >
                    {expandedSection.s3_screenshot_url ? (
                        <img
                            src={expandedSection.s3_screenshot_url}
                            alt="Expanded section"
                            className="max-w-full max-h-full object-contain rounded-lg"
                        />
                    ) : (
                        <BlobImage
                            inline={expandedSection.screenshot_b64}
                            blobRef={expandedSection.screenshot_b64_ref}
                            alt="Expanded section"
                            className="max-w-full max-h-full object-contain rounded-lg"
                        />
                    )}
                </div>
            )}
        </div>
    );
}

export function LPAnalyzeRenderer({ data }: { data: LPAnalyzeResult | LPSectionsResult }) {
    // Check if this is sections mode (has sections array)
    const isSectionsMode = "sections" in data && Array.isArray(data.sections) && data.sections.length > 0;

    // Build subtitle: URL hostname + device info
    const subtitle = (() => {
        const hostname = data.url ? (() => {
            try {
                return new URL(data.url).hostname;
            } catch {
                return data.url;
            }
        })() : null;

        if (isSectionsMode) {
            const sectionsData = data as LPSectionsResult;
            const parts = [];
            if (hostname) parts.push(hostname);
            parts.push(`${sectionsData.sections.length} sections`);
            if (sectionsData.device) parts.push(sectionsData.device);
            return parts.join(" • ");
        }
        return hostname || undefined;
    })();

    // Get cost - different field structure for each mode
    const cost = isSectionsMode
        ? (data as LPSectionsResult).cost_usd
        : (data as LPAnalyzeResult).cost_usd;

    return (
        <div className="space-y-4">
            {/* Header */}
            <div className="flex items-center gap-3">
                <div className="p-2 rounded-lg bg-purple-100 dark:bg-purple-900/30">
                    <FileText className="h-5 w-5 text-purple-500" />
                </div>
                <div>
                    <h3 className="font-semibold">Landing Page Analysis</h3>
                    {subtitle && (
                        <p className="text-sm text-muted-foreground">{subtitle}</p>
                    )}
                </div>
            </div>

            {/* Sections grid or full page card */}
            {isSectionsMode ? (
                <LPSectionsGrid sections={(data as LPSectionsResult).sections} url={data.url} />
            ) : (
                <LPCard result={data as LPAnalyzeResult} />
            )}

            <MetadataFooter cost={cost} />
        </div>
    );
}

export function LPBatchRenderer({ data }: { data: LPBatchResult }) {
    const totalPages = data.total_urls ?? data.total_analyzed ?? data.results.length;
    const costValue = typeof data.cost_usd === "object" ? data.cost_usd.total : data.cost_usd;

    // Batch does NOT support sections mode - only full page analysis
    return (
        <div className="space-y-4">
            {/* Header */}
            <div className="flex items-center gap-3">
                <div className="p-2 rounded-lg bg-purple-100 dark:bg-purple-900/30">
                    <Layers className="h-5 w-5 text-purple-500" />
                </div>
                <div className="flex-1">
                    <h3 className="font-semibold">Batch Landing Page Analysis</h3>
                    <p className="text-sm text-muted-foreground">
                        Analyzed {totalPages} pages
                        {data.successful !== undefined && data.failed !== undefined && (
                            <span> • {data.successful} successful, {data.failed} failed</span>
                        )}
                    </p>
                </div>
                {data.language && (
                    <Badge variant="outline" className="text-xs">
                        {data.language.toUpperCase()}
                    </Badge>
                )}
            </div>

            {/* LP cards */}
            <div className="space-y-3">
                {data.results.map((result, index) => (
                    <LPCard key={index} result={result} />
                ))}
            </div>

            <MetadataFooter cost={costValue} />
        </div>
    );
}

// =============================================================================
// Creative Analyze Renderer
// =============================================================================

// Helper to check if data is new format
function isNewCreativeFormat(data: CreativeAnalyzeResult | CreativeAnalyzeResultLegacy): data is CreativeAnalyzeResult {
    return "ads_analyzed" in data && "analysis" in data;
}

// Persuasion technique display names
const PERSUASION_LABELS: Record<string, string> = {
    authority: "Authority",
    price_anchoring: "Price Anchoring",
    risk_reversal: "Risk Reversal",
    scarcity: "Scarcity",
    social_proof: "Social Proof",
    urgency: "Urgency",
};

export function CreativeAnalyzeRenderer({ data }: { data: CreativeAnalyzeResult | CreativeAnalyzeResultLegacy }) {
    const [expandedSections, setExpandedSections] = useState<Record<string, boolean>>({
        recommendations: true,
        gaps: true,
    });

    const toggleSection = (key: string) => {
        setExpandedSections(prev => ({ ...prev, [key]: !prev[key] }));
    };

    // New comprehensive format
    if (isNewCreativeFormat(data)) {
        const { analysis } = data;
        const techniques = analysis.persuasion_techniques;
        const usedTechniques = Object.entries(techniques).filter(([, v]) => v.used);
        const unusedTechniques = Object.entries(techniques).filter(([, v]) => !v.used);

        return (
            <div className="space-y-4">
                {/* Header */}
                <div className="flex items-center gap-3">
                    <div className="p-2 rounded-lg bg-pink-100 dark:bg-pink-900/30">
                        <FileText className="h-5 w-5 text-pink-500" />
                    </div>
                    <div className="flex-1">
                        <h3 className="font-semibold">Ad Creative Analysis</h3>
                        <p className="text-sm text-muted-foreground">
                            {data.ads_analyzed} ad{data.ads_analyzed !== 1 ? "s" : ""} analyzed
                        </p>
                    </div>
                </div>

                {/* Recommendations - Most important, shown first */}
                {analysis.recommendations.length > 0 && (
                    <Collapsible open={expandedSections.recommendations} onOpenChange={() => toggleSection("recommendations")}>
                        <Card className="border-pink-200 dark:border-pink-800">
                            <CollapsibleTrigger asChild>
                                <CardHeader className="py-3 px-4 cursor-pointer hover:bg-muted/50 transition-colors">
                                    <div className="flex items-center justify-between">
                                        <CardTitle className="text-sm flex items-center gap-2">
                                            <span className="text-pink-500">💡</span>
                                            Recommendations ({analysis.recommendations.length})
                                        </CardTitle>
                                        {expandedSections.recommendations ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
                                    </div>
                                </CardHeader>
                            </CollapsibleTrigger>
                            <CollapsibleContent>
                                <CardContent className="pt-0 px-4 pb-4">
                                    <div className="space-y-3">
                                        {analysis.recommendations.map((rec, i) => (
                                            <div key={i} className="p-3 rounded-lg bg-pink-50 dark:bg-pink-950/30 border border-pink-100 dark:border-pink-900">
                                                <div className="flex items-start gap-2">
                                                    <Badge variant="outline" className="text-xs shrink-0 capitalize">
                                                        {rec.area}
                                                    </Badge>
                                                </div>
                                                <p className="text-sm mt-2 font-medium">{rec.suggestion}</p>
                                                <p className="text-xs text-muted-foreground mt-1">
                                                    Based on: {rec.based_on}
                                                </p>
                                            </div>
                                        ))}
                                    </div>
                                </CardContent>
                            </CollapsibleContent>
                        </Card>
                    </Collapsible>
                )}

                {/* Competitive Gaps */}
                {analysis.competitive_gaps.length > 0 && (
                    <Collapsible open={expandedSections.gaps} onOpenChange={() => toggleSection("gaps")}>
                        <Card>
                            <CollapsibleTrigger asChild>
                                <CardHeader className="py-3 px-4 cursor-pointer hover:bg-muted/50 transition-colors">
                                    <div className="flex items-center justify-between">
                                        <CardTitle className="text-sm flex items-center gap-2">
                                            <span>🎯</span>
                                            Competitive Gaps ({analysis.competitive_gaps.length})
                                        </CardTitle>
                                        {expandedSections.gaps ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
                                    </div>
                                </CardHeader>
                            </CollapsibleTrigger>
                            <CollapsibleContent>
                                <CardContent className="pt-0 px-4 pb-4">
                                    <div className="space-y-2">
                                        {analysis.competitive_gaps.map((gap, i) => (
                                            <div key={i} className="grid grid-cols-[1fr,auto,1fr] gap-2 items-start text-sm p-2 rounded bg-muted/30">
                                                <div>
                                                    <span className="text-red-500 text-xs font-medium">Gap</span>
                                                    <p className="text-sm">{gap.gap}</p>
                                                </div>
                                                <span className="text-muted-foreground">→</span>
                                                <div>
                                                    <span className="text-green-500 text-xs font-medium">Opportunity</span>
                                                    <p className="text-sm">{gap.opportunity}</p>
                                                </div>
                                            </div>
                                        ))}
                                    </div>
                                </CardContent>
                            </CollapsibleContent>
                        </Card>
                    </Collapsible>
                )}

                {/* Persuasion Techniques */}
                <Card>
                    <CardHeader className="py-3 px-4">
                        <CardTitle className="text-sm flex items-center gap-2">
                            <span>🧠</span>
                            Persuasion Techniques
                        </CardTitle>
                    </CardHeader>
                    <CardContent className="pt-0 px-4 pb-4">
                        <div className="grid grid-cols-2 gap-4">
                            {/* Used techniques */}
                            <div>
                                <p className="text-xs text-green-600 dark:text-green-400 font-medium mb-2">✓ Used</p>
                                <div className="space-y-2">
                                    {usedTechniques.map(([key, tech]) => (
                                        <div key={key} className="p-2 rounded bg-green-50 dark:bg-green-950/30 border border-green-200 dark:border-green-800">
                                            <p className="text-xs font-medium">{PERSUASION_LABELS[key] || key}</p>
                                            {tech.examples.length > 0 && (
                                                <p className="text-xs text-muted-foreground mt-1 italic">
                                                    「{tech.examples[0]}」
                                                </p>
                                            )}
                                        </div>
                                    ))}
                                    {usedTechniques.length === 0 && (
                                        <p className="text-xs text-muted-foreground">None</p>
                                    )}
                                </div>
                            </div>
                            {/* Unused techniques */}
                            <div>
                                <p className="text-xs text-red-600 dark:text-red-400 font-medium mb-2">✗ Not Used</p>
                                <div className="flex flex-wrap gap-1">
                                    {unusedTechniques.map(([key]) => (
                                        <Badge key={key} variant="outline" className="text-xs text-muted-foreground">
                                            {PERSUASION_LABELS[key] || key}
                                        </Badge>
                                    ))}
                                    {unusedTechniques.length === 0 && (
                                        <p className="text-xs text-muted-foreground">None</p>
                                    )}
                                </div>
                            </div>
                        </div>
                    </CardContent>
                </Card>

                {/* Emotional Triggers */}
                {analysis.emotional_triggers.length > 0 && (
                    <Card>
                        <CardHeader className="py-3 px-4">
                            <CardTitle className="text-sm flex items-center gap-2">
                                <span>❤️</span>
                                Emotional Triggers ({analysis.emotional_triggers.length})
                            </CardTitle>
                        </CardHeader>
                        <CardContent className="pt-0 px-4 pb-4">
                            <div className="flex flex-wrap gap-2">
                                {analysis.emotional_triggers.map((trigger, i) => (
                                    <div key={i} className="inline-flex items-center gap-2 p-2 rounded-lg bg-muted/50 text-sm">
                                        <Badge className="bg-pink-600 text-xs capitalize">{trigger.emotion}</Badge>
                                        <span className="italic">「{trigger.trigger_phrase}」</span>
                                    </div>
                                ))}
                            </div>
                        </CardContent>
                    </Card>
                )}

                {/* Headline Patterns */}
                {analysis.headline_patterns.length > 0 && (
                    <Collapsible>
                        <Card>
                            <CollapsibleTrigger asChild>
                                <CardHeader className="py-3 px-4 cursor-pointer hover:bg-muted/50 transition-colors">
                                    <div className="flex items-center justify-between">
                                        <CardTitle className="text-sm flex items-center gap-2">
                                            <span>📝</span>
                                            Headline Patterns ({analysis.headline_patterns.length})
                                        </CardTitle>
                                        <ChevronDown className="h-4 w-4" />
                                    </div>
                                </CardHeader>
                            </CollapsibleTrigger>
                            <CollapsibleContent>
                                <CardContent className="pt-0 px-4 pb-4">
                                    <div className="space-y-2">
                                        {analysis.headline_patterns.map((pattern, i) => (
                                            <div key={i} className="p-2 rounded bg-muted/30">
                                                <div className="flex items-center gap-2">
                                                    <p className="text-sm font-medium">{pattern.pattern}</p>
                                                    <Badge variant="outline" className="text-xs">{pattern.frequency}</Badge>
                                                </div>
                                                {pattern.examples.length > 0 && (
                                                    <p className="text-xs text-muted-foreground mt-1">
                                                        Examples: {pattern.examples.join(", ")}
                                                    </p>
                                                )}
                                            </div>
                                        ))}
                                    </div>
                                </CardContent>
                            </CollapsibleContent>
                        </Card>
                    </Collapsible>
                )}

                {/* Value Propositions */}
                {analysis.value_proposition_themes.length > 0 && (
                    <Collapsible>
                        <Card>
                            <CollapsibleTrigger asChild>
                                <CardHeader className="py-3 px-4 cursor-pointer hover:bg-muted/50 transition-colors">
                                    <div className="flex items-center justify-between">
                                        <CardTitle className="text-sm flex items-center gap-2">
                                            <span>💎</span>
                                            Value Propositions ({analysis.value_proposition_themes.length})
                                        </CardTitle>
                                        <ChevronDown className="h-4 w-4" />
                                    </div>
                                </CardHeader>
                            </CollapsibleTrigger>
                            <CollapsibleContent>
                                <CardContent className="pt-0 px-4 pb-4">
                                    <div className="space-y-2">
                                        {analysis.value_proposition_themes.map((theme, i) => (
                                            <div key={i} className="p-2 rounded bg-muted/30">
                                                <p className="text-sm font-medium">{theme.theme}</p>
                                                <div className="flex flex-wrap gap-1 mt-1">
                                                    {theme.example_phrases.map((phrase, j) => (
                                                        <Badge key={j} variant="secondary" className="text-xs">
                                                            {phrase}
                                                        </Badge>
                                                    ))}
                                                </div>
                                            </div>
                                        ))}
                                    </div>
                                </CardContent>
                            </CollapsibleContent>
                        </Card>
                    </Collapsible>
                )}

                {/* CTA Patterns */}
                {analysis.cta_patterns.length > 0 && (
                    <Collapsible>
                        <Card>
                            <CollapsibleTrigger asChild>
                                <CardHeader className="py-3 px-4 cursor-pointer hover:bg-muted/50 transition-colors">
                                    <div className="flex items-center justify-between">
                                        <CardTitle className="text-sm flex items-center gap-2">
                                            <span>👆</span>
                                            CTA Patterns ({analysis.cta_patterns.length})
                                        </CardTitle>
                                        <ChevronDown className="h-4 w-4" />
                                    </div>
                                </CardHeader>
                            </CollapsibleTrigger>
                            <CollapsibleContent>
                                <CardContent className="pt-0 px-4 pb-4">
                                    <div className="space-y-2">
                                        {analysis.cta_patterns.map((cta, i) => (
                                            <div key={i} className="flex items-center gap-3 p-2 rounded bg-muted/30">
                                                <Badge variant="outline" className="text-xs capitalize">{cta.action_type.replace(/_/g, " ")}</Badge>
                                                <span className="text-sm flex-1">「{cta.text}」</span>
                                                <Badge
                                                    className={cn(
                                                        "text-xs",
                                                        cta.urgency_level === "high" ? "bg-red-500" :
                                                        cta.urgency_level === "medium" ? "bg-yellow-500" : "bg-gray-400"
                                                    )}
                                                >
                                                    Urgency: {cta.urgency_level}
                                                </Badge>
                                            </div>
                                        ))}
                                    </div>
                                </CardContent>
                            </CollapsibleContent>
                        </Card>
                    </Collapsible>
                )}

                <MetadataFooter cost={data.cost_usd} timestamp={data.timestamp} />
            </div>
        );
    }

    // Legacy format
    return (
        <div className="space-y-4">
            {/* Header */}
            <div className="flex items-center gap-3">
                <div className="p-2 rounded-lg bg-pink-100 dark:bg-pink-900/30">
                    <FileText className="h-5 w-5 text-pink-500" />
                </div>
                <div>
                    <h3 className="font-semibold">Ad Creative Analysis</h3>
                    {data.industry && (
                        <p className="text-sm text-muted-foreground">
                            Industry: {data.industry}
                        </p>
                    )}
                </div>
            </div>

            {/* Analysis cards */}
            <div className="space-y-3">
                {data.analyses.map((analysis, index) => (
                    <Card key={index}>
                        <CardContent className="p-4 space-y-3">
                            {/* Original ad */}
                            <div className="p-3 rounded bg-muted/50 border-l-4 border-pink-500">
                                <p className="font-medium">{analysis.headline}</p>
                                <p className="text-sm text-muted-foreground mt-1">
                                    {analysis.description}
                                </p>
                            </div>

                            {/* Persuasion techniques */}
                            {analysis.persuasion_techniques && analysis.persuasion_techniques.length > 0 && (
                                <div>
                                    <p className="text-xs text-muted-foreground mb-1">Persuasion Techniques</p>
                                    <div className="flex flex-wrap gap-1">
                                        {analysis.persuasion_techniques.map((tech, i) => (
                                            <Badge key={i} variant="secondary" className="text-xs">
                                                {tech}
                                            </Badge>
                                        ))}
                                    </div>
                                </div>
                            )}

                            {/* Emotional triggers */}
                            {analysis.emotional_triggers && analysis.emotional_triggers.length > 0 && (
                                <div>
                                    <p className="text-xs text-muted-foreground mb-1">Emotional Triggers</p>
                                    <div className="flex flex-wrap gap-1">
                                        {analysis.emotional_triggers.map((trigger, i) => (
                                            <Badge key={i} className="bg-pink-600 text-xs">
                                                {trigger}
                                            </Badge>
                                        ))}
                                    </div>
                                </div>
                            )}

                            {/* Strengths & Weaknesses */}
                            <div className="grid grid-cols-2 gap-3">
                                {analysis.strengths && analysis.strengths.length > 0 && (
                                    <div>
                                        <p className="text-xs text-green-600 dark:text-green-400 font-medium mb-1">Strengths</p>
                                        <ul className="text-xs space-y-1">
                                            {analysis.strengths.map((s, i) => (
                                                <li key={i} className="flex items-start gap-1">
                                                    <span className="text-green-600">+</span>
                                                    {s}
                                                </li>
                                            ))}
                                        </ul>
                                    </div>
                                )}
                                {analysis.weaknesses && analysis.weaknesses.length > 0 && (
                                    <div>
                                        <p className="text-xs text-red-600 dark:text-red-400 font-medium mb-1">Weaknesses</p>
                                        <ul className="text-xs space-y-1">
                                            {analysis.weaknesses.map((w, i) => (
                                                <li key={i} className="flex items-start gap-1">
                                                    <span className="text-red-600">-</span>
                                                    {w}
                                                </li>
                                            ))}
                                        </ul>
                                    </div>
                                )}
                            </div>

                            {/* Suggestions */}
                            {analysis.suggestions && analysis.suggestions.length > 0 && (
                                <div>
                                    <p className="text-xs text-muted-foreground mb-1">Optimization Suggestions</p>
                                    <ul className="text-sm space-y-1">
                                        {analysis.suggestions.map((s, i) => (
                                            <li key={i} className="flex items-start gap-2">
                                                <span className="text-blue-600">→</span>
                                                {s}
                                            </li>
                                        ))}
                                    </ul>
                                </div>
                            )}
                        </CardContent>
                    </Card>
                ))}
            </div>

            <MetadataFooter cost={data.cost_usd} />
        </div>
    );
}

// =============================================================================
// Screenshot Renderer
// =============================================================================

export function ScreenshotRenderer({ data }: { data: ScreenshotResult }) {
    // Support multiple field names for base64 image: screenshot, screenshot_base64
    const base64Data = data.screenshot || data.screenshot_base64;
    const imageUrl = data.screenshot_url || (base64Data ? `data:${data.content_type || 'image/png'};base64,${base64Data}` : null);

    return (
        <div className="space-y-4">
            {/* Header */}
            <div className="flex items-center gap-3">
                <div className="p-2 rounded-lg bg-blue-100 dark:bg-blue-900/30">
                    <Camera className="h-5 w-5 text-blue-500" />
                </div>
                <div className="flex-1 min-w-0">
                    <h3 className="font-semibold">Page Screenshot</h3>
                    <ExternalLinkButton href={data.url}>
                        {new URL(data.url).hostname}
                    </ExternalLinkButton>
                </div>
                {data.elapsed_ms !== undefined && (
                    <Badge variant="outline" className="text-xs shrink-0">
                        <Clock className="h-3 w-3 mr-1" />
                        {safeToFixed(safeNumber(data.elapsed_ms) / 1000, 2)}s
                    </Badge>
                )}
            </div>

            {/* Page title */}
            {data.title && (
                <div className="p-2 rounded bg-muted/50">
                    <p className="text-xs text-muted-foreground">Page Title</p>
                    <p className="text-sm font-medium">{data.title}</p>
                </div>
            )}

            {/* Screenshot */}
            {imageUrl ? (
                <Card className="overflow-hidden">
                    <CardContent className="p-0">
                        <img
                            src={imageUrl}
                            alt={data.title || "Page screenshot"}
                            className="w-full"
                        />
                    </CardContent>
                </Card>
            ) : (
                <Card>
                    <CardContent className="p-8 text-center text-muted-foreground">
                        <Camera className="h-12 w-12 mx-auto mb-2 opacity-50" />
                        <p>Screenshot not available</p>
                    </CardContent>
                </Card>
            )}

            <MetadataFooter cost={data.cost_usd} />
        </div>
    );
}

// =============================================================================
// Main Tool Output Renderer
// =============================================================================

export interface ToolOutputData {
    type: "serp_ads" | "competitor_discover" | "ads_transparency" | "keyword_extract" | "lp_analyze" | "lp_batch_analyze" | "creative_analyze" | "browser_screenshot" | "sec_filings" | "twitter_sentiment" | "alpaca_news";
    data: unknown;
}

/**
 * Detects the tool output type from JSON content
 */
export function detectToolOutputType(content: string): ToolOutputData | null {
    try {
        const parsed = JSON.parse(content);

        // SERP Ads - has keywords field (string) and ads array
        if (parsed.ads && Array.isArray(parsed.ads) && typeof parsed.keywords === "string" && parsed.total_ads !== undefined) {
            return { type: "serp_ads", data: parsed as SerpAdsResult };
        }

        // Competitor Discovery - has competitors array
        if (parsed.competitors && Array.isArray(parsed.competitors)) {
            return { type: "competitor_discover", data: parsed as CompetitorResult };
        }

        // Ads Transparency - has creatives array OR (advertiser + ads array + domain)
        if ((parsed.creatives && Array.isArray(parsed.creatives)) ||
            (parsed.advertiser !== undefined && parsed.ads && Array.isArray(parsed.ads) && parsed.domain)) {
            return { type: "ads_transparency", data: parsed as AdsTransparencyResult };
        }

        // Keywords - backend returns detected_country/detected_language instead of query
        if (parsed.keywords && Array.isArray(parsed.keywords) && (parsed.query || parsed.detected_country || parsed.detected_language)) {
            return { type: "keyword_extract", data: parsed as KeywordResult };
        }

        // LP Batch Analyze - has results array and (total_analyzed OR total_urls OR results with sections/url)
        if (parsed.results && Array.isArray(parsed.results) && parsed.results.length > 0) {
            const firstResult = parsed.results[0];
            // Check if it looks like LP batch (has url or sections in results)
            if (parsed.total_analyzed !== undefined || parsed.total_urls !== undefined || firstResult.url !== undefined || firstResult.sections !== undefined) {
                return { type: "lp_batch_analyze", data: parsed as LPBatchResult };
            }
        }

        // LP Analyze (sections mode) - has sections array with block_type items
        if (parsed.sections && Array.isArray(parsed.sections) && parsed.sections.length > 0 && parsed.sections[0].block_type !== undefined) {
            return { type: "lp_analyze", data: parsed as LPSectionsResult };
        }

        // LP Analyze (single) - has url and (vision_analysis OR legacy fields)
        if (parsed.url && (parsed.vision_analysis !== undefined || parsed.headline !== undefined || parsed.analysis !== undefined || parsed.capture_method !== undefined)) {
            return { type: "lp_analyze", data: parsed as LPAnalyzeResult };
        }

        // Creative Analyze (new format) - has ads_analyzed and analysis object with recommendations
        if (parsed.ads_analyzed !== undefined && parsed.analysis && parsed.analysis.recommendations) {
            return { type: "creative_analyze", data: parsed as CreativeAnalyzeResult };
        }

        // Creative Analyze (legacy) - has analyses array
        if (parsed.analyses && Array.isArray(parsed.analyses)) {
            return { type: "creative_analyze", data: parsed as CreativeAnalyzeResultLegacy };
        }

        // Screenshot - has url and (screenshot OR screenshot_base64 OR screenshot_url)
        if (parsed.url && (parsed.screenshot !== undefined || parsed.screenshot_base64 !== undefined || parsed.screenshot_url !== undefined)) {
            return { type: "browser_screenshot", data: parsed as ScreenshotResult };
        }

        // SEC Filings - has filings array and ticker/company_name
        if (parsed.filings && Array.isArray(parsed.filings) && parsed.ticker && parsed.company_name) {
            return { type: "sec_filings", data: parsed as SECFilingsResult };
        }

        // Twitter Sentiment - has analysis, sentiment, and ticker
        if (parsed.analysis && typeof parsed.analysis === "string" && parsed.sentiment && parsed.ticker) {
            return { type: "twitter_sentiment", data: parsed as TwitterSentimentResult };
        }

        // Alpaca News - has articles array and symbols
        if (parsed.articles && Array.isArray(parsed.articles) && parsed.symbols) {
            return { type: "alpaca_news", data: parsed as AlpacaNewsResult };
        }

        return null;
    } catch {
        return null;
    }
}

// =============================================================================
// SEC Filings Renderer
// =============================================================================

function SECFilingsRenderer({ data }: { data: SECFilingsResult }) {
    const [showAll, setShowAll] = useState(false);
    const displayFilings = showAll ? data.filings : data.filings.slice(0, 5);
    const hasMore = data.filings.length > 5;

    const getFormTypeBadge = (formType: string) => {
        switch (formType) {
            case "10-K":
                return "bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400";
            case "10-Q":
                return "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400";
            case "8-K":
                return "bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400";
            case "4":
                return "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400";
            default:
                return "bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-400";
        }
    };

    const getFormTypeLabel = (formType: string) => {
        switch (formType) {
            case "10-K": return "Annual Report";
            case "10-Q": return "Quarterly Report";
            case "8-K": return "Current Report";
            case "4": return "Insider Trading";
            default: return formType;
        }
    };

    return (
        <div className="space-y-4">
            {/* Header */}
            <div className="flex items-center gap-3">
                <div className="p-2 rounded-lg bg-teal-100 dark:bg-teal-900/30">
                    <FileText className="h-5 w-5 text-teal-600 dark:text-teal-400" />
                </div>
                <div className="flex-1">
                    <h3 className="font-semibold">SEC Filings</h3>
                    <p className="text-sm text-muted-foreground">
                        {data.company_name} ({data.ticker}) • {data.total_count} filings
                    </p>
                </div>
                {data.has_material_events && (
                    <Badge variant="destructive" className="text-xs">
                        Material Events
                    </Badge>
                )}
            </div>

            {/* Company Info */}
            <Card>
                <CardContent className="p-3">
                    <div className="flex items-center justify-between text-sm">
                        <div className="flex items-center gap-4">
                            <div>
                                <span className="text-muted-foreground">Ticker: </span>
                                <span className="font-semibold">{data.ticker}</span>
                            </div>
                            <div>
                                <span className="text-muted-foreground">CIK: </span>
                                <span className="font-mono text-xs">{data.cik}</span>
                            </div>
                        </div>
                        <Badge variant="outline" className="text-xs">
                            {data.source.toUpperCase()}
                        </Badge>
                    </div>
                </CardContent>
            </Card>

            {/* Filings List */}
            <div className="space-y-2">
                {displayFilings.map((filing, index) => (
                    <Card key={filing.accession_number || index} className={cn(
                        "transition-colors",
                        filing.is_material && "border-orange-200 dark:border-orange-800"
                    )}>
                        <CardContent className="p-3">
                            <div className="flex items-start justify-between gap-3">
                                <div className="flex-1 min-w-0">
                                    <div className="flex items-center gap-2 flex-wrap">
                                        <Badge className={cn("text-xs font-medium", getFormTypeBadge(filing.form_type))}>
                                            {filing.form_type}
                                        </Badge>
                                        <span className="text-xs text-muted-foreground">
                                            {getFormTypeLabel(filing.form_type)}
                                        </span>
                                        {filing.is_material && (
                                            <Badge variant="outline" className="text-xs text-orange-600 border-orange-300">
                                                Material
                                            </Badge>
                                        )}
                                    </div>
                                    <p className="text-sm font-medium mt-1 line-clamp-1">
                                        {filing.description}
                                    </p>
                                    <div className="flex items-center gap-3 mt-1 text-xs text-muted-foreground">
                                        <span className="flex items-center gap-1">
                                            <Clock className="h-3 w-3" />
                                            {filing.filed_date}
                                        </span>
                                        <span className="font-mono text-[10px]">
                                            {filing.accession_number}
                                        </span>
                                    </div>
                                </div>
                                <Button
                                    variant="ghost"
                                    size="sm"
                                    className="shrink-0"
                                    onClick={() => window.open(filing.filing_url, "_blank")}
                                >
                                    <ExternalLink className="h-4 w-4" />
                                </Button>
                            </div>
                        </CardContent>
                    </Card>
                ))}
            </div>

            {/* Show More/Less */}
            {hasMore && (
                <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setShowAll(!showAll)}
                    className="w-full"
                >
                    {showAll ? (
                        <>
                            <ChevronUp className="h-4 w-4 mr-2" />
                            Show Less
                        </>
                    ) : (
                        <>
                            <ChevronDown className="h-4 w-4 mr-2" />
                            Show All {data.filings.length} Filings
                        </>
                    )}
                </Button>
            )}
        </div>
    );
}

// =============================================================================
// Twitter Sentiment Renderer
// =============================================================================

function TwitterSentimentRenderer({ data }: { data: TwitterSentimentResult }) {
    const getSentimentColor = (sentiment: string) => {
        switch (sentiment.toLowerCase()) {
            case "bullish":
            case "positive":
                return "text-green-600 bg-green-100 dark:bg-green-900/30 dark:text-green-400";
            case "bearish":
            case "negative":
                return "text-red-600 bg-red-100 dark:bg-red-900/30 dark:text-red-400";
            default:
                return "text-yellow-600 bg-yellow-100 dark:bg-yellow-900/30 dark:text-yellow-400";
        }
    };

    const getSentimentIcon = (sentiment: string) => {
        switch (sentiment.toLowerCase()) {
            case "bullish":
            case "positive":
                return <TrendingUp className="h-4 w-4" />;
            case "bearish":
            case "negative":
                return <TrendingDown className="h-4 w-4" />;
            default:
                return <Minus className="h-4 w-4" />;
        }
    };

    // Parse analysis text into React elements (safe rendering without dangerouslySetInnerHTML)
    const renderAnalysis = (text: string) => {
        // Split by line breaks first
        const lines = text.split(/\\n|\n/);

        return lines.map((line, lineIdx) => {
            if (!line.trim()) return <br key={lineIdx} />;

            // Parse inline elements: **bold**, [[n]](url)
            const parts: React.ReactNode[] = [];
            let remaining = line;
            let partKey = 0;

            // Pattern for bold and citations
            const pattern = /(\*\*[^*]+\*\*|\[\[\d+\]\]\([^)]+\))/g;
            let lastIndex = 0;
            let match;

            while ((match = pattern.exec(remaining)) !== null) {
                // Add text before match
                if (match.index > lastIndex) {
                    parts.push(<span key={partKey++}>{remaining.slice(lastIndex, match.index)}</span>);
                }

                const matched = match[0];
                if (matched.startsWith("**")) {
                    // Bold text
                    const boldText = matched.slice(2, -2);
                    parts.push(<strong key={partKey++}>{boldText}</strong>);
                } else if (matched.startsWith("[[")) {
                    // Citation link [[n]](url)
                    const citationMatch = matched.match(/\[\[(\d+)\]\]\(([^)]+)\)/);
                    if (citationMatch) {
                        parts.push(
                            <a
                                key={partKey++}
                                href={citationMatch[2]}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="text-cyan-500 hover:underline text-xs align-super"
                            >
                                [{citationMatch[1]}]
                            </a>
                        );
                    }
                }
                lastIndex = match.index + matched.length;
            }

            // Add remaining text
            if (lastIndex < remaining.length) {
                parts.push(<span key={partKey++}>{remaining.slice(lastIndex)}</span>);
            }

            // Convert bullet points
            const isBullet = line.trimStart().startsWith("- ");
            if (isBullet) {
                return (
                    <div key={lineIdx} className="flex gap-2 ml-2 my-1">
                        <span className="text-muted-foreground">•</span>
                        <span>{parts.length > 0 ? parts : line.replace(/^-\s*/, "")}</span>
                    </div>
                );
            }

            return <p key={lineIdx} className="my-1">{parts.length > 0 ? parts : line}</p>;
        });
    };

    const formatDate = (dateStr: string) => {
        try {
            return new Date(dateStr).toLocaleDateString("en-US", {
                month: "short",
                day: "numeric",
                hour: "2-digit",
                minute: "2-digit",
            });
        } catch {
            return dateStr;
        }
    };

    return (
        <div className="space-y-4">
            {/* Header */}
            <div className="flex items-center gap-3">
                <div className="p-2 rounded-lg bg-cyan-100 dark:bg-cyan-900/30">
                    <Newspaper className="h-5 w-5 text-cyan-600 dark:text-cyan-400" />
                </div>
                <div className="flex-1">
                    <h3 className="font-semibold">Twitter Sentiment Analysis</h3>
                    <p className="text-sm text-muted-foreground">
                        {data.ticker} • {data.source || "Twitter/X"}
                    </p>
                </div>
                <Badge className={cn("flex items-center gap-1", getSentimentColor(data.sentiment))}>
                    {getSentimentIcon(data.sentiment)}
                    {data.sentiment.toUpperCase()}
                </Badge>
            </div>

            {/* Date Range */}
            {data.date_range && (
                <Card>
                    <CardContent className="p-3">
                        <div className="flex items-center justify-between text-sm">
                            <div className="flex items-center gap-2 text-muted-foreground">
                                <Clock className="h-4 w-4" />
                                <span>
                                    {formatDate(data.date_range.from)} — {formatDate(data.date_range.to)}
                                </span>
                            </div>
                            {data.model && (
                                <Badge variant="outline" className="text-xs">
                                    {data.model}
                                </Badge>
                            )}
                        </div>
                    </CardContent>
                </Card>
            )}

            {/* Analysis Content */}
            <Card>
                <CardHeader className="pb-2">
                    <CardTitle className="text-sm font-medium">Analysis</CardTitle>
                </CardHeader>
                <CardContent>
                    <div className="text-sm leading-relaxed">
                        {renderAnalysis(data.analysis)}
                    </div>
                </CardContent>
            </Card>

            {/* Cost */}
            {data.cost_usd !== undefined && (
                <div className="flex justify-end">
                    <span className="text-xs text-muted-foreground">
                        API Cost: ${safeToFixed(data.cost_usd, 4)}
                    </span>
                </div>
            )}
        </div>
    );
}

// =============================================================================
// Alpaca News Renderer
// =============================================================================

function AlpacaNewsRenderer({ data }: { data: AlpacaNewsResult }) {
    const [showAll, setShowAll] = useState(false);
    const displayArticles = showAll ? data.articles : data.articles.slice(0, 5);
    const hasMore = data.articles.length > 5;

    const sentimentScore = data.heuristic_sentiment_score ?? 0.5;
    const sentimentLabel = sentimentScore > 0.6 ? "Positive" : sentimentScore < 0.4 ? "Negative" : "Neutral";
    const sentimentColor = sentimentScore > 0.6
        ? "text-green-600 bg-green-100 dark:bg-green-900/30"
        : sentimentScore < 0.4
            ? "text-red-600 bg-red-100 dark:bg-red-900/30"
            : "text-yellow-600 bg-yellow-100 dark:bg-yellow-900/30";

    const formatDate = (dateStr: string) => {
        try {
            const date = new Date(dateStr);
            const now = new Date();
            const diffMs = now.getTime() - date.getTime();
            const diffHours = Math.floor(diffMs / (1000 * 60 * 60));
            const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));

            if (diffHours < 1) return "Just now";
            if (diffHours < 24) return `${diffHours}h ago`;
            if (diffDays < 7) return `${diffDays}d ago`;
            return date.toLocaleDateString("en-US", { month: "short", day: "numeric" });
        } catch {
            return dateStr;
        }
    };

    return (
        <div className="space-y-4">
            {/* Header */}
            <div className="flex items-center gap-3">
                <div className="p-2 rounded-lg bg-emerald-100 dark:bg-emerald-900/30">
                    <Newspaper className="h-5 w-5 text-emerald-600 dark:text-emerald-400" />
                </div>
                <div className="flex-1">
                    <h3 className="font-semibold">Market News</h3>
                    <p className="text-sm text-muted-foreground">
                        {data.symbols} • {data.articles.length} articles • {data.source || "Alpaca"}
                    </p>
                </div>
                <Badge className={cn("text-xs", sentimentColor)}>
                    {sentimentLabel}
                </Badge>
            </div>

            {/* Sentiment Summary */}
            {data.sentiment_summary && (
                <Card>
                    <CardContent className="p-3">
                        <div className="flex items-center justify-between">
                            <div className="flex items-center gap-4 text-sm">
                                <div className="flex items-center gap-1">
                                    <TrendingUp className="h-4 w-4 text-green-500" />
                                    <span className="text-green-600 font-medium">{data.sentiment_summary.positive}</span>
                                    <span className="text-muted-foreground">positive</span>
                                </div>
                                <div className="flex items-center gap-1">
                                    <Minus className="h-4 w-4 text-yellow-500" />
                                    <span className="text-yellow-600 font-medium">{data.sentiment_summary.neutral}</span>
                                    <span className="text-muted-foreground">neutral</span>
                                </div>
                                <div className="flex items-center gap-1">
                                    <TrendingDown className="h-4 w-4 text-red-500" />
                                    <span className="text-red-600 font-medium">{data.sentiment_summary.negative}</span>
                                    <span className="text-muted-foreground">negative</span>
                                </div>
                            </div>
                            <div className="text-sm text-muted-foreground">
                                Score: {safeToFixed(safeNumber(sentimentScore) * 100, 0)}%
                            </div>
                        </div>
                    </CardContent>
                </Card>
            )}

            {/* Articles List */}
            <div className="space-y-2">
                {displayArticles.map((article) => (
                    <Card key={article.id} className="hover:border-primary/50 transition-colors">
                        <CardContent className="p-3">
                            <div className="space-y-2">
                                <div className="flex items-start justify-between gap-2">
                                    <a
                                        href={article.url}
                                        target="_blank"
                                        rel="noopener noreferrer"
                                        className="font-medium text-sm hover:text-primary line-clamp-2 flex-1"
                                    >
                                        {article.headline}
                                    </a>
                                    <Button
                                        variant="ghost"
                                        size="sm"
                                        className="shrink-0 h-7 w-7 p-0"
                                        onClick={() => window.open(article.url, "_blank")}
                                    >
                                        <ExternalLink className="h-3.5 w-3.5" />
                                    </Button>
                                </div>
                                {article.summary && (
                                    <p className="text-xs text-muted-foreground line-clamp-2">
                                        {article.summary.replace(/&#39;/g, "'")}
                                    </p>
                                )}
                                <div className="flex items-center justify-between text-xs text-muted-foreground">
                                    <div className="flex items-center gap-2">
                                        <span className="font-medium">{article.source}</span>
                                        {article.author && <span>• {article.author}</span>}
                                    </div>
                                    <span>{formatDate(article.published_at)}</span>
                                </div>
                                {article.symbols && article.symbols.length > 0 && (
                                    <div className="flex flex-wrap gap-1 mt-1">
                                        {article.symbols.slice(0, 8).map((symbol) => (
                                            <Badge
                                                key={symbol}
                                                variant="outline"
                                                className={cn(
                                                    "text-[10px] px-1.5 py-0",
                                                    symbol === data.symbols && "bg-primary/10 border-primary/50"
                                                )}
                                            >
                                                {symbol}
                                            </Badge>
                                        ))}
                                        {article.symbols.length > 8 && (
                                            <Badge variant="outline" className="text-[10px] px-1.5 py-0">
                                                +{article.symbols.length - 8}
                                            </Badge>
                                        )}
                                    </div>
                                )}
                            </div>
                        </CardContent>
                    </Card>
                ))}
            </div>

            {/* Show More/Less */}
            {hasMore && (
                <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setShowAll(!showAll)}
                    className="w-full"
                >
                    {showAll ? (
                        <>
                            <ChevronUp className="h-4 w-4 mr-2" />
                            Show Less
                        </>
                    ) : (
                        <>
                            <ChevronDown className="h-4 w-4 mr-2" />
                            Show All {data.articles.length} Articles
                        </>
                    )}
                </Button>
            )}
        </div>
    );
}

// =============================================================================
// Financial News Renderer
// =============================================================================

function FinancialNewsRenderer({ content }: { content: string }) {
    const lines = content.split("\n");
    const ticker = lines[0]?.match(/for (\w+)/)?.[1] || "Stock";

    const sentimentMatch = content.match(/Overall Sentiment:\s*(\w+)/i);
    const sentiment = sentimentMatch?.[1]?.toLowerCase() || "mixed";
    const scoreMatch = content.match(/Score:\s*([\d.]+)/);
    const score = scoreMatch ? parseFloat(scoreMatch[1]) : 0.5;
    const confidenceMatch = content.match(/Confidence:\s*([\d.]+)/);
    const confidence = confidenceMatch ? parseFloat(confidenceMatch[1]) : 0;

    const newsItems: Array<{ title: string; source: string }> = [];
    const newsRegex = /- \*\*(.+?)\*\*\s*\n?\s*Source:\s*(\w+)/g;
    let match;
    while ((match = newsRegex.exec(content)) !== null) {
        newsItems.push({ title: match[1], source: match[2] });
    }

    const sourcesMatch = content.match(/Sources:\s*(.+)/);
    const sources = sourcesMatch?.[1]?.split(",").map(s => s.trim()) || [];

    const getSentimentColor = (s: string) => {
        switch (s) {
            case "bullish": return "text-green-600 bg-green-100 dark:bg-green-900/30";
            case "bearish": return "text-red-600 bg-red-100 dark:bg-red-900/30";
            default: return "text-yellow-600 bg-yellow-100 dark:bg-yellow-900/30";
        }
    };

    const getSentimentIcon = (s: string) => {
        switch (s) {
            case "bullish": return <TrendingUp className="h-4 w-4" />;
            case "bearish": return <TrendingDown className="h-4 w-4" />;
            default: return <Minus className="h-4 w-4" />;
        }
    };

    return (
        <div className="space-y-4">
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                    <div className="p-2 rounded-lg bg-green-100 dark:bg-green-900/30">
                        <Newspaper className="h-5 w-5 text-green-600" />
                    </div>
                    <div>
                        <h3 className="font-semibold">{ticker} News Summary</h3>
                        <p className="text-xs text-muted-foreground">
                            {sources.length > 0 && `Sources: ${sources.join(", ")}`}
                        </p>
                    </div>
                </div>
                <Badge className={cn("flex items-center gap-1", getSentimentColor(sentiment))}>
                    {getSentimentIcon(sentiment)}
                    {sentiment.toUpperCase()}
                </Badge>
            </div>

            {safeNumber(score) > 0 && (
                <Card>
                    <CardContent className="p-4">
                        <div className="flex items-center justify-between text-sm">
                            <span className="text-muted-foreground">Sentiment Score</span>
                            <span className="font-medium">{safeToFixed(safeNumber(score) * 100, 0)}%</span>
                        </div>
                        <div className="mt-2 h-2 bg-muted rounded-full overflow-hidden">
                            <div
                                className={cn(
                                    "h-full rounded-full transition-all",
                                    safeNumber(score) > 0.6 ? "bg-green-500" : safeNumber(score) < 0.4 ? "bg-red-500" : "bg-yellow-500"
                                )}
                                style={{ width: `${safeNumber(score) * 100}%` }}
                            />
                        </div>
                        {safeNumber(confidence) > 0 && (
                            <p className="text-xs text-muted-foreground mt-1">
                                Confidence: {safeToFixed(safeNumber(confidence) * 100, 0)}%
                            </p>
                        )}
                    </CardContent>
                </Card>
            )}

            {newsItems.length > 0 && (
                <Card>
                    <CardHeader className="pb-2">
                        <CardTitle className="text-sm font-medium">Recent News ({newsItems.length})</CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-2">
                        {newsItems.map((item, idx) => (
                            <div key={idx} className="flex items-start gap-2 p-2 rounded hover:bg-muted/50">
                                <FileText className="h-4 w-4 mt-0.5 text-muted-foreground shrink-0" />
                                <div className="min-w-0 flex-1">
                                    <p className="text-sm font-medium line-clamp-2">{item.title}</p>
                                    <p className="text-xs text-muted-foreground">{item.source}</p>
                                </div>
                            </div>
                        ))}
                    </CardContent>
                </Card>
            )}

            {newsItems.length === 0 && (
                <Card>
                    <CardContent className="p-4">
                        <pre className="text-sm whitespace-pre-wrap font-sans">{content}</pre>
                    </CardContent>
                </Card>
            )}
        </div>
    );
}

/**
 * Main renderer that auto-detects and renders tool output
 */
export function ToolOutputRenderer({ content }: { content: string }) {
    // First try to detect JSON-based tool outputs (higher priority)
    const detected = detectToolOutputType(content);

    if (detected) {
        // JSON-based renderers take priority
        switch (detected.type) {
            case "serp_ads":
                return <SerpAdsRenderer data={detected.data as SerpAdsResult} />;
            case "competitor_discover":
                return <CompetitorRenderer data={detected.data as CompetitorResult} />;
            case "ads_transparency":
                return <AdsTransparencyRenderer data={detected.data as AdsTransparencyResult} />;
            case "keyword_extract":
                return <KeywordRenderer data={detected.data as KeywordResult} />;
            case "lp_analyze":
                return <LPAnalyzeRenderer data={detected.data as (LPAnalyzeResult | LPSectionsResult)} />;
            case "lp_batch_analyze":
                return <LPBatchRenderer data={detected.data as LPBatchResult} />;
            case "creative_analyze":
                return <CreativeAnalyzeRenderer data={detected.data as CreativeAnalyzeResult} />;
            case "browser_screenshot":
                return <ScreenshotRenderer data={detected.data as ScreenshotResult} />;
            case "sec_filings":
                return <SECFilingsRenderer data={detected.data as SECFilingsResult} />;
            case "twitter_sentiment":
                return <TwitterSentimentRenderer data={detected.data as TwitterSentimentResult} />;
            case "alpaca_news":
                return <AlpacaNewsRenderer data={detected.data as AlpacaNewsResult} />;
        }
    }

    // Fallback to markdown-based Financial News renderer
    if (content.startsWith("# Financial News Summary") ||
        content.startsWith("# Stock News for") ||
        content.includes("Overall Sentiment:")) {
        return <FinancialNewsRenderer content={content} />;
    }

    // Default: raw content display
    return (
        <div className="p-4 rounded-lg border bg-muted/30">
            <pre className="text-sm whitespace-pre-wrap overflow-x-auto font-mono">{content}</pre>
        </div>
    );
}
