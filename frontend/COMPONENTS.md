# Adding shadcn/ui Components

This project uses [shadcn/ui](https://ui.shadcn.com/) - a collection of re-usable components built with Radix UI and Tailwind CSS.

## Current Components

Already installed in `src/components/ui/`:
- `button.tsx` - Buttons with variants
- `card.tsx` - Card layouts
- `dialog.tsx` - Modals/dialogs
- `input.tsx` - Form inputs
- `label.tsx` - Form labels
- `tabs.tsx` - Tab navigation

## Adding New Components

shadcn/ui components are added manually (not via npx) since we've customized the setup.

### Example: Adding a Badge Component

1. **Create the component file**: `src/components/ui/badge.tsx`

```typescript
import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "@/lib/utils"

const badgeVariants = cva(
  "inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2",
  {
    variants: {
      variant: {
        default:
          "border-transparent bg-primary text-primary-foreground hover:bg-primary/80",
        secondary:
          "border-transparent bg-secondary text-secondary-foreground hover:bg-secondary/80",
        destructive:
          "border-transparent bg-destructive text-destructive-foreground hover:bg-destructive/80",
        outline: "text-foreground",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

export interface BadgeProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, ...props }: BadgeProps) {
  return (
    <div className={cn(badgeVariants({ variant }), className)} {...props} />
  )
}

export { Badge, badgeVariants }
```

2. **Use it in your components**:

```typescript
import { Badge } from '@/components/ui/badge'

function MyComponent() {
  return (
    <div>
      <Badge>Default</Badge>
      <Badge variant="secondary">Secondary</Badge>
      <Badge variant="destructive">Destructive</Badge>
      <Badge variant="outline">Outline</Badge>
    </div>
  )
}
```

## Common Components to Add

### Dropdown Menu

```typescript
// src/components/ui/dropdown-menu.tsx
import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu"
// See shadcn/ui docs for full implementation
```

Usage:
```typescript
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

<DropdownMenu>
  <DropdownMenuTrigger>Open</DropdownMenuTrigger>
  <DropdownMenuContent>
    <DropdownMenuItem>Profile</DropdownMenuItem>
    <DropdownMenuItem>Settings</DropdownMenuItem>
  </DropdownMenuContent>
</DropdownMenu>
```

### Select

```typescript
// src/components/ui/select.tsx
import * as SelectPrimitive from "@radix-ui/react-select"
```

### Checkbox

```typescript
// src/components/ui/checkbox.tsx
import * as CheckboxPrimitive from "@radix-ui/react-checkbox"
```

### Toast / Sonner

```bash
npm install sonner
```

```typescript
// src/components/ui/toast.tsx
import { Toaster, toast } from 'sonner'

// In your layout
<Toaster />

// Usage
toast.success('API key created!')
toast.error('Failed to delete key')
```

## Finding Components

Browse all components at: https://ui.shadcn.com/docs/components

Each component page shows:
1. Installation command (adapt manually)
2. Component code (copy to `src/components/ui/`)
3. Usage examples
4. Dependencies (Radix UI packages)

## Dependencies

Most components need these (already installed):
```json
{
  "@radix-ui/react-*": "Latest",
  "class-variance-authority": "^0.7.1",
  "clsx": "^2.1.1",
  "tailwind-merge": "^2.5.5"
}
```

## Customization

### Variants

Modify the `cva()` call to add/change variants:

```typescript
const buttonVariants = cva(
  "base-classes",
  {
    variants: {
      variant: {
        default: "...",
        custom: "bg-purple-500 text-white", // New variant
      },
      size: {
        sm: "...",
        xl: "h-14 px-10", // New size
      }
    }
  }
)
```

### Colors

Edit `src/index.css` to change design tokens:

```css
:root {
  --primary: 221.2 83.2% 53.3%; /* Change primary color */
  --destructive: 0 84.2% 60.2%;
  /* etc. */
}
```

### Tailwind Config

Edit `tailwind.config.js` for global changes:

```javascript
theme: {
  extend: {
    colors: {
      // Custom colors
    },
    borderRadius: {
      lg: 'var(--radius)',
    }
  }
}
```

## Component Patterns

### Compound Components

```typescript
<Card>
  <CardHeader>
    <CardTitle>Title</CardTitle>
    <CardDescription>Description</CardDescription>
  </CardHeader>
  <CardContent>
    Content here
  </CardContent>
  <CardFooter>
    <Button>Action</Button>
  </CardFooter>
</Card>
```

### Controlled Components

```typescript
const [open, setOpen] = useState(false)

<Dialog open={open} onOpenChange={setOpen}>
  <DialogContent>...</DialogContent>
</Dialog>
```

### Composition

```typescript
import { Button } from '@/components/ui/button'

function DangerButton(props) {
  return <Button variant="destructive" {...props} />
}
```

## Tips

1. **Copy from shadcn/ui directly** - their components are production-ready
2. **Use Radix UI primitives** - they handle accessibility
3. **Leverage Tailwind** - utility classes for quick styling
4. **Keep components in `ui/`** - separate from business logic
5. **Use CVA for variants** - type-safe variant management

## Resources

- shadcn/ui Components: https://ui.shadcn.com/docs/components
- Radix UI Docs: https://www.radix-ui.com/primitives
- Tailwind CSS: https://tailwindcss.com/docs
- CVA Docs: https://cva.style/docs
