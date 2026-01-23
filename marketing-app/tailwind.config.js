/** @type {import('tailwindcss').Config} */
module.exports = {
  darkMode: ["class"],
  content: [
    './pages/**/*.{ts,tsx}',
    './components/**/*.{ts,tsx}',
    './app/**/*.{ts,tsx}',
    './lib/**/*.{ts,tsx}',
  ],
  theme: {
    extend: {
      colors: {
        // Kocoro Primary Colors
        primary: {
          DEFAULT: '#fe2f5c',
          coral: '#fe6c4b',
          foreground: '#ffffff',
        },
        // Background Colors
        background: {
          DEFAULT: '#ffffff',
          dark: '#0f0f26',
        },
        // Neutral backgrounds
        neutral: {
          1: '#faf9f9',
          2: '#f5f5f5',
          3: '#f4f2f1',
        },
        // Accent colors
        accent: {
          DEFAULT: '#f3eced',
          light: '#f3eced',
          gray: '#ececec',
          foreground: '#0f0f26',
        },
        // Semantic colors
        muted: {
          DEFAULT: '#f5f5f5',
          foreground: '#71717a',
        },
        border: '#e4e4e7',
        input: '#e4e4e7',
        ring: '#fe2f5c',
        foreground: '#0f0f26',
        card: {
          DEFAULT: '#ffffff',
          foreground: '#0f0f26',
        },
        destructive: {
          DEFAULT: '#ef4444',
          foreground: '#ffffff',
        },
        success: {
          DEFAULT: '#22c55e',
          foreground: '#ffffff',
        },
        warning: {
          DEFAULT: '#f59e0b',
          foreground: '#ffffff',
        },
      },
      fontFamily: {
        sans: ['Inter', 'Noto Sans JP', 'Poppins', 'sans-serif'],
        serif: ['Noto Serif JP', 'serif'],
      },
      borderRadius: {
        lg: '12px',
        md: '8px',
        sm: '4px',
      },
      boxShadow: {
        card: '0 1px 3px rgba(0, 0, 0, 0.08), 0 1px 2px rgba(0, 0, 0, 0.06)',
        'card-hover': '0 4px 12px rgba(0, 0, 0, 0.1), 0 2px 4px rgba(0, 0, 0, 0.06)',
      },
      screens: {
        'mobile': { max: '809px' },
        'tablet': { min: '810px', max: '1199px' },
        'desktop': { min: '1200px' },
      },
      keyframes: {
        'fade-in': {
          '0%': { opacity: '0', transform: 'translateY(10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' },
        },
        'slide-in': {
          '0%': { transform: 'translateX(-100%)' },
          '100%': { transform: 'translateX(0)' },
        },
        'pulse-soft': {
          '0%, 100%': { opacity: '1' },
          '50%': { opacity: '0.7' },
        },
      },
      animation: {
        'fade-in': 'fade-in 0.3s ease-out',
        'slide-in': 'slide-in 0.3s ease-out',
        'pulse-soft': 'pulse-soft 2s ease-in-out infinite',
      },
    },
  },
  plugins: [
    require('@tailwindcss/typography'),
  ],
}
