import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import Link from "next/link";
import "./globals.css";

const geistSans = Geist({ variable: "--font-geist-sans", subsets: ["latin"] });
const geistMono = Geist_Mono({ variable: "--font-geist-mono", subsets: ["latin"] });

export const metadata: Metadata = {
  title: "Sentinel — AI Ops",
  description: "AI-driven alert investigation platform",
};

const nav = [
  { href: "/", label: "Dashboard" },
  { href: "/investigations", label: "Investigations" },
  { href: "/alerts", label: "Alerts" },
  { href: "/runbooks", label: "Runbooks" },
];

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN" className={`${geistSans.variable} ${geistMono.variable} h-full`}>
      <body className="antialiased bg-gray-950 text-gray-100 min-h-full flex">
        {/* Sidebar */}
        <aside className="w-52 shrink-0 bg-gray-900 border-r border-gray-800 flex flex-col">
          <div className="px-5 py-4 border-b border-gray-800">
            <span className="text-base font-bold text-white">Sentinel</span>
            <span className="ml-2 text-xs text-gray-500">AI Ops</span>
          </div>
          <nav className="flex-1 px-2 py-3 space-y-0.5">
            {nav.map(({ href, label }) => (
              <Link
                key={href}
                href={href}
                className="flex items-center px-3 py-2 rounded-md text-sm text-gray-300 hover:text-white hover:bg-gray-800 transition-colors"
              >
                {label}
              </Link>
            ))}
          </nav>
          <div className="px-5 py-3 border-t border-gray-800 text-xs text-gray-600">v0.1.0</div>
        </aside>

        {/* Main */}
        <main className="flex-1 overflow-y-auto">{children}</main>
      </body>
    </html>
  );
}
