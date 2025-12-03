"use client";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Sparkles, Microscope, ArrowRight } from "lucide-react";
import { useRouter } from "next/navigation";
import { useDispatch } from "react-redux";
import { setSelectedAgent, setResearchStrategy } from "@/lib/features/runSlice";

export default function AgentsPage() {
    const router = useRouter();
    const dispatch = useDispatch();

    const handleSelectAgent = (agentType: "normal" | "deep_research") => {
        dispatch(setSelectedAgent(agentType));
        if (agentType === "deep_research") {
            dispatch(setResearchStrategy("standard"));
        }
        router.push("/run-detail?session_id=new");
    };

    return (
        <div className="p-4 sm:p-8 space-y-6 sm:space-y-8">
            <div>
                <h1 className="text-3xl font-bold tracking-tight">My Agents</h1>
                <p className="text-muted-foreground">
                    Choose an agent to start a new conversation.
                </p>
            </div>

            <div className="grid gap-6 md:grid-cols-2 max-w-4xl">
                {/* Everyday Agent */}
                <Card className="group hover:border-amber-300 hover:shadow-lg transition-all cursor-pointer" onClick={() => handleSelectAgent("normal")}>
                    <CardHeader>
                        <div className="flex items-center gap-3">
                            <div className="p-3 rounded-xl bg-amber-100 dark:bg-amber-900/30">
                                <Sparkles className="h-6 w-6 text-amber-500" />
                            </div>
                            <div>
                                <CardTitle className="text-xl">Everyday Agent</CardTitle>
                                <CardDescription>Quick answers & assistance</CardDescription>
                            </div>
                        </div>
                    </CardHeader>
                    <CardContent className="space-y-4">
                        <p className="text-sm text-muted-foreground">
                            Perfect for quick questions, calculations, simple research, and everyday tasks. 
                            Fast responses with efficient token usage.
                        </p>
                        <ul className="text-sm space-y-2">
                            <li className="flex items-center gap-2">
                                <span className="w-1.5 h-1.5 rounded-full bg-amber-500" />
                                Quick answers to questions
                            </li>
                            <li className="flex items-center gap-2">
                                <span className="w-1.5 h-1.5 rounded-full bg-amber-500" />
                                Calculations & analysis
                            </li>
                            <li className="flex items-center gap-2">
                                <span className="w-1.5 h-1.5 rounded-full bg-amber-500" />
                                Web search & summaries
                            </li>
                        </ul>
                        <Button variant="ghost" className="w-full group-hover:bg-amber-50 dark:group-hover:bg-amber-900/20">
                            Start Chat
                            <ArrowRight className="ml-2 h-4 w-4" />
                        </Button>
                    </CardContent>
                </Card>

                {/* Deep Research Agent */}
                <Card className="group hover:border-violet-300 hover:shadow-lg transition-all cursor-pointer" onClick={() => handleSelectAgent("deep_research")}>
                    <CardHeader>
                        <div className="flex items-center gap-3">
                            <div className="p-3 rounded-xl bg-violet-100 dark:bg-violet-900/30">
                                <Microscope className="h-6 w-6 text-violet-500" />
                            </div>
                            <div>
                                <CardTitle className="text-xl">Deep Research Agent</CardTitle>
                                <CardDescription>Comprehensive analysis</CardDescription>
                            </div>
                        </div>
                    </CardHeader>
                    <CardContent className="space-y-4">
                        <p className="text-sm text-muted-foreground">
                            For in-depth research, comprehensive reports, and complex analysis. 
                            Multi-step research with citations and detailed insights.
                        </p>
                        <ul className="text-sm space-y-2">
                            <li className="flex items-center gap-2">
                                <span className="w-1.5 h-1.5 rounded-full bg-violet-500" />
                                Multi-source research
                            </li>
                            <li className="flex items-center gap-2">
                                <span className="w-1.5 h-1.5 rounded-full bg-violet-500" />
                                Cited reports & analysis
                            </li>
                            <li className="flex items-center gap-2">
                                <span className="w-1.5 h-1.5 rounded-full bg-violet-500" />
                                Comprehensive insights
                            </li>
                        </ul>
                        <Button variant="ghost" className="w-full group-hover:bg-violet-50 dark:group-hover:bg-violet-900/20">
                            Start Research
                            <ArrowRight className="ml-2 h-4 w-4" />
                        </Button>
                    </CardContent>
                </Card>
            </div>
        </div>
    );
}

