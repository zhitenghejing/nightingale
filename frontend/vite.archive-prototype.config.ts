// A separate entry: main's real router, menus and API code remain untouched.
import { defineConfig } from 'vite';
import path from 'path';
import base from './vite.config';

export default defineConfig(async (environment) => {
  const config = typeof base === 'function' ? await base(environment) : await base;
  return {
    ...config,
    root: path.resolve(__dirname, 'src/pages/archiveManagementPrototype'),
    base: '/prototype/archive-management/',
    publicDir: false,
    define: { ...config.define, 'import.meta.env.VITE_ARCHIVE_UI_PROTOTYPE': 'true' },
    resolve: { ...config.resolve, alias: [{ find: '@', replacement: path.resolve(__dirname, 'src') }] },
    css: {
      ...config.css,
      preprocessorOptions: {
        ...config.css?.preprocessorOptions,
        less: {
          ...config.css?.preprocessorOptions?.less,
          additionalData: `@import "${path.resolve(__dirname, 'src/global.variable.less')}";`,
        },
      },
    },
    build: {
      outDir: path.resolve(__dirname, 'dist/archive-prototype'),
      emptyOutDir: true,
      sourcemap: false,
      rollupOptions: { output: { inlineDynamicImports: true } },
      chunkSizeWarningLimit: 2500,
    },
  };
});
