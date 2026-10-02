/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        background: '#090d16',
        surface: '#111827',
        surfaceHover: '#1f2937',
        border: '#1e293b',
        primary: {
          50: '#ecfdf5',
          500: '#10b981',
          600: '#059669',
          700: '#047857'
        },
        accent: {
          500: '#6366f1',
          600: '#4f46e5'
        }
      }
    },
  },
  plugins: [],
}
