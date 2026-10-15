import './app.css';

import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { HashRouter } from 'react-router-dom';
import { App } from './App';
import { PlaybackSessionProvider } from './lib/session';

/**
 * 渲染进程入口。
 *
 * 路由用 **HashRouter**：生产环境主进程用 `win.loadFile(dist/renderer/index.html)`
 * 加载（file:// 协议），BrowserRouter 的 history 路由在 file:// 下刷新即 404，
 * hash 路由是 Electron 里的稳妥选择。
 *
 * 这里刻意不注册任何全局错误吞噬：React 的 error boundary（App 内）负责 UI 降级，
 * 主进程 `window.onerror` 无需接管。
 */
const container = document.getElementById('root');
if (!container) {
  throw new Error('index.html 缺少 #root 挂载点');
}

createRoot(container).render(
  <StrictMode>
    <HashRouter>
      <PlaybackSessionProvider>
        <App />
      </PlaybackSessionProvider>
    </HashRouter>
  </StrictMode>,
);
