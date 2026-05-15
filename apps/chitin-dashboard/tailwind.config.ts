import type { Config } from 'tailwindcss';

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        ink: 'var(--ink)',
        panel: 'var(--panel)',
        line: 'var(--line)',
        muted: 'var(--muted)',
        allow: 'var(--allow)',
        deny: 'var(--deny)',
        heuristic: 'var(--heuristic)',
        signal: 'var(--signal)',
        accent: 'var(--accent)',
      },
      boxShadow: {
        frame: '0 18px 60px rgba(12, 23, 33, 0.14)',
      },
      fontFamily: {
        sans: ['IBM Plex Sans', 'Avenir Next', 'Segoe UI', 'sans-serif'],
        mono: ['IBM Plex Mono', 'SFMono-Regular', 'monospace'],
      },
    },
  },
  plugins: [],
} satisfies Config;
