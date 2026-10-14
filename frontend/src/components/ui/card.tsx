import { cn } from "@/lib/utils"
import type { HTMLAttributes } from "react"

// Cards are not boxes: a block opens with a rule in ink and sits on the page
// background. Hierarchy comes from the title and whitespace, so a status pill, a
// table and a chart no longer all wear the same bordered tile.
export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("border-t border-foreground text-card-foreground", className)} {...props} />
}

export function CardHeader({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex flex-col gap-1 pt-3 pb-3", className)} {...props} />
}

export function CardTitle({ className, children, ...props }: HTMLAttributes<HTMLHeadingElement>) {
  return (
    <h3 className={cn("text-[15px] font-semibold leading-snug text-foreground", className)} {...props}>
      {children}
    </h3>
  )
}

export function CardDescription({ className, ...props }: HTMLAttributes<HTMLParagraphElement>) {
  return <p className={cn("text-[13px] text-muted-foreground", className)} {...props} />
}

export function CardContent({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("pb-2", className)} {...props} />
}
