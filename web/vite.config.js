import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
export default defineConfig({
    plugins: [react()],
    build: {
        chunkSizeWarningLimit: 900,
        rollupOptions: {
            output: {
                manualChunks(id) {
                    if (id.includes('/node_modules/react') || id.includes('/node_modules/scheduler')) {
                        return 'vendor-react';
                    }
                    if (id.includes('/node_modules/antd') ||
                        id.includes('/node_modules/@ant-design') ||
                        id.includes('/node_modules/@rc-component')) {
                        return 'vendor-antd';
                    }
                    if (id.includes('/node_modules/@tanstack')) {
                        return 'vendor-query';
                    }
                },
            },
        },
    },
    server: {
        port: 5173,
        proxy: {
            '/api': 'http://localhost:8080',
            '/healthz': 'http://localhost:8080',
            '/readyz': 'http://localhost:8080',
        },
    },
});
