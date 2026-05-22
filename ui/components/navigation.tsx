"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { useTheme } from "next-themes"
import { useSyncExternalStore } from "react"
import { Moon, Sun } from "lucide-react"
import { Button } from "@/components/ui/button"
import Image from "next/image"

export function Navigation() {
  const pathname = usePathname()
  const { theme, setTheme } = useTheme()
  const mounted = useSyncExternalStore(() => () => {}, () => true, () => false)

  const isActive = (path: string) => {
    if (path === "/") {
      return pathname === "/"
    }
    return pathname.startsWith(path)
  }

  return (
    <nav className="border-b bg-background sticky top-0 z-50">
      <div className="container mx-auto px-6">
        <div className="flex items-center gap-10 h-14">
          <Link href="/" className="flex items-center shrink-0 rounded-md px-2 py-1">
            <Image
              src={mounted && theme === "dark" ? "/logo-dark.svg" : "/logo-light.svg"}
              alt="Agent Registry"
              width={180}
              height={60}
              className="h-12 w-auto"
            />
          </Link>

          <div className="flex items-center gap-1">
            <Link
              href="/"
              className={`relative px-3 py-1.5 text-[15px] font-medium transition-colors ${
                isActive("/")
                  ? "text-foreground after:absolute after:bottom-[-13px] after:left-1 after:right-1 after:h-[2px] after:bg-primary after:rounded-full"
                  : "text-muted-foreground hover:text-foreground"
              }`}
            >
              Catalog
            </Link>
            <Link
              href="/deployed"
              className={`relative px-3 py-1.5 text-[15px] font-medium transition-colors ${
                isActive("/deployed")
                  ? "text-foreground after:absolute after:bottom-[-13px] after:left-1 after:right-1 after:h-[2px] after:bg-primary after:rounded-full"
                  : "text-muted-foreground hover:text-foreground"
              }`}
            >
              Deployed
            </Link>
          </div>

          <div className="ml-auto">
            {mounted && (
              <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8"
                onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
                title={`Switch to ${theme === "dark" ? "light" : "dark"} mode`}
              >
                {theme === "dark" ? (
                  <Sun className="h-4 w-4" />
                ) : (
                  <Moon className="h-4 w-4" />
                )}
              </Button>
            )}
          </div>
        </div>
      </div>
    </nav>
  )
}
