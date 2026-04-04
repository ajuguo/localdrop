/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js}'],
  theme: {
    extend: {
      fontFamily: {
        display: ['"Avenir Next"', '"PingFang SC"', '"Noto Sans SC"', 'sans-serif'],
        body: ['"Avenir Next"', '"PingFang SC"', '"Noto Sans SC"', 'sans-serif']
      },
      boxShadow: {
        glow: '0 20px 60px rgba(4, 120, 87, 0.16)'
      },
      animation: {
        drift: 'drift 16s ease-in-out infinite',
        rise: 'rise 400ms ease-out'
      },
      keyframes: {
        drift: {
          '0%, 100%': { transform: 'translate3d(0, 0, 0)' },
          '50%': { transform: 'translate3d(0, -12px, 0)' }
        },
        rise: {
          '0%': { opacity: '0', transform: 'translateY(10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        }
      }
    }
  },
  plugins: []
}

