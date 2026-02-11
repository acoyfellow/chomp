/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./templates/**/*.html"],
  darkMode: 'class',
  theme: {
    extend: {
      fontFamily: {
        sans: ['Sora', 'system-ui', '-apple-system', 'sans-serif'],
      },
      colors: {
        gold: { DEFAULT: '#C8A630', light: '#E8C872' },
        brand: {
          blue: '#4F6EC5',
          purple: '#9B4FBF',
          orange: '#D96F0E',
        }
      },
      animation: {
        'pulse-slow': 'pulse 2s ease-in-out infinite',
        'spin-slow': 'spin 0.8s linear infinite',
      }
    },
  },
  plugins: [],
}
