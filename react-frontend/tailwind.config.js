/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        brand: {
          50: '#f0f7ff',
          100: '#e0effe',
          200: '#bbddfc',
          300: '#7cc0fa',
          400: '#389df6',
          500: '#0e7ee6',
          600: '#0262c0',
          700: '#034ea3',
          800: '#074385',
          900: '#0c396e',
          950: '#082449',
        }
      }
    },
  },
  plugins: [],
}
