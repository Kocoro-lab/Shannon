import type { Metadata } from "next";
import "./globals.css";
import { Providers } from "./providers";
import { UserMenu } from "@/components/user-menu";

export const metadata: Metadata = {
  title: "Marketing Strategy App",
  description: "マーケティング戦略・実行プラン作成アプリケーション",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="ja">
      <head>
        <link rel="preconnect" href="https://fonts.googleapis.com" />
        <link rel="preconnect" href="https://fonts.gstatic.com" crossOrigin="anonymous" />
        <link
          href="https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&family=Noto+Sans+JP:wght@300;400;500;700&family=Noto+Serif+JP:wght@400;500;700&family=Poppins:wght@400;500;600;700&display=swap"
          rel="stylesheet"
        />
      </head>
      <body className="min-h-screen bg-background antialiased">
        <Providers>
          <div className="flex min-h-screen flex-col">
            <Header />
            <main className="flex-1">{children}</main>
          </div>
        </Providers>
      </body>
    </html>
  );
}

function Header() {
  return (
    <header className="sticky top-0 z-50 w-full border-b border-border bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="container-kocoro flex h-16 items-center justify-between">
        <div className="flex items-center gap-6">
          <a href="/" className="flex items-center gap-2">
            <div className="h-8 w-8 rounded-lg bg-gradient-to-br from-primary to-primary-coral flex items-center justify-center">
              <span className="text-white font-bold text-sm">M</span>
            </div>
            <span className="font-serif font-semibold text-lg">Marketing Strategy</span>
          </a>
          <nav className="hidden md:flex items-center gap-6">
            <a
              href="/goals"
              className="text-sm font-medium text-muted-foreground hover:text-foreground transition-colors"
            >
              目標設定
            </a>
            <a
              href="/benchmark"
              className="text-sm font-medium text-muted-foreground hover:text-foreground transition-colors"
            >
              ベンチマーク
            </a>
            <a
              href="/strategies"
              className="text-sm font-medium text-muted-foreground hover:text-foreground transition-colors"
            >
              施策
            </a>
          </nav>
        </div>
        <div className="flex items-center gap-4">
          <a
            href="/goals/new"
            className="btn-primary text-sm px-4 py-2"
          >
            新しい目標を設定
          </a>
          <UserMenu />
        </div>
      </div>
    </header>
  );
}
