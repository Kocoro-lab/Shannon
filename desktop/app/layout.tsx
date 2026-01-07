import type { Metadata } from "next";
import { Providers } from "@/components/providers";
import "./globals.css";

export const metadata: Metadata = {
    title: {
        default: "Planet",
        template: "%s | Planet",
    },
    description: "Multi-agent AI orchestration platform.",
    keywords: ["AI agents", "automation", "multi-agent", "orchestration", "open source", "agent scheduling"],
    authors: [{ name: "Planet" }],
    creator: "Planet",
    openGraph: {
        type: "website",
        locale: "en_US",
        siteName: "Planet",
        title: "Planet",
        description: "Multi-agent AI orchestration platform.",
        images: [
            {
                url: "/og-image.png",
                width: 1200,
                height: 630,
                alt: "Planet",
            },
        ],
    },
    twitter: {
        card: "summary_large_image",
        title: "Planet",
        description: "Multi-agent AI orchestration platform.",
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
                className="antialiased"
            >
                <Providers>
                    {children}
                </Providers>
            </body>
        </html>
    );
}
