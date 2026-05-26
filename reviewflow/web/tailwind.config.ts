import type { Config } from 'tailwindcss';

export default {
	content: ['./src/**/*.{html,js,svelte,ts}'],
	theme: {
		extend: {
			fontFamily: {
				sans: ['Inter', 'system-ui', 'sans-serif'],
				display: ['"Space Grotesk"', 'Inter', 'sans-serif'],
				mono: ['"JetBrains Mono"', 'ui-monospace', 'monospace']
			},
			colors: {
				// Bespoke palette: deep space ink + electric accents.
				ink: {
					900: '#06060c',
					800: '#0a0a16',
					700: '#10101f',
					600: '#16162a'
				},
				aurora: {
					violet: '#7c5cff',
					blue: '#39a0ff',
					cyan: '#22d3ee',
					pink: '#ff5cc8',
					lime: '#9bff5c'
				}
			},
			boxShadow: {
				glow: '0 0 0 1px rgba(124,92,255,0.25), 0 8px 40px -8px rgba(124,92,255,0.45)',
				'glow-cyan': '0 0 0 1px rgba(34,211,238,0.25), 0 8px 40px -8px rgba(34,211,238,0.45)'
			},
			backgroundImage: {
				'aurora-grad': 'linear-gradient(120deg,#7c5cff 0%,#39a0ff 40%,#22d3ee 75%,#9bff5c 100%)'
			},
			keyframes: {
				'aurora-drift': {
					'0%,100%': { transform: 'translate3d(0,0,0) scale(1)' },
					'50%': { transform: 'translate3d(4%,-4%,0) scale(1.15)' }
				},
				'fade-up': {
					'0%': { opacity: '0', transform: 'translateY(12px)' },
					'100%': { opacity: '1', transform: 'translateY(0)' }
				},
				'pop-in': {
					'0%': { opacity: '0', transform: 'scale(0.6)' },
					'70%': { transform: 'scale(1.08)' },
					'100%': { opacity: '1', transform: 'scale(1)' }
				},
				shimmer: {
					'100%': { transform: 'translateX(100%)' }
				},
				'pulse-ring': {
					'0%': { boxShadow: '0 0 0 0 rgba(34,211,238,0.5)' },
					'100%': { boxShadow: '0 0 0 8px rgba(34,211,238,0)' }
				}
			},
			animation: {
				'aurora-drift': 'aurora-drift 18s ease-in-out infinite',
				'fade-up': 'fade-up 0.45s cubic-bezier(0.22,1,0.36,1) both',
				'pop-in': 'pop-in 0.35s cubic-bezier(0.34,1.56,0.64,1) both',
				'pulse-ring': 'pulse-ring 1.6s ease-out infinite'
			}
		}
	},
	plugins: []
} satisfies Config;
