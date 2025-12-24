import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import { Providers } from "@/components/providers";
import "./globals.css";

const geistSans = Geist({
    variable: "--font-geist-sans",
    subsets: ["latin"],
});

const geistMono = Geist_Mono({
    variable: "--font-geist-mono",
    subsets: ["latin"],
});

export const metadata: Metadata = {
    title: {
        default: "Shannon - Multi-Agent AI Platform",
        template: "%s | Shannon",
    },
    description: "Open-source multi-agent AI orchestration platform. Enterprise-grade automation with intelligent agent scheduling and monitoring.",
    keywords: ["AI agents", "automation", "multi-agent", "orchestration", "open source", "agent scheduling"],
    authors: [{ name: "Shannon" }],
    creator: "Shannon",
    openGraph: {
        type: "website",
        locale: "en_US",
        siteName: "Shannon",
        title: "Shannon - Multi-Agent AI Platform",
        description: "Open-source multi-agent AI orchestration platform. Enterprise-grade automation with intelligent agent scheduling and monitoring.",
        images: [
            {
                url: "/og-image.png",
                width: 1200,
                height: 630,
                alt: "Shannon - Multi-Agent AI Platform",
            },
        ],
    },
    twitter: {
        card: "summary_large_image",
        title: "Shannon - Multi-Agent AI Platform",
        description: "Open-source multi-agent AI orchestration platform.",
        images: ["/og-image.png"],
    },
    icons: {
        icon: "/favicon.ico",
        apple: "/apple-touch-icon.png",
    },
    manifest: "/manifest.json",
};

export default function RootLayout({
    children,
}: Readonly<{
    children: React.ReactNode;
}>) {
    return (
        <html lang="en" suppressHydrationWarning>
            <body
                className={`${geistSans.variable} ${geistMono.variable} antialiased`}
            >
                <Providers>
                    {children}
                </Providers>
            </body>
        </html>
    );
}
