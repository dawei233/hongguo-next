import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import { fileURLToPath, URL } from 'node:url';

/**
 * 单测：渲染进程组件跑 jsdom，主进程 / IPC 客户端跑 node。
 * 用 projects 而不是 environmentMatchGlobs（后者在 vitest 3 已废弃）。
 */
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src/renderer', import.meta.url)),
      '@shared': fileURLToPath(new URL('./src/shared', import.meta.url)),
    },
  },
  test: {
    globals: true,
    projects: [
      {
        extends: true,
        test: {
          name: 'node',
          environment: 'node',
          include: ['tests/main/**/*.test.ts'],
        },
      },
      {
        extends: true,
        test: {
          name: 'dom',
          environment: 'jsdom',
          include: ['tests/renderer/**/*.test.tsx'],
          setupFiles: ['tests/setup-dom.ts'],
        },
      },
    ],
  },
});
