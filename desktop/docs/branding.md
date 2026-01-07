# Prometheus Agentic Growth Solutions

## Web Branding Guide

<img src="https://jbcnstuqzsjcsvirfksn.supabase.co/storage/v1/object/public/branding/flame_transparent.png" alt="Prometheus Flame Logo" style="width: 150px; height: auto; margin: 20px auto; display: block;">

This branding guide provides styling information for Prometheus Agentic Growth Solutions web platforms, ensuring consistent brand representation across all digital properties.

## Color Palette

|Color Name|Hex Value|Preview|Usage|
|---|---|---|---|
|Navy|#0A192D|<div style="background-color: #0A192D; width: 50px; height: 25px; display: inline-block; border-radius: 4px;"></div>|Primary brand color|
|Yellow|#FFDD00|<div style="background-color: #FFDD00; width: 50px; height: 25px; display: inline-block; border-radius: 4px;"></div>|Secondary color|
|Orange|#FF5500|<div style="background-color: #FF5500; width: 50px; height: 25px; display: inline-block; border-radius: 4px;"></div>|Accent, used in gradient|
|Red|#FF4D4D|<div style="background-color: #FF4D4D; width: 50px; height: 25px; display: inline-block; border-radius: 4px;"></div>|Error states, used in gradient|
|Turquoise|#00A3A3|<div style="background-color: #00A3A3; width: 50px; height: 25px; display: inline-block; border-radius: 4px;"></div>|Tertiary accent color|
|Light Blue|#4D9FFF|<div style="background-color: #4D9FFF; width: 50px; height: 25px; display: inline-block; border-radius: 4px;"></div>|Primary color in dark mode|
|Lavender|#9D8DF1|<div style="background-color: #9D8DF1; width: 50px; height: 25px; display: inline-block; border-radius: 4px;"></div>|Optional accent color|
|Ultra Light Gray|#F8F8F8|<div style="background-color: #F8F8F8; width: 50px; height: 25px; display: inline-block; border-radius: 4px; border: 1px solid #CCCCCC;"></div>|Background in light mode|
|Light Gray|#F5F5F5|<div style="background-color: #F5F5F5; width: 50px; height: 25px; display: inline-block; border-radius: 4px; border: 1px solid #CCCCCC;"></div>|Card background in light mode|
|Medium Gray|#CCCCCC|<div style="background-color: #CCCCCC; width: 50px; height: 25px; display: inline-block; border-radius: 4px;"></div>|Borders, inputs in light mode|
|Dark Gray|#333333|<div style="background-color: #333333; width: 50px; height: 25px; display: inline-block; border-radius: 4px;"></div>|Text color, borders in dark mode|
|Light Navy|#0F2440|<div style="background-color: #0F2440; width: 50px; height: 25px; display: inline-block; border-radius: 4px;"></div>|Navy variant in dark mode|

### Flame Gradient

The distinctive Prometheus flame gradient transitions from yellow to orange to red to deep navy:

- Top color: #FFDD00 (Yellow)
- Middle-top color: #FF5500 (Orange)
- Middle-bottom color: #FF4D4D (Red)
- Bottom color: #0A192D (Navy)

<div style="width: 100px; height: 150px; background: linear-gradient(to bottom, #FFDD00, #FF5500, #FF4D4D, #0A192D); border-radius: 25% 25% 25% 25% / 35% 35% 25% 25%; margin: 20px auto;"></div>

## Typography

### Font Selection Rationale

Our typography strategy combines professionalism with modern aesthetics, featuring:

- **Roboto**: A versatile, geometric sans-serif for body text that offers excellent screen readability and space efficiency. Roboto was specifically designed for digital interfaces with clean lines and balanced proportions, making it ideal for both technical documentation and marketing content.
- **Inter**: A powerful, distinctive sans-serif for headings that conveys authority and innovation. Inter is specifically optimized for digital interfaces with excellent character spacing and legibility at all sizes.

This pairing balances the technical precision needed for an AI company with the approachable style needed for user interfaces. Roboto's slightly more condensed letterforms (compared to Raleway) allow for more characters per line while maintaining excellent readability across all device types and sizes, making it particularly suitable for dense technical content and documentation.

### Web Typography Specifications (Tailwind CSS)

|Element|Font|Weight|Size Class|Line Height|Color|Usage|
|---|---|---|---|---|---|---|
|h1|Inter|700 (Bold)|text-4xl|leading-tight|text-prometheus-navy|Page titles, hero headlines|
|h2|Inter|700 (Bold)|text-3xl|leading-tight|text-prometheus-navy|Section headers|
|h3|Inter|700 (Bold)|text-2xl|leading-snug|text-prometheus-navy|Sub-section headers|
|h4|Inter|700 (Bold)|text-xl|leading-snug|text-prometheus-navy|Card or panel headers|
|h5|Inter|700 (Bold)|text-lg|leading-normal|text-prometheus-navy|Minor section headers|
|h6|Inter|700 (Bold)|text-base|leading-normal|text-prometheus-navy|Small headers, emphasized text|
|Body|Roboto|400 (Regular)|text-base|leading-relaxed|text-prometheus-navy|Main content text|
|Small|Roboto|400 (Regular)|text-sm|leading-normal|text-gray-600|Supporting text, captions|
|Button|Roboto|500 (Medium)|text-base|leading-none|varies by button type|Call to action text|
|Link|Roboto|500 (Medium)|text-base|inherited|text-prometheus-turquoise hover:text-prometheus-lightBlue|Clickable text|
|Error|Roboto|400 (Regular)|text-sm|leading-normal|text-prometheus-red|Error messages|
|Input Label|Roboto|500 (Medium)|text-sm|leading-none|text-prometheus-navy|Form input labels|
|Input Text|Roboto|400 (Regular)|text-base|leading-normal|text-prometheus-navy|Form input text|
|Input Hint|Roboto|400 (Regular)|text-sm|leading-normal|text-gray-500|Form input hint text|

In dark mode, replace `text-prometheus-navy` with `text-white` or `text-prometheus-ultraLightGray` for better contrast.

### Font Implementation

To properly implement the typography, ensure:

1. **Complete font set**: Include all necessary weights of Roboto (400, 500, 700) and Inter Bold (700)
2. **Line heights**: Follow the specified line heights for proper vertical rhythm
3. **Text color**: Apply the specified colors for light and dark modes
4. **Size hierarchy**: Maintain proper visual hierarchy with the specified text sizes

## Web Implementation (Tailwind CSS with shadcn-ui)

### tailwind.config.ts

```typescript
import type { Config } from "tailwindcss";
import { fontFamily } from "tailwindcss/defaultTheme";

const config: Config = {
  darkMode: ["class"],
  content: [
    './pages/**/*.{ts,tsx}',
    './components/**/*.{ts,tsx}',
    './app/**/*.{ts,tsx}',
    './src/**/*.{ts,tsx}',
  ],
  theme: {
    container: {
      center: true,
      padding: "2rem",
      screens: {
        "2xl": "1400px",
      },
    },
    extend: {
      colors: {
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))",
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        popover: {
          DEFAULT: "hsl(var(--popover))",
          foreground: "hsl(var(--popover-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
        // Custom prometheus colors
        prometheus: {
          navy: "#0a192d",
          yellow: "#ffdd00",
          orange: "#ff5500",
          red: "#ff4d4d",
          lightGray: "#f5f5f5",
          mediumGray: "#cccccc",
          ultraLightGray: "#f8f8f8",
          darkGray: "#333333",
          turquoise: "#00a3a3",
          lavender: "#9d8df1",
          lightBlue: "#4d9fff",
        },
      },
      borderRadius: {
        lg: "var(--radius)",
        md: "calc(var(--radius) - 2px)",
        sm: "calc(var(--radius) - 4px)",
      },
      fontFamily: {
        sans: ["var(--font-roboto)", ...fontFamily.sans],
        heading: ["var(--font-inter)", ...fontFamily.sans],
      },
      keyframes: {
        "accordion-down": {
          from: { height: "0" },
          to: { height: "var(--radix-accordion-content-height)" },
        },
        "accordion-up": {
          from: { height: "var(--radix-accordion-content-height)" },
          to: { height: "0" },
        },
      },
      animation: {
        "accordion-down": "accordion-down 0.2s ease-out",
        "accordion-up": "accordion-up 0.2s ease-out",
      },
    },
  },
  plugins: [require("tailwindcss-animate")],
};

export default config;
```

### global.css

```css
@tailwind base;
@tailwind components;
@tailwind utilities;

@layer base {
  :root {
    /* Base colors */
    --background: 0 0% 97%;
    --foreground: 214 63% 10%;
    
    /* Primary - Navy */
    --primary: 214 64% 11%;
    --primary-foreground: 0 0% 97%;
    
    /* Secondary - Yellow */
    --secondary: 52 100% 50%;
    --secondary-foreground: 214 64% 11%;
    
    /* Accent - Turquoise */
    --accent: 180 100% 32%;
    --accent-foreground: 0 0% 97%;
    
    /* Destructive - Red */
    --destructive: 0 100% 65%;
    --destructive-foreground: 0 0% 97%;
    
    /* Muted - Medium Gray */
    --muted: 0 0% 80%;
    --muted-foreground: 214 64% 11%;
    
    /* Card and Popover - Ultra Light Gray */
    --card: 0 0% 97%;
    --card-foreground: 214 64% 11%;
    --popover: 0 0% 97%;
    --popover-foreground: 214 64% 11%;
    
    /* Border, Input, Ring - Medium Gray */
    --border: 0 0% 80%;
    --input: 0 0% 80%;
    --ring: 214 64% 11%;
    
    --radius: 0.5rem;
  }
  
  .dark {
    /* Base colors */
    --background: 214 64% 11%;
    --foreground: 0 0% 97%;
    
    /* Primary - Light Blue */
    --primary: 213 100% 65%;
    --primary-foreground: 214 64% 11%;
    
    /* Secondary - Yellow */
    --secondary: 52 100% 50%;
    --secondary-foreground: 214 64% 11%;
    
    /* Accent - Turquoise */
    --accent: 180 100% 32%;
    --accent-foreground: 214 64% 11%;
    
    /* Destructive - Red */
    --destructive: 0 100% 65%;
    --destructive-foreground: 214 64% 11%;
    
    /* Muted - Dark Gray */
    --muted: 0 0% 20%;
    --muted-foreground: 0 0% 80%;
    
    /* Card and Popover - Slightly lighter Navy */
    --card: 214 64% 15%;
    --card-foreground: 0 0% 97%;
    --popover: 214 64% 15%;
    --popover-foreground: 0 0% 97%;
    
    /* Border, Input, Ring - Dark Gray */
    --border: 0 0% 20%;
    --input: 0 0% 20%;
    --ring: 213 100% 65%;
  }
}

/* Font setup */
@font-face {
  font-family: 'Roboto';
  font-style: normal;
  font-weight: 400;
  font-display: swap;
  src: url('/fonts/Roboto-Regular.woff2') format('woff2');
}

@font-face {
  font-family: 'Roboto';
  font-style: normal;
  font-weight: 500;
  font-display: swap;
  src: url('/fonts/Roboto-Medium.woff2') format('woff2');
}

@font-face {
  font-family: 'Roboto';
  font-style: normal;
  font-weight: 700;
  font-display: swap;
  src: url('/fonts/Roboto-Bold.woff2') format('woff2');
}

@font-face {
  font-family: 'Inter';
  font-style: normal;
  font-weight: 700;
  font-display: swap;
  src: url('/fonts/Inter-Bold.woff2') format('woff2');
}

@layer base {
  * {
    @apply border-border;
  }
  body {
    @apply bg-background text-foreground font-sans;
  }
  h1, h2, h3, h4, h5, h6 {
    @apply font-heading font-bold;
  }
}

/* Custom utility classes for Prometheus brand colors */
@layer utilities {
  .text-prometheus-gradient {
    background: linear-gradient(to bottom, #ffdd00, #ff5500, #ff4d4d, #0a192d);
    -webkit-background-clip: text;
    background-clip: text;
    color: transparent;
  }
  
  .bg-prometheus-gradient {
    background: linear-gradient(to bottom, #ffdd00, #ff5500, #ff4d4d, #0a192d);
  }
}

/* Custom form styling for flat, borderless inputs */
@layer components {
  .prometheus-input {
    @apply bg-white dark:bg-prometheus-lightNavy px-3 py-2 rounded-md focus:outline-none focus:ring-0 border-0 shadow-sm;
  }
  
  .prometheus-input-error {
    @apply prometheus-input ring-2 ring-prometheus-red;
  }
  
  .prometheus-input-label {
    @apply text-sm font-medium text-prometheus-navy dark:text-prometheus-ultraLightGray mb-1 block;
  }
  
  .prometheus-input-hint {
    @apply text-sm text-gray-500 dark:text-gray-400 mt-1;
  }
  
  .prometheus-input-error-message {
    @apply text-sm text-prometheus-red mt-1;
  }
}
```

### Next.js Font Setup (layout.tsx)

```typescript
import { Roboto, Inter } from 'next/font/google';

// Load Roboto from Google Fonts
const roboto = Roboto({
  subsets: ['latin'],
  weight: ['400', '500', '700'],
  variable: '--font-roboto',
});

// Load Inter from Google Fonts
const inter = Inter({
  subsets: ['latin'],
  weight: ['700'],
  variable: '--font-inter',
});

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" className={`${roboto.variable} ${inter.variable}`}>
      <body>
        {children}
      </body>
    </html>
  );
}
```

## Logo Implementation for Web

The Prometheus logo is a stylized flame that embodies transformation, growth, and innovation. The flame transitions from yellow at the top, through orange and red, down to navy at the base, representing the company's core mission of guiding growth.

### Web Logo Variants

1. **Favicon** - A circular version of the flame for website browser tabs (32x32px, 16x16px)
2. **Full Logo** - The flame icon paired with the PROMETHEUS wordmark for website headers
3. **Header Logo** - Horizontal orientation of the flame with wordmark
4. **Footer Logo** - Smaller version of the full logo for website footers
5. **Social Media Avatar** - Square version of the flame for profile pictures (various sizes)

### Logo HTML/CSS Implementation

#### Favicon

```html
<link rel="icon" type="image/png" sizes="32x32" href="/favicon-32x32.png">
<link rel="icon" type="image/png" sizes="16x16" href="/favicon-16x16.png">
<link rel="apple-touch-icon" sizes="180x180" href="/apple-touch-icon.png">
```

#### CSS-based Flame (Simple Version)

```html
<div class="prometheus-flame-icon">
  <div class="flame-inner"></div>
</div>

<style>
  .prometheus-flame-icon {
    width: 40px;
    height: 60px;
    position: relative;
    background: linear-gradient(to bottom, #ffdd00, #ff5500, #ff4d4d, #0a192d);
    border-radius: 50% 50% 50% 50% / 60% 60% 40% 40%;
  }
  
  .flame-inner {
    position: absolute;
    top: 15%;
    left: 20%;
    right: 20%;
    bottom: 10%;
    background: linear-gradient(to bottom, #ffdd00, #ff5500, #ff4d4d, #0a192d);
    border-radius: 50% 50% 35% 35% / 50% 50% 50% 50%;
    opacity: 0.7;
  }
</style>
```

#### SVG Logo Implementation

```html
<svg width="240" height="80" viewBox="0 0 240 80" xmlns="http://www.w3.org/2000/svg">
  <!-- Flame Icon -->
  <defs>
    <linearGradient id="flameGradient" x1="0%" y1="0%" x2="0%" y2="100%">
      <stop offset="0%" stop-color="#FFDD00" />
      <stop offset="40%" stop-color="#FF5500" />
      <stop offset="60%" stop-color="#FF4D4D" />
      <stop offset="100%" stop-color="#0A192D" />
    </linearGradient>
  </defs>
  
  <!-- Flame Shape -->
  <path d="M30 10 C 45 25, 15 40, 30 70 C 20 65, 10 45, 30 10" fill="url(#flameGradient)" />
  
  <!-- PROMETHEUS Text -->
  <text x="70" y="45" font-family="Inter, sans-serif" font-weight="700" font-size="24" fill="#0A192D">PROMETHEUS</text>
</svg>
```

<div style="display: flex; align-items: center; margin: 20px 0;">
  <img src="https://jbcnstuqzsjcsvirfksn.supabase.co/storage/v1/object/public/branding/flame_transparent.png" 
       alt="Prometheus Flame Logo" 
       style="width: 150px; height: auto;">
  <div style="font-family: 'Inter', sans-serif; font-weight: 700; font-size: 24px; color: #0A192D; margin-left: 15px;">
    PROMETHEUS
  </div>
</div>


### Logo Usage Guidelines

- Always maintain the flame's aspect ratio when resizing
- Ensure adequate spacing around the logo (minimum clear space equals height of the flame)
- The logo gradient should always flow from yellow to navy
- For single-color applications, use the navy color
- Minimum size recommendations:
    - Favicon: 32x32px
    - Header logo: 120px wide
    - Social media avatar: 180x180px

## Design Rationale for Web

### Flat, Borderless Input Design

Our UI components prioritize a clean, flat aesthetic:

1. **Simplified Form Inputs**
    - Removed borders from input fields in favor of subtle background fills
    - Added boxed shadow for subtle depth
    - Added appropriate hover and focus states without animations
    - Error states use the red accent color with minimal visual noise
2. **Typography Implementation**
    - Heading styles (Inter Bold) create a branded look throughout the site
    - Roboto body text provides optimal readability with space efficiency
    - Consistent application of text sizes creates visual hierarchy
    - Color contrast ensures WCAG AA accessibility compliance
    - Line heights optimize readability at different screen sizes
3. **Color Application Best Practices**
    - Use navy as the primary text color on light backgrounds
    - Use the flame gradient sparingly for maximum impact
    - Apply yellow for important call-to-action elements
    - Implement light/dark mode toggle with consistent brand recognition
4. **Enhanced Dark Mode Experience**
    - Custom navy shades create depth in dark interfaces
    - Light blue becomes primary to maintain energy while reducing eye strain
    - Text colors adjust for optimal contrast while maintaining brand feel
5. **Component Design Guidelines**
    - Cards use minimal elevation with rounded corners (12px radius)
    - Buttons feature a flat design with proper text contrast
    - Navigation elements use consistent spacing and hover states
    - Form elements maintain the borderless, clean aesthetic

## Implementation Notes for Web

- Default to system preference for light/dark mode
- Include complete webfont files for all required weights
- Add preloading for critical fonts to optimize page load
- Implement responsive adjustments for mobile and tablet viewports
- Test color contrast with automated tools to ensure accessibility

### Component Examples

#### Button

```html
<button class="bg-prometheus-yellow text-prometheus-navy px-6 py-3 rounded-md font-medium hover:bg-yellow-400 transition">
  Get Started
</button>

<button class="bg-transparent border-2 border-prometheus-navy text-prometheus-navy px-6 py-3 rounded-md font-medium hover:bg-prometheus-navy hover:text-white transition">
  Learn More
</button>

<button class="bg-prometheus-turquoise text-white px-6 py-3 rounded-md font-medium hover:bg-teal-600 transition">
  View Details
</button>
```

#### Form Input

```html
<div class="mb-4">
  <label class="prometheus-input-label">Email Address</label>
  <input type="email" class="prometheus-input w-full" placeholder="yourname@example.com" />
  <p class="prometheus-input-hint">We'll never share your email with anyone else.</p>
</div>

<div class="mb-4">
  <label class="prometheus-input-label">Password</label>
  <input type="password" class="prometheus-input-error w-full" />
  <p class="prometheus-input-error-message">Password must be at least 8 characters</p>
</div>
```

#### Card Component

```html
<div class="bg-prometheus-lightGray dark:bg-prometheus-lightNavy p-6 rounded-xl shadow-sm">
  <h3 class="text-2xl font-bold mb-3">Feature Title</h3>
  <p class="mb-4">
    Feature description goes here with more details and information about what this feature offers.
  </p>
  <a href="#" class="text-prometheus-turquoise font-medium hover:text-prometheus-lightBlue flex items-center">
    Learn more →
  </a>
</div>
```

---

_This web branding guide maintains the integrity of Prometheus Agentic Growth Solutions' visual identity while providing comprehensive implementation details for web platforms._```
