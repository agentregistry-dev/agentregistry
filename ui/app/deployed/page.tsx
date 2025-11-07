"use client"

import Link from "next/link"
import { Card } from "@/components/ui/card"

export default function DeployedPage() {
  return (
    <main className="min-h-screen bg-background">
      {/* Navigation Bar */}
      <nav className="border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60 sticky top-0 z-50">
        <div className="container mx-auto px-6">
          <div className="flex items-center justify-between h-20">
            <Link href="/" className="flex items-center">
              <img 
                src="/arlogo.png" 
                alt="Agent Registry" 
                width={180} 
                height={60}
                className="h-12 w-auto"
              />
            </Link>
            
            <div className="flex items-center gap-6">
              <Link 
                href="/" 
                className="text-sm font-medium text-muted-foreground hover:text-foreground transition-colors"
              >
                Admin
              </Link>
              <Link 
                href="/registry" 
                className="text-sm font-medium text-muted-foreground hover:text-foreground transition-colors"
              >
                Registry
              </Link>
              <Link 
                href="/deployed" 
                className="text-sm font-medium text-foreground hover:text-foreground/80 transition-colors border-b-2 border-foreground pb-1"
              >
                Live View
              </Link>
            </div>
          </div>
        </div>
      </nav>

      {/* Stats Section */}
      <div className="bg-muted/30 border-b">
        <div className="container mx-auto px-6 py-6">
          <div className="grid gap-4 md:grid-cols-3">
            <Card className="p-4 hover:shadow-md transition-all duration-200 border hover:border-primary/20">
              <div className="flex items-center gap-3">
                <div className="p-2 bg-green-500/10 rounded-lg flex items-center justify-center">
                  <svg
                    xmlns="http://www.w3.org/2000/svg"
                    fill="none"
                    viewBox="0 0 24 24"
                    strokeWidth={2}
                    stroke="currentColor"
                    className="h-5 w-5 text-green-600"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      d="M9 12.75L11.25 15 15 9.75M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                    />
                  </svg>
                </div>
                <div>
                  <p className="text-2xl font-bold">0</p>
                  <p className="text-xs text-muted-foreground">Running</p>
                </div>
              </div>
            </Card>

            <Card className="p-4 hover:shadow-md transition-all duration-200 border hover:border-primary/20">
              <div className="flex items-center gap-3">
                <div className="p-2 bg-yellow-500/10 rounded-lg flex items-center justify-center">
                  <svg
                    xmlns="http://www.w3.org/2000/svg"
                    fill="none"
                    viewBox="0 0 24 24"
                    strokeWidth={2}
                    stroke="currentColor"
                    className="h-5 w-5 text-yellow-600"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      d="M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z"
                    />
                  </svg>
                </div>
                <div>
                  <p className="text-2xl font-bold">0</p>
                  <p className="text-xs text-muted-foreground">Starting</p>
                </div>
              </div>
            </Card>

            <Card className="p-4 hover:shadow-md transition-all duration-200 border hover:border-primary/20">
              <div className="flex items-center gap-3">
                <div className="p-2 bg-red-500/10 rounded-lg flex items-center justify-center">
                  <svg
                    xmlns="http://www.w3.org/2000/svg"
                    fill="none"
                    viewBox="0 0 24 24"
                    strokeWidth={2}
                    stroke="currentColor"
                    className="h-5 w-5 text-red-600"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z"
                    />
                  </svg>
                </div>
                <div>
                  <p className="text-2xl font-bold">0</p>
                  <p className="text-xs text-muted-foreground">Errors</p>
                </div>
              </div>
            </Card>
          </div>
        </div>
      </div>

      <div className="container mx-auto px-6 py-12">
        <div className="max-w-4xl mx-auto">
          <div className="mb-8">
            <h1 className="text-3xl font-bold mb-2">Live View</h1>
            <p className="text-muted-foreground">
              Monitor and manage servers, skills, and agents that are currently running on your system.
            </p>
          </div>

          <Card className="p-12">
            <div className="text-center text-muted-foreground">
              <div className="w-16 h-16 mx-auto mb-4 opacity-50 flex items-center justify-center">
                <svg
                  xmlns="http://www.w3.org/2000/svg"
                  fill="none"
                  viewBox="0 0 24 24"
                  strokeWidth={1.5}
                  stroke="currentColor"
                  className="w-16 h-16"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    d="M5.25 14.25h13.5m-13.5 0a3 3 0 01-3-3m3 3a3 3 0 100 6h13.5a3 3 0 100-6m-16.5-3a3 3 0 013-3h13.5a3 3 0 013 3m-19.5 0a4.5 4.5 0 01.9-2.7L5.737 5.1a3.375 3.375 0 012.7-1.35h7.126c1.062 0 2.062.5 2.7 1.35l2.587 3.45a4.5 4.5 0 01.9 2.7m0 0a3 3 0 01-3 3m0 3h.008v.008h-.008v-.008zm0-6h.008v.008h-.008v-.008zm-3 6h.008v.008h-.008v-.008zm0-6h.008v.008h-.008v-.008z"
                  />
                </svg>
              </div>
              <p className="text-lg font-medium mb-2">
                No running resources
              </p>
              <p className="text-sm mb-6">
                Start servers, skills, and agents from the Admin panel to monitor them here.
              </p>
              <Link
                href="/"
                className="inline-flex items-center justify-center rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:opacity-50 disabled:pointer-events-none ring-offset-background bg-primary text-primary-foreground hover:bg-primary/90 h-10 py-2 px-4"
              >
                Go to Admin Panel
              </Link>
            </div>
          </Card>

          <div className="mt-8 p-4 bg-muted/50 rounded-lg">
            <h3 className="text-sm font-semibold mb-2">Coming Soon</h3>
            <ul className="text-sm text-muted-foreground space-y-1">
              <li>• Real-time status of all running MCP servers</li>
              <li>• Monitor active skills and their usage</li>
              <li>• View and manage running agents</li>
              <li>• Start, stop, and restart resources</li>
              <li>• Live logs and performance metrics</li>
              <li>• Health checks and alerts</li>
            </ul>
          </div>
        </div>
      </div>
    </main>
  )
}

