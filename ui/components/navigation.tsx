"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"

export function Navigation() {
  const pathname = usePathname()

  const isActive = (path: string) => {
    if (path === "/") {
      return pathname === "/"
    }
    return pathname.startsWith(path)
  }

  const getLinkClasses = (path: string) => {
    const baseClasses = "text-sm font-medium transition-colors"
    if (isActive(path)) {
      return `${baseClasses} text-foreground hover:text-foreground/80 border-b-2 border-foreground pb-1`
    }
    return `${baseClasses} text-muted-foreground hover:text-foreground`
  }

  return (
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
              className={getLinkClasses("/")}
            >
              Admin
            </Link>
            <Link 
              href="/published" 
              className={getLinkClasses("/published")}
            >
              Published
            </Link>
            <Link 
              href="/deployed" 
              className={getLinkClasses("/deployed")}
            >
              Deployed
            </Link>
          </div>
        </div>
      </div>
    </nav>
  )
}

